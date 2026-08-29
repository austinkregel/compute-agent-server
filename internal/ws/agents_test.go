package ws

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/compute-agent/pkg/cmdsig"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

const testAuthToken = "test-secret-token-for-agents"

func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	l, err := logging.New(logging.Options{Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Sync() })
	return l
}

func agentAuthQuery(clientID string, token string) string {
	ts := time.Now().UnixMilli()
	payload := map[string]any{"clientId": clientID, "ts": ts}
	pj, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(pj)
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("clientId=%s&ts=%d&sig=%s", clientID, ts, sig)
}

func setupAgentServer(t *testing.T) (*httptest.Server, *state.Store, *AgentHandler) {
	t.Helper()
	store := state.New()
	log := testLogger(t)
	handler := NewAgentHandler(store, log, testAuthToken, 10*time.Minute)

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, store, handler
}

func dialAgent(t *testing.T, serverURL, clientID string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/agent?" + agentAuthQuery(clientID, testAuthToken)
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent WS: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func TestAgentHandler_SuccessfulConnection(t *testing.T) {
	server, store, handler := setupAgentServer(t)

	var connectCalled sync.WaitGroup
	connectCalled.Add(1)
	handler.OnConnect = func(clientID string) {
		if clientID != "node-1" {
			t.Errorf("OnConnect clientId = %q, want node-1", clientID)
		}
		connectCalled.Done()
	}

	conn := dialAgent(t, server.URL, "node-1")

	// Should receive hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello_ack: %v", err)
	}

	msg, err := Decode(raw)
	if err != nil {
		t.Fatalf("decode hello_ack: %v", err)
	}
	if msg.Event != "hello_ack" {
		t.Errorf("Event = %q, want hello_ack", msg.Event)
	}

	var ack map[string]string
	json.Unmarshal(msg.Data, &ack)
	if ack["sessionNonce"] == "" {
		t.Error("sessionNonce is empty in hello_ack")
	}

	connectCalled.Wait()

	// Client should be in state
	if !store.HasClient("node-1") {
		t.Error("client node-1 not in store after connect")
	}

	// Client entry should have signer
	entry := store.GetClient("node-1")
	entry.Mu.Lock()
	hasSigner := entry.Signer != nil
	hasNonce := entry.SessionNonce != ""
	entry.Mu.Unlock()
	if !hasSigner {
		t.Error("client entry has no signer")
	}
	if !hasNonce {
		t.Error("client entry has no session nonce")
	}
}

