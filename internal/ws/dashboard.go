package ws

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/audit"
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
	// IsAdmin is resolved once at connect time from the OIDC provider and
	// carried on the connection: relay.authorizeDashboardEvent gates
	// privileged events on it but has no provider handle of its own.
	IsAdmin bool
	mu      sync.Mutex
}

// Send writes a JSON-encoded message to the dashboard connection.
func (dc *DashboardConn) Send(event string, data any) error {
	msg, err := Encode(event, data)
	if err != nil {
		return err
	}
	return dc.writeFrame(msg)
}

// writeFrame writes an already-encoded frame under WriteTimeout. Broadcast
// encodes once for every connection, so it shares this rather than re-encoding
// per connection via Send.
//
// The deadline starts after the write lock is acquired, not before: charging a
// connection's timeout for time spent queued behind another sender would close
// healthy connections under load, which is the opposite of the intent. Once a
// peer does time out the transport closes it, so senders queued behind it fail
// immediately rather than each waiting out their own WriteTimeout.
func (dc *DashboardConn) writeFrame(msg []byte) error {
	if dc.Conn == nil {
		return errors.New("dashboard connection has no socket")
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), WriteTimeout)
	defer cancel()
	return dc.Conn.Write(ctx, websocket.MessageText, msg)
}

// DashboardHandler handles WebSocket connections from dashboards.
type DashboardHandler struct {
	store   *state.Store
	log     *logging.Logger
	oidc    *auth.OIDCProvider // nil if OIDC is disabled
	origins []string           // allowed origins for CORS

	// audit records dashboard session lifecycle. Nil disables recording.
	audit *audit.Logger

	// insecure serves dashboard sessions with no authentication, for running
	// without an OIDC provider. Set only from config.InsecureAllowUnauthenticated,
	// which startup refuses to default on.
	insecure bool

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

// SetAudit attaches the audit logger. Called during server wiring.
func (h *DashboardHandler) SetAudit(a *audit.Logger) { h.audit = a }

// InsecureUser is the principal assigned to dashboard sessions when running
// without authentication. It is a local-development identity: it never comes
// from an IdP, and audit records carrying it mean the session was not
// authenticated at all.
const InsecureUser = "insecure:unauthenticated"

// SetInsecureAllowUnauthenticated serves dashboard sessions without
// authentication. Wired only from config.InsecureAllowUnauthenticated.
func (h *DashboardHandler) SetInsecureAllowUnauthenticated(v bool) { h.insecure = v }

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
		if h.audit != nil {
			// Reachable without credentials; throttled so it cannot be used to
			// drive unbounded appends.
			h.audit.EmitThrottled(audit.Event{
				Type:      audit.TypeLoginFailure,
				Outcome:   audit.OutcomeDeny,
				Remote:    audit.RemoteIP(r),
				UserAgent: r.UserAgent(),
				Action:    "ws/dashboard",
			})
		}
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
	// Match the agent/server frame limit (1 MiB) so chunked uploads
	// (file_put_chunk, 256 KiB) aren't truncated at the 32 KiB default.
	conn.SetReadLimit(1 << 20)

	// Generate connection ID
	connID := generateConnID()

	dc := &DashboardConn{
		Conn:    conn,
		User:    user,
		ID:      connID,
		IsAdmin: h.oidc == nil && h.insecure || h.oidc != nil && h.oidc.IsAdmin(user),
	}

	// Register dashboard connection
	h.dashMu.Lock()
	h.dashboards[connID] = dc
	h.dashMu.Unlock()

	h.log.Info("dashboard connected", "connId", connID, "user", user.Sub,
		"isAdmin", dc.IsAdmin, "groups", user.Groups, "remote", r.RemoteAddr)
	if h.audit != nil {
		h.audit.EmitAccess(audit.Event{
			Type:      audit.TypeDashboardOpen,
			Outcome:   audit.OutcomeAllow,
			Actor:     user.Sub,
			ActorName: user.Email,
			Groups:    user.Groups,
			Remote:    audit.RemoteIP(r),
			UserAgent: r.UserAgent(),
			Detail:    map[string]any{"connId": connID, "isAdmin": dc.IsAdmin},
		})
	}

	// Send initial client list, then replay what the store already knows about
	// each client. Agents only push stats every 30s and push kiosk/variant
	// status on change, so without this a freshly loaded dashboard renders an
	// empty node for up to half a minute (or indefinitely, for the on-change
	// signals) even though the server has the values cached.
	h.sendClientList(dc)
	h.sendCachedState(dc)

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
		if err := dc.writeFrame(msg); err != nil {
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
		if h.insecure {
			return &auth.SessionUser{Sub: InsecureUser, Name: "unauthenticated"}, nil
		}
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

// sendCachedState replays the store's per-client caches to one dashboard,
// using the exact event shapes the live broadcast path emits so the client
// needs no special-casing for a replay. Timestamps are not synthesized: a
// replayed sample keeps whatever the agent reported, so an offline node cannot
// look freshly-reporting.
func (h *DashboardHandler) sendCachedState(dc *DashboardConn) {
	for _, pub := range h.store.PublicClients() {
		id := pub.ClientID

		if stats := h.store.GetStats(id); stats != nil {
			_ = dc.Send("stats", map[string]any{"clientId": id, "data": stats})
		}
		if hist := h.store.GetStatsHistory(id); len(hist) > 0 {
			samples := make([]map[string]any, 0, len(hist))
			for _, entry := range hist {
				samples = append(samples, entry.Stats)
			}
			_ = dc.Send("stats_history", map[string]any{"clientId": id, "samples": samples})
		}
		if alerts := h.store.GetAlerts(id); alerts != nil {
			_ = dc.Send("alerts", map[string]any{"clientId": id, "data": alerts})
		}
		if kiosk := h.store.GetKioskStatus(id); kiosk != nil {
			_ = dc.Send("kiosk_status", map[string]any{"clientId": id, "kiosk": kiosk})
		}
		if variant := h.store.GetVariantStatus(id); variant != nil {
			payload := make(map[string]any, len(variant)+1)
			for k, v := range variant {
				payload[k] = v
			}
			payload["clientId"] = id
			_ = dc.Send("variant_status", payload)
		}
	}
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
