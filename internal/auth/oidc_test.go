package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// --- Mock OIDC provider ---

type mockOIDCServer struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
}

func newMockOIDCServer(t *testing.T) *mockOIDCServer {
	t.Helper()

	// Generate RSA key for signing tokens
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	m := &mockOIDCServer{
		privateKey: privKey,
		keyID:      "test-key-1",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", m.handleDiscovery)
	mux.HandleFunc("/jwks", m.handleJWKS)
	mux.HandleFunc("/token", m.handleToken)
	mux.HandleFunc("/authorize", m.handleAuthorize)

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockOIDCServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"issuer":                 m.server.URL,
		"authorization_endpoint": m.server.URL + "/authorize",
		"token_endpoint":         m.server.URL + "/token",
		"jwks_uri":               m.server.URL + "/jwks",
		"response_types_supported": []string{"code"},
		"subject_types_supported":  []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (m *mockOIDCServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	jwk := jose.JSONWebKey{
		Key:       &m.privateKey.PublicKey,
		KeyID:     m.keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
}

func (m *mockOIDCServer) handleToken(w http.ResponseWriter, r *http.Request) {
	// Issue an ID token
	idToken := m.issueIDToken("test-user-123", "test@example.com", "Test User", "test-client-id")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "mock-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

func (m *mockOIDCServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	// Redirect back with a code
	state := r.URL.Query().Get("state")
	redirect := r.URL.Query().Get("redirect_uri")
	u, _ := url.Parse(redirect)
	q := u.Query()
	q.Set("code", "mock-auth-code")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (m *mockOIDCServer) issueIDToken(sub, email, name, audience string) string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.privateKey},
		(&jose.SignerOptions{}).WithHeader("kid", m.keyID),
	)
	if err != nil {
		panic(err)
	}

	now := time.Now()
	claims := josejwt.Claims{
		Issuer:    m.server.URL,
		Subject:   sub,
		Audience:  josejwt.Audience{audience},
		IssuedAt:  josejwt.NewNumericDate(now),
		Expiry:    josejwt.NewNumericDate(now.Add(1 * time.Hour)),
		NotBefore: josejwt.NewNumericDate(now.Add(-1 * time.Minute)),
	}
	customClaims := map[string]any{
		"email": email,
		"name":  name,
	}

	raw, err := josejwt.Signed(signer).Claims(claims).Claims(customClaims).Serialize()
	if err != nil {
		panic(err)
	}
	return raw
}

func (m *mockOIDCServer) issueAccessToken(sub, email, name, audience string) string {
	// Same as ID token for testing purposes
	return m.issueIDToken(sub, email, name, audience)
}

func (m *mockOIDCServer) issueExpiredToken(sub, audience string) string {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: m.privateKey},
		(&jose.SignerOptions{}).WithHeader("kid", m.keyID),
	)
	if err != nil {
		panic(err)
	}

	past := time.Now().Add(-2 * time.Hour)
	claims := josejwt.Claims{
		Issuer:   m.server.URL,
		Subject:  sub,
		Audience: josejwt.Audience{audience},
		IssuedAt: josejwt.NewNumericDate(past),
		Expiry:   josejwt.NewNumericDate(past.Add(1 * time.Hour)), // expired 1 hour ago
	}

	raw, err := josejwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		panic(err)
	}
	return raw
}

func testOIDCLogger(t *testing.T) *logging.Logger {
	t.Helper()
	l, err := logging.New(logging.Options{Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Sync() })
	return l
}

