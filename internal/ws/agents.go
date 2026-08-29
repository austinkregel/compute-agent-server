package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/audit"
	"github.com/austinkregel/backup-server/internal/auth"
	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/compute-agent/pkg/cmdsig"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// AgentHandler handles WebSocket connections from agents.
type AgentHandler struct {
	store     *state.Store
	log       *logging.Logger
	authToken string
	maxSkew   time.Duration

	// audit records agent handshake outcomes. Nil disables recording.
	audit *audit.Logger

	// OnConnect is called after an agent successfully connects and authenticates.
	// Receives the client ID. Used to broadcast client_list to dashboards.
	OnConnect func(clientID string)

	// OnDisconnect is called when an agent disconnects.
	OnDisconnect func(clientID string)

	// OnEvent is called for each agent event. The handler should dispatch to relay logic.
	OnEvent func(clientID string, msg *Message)

	// OnMetadataChanged is called when a client's metadata fields (hostname, platform, etc.)
	// change from a stats update. Used to re-broadcast the client list.
	OnMetadataChanged func(clientID string)
}

// NewAgentHandler creates a handler for the /ws/agent endpoint.
func NewAgentHandler(store *state.Store, log *logging.Logger, authToken string, maxSkew time.Duration) *AgentHandler {
	return &AgentHandler{
		store:     store,
		log:       log,
		authToken: authToken,
		maxSkew:   maxSkew,
	}
}

// SetAudit attaches the audit logger. Called during server wiring.
func (h *AgentHandler) SetAudit(a *audit.Logger) { h.audit = a }

// ServeHTTP upgrades the HTTP connection to WebSocket for agents.
func (h *AgentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse and validate HMAC auth from query params
	q := r.URL.Query()
	handshake, err := auth.ParseHandshakeParams(
		q.Get("clientId"),
		q.Get("ts"),
		q.Get("sig"),
	)
	if err != nil {
		h.log.Warn("agent auth: parse failed", "error", err, "remote", r.RemoteAddr)
		h.auditAgentAuthFailure(r, q.Get("clientId"), "handshake parse failed")
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	if err := auth.ValidateAgentHandshake(handshake, h.authToken, h.maxSkew); err != nil {
		h.log.Warn("agent auth: validation failed",
			"clientId", handshake.ClientID,
			"error", err,
			"remote", r.RemoteAddr,
		)
		// A failed handshake means the agent endpoint was reached without the
		// shared secret.
		h.auditAgentAuthFailure(r, handshake.ClientID, "signature validation failed")
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	clientID := handshake.ClientID

	// Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// Agent connections come from Go clients, not browsers — no origin check needed.
		InsecureSkipVerify: true,
	})
	if err != nil {
		h.log.Error("agent ws upgrade failed", "clientId", clientID, "error", err)
		return
	}
	// Chunked file transfers (file_get/file_put) send 256 KiB chunks (~341 KiB
	// base64); raise the read limit above coder/websocket's 32 KiB default to
	// the 1 MiB/frame protocol limit, matching the agent + direct listener.
	// Otherwise the first large frame trips "read limited" and drops the agent.
	conn.SetReadLimit(1 << 20)

	h.log.Info("agent connected", "clientId", clientID, "remote", r.RemoteAddr)
	if h.audit != nil {
		h.audit.EmitAccess(audit.Event{
			Type:     audit.TypeAgentConnect,
			Outcome:  audit.OutcomeAllow,
			Actor:    "agent:" + clientID,
			ClientID: clientID,
			Remote:   audit.RemoteIP(r),
		})
	}

	// Generate session nonce and derive session key
	nonce, err := cmdsig.GenerateSessionNonce()
	if err != nil {
		h.log.Error("generate session nonce failed", "clientId", clientID, "error", err)
		conn.Close(websocket.StatusInternalError, "internal error")
		return
	}

	sessionKey := cmdsig.DeriveSessionKey(h.authToken, nonce)
	signer := cmdsig.NewSigner(sessionKey)

	// Register client in state
	h.store.AddClient(clientID, conn)
	entry := h.store.GetClient(clientID)
	if entry != nil {
		entry.Mu.Lock()
		entry.SessionNonce = nonce
		entry.Signer = signer
		entry.Mu.Unlock()
	}

	// Send hello_ack with session nonce
	helloAck, _ := Encode("hello_ack", map[string]string{
		"sessionNonce": nonce,
	})
	if err := conn.Write(r.Context(), websocket.MessageText, helloAck); err != nil {
		h.log.Error("failed to send hello_ack", "clientId", clientID, "error", err)
		conn.Close(websocket.StatusInternalError, "hello_ack failed")
		h.store.RemoveClient(clientID)
		return
	}

	if h.OnConnect != nil {
		h.OnConnect(clientID)
	}

	// Start read loop
	h.readLoop(r.Context(), clientID, conn)

	// Cleanup on disconnect
	h.store.RemoveClient(clientID)
	h.log.Info("agent disconnected", "clientId", clientID)
	if h.OnDisconnect != nil {
		h.OnDisconnect(clientID)
	}
}

