package database

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SMSStore {
	t.Helper()
	dir := t.TempDir()
	db, err := Open("sqlite://" + filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	key, err := LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	return NewSMSStore(db, key)
}

func TestSMSStore_UpsertThread_CreatesAndUpdates(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)

	id1, err := store.UpsertThread("phone-1", "+15551234", "hello", now, true)
	if err != nil {
		t.Fatalf("UpsertThread (create): %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero thread ID")
	}

	later := now.Add(time.Minute)
	id2, err := store.UpsertThread("phone-1", "+15551234", "second message", later, true)
	if err != nil {
		t.Fatalf("UpsertThread (update): %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same thread ID on update, got %d then %d", id1, id2)
	}

	threads, err := store.ListThreads("phone-1", 10)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].Snippet != "second message" {
		t.Errorf("snippet = %q, want %q (decryption round-trip)", threads[0].Snippet, "second message")
	}
	if threads[0].UnreadCount != 2 {
		t.Errorf("unreadCount = %d, want 2", threads[0].UnreadCount)
	}
}

func TestSMSStore_ThreadsAreScopedPerClient(t *testing.T) {
	store := newTestStore(t)
	now := time.Now()

	if _, err := store.UpsertThread("phone-1", "+15551234", "a", now, false); err != nil {
		t.Fatalf("UpsertThread phone-1: %v", err)
	}
	if _, err := store.UpsertThread("phone-2", "+15551234", "b", now, false); err != nil {
		t.Fatalf("UpsertThread phone-2: %v", err)
	}

	phone1Threads, err := store.ListThreads("phone-1", 10)
	if err != nil {
		t.Fatalf("ListThreads phone-1: %v", err)
	}
	if len(phone1Threads) != 1 {
		t.Fatalf("expected phone-1 to have exactly 1 thread (its own), got %d", len(phone1Threads))
	}
}

func TestSMSStore_InsertMessage_EncryptsAndDecrypts(t *testing.T) {
	store := newTestStore(t)
	threadID, err := store.UpsertThread("phone-1", "+15551234", "hi", time.Now(), false)
	if err != nil {
		t.Fatalf("UpsertThread: %v", err)
	}

	_, err = store.InsertMessage("phone-1", threadID, "remote-1", "+15551234", "in", "secret body text", "received", time.Now())
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	messages, err := store.ListMessages("phone-1", threadID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Body != "secret body text" {
		t.Errorf("body = %q, want %q", messages[0].Body, "secret body text")
	}
	if messages[0].Direction != "in" {
		t.Errorf("direction = %q, want in", messages[0].Direction)
	}
}

func TestSMSStore_InsertMessage_DedupesOnRemoteID(t *testing.T) {
	store := newTestStore(t)
	threadID, _ := store.UpsertThread("phone-1", "+15551234", "hi", time.Now(), false)

	id1, err := store.InsertMessage("phone-1", threadID, "dup-id", "+15551234", "out", "first attempt", "sent", time.Now())
	if err != nil {
		t.Fatalf("InsertMessage (first): %v", err)
	}
	id2, err := store.InsertMessage("phone-1", threadID, "dup-id", "+15551234", "out", "retried delivery", "sent", time.Now())
	if err != nil {
		t.Fatalf("InsertMessage (retry): %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected dedup to return the same message ID, got %d then %d", id1, id2)
	}

	messages, err := store.ListMessages("phone-1", threadID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected exactly 1 message after retried insert, got %d", len(messages))
	}
	if messages[0].Body != "first attempt" {
		t.Errorf("expected the original body to win, got %q", messages[0].Body)
	}
}

func TestSMSStore_UpdateMessageStatus(t *testing.T) {
	store := newTestStore(t)
	threadID, _ := store.UpsertThread("phone-1", "+15551234", "hi", time.Now(), false)
	_, err := store.InsertMessage("phone-1", threadID, "m1", "+15551234", "out", "hi", "queued", time.Now())
	if err != nil {
		t.Fatalf("InsertMessage: %v", err)
	}

	if err := store.UpdateMessageStatus("phone-1", "m1", "delivered"); err != nil {
		t.Fatalf("UpdateMessageStatus: %v", err)
	}

	messages, err := store.ListMessages("phone-1", threadID, 10)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if messages[0].Status != "delivered" {
		t.Errorf("status = %q, want delivered", messages[0].Status)
	}
}

func TestSMSStore_FindThreadIDByAddress(t *testing.T) {
	store := newTestStore(t)
	threadID, _ := store.UpsertThread("phone-1", "+15551234", "hi", time.Now(), false)

	got, ok := store.FindThreadIDByAddress("phone-1", "+15551234")
	if !ok || got != threadID {
		t.Errorf("FindThreadIDByAddress = (%d, %v), want (%d, true)", got, ok, threadID)
	}

	_, ok = store.FindThreadIDByAddress("phone-1", "+19998887777")
	if ok {
		t.Error("expected FindThreadIDByAddress to report false for an unknown address")
	}
}

func TestLoadOrCreateKey_PersistsAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")

	key1, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey (create): %v", err)
	}
	if len(key1) != keySize {
		t.Fatalf("key length = %d, want %d", len(key1), keySize)
	}

	key2, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKey (reload): %v", err)
	}
	if string(key1) != string(key2) {
		t.Error("expected the same key to be loaded on a second call")
	}
}

func TestSealOpen_RoundTrip(t *testing.T) {
	key, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}

	plaintext := []byte("a secret message body")
	ciphertext, err := seal(key, plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(ciphertext) == string(plaintext) {
		t.Error("ciphertext should not equal plaintext")
	}

	decrypted, err := open(key, ciphertext)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}
