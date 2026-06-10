package ws

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/auth"
	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/backup-server/internal/state"
)

// --- Mock OIDC provider for dashboard tests ---

type dashMockOIDC struct {
	server     *httptest.Server
	privateKey *rsa.PrivateKey
	keyID      string
	provider   *auth.OIDCProvider
}

func newDashMockOIDC(t *testing.T) *dashMockOIDC {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	m := &dashMockOIDC{
		privateKey: privKey,
		keyID:      "dash-test-key",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                m.server.URL,
			"authorization_endpoint":                m.server.URL + "/authorize",
			"token_endpoint":                        m.server.URL + "/token",
			"jwks_uri":                              m.server.URL + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwk := jose.JSONWebKey{
			Key:       &privKey.PublicKey,
			KeyID:     m.keyID,
			Algorithm: string(jose.RS256),
			Use:       "sig",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
	})

	m.server = httptest.NewServer(mux)
	t.Cleanup(m.server.Close)

	// Create the OIDC provider
	cfg := config.OIDCConfig{
		Enabled:      true,
		Issuer:       m.server.URL,
		ClientID:     "dash-client-id",
		ClientSecret: "dash-client-secret",
		RedirectURI:  m.server.URL + "/auth/callback",
		BaseURL:      m.server.URL,
		Scopes:       []string{"openid"},
	}

	log := testLogger(t)
	provider, err := auth.NewOIDCProvider(t.Context(), cfg, log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	m.provider = provider

	return m
}

func (m *dashMockOIDC) issueAccessToken(sub, email, name string) string {
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
		Audience:  josejwt.Audience{"dash-client-id"},
		IssuedAt:  josejwt.NewNumericDate(now),
		Expiry:    josejwt.NewNumericDate(now.Add(1 * time.Hour)),
		NotBefore: josejwt.NewNumericDate(now.Add(-1 * time.Minute)),
	}
	custom := map[string]any{"email": email, "name": name}

	raw, err := josejwt.Signed(signer).Claims(claims).Claims(custom).Serialize()
	if err != nil {
		panic(err)
	}
	return raw
}

// createSessionCookie creates a valid session cookie value using the OIDC provider.
func (m *dashMockOIDC) createSessionCookie(t *testing.T, sub, email, name string) string {
	t.Helper()
	// Use the provider's ValidateSessionCookie in reverse — we need to create a cookie.
	// Since encryptSession is not exported, we'll use the HandleAuthStatus approach:
	// set up a test that goes through the cookie creation path.
	// For simplicity, we'll test with Bearer tokens primarily and add a cookie-based
	// helper that uses the same encryption the provider uses.

	// Actually, we can use the OIDCProvider's internal methods indirectly.
	// Let's create a session cookie by making a request with a valid cookie header.
	// But that requires the cookie to already exist — chicken and egg problem.

	// The cleanest approach: since we're testing the dashboard handler (not OIDC itself),
	// and the OIDC provider tests already verify cookie encrypt/decrypt,
	// we'll primarily test dashboard auth via Bearer tokens.

	// For cookie testing, we'll use the "set session cookie" approach by
	// bypassing through the provider's internal encryption.
	// Since encryptSession isn't exported, we'll skip direct cookie tests here
	// and rely on the oidc_test.go suite for cookie validation.
	t.Skip("cookie creation requires exported encrypt method")
	return ""
}

