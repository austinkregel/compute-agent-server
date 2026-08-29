package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tempLog(t *testing.T) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l, path
}

func TestEmit_ChainsRecords(t *testing.T) {
	l, path := tempLog(t)
	for i := 0; i < 5; i++ {
		l.Emit(Event{Type: TypeAdminAction, Actor: "u1", Action: "restart"})
	}

	events, err := Read(path, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("read %d events, want 5", len(events))
	}
	if events[0].Prev != "" {
		t.Error("first record should have an empty prev")
	}
	for i := 1; i < len(events); i++ {
		if events[i].Prev != events[i-1].Hash {
			t.Errorf("record %d prev does not match record %d hash", i, i-1)
		}
		if events[i].Seq != events[i-1].Seq+1 {
			t.Errorf("record %d seq = %d, want %d", i, events[i].Seq, events[i-1].Seq+1)
		}
	}
	if res := Verify(events); !res.Valid {
		t.Errorf("Verify on an untouched log = %+v, want valid", res)
	}
}

// Editing a record in place is detectable. Nothing local prevents someone with
// disk access from rewriting the file, only from doing it silently.
func TestVerify_DetectsEditedRecord(t *testing.T) {
	l, path := tempLog(t)
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "alice"})
	l.Emit(Event{Type: TypeAdminAction, Actor: "mallory", Action: "shutdown"})
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "bob"})

	events, _ := Read(path, 0)
	events[1].Actor = "alice" // pin the shutdown on someone else
	if res := Verify(events); res.Valid {
		t.Error("Verify accepted an edited record")
	} else if res.BrokenAt != events[1].Seq {
		t.Errorf("BrokenAt = %d, want %d", res.BrokenAt, events[1].Seq)
	}
}

// Deleting a record also breaks the chain.
func TestVerify_DetectsDeletedRecord(t *testing.T) {
	l, path := tempLog(t)
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "alice"})
	l.Emit(Event{Type: TypeAdminAction, Actor: "mallory", Action: "shell_start"})
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "bob"})

	events, _ := Read(path, 0)
	truncated := []Event{events[0], events[2]}
	if res := Verify(truncated); res.Valid {
		t.Error("Verify accepted a log with a deleted record")
	}
}

func TestVerify_DetectsTruncationOnDisk(t *testing.T) {
	l, path := tempLog(t)
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "alice"})
	l.Emit(Event{Type: TypeAdminAction, Actor: "mallory"})
	l.Emit(Event{Type: TypeLoginSuccess, Actor: "bob"})
	l.Close()

	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	// Drop the middle line and rewrite.
	if err := os.WriteFile(path, []byte(lines[0]+"\n"+lines[2]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	events, _ := Read(path, 0)
	if res := Verify(events); res.Valid {
		t.Error("Verify accepted an on-disk truncated log")
	}
}

// A new actor, or a familiar actor from an unfamiliar network, produces a
// first-seen record; a familiar actor from the same network does not.
func TestEmitAccess_FirstSeen(t *testing.T) {
	l, path := tempLog(t)

	l.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "alice", Remote: "10.0.1.5"})
	l.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "alice", Remote: "10.0.1.9"}) // same /24
	l.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "alice", Remote: "203.0.113.7"})
	l.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "mallory", Remote: "10.0.1.5"})

	events, _ := Read(path, 0)
	var firstSeen []Event
	for _, e := range events {
		if e.Type == TypeFirstSeen {
			firstSeen = append(firstSeen, e)
		}
	}
	if len(firstSeen) != 3 {
		t.Fatalf("first_seen count = %d, want 3 (alice@10.0.1, alice@203.0.113, mallory@10.0.1)", len(firstSeen))
	}
	if firstSeen[1].Actor != "alice" || !strings.HasPrefix(firstSeen[1].Remote, "203.0.113") {
		t.Errorf("second first_seen = %+v, want alice from the new network", firstSeen[1])
	}
	if res := Verify(events); !res.Valid {
		t.Errorf("chain broken by first_seen interleaving: %+v", res)
	}
}

// Restarting continues the existing chain and does not re-announce known
// actors as newly seen.
func TestOpen_ResumesChainAndSeenSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")

	l1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l1.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "alice", Remote: "10.0.1.5"})
	lastHash := l1.prev
	lastSeq := l1.seq
	l1.Close()

	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if l2.prev != lastHash {
		t.Errorf("chain head not recovered: got %q, want %q", l2.prev, lastHash)
	}
	if l2.seq != lastSeq {
		t.Errorf("seq not recovered: got %d, want %d", l2.seq, lastSeq)
	}

	l2.EmitAccess(Event{Type: TypeLoginSuccess, Actor: "alice", Remote: "10.0.1.5"})
	events, _ := Read(path, 0)
	for _, e := range events[1:] {
		if e.Type == TypeFirstSeen {
			t.Error("known actor re-announced as first_seen after restart")
		}
	}
	if res := Verify(events); !res.Valid {
		t.Errorf("chain broken across restart: %+v", res)
	}
}