func setupOIDCProvider(t *testing.T, mock *mockOIDCServer) *OIDCProvider {
	t.Helper()
	cfg := config.OIDCConfig{
		Enabled:      true,
		Issuer:       mock.server.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  mock.server.URL + "/auth/callback",
		BaseURL:      mock.server.URL,
		Scopes:       []string{"openid", "profile", "email"},
	}

	log := testOIDCLogger(t)
	provider, err := NewOIDCProvider(t.Context(), cfg, log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return provider
}

// --- Session cookie tests ---

func TestSessionEncryptDecrypt(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	user := SessionUser{
		Sub:   "user-123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	payload := sessionPayload{
		SessionUser: user,
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}

	encrypted, err := p.encryptSession(payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted is empty")
	}

	decrypted, err := p.decryptSession(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if decrypted.Sub != "user-123" {
		t.Errorf("Sub = %q, want user-123", decrypted.Sub)
	}
	if decrypted.Email != "test@example.com" {
		t.Errorf("Email = %q", decrypted.Email)
	}
	if decrypted.Name != "Test User" {
		t.Errorf("Name = %q", decrypted.Name)
	}
}

func TestSessionDecrypt_Expired(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-123"},
		IssuedAt:    time.Now().Add(-48 * time.Hour).Unix(),
		ExpiresAt:   time.Now().Add(-24 * time.Hour).Unix(), // expired yesterday
	}

	encrypted, err := p.encryptSession(payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = p.decryptSession(encrypted)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want expired", err)
	}
}

func TestSessionDecrypt_Tampered(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-123"},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}

	encrypted, err := p.encryptSession(payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Tamper with the encrypted value
	tampered := encrypted[:len(encrypted)-5] + "XXXXX"
	_, err = p.decryptSession(tampered)
	if err == nil {
		t.Fatal("expected error for tampered session")
	}
}

func TestSessionDecrypt_WrongKey(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-123"},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}

	encrypted, err := p.encryptSession(payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Try decrypting with a different key
	p2 := &OIDCProvider{encKey: deriveEncryptionKey("wrong-secret")}
	_, err = p2.decryptSession(encrypted)
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
}

func TestSessionDecrypt_InvalidJWE(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	_, err := p.decryptSession("not-valid-jwe")
	if err == nil {
		t.Fatal("expected error for invalid JWE")
	}
}

// --- Login handler ---

func TestHandleLogin_Redirects(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/login", nil)
	w := httptest.NewRecorder()
	p.HandleLogin(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "/authorize") {
		t.Errorf("Location = %q, should contain /authorize", location)
	}
	if !strings.Contains(location, "code_challenge=") {
		t.Error("Location should contain PKCE code_challenge")
	}
	if !strings.Contains(location, "code_challenge_method=S256") {
		t.Error("Location should contain code_challenge_method=S256")
	}
	if !strings.Contains(location, "state=") {
		t.Error("Location should contain state parameter")
	}

	// Should set state and PKCE cookies
	cookies := w.Result().Cookies()
	var hasState, hasPKCE bool
	for _, c := range cookies {
		if c.Name == stateCookieName {
			hasState = true
			if !c.HttpOnly {
				t.Error("state cookie should be HttpOnly")
			}
		}
		if c.Name == pkceCookieName {
			hasPKCE = true
			if !c.HttpOnly {
				t.Error("PKCE cookie should be HttpOnly")
			}
		}
	}
	if !hasState {
		t.Error("missing state cookie")
	}
	if !hasPKCE {
		t.Error("missing PKCE cookie")
	}
}

// --- Callback handler ---

func TestHandleCallback_MissingStateCookie(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/callback?code=abc&state=xyz", nil)
	w := httptest.NewRecorder()
	p.HandleCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCallback_StateMismatch(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/callback?code=abc&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "different-state"})
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
	w := httptest.NewRecorder()
	p.HandleCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleCallback_ProviderError(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/callback?error=access_denied&error_description=user+denied&state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
	w := httptest.NewRecorder()
	p.HandleCallback(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHandleCallback_MissingCode(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/callback?state=xyz", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "xyz"})
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: "verifier"})
	w := httptest.NewRecorder()
	p.HandleCallback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// --- Logout handler ---

func TestHandleLogout_ClearsCookie(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/auth/logout", nil)
	w := httptest.NewRecorder()
	p.HandleLogout(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}

	// Should clear the session cookie
	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			if c.MaxAge >= 0 {
				t.Errorf("session cookie MaxAge = %d, want negative (clear)", c.MaxAge)
			}
			return
		}
	}
	t.Error("session cookie not found in response (should be cleared)")
}

// --- Auth status handler ---

func TestHandleAuthStatus_NoSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	p.HandleAuthStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", result["authenticated"])
	}
}

func TestHandleAuthStatus_WithSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	// Create a session cookie
	payload := sessionPayload{
		SessionUser: SessionUser{
			Sub:   "user-123",
			Email: "test@example.com",
			Name:  "Test User",
		},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	}
	encrypted, err := p.encryptSession(payload)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: encrypted})
	w := httptest.NewRecorder()
	p.HandleAuthStatus(w, req)

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", result["authenticated"])
	}
	user := result["user"].(map[string]any)
	if user["sub"] != "user-123" {
		t.Errorf("sub = %v", user["sub"])
	}
	if user["email"] != "test@example.com" {
		t.Errorf("email = %v", user["email"])
	}
}

