package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/oauth2"

	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

const (
	sessionCookieName     = "session"
	stateCookieName       = "oidc_state"
	pkceCookieName        = "oidc_pkce"
	appRedirectCookieName = "app_redirect"
	sessionMaxAge         = 24 * time.Hour
)

// allowedAppRedirects is the closed set of custom-scheme redirect targets the
// desktop app may receive a token at. Restricting this prevents an open
// redirect that would exfiltrate the access token to an attacker-chosen URL.
var allowedAppRedirects = map[string]bool{
	"rebase://callback": true,
}

// SessionUser holds the user claims extracted from an OIDC session.
type SessionUser struct {
	Sub     string `json:"sub"`
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
	// Groups carries the union of the "groups" and "roles" claims (if the IdP
	// emits them). Used for admin role gating; see OIDCProvider.IsAdmin.
	Groups []string `json:"groups,omitempty"`
}

// sessionPayload is what gets encrypted into the session cookie.
type sessionPayload struct {
	SessionUser
	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

// OIDCProvider handles OIDC authentication flows and session management.
type OIDCProvider struct {
	cfg       config.OIDCConfig
	provider  *oidc.Provider
	oauth2Cfg oauth2.Config
	verifier  *oidc.IDTokenVerifier
	encKey    []byte // 32-byte AES key for session cookie encryption
	log       *logging.Logger

	// For access token validation
	jwksMu   sync.Mutex
	jwksSet  jose.JSONWebKeySet
	jwksTime time.Time
}

// contextKey is used for storing session user in request context.
type contextKey string

const userContextKey contextKey = "oidc_user"

// NewOIDCProvider creates a new OIDC provider by performing discovery against
// the configured issuer. This makes a network call to fetch the provider metadata.
func NewOIDCProvider(ctx context.Context, cfg config.OIDCConfig, log *logging.Logger) (*OIDCProvider, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid"}
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	// Derive 32-byte encryption key from client secret using SHA-256
	encKey := deriveEncryptionKey(cfg.ClientSecret)

	return &OIDCProvider{
		cfg:       cfg,
		provider:  provider,
		oauth2Cfg: oauth2Cfg,
		verifier:  verifier,
		encKey:    encKey,
		log:       log,
	}, nil
}

// deriveEncryptionKey derives a 32-byte key from a secret string using SHA-256.
func deriveEncryptionKey(secret string) []byte {
	h := sha256.Sum256([]byte("oidc-session-v1|" + secret))
	return h[:]
}

// --- HTTP Handlers ---

// HandleLogin initiates the OIDC authorization code flow with PKCE.
func (p *OIDCProvider) HandleLogin(w http.ResponseWriter, r *http.Request) {
	// Generate state parameter
	state, err := randomString(32)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Generate PKCE code verifier
	verifier, err := randomString(43)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Store state and PKCE verifier in cookies
	secure := strings.HasPrefix(p.cfg.BaseURL, "https://")

	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     pkceCookieName,
		Value:    verifier,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Build authorization URL with PKCE
	challenge := pkceChallenge(verifier)
	url := p.oauth2Cfg.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)

	http.Redirect(w, r, url, http.StatusFound)
}