func setupDashboardServer(t *testing.T, oidcMock *dashMockOIDC) (*httptest.Server, *state.Store, *DashboardHandler) {
	t.Helper()
	store := state.New()
	log := testLogger(t)

	handler := NewDashboardHandler(store, log, oidcMock.provider, []string{"*"})

	mux := http.NewServeMux()
	mux.Handle("/ws/dashboard", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, store, handler
}

func dialDashboard(t *testing.T, serverURL string, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/dashboard"
	headers := http.Header{}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		t.Fatalf("dial dashboard WS: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

// --- Tests ---

func TestDashboardHandler_SuccessfulConnection(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, store, _ := setupDashboardServer(t, mock)

	// Add some clients to state
	store.AddClient("node-1", nil)
	store.AddClient("node-2", nil)

	token := mock.issueAccessToken("user-1", "user@example.com", "Test User")
	conn := dialDashboard(t, server.URL, token)

	// Should receive client_list on connect
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read client_list: %v", err)
	}

	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Event != "client_list" {
		t.Errorf("Event = %q, want client_list", msg.Event)
	}

	var data map[string]any
	json.Unmarshal(msg.Data, &data)
	if data["timestamp"] == nil {
		t.Error("timestamp should be present")
	}
}

func TestDashboardHandler_RejectsNoAuth(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, _ := setupDashboardServer(t, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/dashboard"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without auth")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestDashboardHandler_RejectsInvalidToken(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, _ := setupDashboardServer(t, mock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/dashboard"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer invalid-token")

	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err == nil {
		t.Fatal("expected dial to fail with invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestDashboardHandler_EventDispatch(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, handler := setupDashboardServer(t, mock)

	var received sync.WaitGroup
	received.Add(1)
	var gotEvent string
	handler.OnEvent = func(dc *DashboardConn, msg *Message) {
		gotEvent = msg.Event
		received.Done()
	}

	token := mock.issueAccessToken("user-1", "user@example.com", "Test User")
	conn := dialDashboard(t, server.URL, token)

	// Read client_list
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	// Send an event
	msg, _ := Encode("shell_start", map[string]any{"clientId": "node-1"})
	conn.Write(ctx, websocket.MessageText, msg)

	done := make(chan struct{})
	go func() {
		received.Wait()
		close(done)
	}()
	select {
	case <-done:
		if gotEvent != "shell_start" {
			t.Errorf("event = %q, want shell_start", gotEvent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event not dispatched within timeout")
	}
}

func TestDashboardHandler_Broadcast(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, handler := setupDashboardServer(t, mock)

	// Connect two dashboards
	token1 := mock.issueAccessToken("user-1", "u1@example.com", "User 1")
	token2 := mock.issueAccessToken("user-2", "u2@example.com", "User 2")

	conn1 := dialDashboard(t, server.URL, token1)
	conn2 := dialDashboard(t, server.URL, token2)

	// Read initial client_list from both
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn1.Read(ctx)
	conn2.Read(ctx)

	// Wait for connections to be registered
	time.Sleep(50 * time.Millisecond)

	if handler.ConnectedCount() != 2 {
		t.Fatalf("ConnectedCount = %d, want 2", handler.ConnectedCount())
	}

	// Broadcast a message
	handler.Broadcast("test_event", map[string]string{"msg": "hello"})

	// Both should receive it
	_, raw1, err := conn1.Read(ctx)
	if err != nil {
		t.Fatalf("conn1 read: %v", err)
	}
	msg1, _ := Decode(raw1)
	if msg1.Event != "test_event" {
		t.Errorf("conn1 event = %q", msg1.Event)
	}

	_, raw2, err := conn2.Read(ctx)
	if err != nil {
		t.Fatalf("conn2 read: %v", err)
	}
	msg2, _ := Decode(raw2)
	if msg2.Event != "test_event" {
		t.Errorf("conn2 event = %q", msg2.Event)
	}
}

func TestDashboardHandler_DisconnectCleanup(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, handler := setupDashboardServer(t, mock)

	disconnected := make(chan string, 1)
	handler.OnDisconnect = func(dc *DashboardConn) {
		disconnected <- dc.ID
	}

	token := mock.issueAccessToken("user-1", "user@example.com", "Test User")
	conn := dialDashboard(t, server.URL, token)

	// Read client_list
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	time.Sleep(50 * time.Millisecond)
	if handler.ConnectedCount() != 1 {
		t.Fatalf("ConnectedCount = %d, want 1", handler.ConnectedCount())
	}

	// Close the connection
	conn.Close(websocket.StatusNormalClosure, "bye")

	select {
	case <-disconnected:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect not received")
	}

	time.Sleep(50 * time.Millisecond)
	if handler.ConnectedCount() != 0 {
		t.Errorf("ConnectedCount = %d after disconnect, want 0", handler.ConnectedCount())
	}
}

func TestDashboardHandler_SendTo(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, handler := setupDashboardServer(t, mock)

	token := mock.issueAccessToken("user-1", "user@example.com", "Test User")
	conn := dialDashboard(t, server.URL, token)

	// Read client_list
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	time.Sleep(50 * time.Millisecond)

	// Get the connection ID
	handler.dashMu.RLock()
	var connID string
	for id := range handler.dashboards {
		connID = id
		break
	}
	handler.dashMu.RUnlock()

	if connID == "" {
		t.Fatal("no connection registered")
	}

	// Send to specific connection
	ok := handler.SendTo(connID, "targeted_event", map[string]string{"for": "you"})
	if !ok {
		t.Fatal("SendTo returned false")
	}

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read targeted: %v", err)
	}
	msg, _ := Decode(raw)
	if msg.Event != "targeted_event" {
		t.Errorf("event = %q, want targeted_event", msg.Event)
	}

	// SendTo unknown ID returns false
	ok = handler.SendTo("nonexistent", "test", nil)
	if ok {
		t.Error("SendTo should return false for unknown ID")
	}
}

func TestDashboardHandler_BroadcastClientList(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, store, handler := setupDashboardServer(t, mock)

	token := mock.issueAccessToken("user-1", "user@example.com", "Test User")
	conn := dialDashboard(t, server.URL, token)

	// Read initial client_list
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	time.Sleep(50 * time.Millisecond)

	// Add a client and broadcast
	store.AddClient("node-new", nil)
	handler.BroadcastClientList()

	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	msg, _ := Decode(raw)
	if msg.Event != "client_list" {
		t.Errorf("event = %q, want client_list", msg.Event)
	}

	var data map[string]any
	json.Unmarshal(msg.Data, &data)
	ids, ok := data["clientIds"].([]any)
	if !ok {
		t.Fatal("clientIds not an array")
	}
	found := false
	for _, entry := range ids {
		if obj, ok := entry.(map[string]any); ok {
			if obj["clientId"] == "node-new" {
				found = true
			}
		}
	}
	if !found {
		t.Error("node-new not in broadcast client list")
	}
}

func TestDashboardHandler_NoOIDC_RejectsAll(t *testing.T) {
	store := state.New()
	log := testLogger(t)

	// No OIDC provider — should reject everything
	handler := NewDashboardHandler(store, log, nil, []string{"*"})

	mux := http.NewServeMux()
	mux.Handle("/ws/dashboard", handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/dashboard"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail when OIDC is nil")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestDashboardHandler_UserInEventCallback(t *testing.T) {
	mock := newDashMockOIDC(t)
	server, _, handler := setupDashboardServer(t, mock)

	var gotUser *auth.SessionUser
	var wg sync.WaitGroup
	wg.Add(1)
	handler.OnEvent = func(dc *DashboardConn, msg *Message) {
		gotUser = dc.User
		wg.Done()
	}

	token := mock.issueAccessToken("user-xyz", "xyz@example.com", "XYZ User")
	conn := dialDashboard(t, server.URL, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx) // client_list

	msg, _ := Encode("ping", nil)
	conn.Write(ctx, websocket.MessageText, msg)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event callback not called")
	}

	if gotUser == nil {
		t.Fatal("user is nil in callback")
	}
	if gotUser.Sub != "user-xyz" {
		t.Errorf("user.Sub = %q, want user-xyz", gotUser.Sub)
	}
}