// --- RequireAuth middleware ---

func TestRequireAuth_NoSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	handler := p.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_WithSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	var ctxUser *SessionUser
	handler := p.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	// Create a valid session cookie
	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-123", Email: "test@example.com"},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}
	encrypted, _ := p.encryptSession(payload)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: encrypted})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ctxUser == nil {
		t.Fatal("user not set in context")
	}
	if ctxUser.Sub != "user-123" {
		t.Errorf("context user sub = %q", ctxUser.Sub)
	}
}

func TestRequireAuth_ExpiredSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	handler := p.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create an expired session cookie
	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-123"},
		IssuedAt:    time.Now().Add(-48 * time.Hour).Unix(),
		ExpiresAt:   time.Now().Add(-24 * time.Hour).Unix(),
	}
	encrypted, _ := p.encryptSession(payload)

	req := httptest.NewRequest("GET", "/protected", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: encrypted})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for expired session", w.Code)
	}
}

// --- Admin role gating ---

func setupAdminProvider(t *testing.T, mock *mockOIDCServer, adminGroup string) *OIDCProvider {
	t.Helper()
	cfg := config.OIDCConfig{
		Enabled:      true,
		Issuer:       mock.server.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURI:  mock.server.URL + "/auth/callback",
		BaseURL:      mock.server.URL,
		Scopes:       []string{"openid", "profile", "email", "groups"},
		AdminGroup:   adminGroup,
	}
	provider, err := NewOIDCProvider(t.Context(), cfg, testOIDCLogger(t))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return provider
}

func TestIsAdmin_NilUser(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "admins")
	if p.IsAdmin(nil) {
		t.Error("nil user should never be admin")
	}
}

func TestIsAdmin_NoGroupConfigured_AllowsAny(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "") // gate inert
	if !p.IsAdmin(&SessionUser{Sub: "u1"}) {
		t.Error("with no admin group configured, any authenticated user should pass")
	}
}

func TestIsAdmin_GroupMembership(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "admins")

	if !p.IsAdmin(&SessionUser{Sub: "u1", Groups: []string{"users", "admins"}}) {
		t.Error("user in admins group should be admin")
	}
	if p.IsAdmin(&SessionUser{Sub: "u2", Groups: []string{"users"}}) {
		t.Error("user not in admins group should not be admin")
	}
	if p.IsAdmin(&SessionUser{Sub: "u3"}) {
		t.Error("user with no groups should not be admin when a group is required")
	}
}

func TestIsAdmin_CaseInsensitive(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "Admins")
	if !p.IsAdmin(&SessionUser{Sub: "u1", Groups: []string{"  ADMINS "}}) {
		t.Error("group match should be case-insensitive and whitespace-trimmed")
	}
}

func TestRequireAdmin_NoSession_401(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "admins")

	h := p.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequireAdmin_NonAdmin_403(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "admins")

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "u1", Groups: []string{"users"}},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}
	encrypted, _ := p.encryptSession(payload)

	h := p.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: encrypted})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for non-admin", w.Code)
	}
}

func TestRequireAdmin_Admin_PassesWithUserInContext(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupAdminProvider(t, mock, "admins")

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "u1", Groups: []string{"admins"}},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}
	encrypted, _ := p.encryptSession(payload)

	var ctxUser *SessionUser
	h := p.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: encrypted})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for admin", w.Code)
	}
	if ctxUser == nil || ctxUser.Sub != "u1" {
		t.Fatal("admin user should be in request context")
	}
}