// readLoop reads messages from the agent WebSocket until the connection closes.
func (h *AgentHandler) readLoop(ctx context.Context, clientID string, conn *websocket.Conn) {
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			// Normal closure or context cancellation — not an error
			if websocket.CloseStatus(err) != -1 || ctx.Err() != nil {
				return
			}
			h.log.Warn("agent read error", "clientId", clientID, "error", err)
			return
		}

		msg, err := Decode(raw)
		if err != nil {
			h.log.Warn("agent message decode error", "clientId", clientID, "error", err)
			continue
		}

		// Handle pong internally: update last-pong time and measure round-trip.
		// The agent echoes our ping's `ts` (Unix ms), so RTT = now - echoed ts.
		if msg.Event == "pong" {
			now := time.Now()
			var pong struct {
				TS int64 `json:"ts"`
			}
			_ = json.Unmarshal(msg.Data, &pong)
			entry := h.store.GetClient(clientID)
			if entry != nil {
				entry.Mu.Lock()
				entry.LastPong = now
				// Guard against clock skew / missing ts: only record sane RTTs.
				if rtt := now.UnixMilli() - pong.TS; pong.TS > 0 && rtt >= 0 && rtt < 60_000 {
					entry.RttMs = rtt
				}
				entry.Mu.Unlock()
			}
			continue
		}

		// Handle agent-initiated ping (reply with pong + timestamp)
		if msg.Event == "ping" {
			pongMsg, err := Encode("pong", map[string]any{
				"ts": time.Now().UnixMilli(),
			})
			if err == nil {
				writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
				conn.Write(writeCtx, websocket.MessageText, pongMsg)
				writeCancel()
			}
			continue
		}

		// Handle stats internally (update cache)
		if msg.Event == "stats" {
			var data struct {
				Data json.RawMessage `json:"data"`
			}
			// Try to extract .data field; if not present, use raw data
			var stats map[string]any
			if err := json.Unmarshal(msg.Data, &data); err == nil && len(data.Data) > 0 {
				json.Unmarshal(data.Data, &stats)
			} else {
				json.Unmarshal(msg.Data, &stats)
			}
			if stats != nil {
				metadataChanged := h.store.UpdateStats(clientID, stats)
				if metadataChanged && h.OnMetadataChanged != nil {
					h.OnMetadataChanged(clientID)
				}
			}
		}

		// Dispatch all events (including stats) to the relay/event handler
		if h.OnEvent != nil {
			h.OnEvent(clientID, msg)
		}
	}
}

// SendSignedCommand sends a signed command to a connected agent.
// ActorSystem attributes a command to the control plane itself rather than to
// a person: allowlist pushes on agent connect, release-webhook fan-out, session
// cleanup on disconnect. It is a principal name, not a credential; no HTTP
// route accepts it.
const ActorSystem = "system:backup-server"

// actorPayloadKey carries the acting principal inside the signed payload.
//
// It rides in the payload rather than in a new envelope field because the
// payload is already covered by the signature (canonicalPayload sorts and
// re-encodes it), which keeps the canonical string byte-identical to the
// previous format. Agents predating this field ignore the unknown key instead
// of failing verification, so servers and agents upgrade in either order.
const actorPayloadKey = "_actor"

// SendSignedCommand signs and sends a command attributed to the control plane
// itself. Use SendSignedCommandAs for anything a person initiated.
func SendSignedCommand(store *state.Store, clientID string, event string, payload any, log *logging.Logger) bool {
	return SendSignedCommandAs(store, clientID, event, payload, ActorSystem, log)
}

// SendSignedCommandAs signs and sends a command attributed to actor. The actor
// is folded into the signed payload, so rewriting it invalidates the signature.
func SendSignedCommandAs(store *state.Store, clientID string, event string, payload any, actor string, log *logging.Logger) bool {
	payload = withActor(payload, actor)
	return sendSignedCommand(store, clientID, event, payload, log)
}

// withActor returns payload with the acting principal attached. A payload that
// is not a JSON object is wrapped so attribution is not dropped.
func withActor(payload any, actor string) any {
	if actor == "" {
		actor = ActorSystem
	}
	switch p := payload.(type) {
	case map[string]any:
		out := make(map[string]any, len(p)+1)
		for k, v := range p {
			out[k] = v
		}
		out[actorPayloadKey] = actor
		return out
	case nil:
		return map[string]any{actorPayloadKey: actor}
	default:
		return map[string]any{actorPayloadKey: actor, "value": p}
	}
}

func sendSignedCommand(store *state.Store, clientID string, event string, payload any, log *logging.Logger) bool {
	entry := store.GetClient(clientID)
	if entry == nil {
		log.Warn("send command: client not found", "clientId", clientID, "event", event)
		return false
	}

	entry.Mu.Lock()
	signer, ok := entry.Signer.(*cmdsig.Signer)
	conn := entry.Conn
	entry.Mu.Unlock()

	if !ok || signer == nil {
		log.Error("send command: no signer for client", "clientId", clientID, "event", event)
		return false
	}

	envelope, err := signer.Sign(event, payload)
	if err != nil {
		log.Error("sign command failed", "clientId", clientID, "event", event, "error", err)
		return false
	}

	// Wrap the signed envelope in our protocol message format
	envBytes, err := json.Marshal(envelope)
	if err != nil {
		log.Error("marshal envelope failed", "clientId", clientID, "event", event, "error", err)
		return false
	}

	msg, err := Encode("signed_command", json.RawMessage(envBytes))
	if err != nil {
		log.Error("encode command message failed", "clientId", clientID, "event", event, "error", err)
		return false
	}

	if err := conn.Write(context.Background(), websocket.MessageText, msg); err != nil {
		log.Warn("send command failed", "clientId", clientID, "event", event, "error", err)
		return false
	}

	log.Debug("sent signed command", "clientId", clientID, "event", event, "seq", envelope.Seq)
	return true
}

// auditAgentAuthFailure records a rejected agent handshake.
func (h *AgentHandler) auditAgentAuthFailure(r *http.Request, clientID, reason string) {
	if h.audit == nil {
		return
	}
	h.audit.Emit(audit.Event{
		Type:      audit.TypeAgentAuthFail,
		Outcome:   audit.OutcomeDeny,
		Actor:     "agent:" + clientID,
		ClientID:  clientID,
		Remote:    audit.RemoteIP(r),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"reason": reason},
	})
}