func TestAgentHandler_RejectsInvalidSignature(t *testing.T) {
	server, _, _ := setupAgentServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use wrong token for signature
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent?" + agentAuthQuery("node-1", "wrong-token")
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail with wrong token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAgentHandler_RejectsStaleTimestamp(t *testing.T) {
	store := state.New()
	log := testLogger(t)
	handler := NewAgentHandler(store, log, testAuthToken, 1*time.Second) // Very tight skew

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", handler)
	server := httptest.NewServer(mux)
	defer server.Close()

	// Create a handshake with an old timestamp
	ts := time.Now().Add(-10 * time.Second).UnixMilli()
	payload := map[string]any{"clientId": "node-1", "ts": ts}
	pj, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(testAuthToken))
	mac.Write(pj)
	sig := hex.EncodeToString(mac.Sum(nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := fmt.Sprintf("ws%s/ws/agent?clientId=node-1&ts=%d&sig=%s",
		strings.TrimPrefix(server.URL, "http"), ts, sig)
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail with stale timestamp")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAgentHandler_RejectsMissingParams(t *testing.T) {
	server, _, _ := setupAgentServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/agent"
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected dial to fail without params")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAgentHandler_PongUpdatesLastPong(t *testing.T) {
	server, store, _ := setupAgentServer(t)
	conn := dialAgent(t, server.URL, "node-1")

	// Read hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	// Record time before pong
	entry := store.GetClient("node-1")
	entry.Mu.Lock()
	before := entry.LastPong
	entry.Mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	// Send pong
	pong, _ := Encode("pong", map[string]any{"ts": time.Now().UnixMilli()})
	conn.Write(ctx, websocket.MessageText, pong)

	// Give time for processing
	time.Sleep(50 * time.Millisecond)

	entry.Mu.Lock()
	after := entry.LastPong
	entry.Mu.Unlock()

	if !after.After(before) {
		t.Error("LastPong not updated after pong message")
	}
}

func TestAgentHandler_StatsUpdateCache(t *testing.T) {
	server, store, _ := setupAgentServer(t)
	conn := dialAgent(t, server.URL, "node-1")

	// Read hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	// Send stats
	stats, _ := Encode("stats", map[string]any{
		"platform": "linux",
		"hostname": "testhost",
		"arch":     "amd64",
		"cpus":     float64(8),
	})
	conn.Write(ctx, websocket.MessageText, stats)

	// Give time for processing
	time.Sleep(100 * time.Millisecond)

	cached := store.GetStats("node-1")
	if cached == nil {
		t.Fatal("stats not cached after stats event")
	}
	if cached["platform"] != "linux" {
		t.Errorf("platform = %v", cached["platform"])
	}
}

func TestAgentHandler_EventsDispatched(t *testing.T) {
	server, _, handler := setupAgentServer(t)

	var received sync.WaitGroup
	received.Add(1)
	handler.OnEvent = func(clientID string, msg *Message) {
		if clientID == "node-1" && msg.Event == "shell_output" {
			received.Done()
		}
	}

	conn := dialAgent(t, server.URL, "node-1")

	// Read hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	// Send an event
	msg, _ := Encode("shell_output", map[string]any{"session": "s1", "data": "hello"})
	conn.Write(ctx, websocket.MessageText, msg)

	// Wait for dispatch
	done := make(chan struct{})
	go func() {
		received.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("event not dispatched within timeout")
	}
}

func TestAgentHandler_DisconnectCleansUp(t *testing.T) {
	server, store, handler := setupAgentServer(t)

	disconnected := make(chan string, 1)
	handler.OnDisconnect = func(clientID string) {
		disconnected <- clientID
	}

	conn := dialAgent(t, server.URL, "node-1")

	// Read hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn.Read(ctx)

	if !store.HasClient("node-1") {
		t.Fatal("client should be in store")
	}

	// Close the connection
	conn.Close(websocket.StatusNormalClosure, "bye")

	select {
	case id := <-disconnected:
		if id != "node-1" {
			t.Errorf("disconnected clientId = %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect not received")
	}

	// Client should be removed from state
	time.Sleep(50 * time.Millisecond)
	if store.HasClient("node-1") {
		t.Error("client should be removed after disconnect")
	}
}

func TestSendSignedCommand(t *testing.T) {
	server, store, _ := setupAgentServer(t)
	conn := dialAgent(t, server.URL, "node-1")

	// Read hello_ack
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, _ := conn.Read(ctx)
	msg, _ := Decode(raw)
	var ack map[string]string
	json.Unmarshal(msg.Data, &ack)
	nonce := ack["sessionNonce"]

	// Send a signed command from server to agent
	log := testLogger(t)
	ok := SendSignedCommand(store, "node-1", "shell_start", map[string]string{"session": "s1"}, log)
	if !ok {
		t.Fatal("SendSignedCommand() = false")
	}

	// Agent should receive the signed_command event
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read signed command: %v", err)
	}
	cmdMsg, _ := Decode(raw)
	if cmdMsg.Event != "signed_command" {
		t.Errorf("Event = %q, want signed_command", cmdMsg.Event)
	}

	// Verify the envelope is valid
	var envelope cmdsig.SignedEnvelope
	if err := json.Unmarshal(cmdMsg.Data, &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if envelope.Event != "shell_start" {
		t.Errorf("envelope.Event = %q", envelope.Event)
	}
	if envelope.Seq <= 0 {
		t.Errorf("envelope.Seq = %d", envelope.Seq)
	}

	// Verify the agent can validate the signature
	sessionKey := cmdsig.DeriveSessionKey(testAuthToken, nonce)
	verifier := cmdsig.NewVerifier(sessionKey)
	if err := verifier.Verify(&envelope); err != nil {
		t.Fatalf("agent-side verify failed: %v", err)
	}

	// Decode the payload the way the agent does. A valid signature over a
	// payload whose fields moved is still a broken command: json.Unmarshal
	// reports no error and leaves the struct zeroed.
	var decoded struct {
		Session string `json:"session"`
		Actor   string `json:"_actor"`
	}
	if err := json.Unmarshal(envelope.Payload, &decoded); err != nil {
		t.Fatalf("agent-side payload decode: %v", err)
	}
	if decoded.Session != "s1" {
		t.Errorf("agent decoded session = %q, want s1 (payload: %s)", decoded.Session, envelope.Payload)
	}
	if decoded.Actor != ActorSystem {
		t.Errorf("agent decoded _actor = %q, want %q", decoded.Actor, ActorSystem)
	}
}

// The acting principal is folded into the signed payload rather than added as
// a new envelope field, leaving the canonical signing string unchanged so an
// agent predating the field still verifies commands carrying it.
func TestWithActor_KeepsPayloadSignable(t *testing.T) {
	key := cmdsig.DeriveSessionKey("token", "nonce")
	signer := cmdsig.NewSigner(key)

	withActorPayload, _ := withActor(map[string]any{"command": "id"}, "alice")
	env, err := signer.Sign("exec_request", withActorPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdsig.NewVerifier(key).Verify(env); err != nil {
		t.Fatalf("signed command with actor failed to verify: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(env.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["_actor"] != "alice" {
		t.Errorf("_actor = %v, want alice", got["_actor"])
	}
	if got["command"] != "id" {
		t.Errorf("original payload field lost: %v", got)
	}
}

// Attribution is not dropped, whatever shape the payload has.
func TestWithActor_AlwaysAttributes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload any
		actor   string
		want    string
	}{
		{"map", map[string]any{"a": 1}, "alice", "alice"},
		{"nil payload", nil, "alice", "alice"},
		{"empty actor falls back to system", map[string]any{}, "", ActorSystem},
		{"typed string map", map[string]string{"session": "s1"}, "alice", "alice"},
		{"struct", struct {
			Session string `json:"session"`
		}{"s1"}, "alice", "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, attributed := withActor(tc.payload, tc.actor)
			if !attributed {
				t.Fatalf("withActor reported no attribution for %T", tc.payload)
			}
			got, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("withActor did not return an object: %T", v)
			}
			if got[actorPayloadKey] != tc.want {
				t.Errorf("actor = %v, want %v", got[actorPayloadKey], tc.want)
			}
		})
	}
}

// withActor must not mutate the caller's map: relay handlers build a payload
// once, and mutation would leak the actor across sends.
func TestWithActor_DoesNotMutateInput(t *testing.T) {
	original := map[string]any{"command": "id"}
	withActor(original, "alice") //nolint:errcheck // mutation is what's under test
	if _, leaked := original[actorPayloadKey]; leaked {
		t.Error("withActor mutated the caller's payload")
	}
}

// Relay handlers send session-scoped commands as map[string]string. Wrapping
// those under a "value" key produces a payload the agent unmarshals without
// error into a struct with every field zeroed, so shells never start and PTYs
// are never closed on dashboard disconnect.
func TestWithActor_PreservesTypedStringMaps(t *testing.T) {
	v, attributed := withActor(map[string]string{"session": "sess-1"}, "alice")
	if !attributed {
		t.Fatal("withActor dropped attribution for map[string]string")
	}
	got, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("withActor returned %T, want map[string]any", v)
	}
	if got["session"] != "sess-1" {
		t.Errorf("session = %v, want sess-1 (payload must stay flat, not nested under a wrapper key)", got["session"])
	}
	if got[actorPayloadKey] != "alice" {
		t.Errorf("_actor = %v, want alice", got[actorPayloadKey])
	}
	if _, wrapped := got["value"]; wrapped {
		t.Error("payload was nested under a \"value\" key; the agent decodes fields at the top level")
	}
}

// End to end: what the agent actually decodes has to match what the handler sent.
func TestWithActor_AgentSeesOriginalFields(t *testing.T) {
	for _, payload := range []any{
		map[string]string{"session": "sess-1"},
		map[string]any{"session": "sess-1"},
	} {
		attributed, _ := withActor(payload, "alice")
		raw, err := json.Marshal(attributed)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Session string `json:"session"`
			Actor   string `json:"_actor"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%T: %v", payload, err)
		}
		if decoded.Session != "sess-1" {
			t.Errorf("%T: session decoded as %q, want sess-1 (raw: %s)", payload, decoded.Session, raw)
		}
		if decoded.Actor != "alice" {
			t.Errorf("%T: _actor decoded as %q, want alice", payload, decoded.Actor)
		}
	}
}