// HandleAppLogin begins the OIDC flow on behalf of the desktop app. After
// authentication, HandleCallback redirects the access token to the app's
// custom-scheme `redirect` (validated against allowedAppRedirects) instead of
// the dashboard. The app receives it via its rebase:// deep-link handler.
func (p *OIDCProvider) HandleAppLogin(w http.ResponseWriter, r *http.Request) {
	redirect := r.URL.Query().Get("redirect")
	if !allowedAppRedirects[redirect] {
		http.Error(w, "invalid redirect", http.StatusBadRequest)
		return
	}
	secure := strings.HasPrefix(p.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     appRedirectCookieName,
		Value:    redirect,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	// Reuse the standard PKCE/state setup + provider redirect.
	p.HandleLogin(w, r)
}

// HandleCallback handles the OIDC callback, exchanges the code for tokens,
// creates a session, and redirects to the dashboard.
func (p *OIDCProvider) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	// Get PKCE verifier
	pkceCookie, err := r.Cookie(pkceCookieName)
	if err != nil || pkceCookie.Value == "" {
		http.Error(w, "missing pkce cookie", http.StatusBadRequest)
		return
	}

	// Check for error from provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		p.log.Error("oidc callback error", "error", errParam, "description", desc)
		http.Error(w, "authentication failed: "+errParam, http.StatusUnauthorized)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens with PKCE verifier
	ctx := r.Context()
	token, err := p.oauth2Cfg.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pkceCookie.Value),
	)
	if err != nil {
		p.log.Error("oidc token exchange failed", "error", err.Error())
		if isJWTError(err) {
			p.renderJWTErrorPage(w, err)
			return
		}
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		p.log.Error("oidc id_token verification failed", "error", err.Error())
		if isJWTError(err) {
			p.renderJWTErrorPage(w, err)
			return
		}
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}

	// Extract claims
	var claims struct {
		Sub               string   `json:"sub"`
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Picture           string   `json:"picture"`
		Groups            []string `json:"groups"`
		Roles             []string `json:"roles"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	if name == "" {
		name = claims.Email
	}

	user := SessionUser{
		Sub:     claims.Sub,
		Email:   claims.Email,
		Name:    name,
		Picture: claims.Picture,
		Groups:  mergeGroups(claims.Groups, claims.Roles),
	}

	// Create encrypted session cookie
	if err := p.setSessionCookie(w, user); err != nil {
		p.log.Error("failed to create session cookie", "error", err.Error())
		http.Error(w, "session creation failed", http.StatusInternalServerError)
		return
	}

	// Clear state and PKCE cookies
	secure := strings.HasPrefix(p.cfg.BaseURL, "https://")
	clearCookie(w, stateCookieName, secure)
	clearCookie(w, pkceCookieName, secure)

	// Desktop-app sign-in: hand the access token back to the app via its
	// custom-scheme redirect (validated when it was set) instead of the dashboard.
	// An interstitial page (rather than a raw 302 to a custom scheme, which
	// browsers handle inconsistently) auto-opens the app and offers a manual
	// fallback, so the handoff is visible and doesn't bounce to the prior page.
	if c, err := r.Cookie(appRedirectCookieName); err == nil && allowedAppRedirects[c.Value] {
		clearCookie(w, appRedirectCookieName, secure)
		dest := c.Value + "?token=" + url.QueryEscape(token.AccessToken)
		p.renderAppHandoff(w, dest)
		return
	}

	// Redirect to dashboard
	http.Redirect(w, r, "/", http.StatusFound)
}

// renderAppHandoff returns a page that opens the desktop app at `dest` (a
// validated rebase:// URL carrying the token) and offers a manual fallback.
// The token reaches the app via its custom-scheme handler, never another origin.
func (p *OIDCProvider) renderAppHandoff(w http.ResponseWriter, dest string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><title>rebase</title>
<style>body{font-family:system-ui,-apple-system,sans-serif;background:#0d1117;color:#d4dae2;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
.card{text-align:center}h1{letter-spacing:.08em}a{display:inline-block;margin-top:16px;padding:10px 16px;background:#7aa2f7;color:#0d1117;border-radius:6px;text-decoration:none;font-weight:600}
p{color:#9aa6b2}.hint{color:#5e6a76;font-size:13px}</style></head>
<body><div class="card"><h1>rebase</h1><p>Opening the rebase app…</p>
<a id="open" href="%s">Open rebase</a>
<p class="hint">You can close this tab.</p></div>
<script>location.href=document.getElementById('open').getAttribute('href');</script>
</body></html>`, html.EscapeString(dest))
	_, _ = io.WriteString(w, page)
}

