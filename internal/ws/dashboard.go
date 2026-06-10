package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/auth"
	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// isoMillis matches JavaScript's Date.toISOString() output (with milliseconds).
const isoMillis = "2006-01-02T15:04:05.000Z"

// DashboardConn represents a connected dashboard WebSocket.
type DashboardConn struct {
	Conn *websocket.Conn
	User *auth.SessionUser
	ID   string // unique connection ID
	mu   sync.Mutex
}

// Send writes a JSON-encoded message to the dashboard connection.
func (dc *DashboardConn) Send(event string, data any) error {
	msg, err := Encode(event, data)
	if err != nil {
		return err
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.Conn.Write(context.Background(), websocket.MessageText, msg)
}

// DashboardHandler handles WebSocket connections from dashboards.
type DashboardHandler struct {
	store   *state.Store
	log     *logging.Logger
	oidc    *auth.OIDCProvider // nil if OIDC is disabled
	origins []string           // allowed origins for CORS

	// dashMu protects dashboards map
	dashMu     sync.RWMutex
	dashboards map[string]*DashboardConn

	// OnEvent is called for each dashboard event.
	// The handler should dispatch to relay logic.
	OnEvent func(dc *DashboardConn, msg *Message)

	// OnDisconnect is called when a dashboard disconnects.
	OnDisconnect func(dc *DashboardConn)
}

// NewDashboardHandler creates a handler for the /ws/dashboard endpoint.
func NewDashboardHandler(store *state.Store, log *logging.Logger, oidc *auth.OIDCProvider, origins []string) *DashboardHandler {
	return &DashboardHandler{
		store:      store,
		log:        log,
		oidc:       oidc,
		origins:    origins,
		dashboards: make(map[string]*DashboardConn),
	}
}

// ServeHTTP upgrades the HTTP connection to WebSocket for dashboards.
func (h *DashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate: session cookie or Bearer token
	user, err := h.authenticate(r)
	if err != nil || user == nil {
		h.log.Warn("dashboard auth failed",
			"error", err,
			"remote", r.RemoteAddr,
			"hasCookie", r.Header.Get("Cookie") != "",
			"hasAuth", r.Header.Get("Authorization") != "",
		)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns(),
	})
	if err != nil {
		h.log.Error("dashboard ws upgrade failed", "error", err, "user", user.Sub)
		return
	}

	// Generate connection ID
	connID := generateConnID()

	dc := &DashboardConn{
		Conn: conn,
		User: user,
		ID:   connID,
	}

	// Register dashboard connection
	h.dashMu.Lock()
	h.dashboards[connID] = dc
	h.dashMu.Unlock()

	h.log.Info("dashboard connected", "connId", connID, "user", user.Sub, "remote", r.RemoteAddr)

	// Send initial client list
	h.sendClientList(dc)

	// Start read loop
	h.readLoop(r.Context(), dc)

	// Cleanup on disconnect
	h.dashMu.Lock()
	delete(h.dashboards, connID)
	h.dashMu.Unlock()

	h.log.Info("dashboard disconnected", "connId", connID, "user", user.Sub)
	if h.OnDisconnect != nil {
		h.OnDisconnect(dc)
	}
}

// Broadcast sends a message to all connected dashboards.
func (h *DashboardHandler) Broadcast(event string, data any) {
	msg, err := Encode(event, data)
	if err != nil {
		h.log.Error("broadcast encode failed", "event", event, "error", err)
		return
	}

	h.dashMu.RLock()
	conns := make([]*DashboardConn, 0, len(h.dashboards))
	for _, dc := range h.dashboards {
		conns = append(conns, dc)
	}
	h.dashMu.RUnlock()

	for _, dc := range conns {
		dc.mu.Lock()
		err := dc.Conn.Write(context.Background(), websocket.MessageText, msg)
		dc.mu.Unlock()
		if err != nil {
			h.log.Debug("broadcast write failed", "connId", dc.ID, "event", event, "error", err)
		}
	}
}

// SendTo sends a message to a specific dashboard connection by ID.
func (h *DashboardHandler) SendTo(connID string, event string, data any) bool {
	h.dashMu.RLock()
	dc, ok := h.dashboards[connID]
	h.dashMu.RUnlock()
	if !ok {
		return false
	}
	return dc.Send(event, data) == nil
}

// ConnectedCount returns the number of connected dashboards.
func (h *DashboardHandler) ConnectedCount() int {
	h.dashMu.RLock()
	defer h.dashMu.RUnlock()
	return len(h.dashboards)
}

// --- Internal ---

func (h *DashboardHandler) authenticate(r *http.Request) (*auth.SessionUser, error) {
	if h.oidc == nil {
		// No OIDC configured — reject all connections
		return nil, nil
	}

	// Try Bearer token first (for service accounts / non-browser clients)
	if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		user, err := h.oidc.ValidateAccessToken(r.Context(), token)
		if err == nil && user != nil {
			return user, nil
		}
		// Fall through to cookie auth
	}

	// Try session cookie
	user := h.oidc.GetSessionUser(r)
	if user != nil {
		return user, nil
	}

	// Try raw Cookie header (for WebSocket upgrades where cookies aren't parsed)
	if cookieHeader := r.Header.Get("Cookie"); cookieHeader != "" {
		user, err := h.oidc.ValidateCookieHeader(cookieHeader)
		if err == nil && user != nil {
			return user, nil
		}
	}

	return nil, nil
}

func (h *DashboardHandler) readLoop(ctx context.Context, dc *DashboardConn) {
	for {
		_, raw, err := dc.Conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || ctx.Err() != nil {
				return
			}
			h.log.Warn("dashboard read error", "connId", dc.ID, "error", err)
			return
		}

		msg, err := Decode(raw)
		if err != nil {
			h.log.Warn("dashboard message decode error", "connId", dc.ID, "error", err)
			continue
		}

		if h.OnEvent != nil {
			h.OnEvent(dc, msg)
		}
	}
}

func (h *DashboardHandler) sendClientList(dc *DashboardConn) {
	clients := h.store.PublicClients()
	dc.Send("client_list", map[string]any{
		"clientIds": clients,
		"timestamp": time.Now().UTC().Format(isoMillis),
	})
}

// BroadcastClientList sends the current client list to all dashboards.
// Called when agents connect/disconnect.
func (h *DashboardHandler) BroadcastClientList() {
	clients := h.store.PublicClients()
	h.Broadcast("client_list", map[string]any{
		"clientIds": clients,
		"timestamp": time.Now().UTC().Format(isoMillis),
	})
}

func (h *DashboardHandler) originPatterns() []string {
	if len(h.origins) > 0 {
		return h.origins
	}
	// Default: allow all origins since the server is typically behind a reverse proxy
	// where the Origin header won't match the internal Host. Auth is enforced via
	// session cookie / Bearer token, not origin check.
	return []string{"*"}
}

func generateConnID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
