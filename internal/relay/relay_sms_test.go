package relay

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/austinkregel/backup-server/internal/database"
)

func testSMSStore(t *testing.T) *database.SMSStore {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open("sqlite://" + filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	key, err := database.LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	return database.NewSMSStore(db, key)
}

func TestHandleAgentEvent_SMSReceived_PersistsAndBroadcasts(t *testing.T) {
	r, md, _ := testRelay(t)
	store := testSMSStore(t)
	r.SetSMSStore(store)

	msg := makeMsg("sms_received", map[string]any{
		"address": "+15551234",
		"body":    "hello there",
		"ts":      float64(time.Now().UnixMilli()),
	})
	r.HandleAgentEvent("phone-1", msg)

	b := md.findBroadcast("sms_received")
	if b == nil {
		t.Fatal("expected sms_received to be broadcast")
	}
	if b.Data["clientId"] != "phone-1" {
		t.Errorf("clientId = %v, want phone-1", b.Data["clientId"])
	}

	threads, err := store.ListThreads("phone-1", 10)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 persisted thread, got %d", len(threads))
	}
	if threads[0].Snippet != "hello there" {
		t.Errorf("snippet = %q, want %q", threads[0].Snippet, "hello there")
	}
	if threads[0].UnreadCount != 1 {
		t.Errorf("unreadCount = %d, want 1", threads[0].UnreadCount)
	}

	messages, err := store.ListMessages("phone-1", threads[0].ThreadID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Direction != "in" {
		t.Fatalf("unexpected messages: %+v", messages)
	}
}

func TestHandleAgentEvent_SMSReceived_DedupesRedelivery(t *testing.T) {
	r, _, _ := testRelay(t)
	store := testSMSStore(t)
	r.SetSMSStore(store)

	ts := float64(time.Now().UnixMilli())
	msg := makeMsg("sms_received", map[string]any{"address": "+15551234", "body": "hi", "ts": ts})
	r.HandleAgentEvent("phone-1", msg)
	r.HandleAgentEvent("phone-1", msg) // simulate a redelivered push (same content+ts)

	threads, err := store.ListThreads("phone-1", 10)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	messages, err := store.ListMessages("phone-1", threads[0].ThreadID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("expected redelivery to dedupe to 1 message, got %d", len(messages))
	}
}

func TestHandleAgentEvent_SMSReceived_NoStoreStillBroadcasts(t *testing.T) {
	r, md, _ := testRelay(t)
	// No SetSMSStore call — r.sms stays nil; persistence must be skipped, not panic.

	msg := makeMsg("sms_received", map[string]any{
		"address": "+15551234", "body": "hi", "ts": float64(time.Now().UnixMilli()),
	})
	r.HandleAgentEvent("phone-1", msg)

	if md.findBroadcast("sms_received") == nil {
		t.Error("expected sms_received to still broadcast even with no SMS store configured")
	}
}

func TestHandleAgentEvent_SMSSendResult_ResolvesPendingAndBroadcasts(t *testing.T) {
	r, md, _ := testRelay(t)

	token := "tok-1"
	ch := r.RegisterGenericPending(token)
	defer r.UnregisterGenericPending(token)

	msg := makeMsg("sms_send_result", map[string]any{"token": token, "to": "+15551234", "status": "sent"})
	r.HandleAgentEvent("phone-1", msg)

	select {
	case result := <-ch:
		if result["status"] != "sent" {
			t.Errorf("unexpected pending result: %+v", result)
		}
	default:
		t.Fatal("expected pending channel to be resolved")
	}

	if md.findBroadcast("sms_send_result") == nil {
		t.Error("expected sms_send_result to also be broadcast")
	}
}