// HandleLogout clears the session and redirects to root.
func (p *OIDCProvider) HandleLogout(w http.ResponseWriter, r *http.Request) {
	secure := strings.HasPrefix(p.cfg.BaseURL, "https://")
	clearCookie(w, sessionCookieName, secure)
	http.Redirect(w, r, "/", http.StatusFound)
}

// HandleAuthStatus returns the current authentication status as JSON.
func (p *OIDCProvider) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	user := p.GetSessionUser(r)
	w.Header().Set("Content-Type", "application/json")
	if user != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"authenticated": true,
			"user":          user,
			"isAdmin":       p.IsAdmin(user),
		})
	} else {
		json.NewEncoder(w).Encode(map[string]any{
			"authenticated": false,
			"user":          nil,
		})
	}
}

// --- Middleware ---

// RequireAuth is a middleware that rejects unauthenticated requests with 401.
func (p *OIDCProvider) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := p.GetSessionUser(r)
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
			return
		}
		// Store user in context for downstream handlers
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin is a middleware that rejects unauthenticated requests with 401
// and authenticated-but-non-admin requests with 403. It is self-contained (it
// resolves the session itself) so it can be used independently of RequireAuth.
func (p *OIDCProvider) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := p.GetSessionUser(r)
		if user == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
			return
		}
		if !p.IsAdmin(user) {
			p.log.Warn("admin endpoint denied for non-admin user", "sub", user.Sub, "path", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"error": "forbidden: admin role required"})
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// IsAdmin reports whether the user passes the admin role gate. When no admin
// group is configured the gate is inert and every authenticated user is treated
// as admin (preserving pre-gate behavior); a startup warning is logged in that
// case. When a group is configured, the user must carry it in their groups/roles
// claim. Matching is case-insensitive and whitespace-trimmed.
func (p *OIDCProvider) IsAdmin(user *SessionUser) bool {
	if user == nil {
		return false
	}
	group := strings.TrimSpace(p.cfg.AdminGroup)
	if group == "" {
		return true
	}
	for _, g := range user.Groups {
		if strings.EqualFold(strings.TrimSpace(g), group) {
			return true
		}
	}
	return false
}

// UserFromContext retrieves the session user from the request context.
func UserFromContext(ctx context.Context) *SessionUser {
	user, _ := ctx.Value(userContextKey).(*SessionUser)
	return user
}

// mergeGroups returns the de-duplicated union of the groups and roles claims,
// preserving order and dropping empties. Used so an IdP that emits either claim
// (or both) feeds a single membership list for admin gating.
func mergeGroups(groups, roles []string) []string {
	if len(groups) == 0 && len(roles) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(groups)+len(roles))
	out := make([]string, 0, len(groups)+len(roles))
	for _, g := range append(append([]string(nil), groups...), roles...) {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		key := strings.ToLower(g)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, g)
	}
	return out
}

// --- Session Management ---

// GetSessionUser extracts the user from the session cookie on the request.
// Returns nil if no valid session exists.
func (p *OIDCProvider) GetSessionUser(r *http.Request) *SessionUser {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	user, err := p.decryptSession(cookie.Value)
	if err != nil {
		return nil
	}
	return user
}

// ValidateSessionCookie validates a raw session cookie value and returns the user.
func (p *OIDCProvider) ValidateSessionCookie(cookieValue string) (*SessionUser, error) {
	if cookieValue == "" {
		return nil, errors.New("empty cookie")
	}
	return p.decryptSession(cookieValue)
}

