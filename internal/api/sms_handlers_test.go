package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/austinkregel/backup-server/internal/database"
	"github.com/austinkregel/backup-server/internal/relay"
)

func testDepsWithSMS(t *testing.T) Deps {
	t.Helper()
	deps := testDeps(t)

	dir := t.TempDir()
	db, err := database.Open("sqlite://" + filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	key, err := database.LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	deps.SMS = database.NewSMSStore(db, key)
	deps.Relay = relay.New(deps.Store, deps.Log, noopDash{}, t.TempDir())
	return deps
}

type noopDash struct{}

func (noopDash) Broadcast(event string, data any)                  {}
func (noopDash) SendTo(connID string, event string, data any) bool { return false }

func TestHandleSMSThreads_NoStoreConfigured(t *testing.T) {
	deps := testDeps(t) // no SMS store
	deps.Relay = relay.New(deps.Store, deps.Log, noopDash{}, t.TempDir())
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/phone-1/sms/threads", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestHandleSMSThreads_ReadsFromStore(t *testing.T) {
	deps := testDepsWithSMS(t)
	if _, err := deps.SMS.UpsertThread("phone-1", "+15551234", "hello", time.Now(), false); err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/phone-1/sms/threads", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	result := decodeJSON(t, w)
	threads, ok := result["threads"].([]any)
	if !ok || len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %+v", result)
	}
}

func TestHandleSMSMessages_ReadsFromStore(t *testing.T) {
	deps := testDepsWithSMS(t)
	threadID, err := deps.SMS.UpsertThread("phone-1", "+15551234", "hello", time.Now(), false)
	if err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	if _, err := deps.SMS.InsertMessage("phone-1", threadID, "m1", "+15551234", "in", "hello there", "received", time.Now()); err != nil {
		t.Fatalf("seed message: %v", err)
	}
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", fmt.Sprintf("/api/client/phone-1/sms/threads/%d/messages", threadID), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	result := decodeJSON(t, w)
	messages, ok := result["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected 1 message, got %+v", result)
	}
}

func TestHandleSMSSend_ClientOffline(t *testing.T) {
	deps := testDepsWithSMS(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/client/phone-1/sms/send", `{"to":"+15551234","body":"hi"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (client offline), body=%s", w.Code, w.Body.String())
	}
}

func TestHandleSMSSend_InvalidBody(t *testing.T) {
	deps := testDepsWithSMS(t)
	deps.Store.AddClient("phone-1", nil)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/client/phone-1/sms/send", `{"to":""}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// persistSentSMS is exercised directly rather than through the full HTTP ->
// relay -> generic-pending round trip: the handler generates its wait token
// internally, so a test can't intercept and resolve it without either an
// invasive testability hook or a fragile timing hack. Unit-testing the
// extracted persistence step covers the actually-interesting logic (upsert
// thread, insert message, response shape) without either problem.
func TestPersistSentSMS(t *testing.T) {
	deps := testDepsWithSMS(t)

	resp := persistSentSMS(deps, "phone-1", "+15551234", "hello", "sent", "msg-1")
	if resp["to"] != "+15551234" || resp["status"] != "sent" || resp["messageId"] != "msg-1" {
		t.Errorf("unexpected response: %+v", resp)
	}
	threadID, ok := resp["threadId"]
	if !ok {
		t.Fatal("expected threadId in response")
	}

	messages, err := deps.SMS.ListMessages("phone-1", threadID.(uint), 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Direction != "out" || messages[0].Body != "hello" {
		t.Fatalf("unexpected persisted message: %+v", messages)
	}
}

func TestPersistSentSMS_NoStoreConfigured(t *testing.T) {
	deps := testDeps(t) // deps.SMS stays nil

	resp := persistSentSMS(deps, "phone-1", "+15551234", "hello", "sent", "msg-1")
	if resp["to"] != "+15551234" || resp["status"] != "sent" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if _, hasThreadID := resp["threadId"]; hasThreadID {
		t.Error("should not include threadId when no SMS store is configured")
	}
}