func TestOpen_UsesRestrictiveMode(t *testing.T) {
	_, path := tempLog(t)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %o, want 600 (it names who was where and when)", perm)
	}
}

// A write failure does not propagate into the request path.
func TestEmit_SurvivesClosedFile(t *testing.T) {
	l, _ := tempLog(t)
	l.Close()
	got := l.Emit(Event{Type: TypeAdminAction, Actor: "u1"})
	if got.Hash == "" {
		t.Error("Emit did not produce a record after the file was closed")
	}
}

func TestRemoteIP_IgnoresForwardingHeaders(t *testing.T) {
	// Caller-controlled headers must not rewrite the recorded address.
	r := newRequest("203.0.113.9:5555", map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got := RemoteIP(r); got != "203.0.113.9" {
		t.Errorf("RemoteIP = %q, want the socket address 203.0.113.9", got)
	}
}

func TestReadLimit(t *testing.T) {
	l, path := tempLog(t)
	for i := 0; i < 10; i++ {
		l.Emit(Event{Type: TypeAdminAction, Actor: "u1"})
	}
	events, _ := Read(path, 3)
	if len(events) != 3 {
		t.Fatalf("limit 3 returned %d", len(events))
	}
	if events[len(events)-1].Seq != 10 {
		t.Errorf("limit should return the most recent records, got last seq %d", events[len(events)-1].Seq)
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	l, path := tempLog(t)
	l.Emit(Event{Type: TypeAdminAction, Actor: "u1", Detail: map[string]any{"k": "v"}})
	raw, _ := os.ReadFile(path)
	var e Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &e); err != nil {
		t.Fatalf("record is not valid JSON: %v", err)
	}
	if e.Detail["k"] != "v" {
		t.Errorf("detail lost in round trip: %+v", e.Detail)
	}
}

// An unauthenticated caller must not be able to drive unbounded appends: the
// same key inside the window is counted rather than written.
func TestEmitThrottled_SuppressesRepeats(t *testing.T) {
	l, path := tempLog(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return base }

	for i := 0; i < 500; i++ {
		l.EmitThrottled(Event{Type: TypeLoginFailure, Remote: "203.0.113.5"})
	}

	events, _ := Read(path, 0)
	if len(events) != 1 {
		t.Fatalf("wrote %d records for 500 identical attempts, want 1", len(events))
	}

	// After the window, the next admitted record reports what was dropped.
	l.now = func() time.Time { return base.Add(ThrottleWindow + time.Second) }
	l.EmitThrottled(Event{Type: TypeLoginFailure, Remote: "203.0.113.5"})

	events, _ = Read(path, 0)
	if len(events) != 2 {
		t.Fatalf("wrote %d records, want 2 after the window elapsed", len(events))
	}
	if got := events[1].Detail["suppressed"]; got != float64(499) {
		t.Errorf("suppressed = %v, want 499 — the log must say what it did not write", got)
	}
	if res := Verify(events); !res.Valid {
		t.Errorf("throttling broke the chain: %+v", res)
	}
}

// Varying the source address must not defeat the bound, or a distributed scan
// reintroduces the same amplification.
func TestEmitThrottled_BoundsDistinctKeys(t *testing.T) {
	l, path := tempLog(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return base }

	for i := 0; i < maxThrottleKeys*4; i++ {
		l.EmitThrottled(Event{
			Type:   TypeLoginFailure,
			Remote: fmt.Sprintf("10.%d.%d.1", i/256%256, i%256),
		})
	}

	events, _ := Read(path, 0)
	if len(events) > maxThrottleKeys+1 {
		t.Errorf("wrote %d records for %d distinct sources; want at most %d",
			len(events), maxThrottleKeys*4, maxThrottleKeys+1)
	}
}

// Distinct keys inside the window are each recorded once — throttling must not
// collapse different actors or networks into one another.
func TestEmitThrottled_DistinguishesKeys(t *testing.T) {
	l, path := tempLog(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return base }

	l.EmitThrottled(Event{Type: TypeLoginFailure, Remote: "203.0.113.5"})
	l.EmitThrottled(Event{Type: TypeLoginFailure, Remote: "198.51.100.5"})
	l.EmitThrottled(Event{Type: TypeAgentAuthFail, Remote: "203.0.113.5"})
	l.EmitThrottled(Event{Type: TypeLoginFailure, Remote: "203.0.113.9"}) // same /24 as the first

	events, _ := Read(path, 0)
	if len(events) != 3 {
		t.Fatalf("wrote %d records, want 3 (two networks + one other type)", len(events))
	}
}

// Authenticated and privileged emission must not be throttled.
func TestEmitAccess_NotThrottled(t *testing.T) {
	l, path := tempLog(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return base }

	for i := 0; i < 50; i++ {
		l.EmitAccess(Event{Type: TypePrivilegedEvent, Actor: "alice", Remote: "10.0.1.5", Action: "shell_start"})
	}

	events, _ := Read(path, 0)
	var privileged int
	for _, e := range events {
		if e.Type == TypePrivilegedEvent {
			privileged++
		}
	}
	if privileged != 50 {
		t.Errorf("recorded %d privileged events, want all 50", privileged)
	}
}