// ValidateAccessToken validates a JWT access token against the provider's JWKS.
func (p *OIDCProvider) ValidateAccessToken(ctx context.Context, tokenStr string) (*SessionUser, error) {
	if tokenStr == "" {
		return nil, errors.New("empty token")
	}

	// Parse the token without verification first to get the header
	tok, err := josejwt.ParseSigned(tokenStr, []jose.SignatureAlgorithm{
		jose.RS256, jose.RS384, jose.RS512,
		jose.ES256, jose.ES384, jose.ES512,
		jose.PS256, jose.PS384, jose.PS512,
	})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	// Get JWKS
	jwks, err := p.getJWKS(ctx)
	if err != nil {
		return nil, fmt.Errorf("get jwks: %w", err)
	}

	// Try to verify against each key
	var claims josejwt.Claims
	var customClaims struct {
		Email             string   `json:"email"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
		Picture           string   `json:"picture"`
		ClientID          string   `json:"client_id"`
		Azp               string   `json:"azp"`
		Groups            []string `json:"groups"`
		Roles             []string `json:"roles"`
	}

	verified := false
	for _, key := range jwks.Keys {
		if err := tok.Claims(key, &claims, &customClaims); err == nil {
			verified = true
			break
		}
	}
	if !verified {
		return nil, errors.New("no matching key for token signature")
	}

	// Validate standard claims
	now := time.Now()
	expected := josejwt.Expected{
		Time: now,
	}
	if p.cfg.ClientID != "" {
		expected.AnyAudience = []string{p.cfg.ClientID}
	}
	if err := claims.ValidateWithLeeway(expected, 30*time.Second); err != nil {
		return nil, fmt.Errorf("claims validation: %w", err)
	}

	// Verify issuer if present
	if claims.Issuer != "" && claims.Issuer != p.cfg.Issuer {
		return nil, errors.New("issuer mismatch")
	}

	sub := claims.Subject
	if sub == "" {
		sub = customClaims.ClientID
	}
	if sub == "" {
		sub = customClaims.Azp
	}
	if sub == "" {
		return nil, errors.New("missing sub claim")
	}

	name := customClaims.Name
	if name == "" {
		name = customClaims.PreferredUsername
	}
	if name == "" {
		name = customClaims.Email
	}
	if name == "" {
		name = "service"
	}

	return &SessionUser{
		Sub:     sub,
		Email:   customClaims.Email,
		Name:    name,
		Picture: customClaims.Picture,
		Groups:  mergeGroups(customClaims.Groups, customClaims.Roles),
	}, nil
}

// ValidateCookieHeader validates an OIDC session from a raw Cookie header string.
// Used for WebSocket upgrade requests where we only have headers.
func (p *OIDCProvider) ValidateCookieHeader(cookieHeader string) (*SessionUser, error) {
	cookies := parseCookieHeader(cookieHeader)
	value := cookies[sessionCookieName]
	if value == "" {
		return nil, errors.New("no session cookie")
	}
	return p.decryptSession(value)
}

// --- Internal helpers ---

func (p *OIDCProvider) setSessionCookie(w http.ResponseWriter, user SessionUser) error {
	now := time.Now()
	payload := sessionPayload{
		SessionUser: user,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(sessionMaxAge).Unix(),
	}

	encrypted, err := p.encryptSession(payload)
	if err != nil {
		return err
	}

	secure := strings.HasPrefix(p.cfg.BaseURL, "https://")
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    encrypted,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (p *OIDCProvider) encryptSession(payload sessionPayload) (string, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	enc, err := jose.NewEncrypter(
		jose.A256GCM,
		jose.Recipient{
			Algorithm: jose.DIRECT,
			Key:       p.encKey,
		},
		(&jose.EncrypterOptions{}).WithContentType("json"),
	)
	if err != nil {
		return "", fmt.Errorf("new encrypter: %w", err)
	}

	jwe, err := enc.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	return jwe.CompactSerialize()
}

func (p *OIDCProvider) decryptSession(encrypted string) (*SessionUser, error) {
	jwe, err := jose.ParseEncrypted(encrypted,
		[]jose.KeyAlgorithm{jose.DIRECT},
		[]jose.ContentEncryption{jose.A256GCM},
	)
	if err != nil {
		return nil, fmt.Errorf("parse jwe: %w", err)
	}

	plaintext, err := jwe.Decrypt(p.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var payload sessionPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}

	// Check expiration
	if time.Now().Unix() > payload.ExpiresAt {
		return nil, errors.New("session expired")
	}

	return &payload.SessionUser, nil
}

func (p *OIDCProvider) getJWKS(ctx context.Context) (*jose.JSONWebKeySet, error) {
	p.jwksMu.Lock()
	defer p.jwksMu.Unlock()

	// Cache for 5 minutes
	if len(p.jwksSet.Keys) > 0 && time.Since(p.jwksTime) < 5*time.Minute {
		return &p.jwksSet, nil
	}

	// Fetch from provider
	// The go-oidc provider doesn't directly expose JWKS URL, but we can
	// get it from the well-known endpoint.
	wellKnown := strings.TrimSuffix(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, "GET", wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery: %w", err)
	}
	defer resp.Body.Close()

	var disc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil {
		return nil, fmt.Errorf("parse discovery: %w", err)
	}
	if disc.JWKSURI == "" {
		return nil, errors.New("no jwks_uri in discovery")
	}

	// Fetch JWKS
	jwksReq, err := http.NewRequestWithContext(ctx, "GET", disc.JWKSURI, nil)
	if err != nil {
		return nil, err
	}
	jwksResp, err := http.DefaultClient.Do(jwksReq)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer jwksResp.Body.Close()

	body, err := io.ReadAll(jwksResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read jwks: %w", err)
	}

	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("parse jwks: %w", err)
	}

	p.jwksSet = jwks
	p.jwksTime = time.Now()
	return &p.jwksSet, nil
}

// --- Utility functions ---

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func parseCookieHeader(header string) map[string]string {
	cookies := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		idx := strings.IndexByte(part, '=')
		if idx == -1 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		if k != "" {
			cookies[k] = v
		}
	}
	return cookies
}

// isJWTError checks if an error is related to JWT signature verification.
func isJWTError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "jwt signature") ||
		strings.Contains(msg, "failed to verify signature") ||
		(strings.Contains(msg, "signature") && strings.Contains(msg, "verify"))
}

// renderJWTErrorPage renders a styled HTML error page for JWT signature errors.
func (p *OIDCProvider) renderJWTErrorPage(w http.ResponseWriter, err error) {
	signingAlg := p.cfg.IDTokenSignAlg
	if signingAlg == "" {
		signingAlg = "auto-detect"
	}
	wellKnown := strings.TrimSuffix(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, jwtErrorPageHTML,
		html.EscapeString(err.Error()),
		html.EscapeString(wellKnown),
		html.EscapeString(wellKnown),
		html.EscapeString(signingAlg),
	)
}

const jwtErrorPageHTML = `<!DOCTYPE html>
<html>
<head><title>Authentication Error</title></head>
<body>
  <h1>Authentication Failed</h1>
  <p>JWT signature validation failed: %s</p>
  <p>This usually means:</p>
  <ul>
    <li>The <code>idTokenSigningAlg</code> in your config doesn't match what your provider uses</li>
    <li>The <code>clientSecret</code> doesn't match what your provider expects (for HS256)</li>
  </ul>
  <p><strong>To fix:</strong></p>
  <ol>
    <li>Check your provider's discovery endpoint: <a href="%s">%s</a></li>
    <li>Look for <code>id_token_signing_alg_values_supported</code> to see what algorithms are supported</li>
    <li>If your provider uses RS256, remove <code>idTokenSigningAlg</code> from your config (or set it to "RS256")</li>
    <li>If your provider uses HS256, ensure your <code>clientSecret</code> matches exactly</li>
  </ol>
  <p>Current config: <code>idTokenSigningAlg: %s</code></p>
  <p><a href="/auth/login">Try again</a></p>
</body>
</html>`

// MountOIDCRoutes mounts the OIDC login/callback/logout routes on a chi-compatible router.
// Call this from the main router setup.
func (p *OIDCProvider) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", p.HandleLogin)
	mux.HandleFunc("/auth/app", p.HandleAppLogin)
	mux.HandleFunc("/auth/callback", p.HandleCallback)
	mux.HandleFunc("/auth/logout", p.HandleLogout)
	return mux
}