func TestMergeGroups(t *testing.T) {
	got := mergeGroups([]string{"a", "b", ""}, []string{"B", "c"})
	want := []string{"a", "b", "c"} // case-insensitive dedupe, empties dropped
	if len(got) != len(want) {
		t.Fatalf("mergeGroups = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("mergeGroups[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if mergeGroups(nil, nil) != nil {
		t.Error("mergeGroups(nil,nil) should be nil")
	}
}

// --- Access token validation ---

func TestValidateAccessToken_Valid(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	token := mock.issueAccessToken("svc-user", "svc@example.com", "Service Account", "test-client-id")

	user, err := p.ValidateAccessToken(t.Context(), token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if user.Sub != "svc-user" {
		t.Errorf("Sub = %q, want svc-user", user.Sub)
	}
	if user.Email != "svc@example.com" {
		t.Errorf("Email = %q", user.Email)
	}
}

func TestValidateAccessToken_Expired(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	token := mock.issueExpiredToken("svc-user", "test-client-id")

	_, err := p.ValidateAccessToken(t.Context(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestValidateAccessToken_WrongAudience(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	// Issue token for a different audience
	token := mock.issueAccessToken("svc-user", "svc@example.com", "Service", "wrong-audience")

	_, err := p.ValidateAccessToken(t.Context(), token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestValidateAccessToken_Empty(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	_, err := p.ValidateAccessToken(t.Context(), "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestValidateAccessToken_Garbage(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	_, err := p.ValidateAccessToken(t.Context(), "not-a-jwt")
	if err == nil {
		t.Fatal("expected error for garbage token")
	}
}

// --- Cookie header validation ---

func TestValidateCookieHeader_ValidSession(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	payload := sessionPayload{
		SessionUser: SessionUser{Sub: "user-456", Email: "user@example.com"},
		IssuedAt:    time.Now().Unix(),
		ExpiresAt:   time.Now().Add(24 * time.Hour).Unix(),
	}
	encrypted, _ := p.encryptSession(payload)

	header := sessionCookieName + "=" + encrypted + "; other_cookie=abc"
	user, err := p.ValidateCookieHeader(header)
	if err != nil {
		t.Fatalf("ValidateCookieHeader: %v", err)
	}
	if user.Sub != "user-456" {
		t.Errorf("Sub = %q", user.Sub)
	}
}

func TestValidateCookieHeader_NoSessionCookie(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	_, err := p.ValidateCookieHeader("other_cookie=abc")
	if err == nil {
		t.Fatal("expected error when session cookie missing")
	}
}

func TestValidateCookieHeader_EmptyHeader(t *testing.T) {
	mock := newMockOIDCServer(t)
	p := setupOIDCProvider(t, mock)

	_, err := p.ValidateCookieHeader("")
	if err == nil {
		t.Fatal("expected error for empty header")
	}
}

// --- UserFromContext ---

func TestUserFromContext_NilContext(t *testing.T) {
	// Should return nil when not set
	user := UserFromContext(httptest.NewRequest("GET", "/", nil).Context())
	if user != nil {
		t.Error("expected nil user from empty context")
	}
}

// --- Helper tests ---

func TestPkceChallenge(t *testing.T) {
	// Ensure PKCE challenge is deterministic for same verifier
	c1 := pkceChallenge("test-verifier")
	c2 := pkceChallenge("test-verifier")
	if c1 != c2 {
		t.Error("PKCE challenge should be deterministic")
	}

	// Different verifiers produce different challenges
	c3 := pkceChallenge("different-verifier")
	if c1 == c3 {
		t.Error("different verifiers should produce different challenges")
	}
}

func TestParseCookieHeader(t *testing.T) {
	cookies := parseCookieHeader("a=1; b=2; c=hello%20world")
	if cookies["a"] != "1" {
		t.Errorf("a = %q", cookies["a"])
	}
	if cookies["b"] != "2" {
		t.Errorf("b = %q", cookies["b"])
	}
	if cookies["c"] != "hello%20world" {
		t.Errorf("c = %q", cookies["c"])
	}
}

func TestParseCookieHeader_Empty(t *testing.T) {
	cookies := parseCookieHeader("")
	if len(cookies) != 0 {
		t.Errorf("expected empty map, got %v", cookies)
	}
}

func TestDeriveEncryptionKey_Deterministic(t *testing.T) {
	k1 := deriveEncryptionKey("secret")
	k2 := deriveEncryptionKey("secret")
	if string(k1) != string(k2) {
		t.Error("same secret should produce same key")
	}
}

func TestDeriveEncryptionKey_DifferentSecrets(t *testing.T) {
	k1 := deriveEncryptionKey("secret-a")
	k2 := deriveEncryptionKey("secret-b")
	if string(k1) == string(k2) {
		t.Error("different secrets should produce different keys")
	}
}

func TestDeriveEncryptionKey_Length(t *testing.T) {
	k := deriveEncryptionKey("secret")
	if len(k) != 32 {
		t.Errorf("key length = %d, want 32", len(k))
	}
}
