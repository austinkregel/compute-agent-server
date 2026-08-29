// Package audit records security-relevant events to a tamper-evident,
// append-only log.
//
// Two properties define it:
//
//   - Emission happens at authorization choke points rather than at call sites.
//     Every privileged action passes through RequireAdmin, the relay's
//     dashboard-event gate, or an authentication handler, and each of those
//     emits. A privileged feature cannot be added without routing through one
//     of them.
//
//   - Records are hash-chained. Each entry carries the SHA-256 of the previous
//     entry, so deleting or editing history breaks the chain at a detectable
//     point. This does not prevent tampering, only silent tampering.
//
// The log is kept separate from the application log so that rotation and
// filtering of operational output cannot affect access records.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Event types. Kept as constants so the set stays greppable and stable.
const (
	TypeLoginSuccess    = "login_success"
	TypeLoginFailure    = "login_failure"
	TypeDashboardOpen   = "dashboard_connect"
	TypeDashboardClose  = "dashboard_disconnect"
	TypeAgentConnect    = "agent_connect"
	TypeAgentAuthFail   = "agent_auth_failure"
	TypeAdminAction     = "admin_action"
	TypeAdminDenied     = "admin_denied"
	TypePrivilegedEvent = "privileged_event"
	TypeEventDenied     = "event_denied"
	TypeAllowlistChange = "allowlist_change"

	// TypeFirstSeen marks the first time a given (subject, network, client)
	// combination appears: a new account, or a familiar account from an
	// unfamiliar network, gets its own record rather than blending into
	// routine logins.
	TypeFirstSeen = "first_seen"
)

// Outcome values.
const (
	OutcomeAllow = "allow"
	OutcomeDeny  = "deny"
)

// Event is a single audit record. Fields are flat and stable.
type Event struct {
	Seq       uint64         `json:"seq"`
	Time      string         `json:"time"`
	Type      string         `json:"type"`
	Outcome   string         `json:"outcome,omitempty"`
	Actor     string         `json:"actor,omitempty"`     // OIDC sub, agent client ID, or "system"
	ActorName string         `json:"actorName,omitempty"` // email or display name, when known
	Groups    []string       `json:"groups,omitempty"`
	Remote    string         `json:"remote,omitempty"` // IP only, no port
	UserAgent string         `json:"userAgent,omitempty"`
	ClientID  string         `json:"clientId,omitempty"` // the managed machine acted upon
	Action    string         `json:"action,omitempty"`   // event name, HTTP method+path, command
	Detail    map[string]any `json:"detail,omitempty"`

	// Prev is the hash of the preceding record; Hash covers this record with
	// Hash itself zeroed. Together they form the chain.
	Prev string `json:"prev"`
	Hash string `json:"hash"`
}

// Logger appends events to the audit file.
type Logger struct {
	mu   sync.Mutex
	f    *os.File
	path string
	seq  uint64
	prev string

	// seen tracks (actor, network, client) tuples already observed, so
	// TypeFirstSeen fires once per combination rather than on every request.
	seen map[string]bool

	// onEvent is invoked after a successful append, to surface records without
	// re-reading the file.
	onEvent func(Event)
}

// Open opens (creating if needed) the audit log at path and recovers the chain
// head from it. It returns an error rather than degrading to a no-op logger, so
// the caller decides what an unwritable audit trail means.
func Open(path string) (*Logger, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit: empty path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("audit: create dir: %w", err)
		}
	}

	l := &Logger{path: path, seen: make(map[string]bool)}

	// Recover chain state and the first-seen set by replaying the existing log.
	// Replaying rather than keeping a sidecar file leaves one source of truth,
	// and it is the hash-chained one.
	if existing, err := Read(path, 0); err == nil {
		for _, e := range existing {
			l.seq = e.Seq
			l.prev = e.Hash
			if key := seenKey(e.Actor, e.Remote, e.ClientID); key != "" {
				l.seen[key] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("audit: read existing log: %w", err)
	}

	// 0600: the trail names who was where and when.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open: %w", err)
	}
	l.f = f
	return l, nil
}

// SetOnEvent registers a callback invoked after each appended event.
func (l *Logger) SetOnEvent(fn func(Event)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onEvent = fn
}

// Close flushes and closes the underlying file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// Emit appends an event, filling in Seq, Time, Prev and Hash. It returns no
// error: a failed audit write must not fail a privileged action or the request
// path. A dropped record surfaces as a break in the chain.
func (l *Logger) Emit(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.emitLocked(e)
}

func (l *Logger) emitLocked(e Event) Event {
	l.seq++
	e.Seq = l.seq
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	e.Prev = l.prev
	e.Hash = hashEvent(e)
	l.prev = e.Hash

	if l.f != nil {
		if line, err := json.Marshal(e); err == nil {
			_, _ = l.f.Write(append(line, '\n'))
			_ = l.f.Sync()
		}
	}
	if l.onEvent != nil {
		l.onEvent(e)
	}
	return e
}

// EmitAccess records an access event, preceded by a TypeFirstSeen record when
// this (actor, network, client) combination has not been seen before. Callers
// on the authorization path use this rather than Emit.
func (l *Logger) EmitAccess(e Event) Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	if key := seenKey(e.Actor, e.Remote, e.ClientID); key != "" && !l.seen[key] {
		l.seen[key] = true
		l.emitLocked(Event{
			Type:      TypeFirstSeen,
			Actor:     e.Actor,
			ActorName: e.ActorName,
			Groups:    e.Groups,
			Remote:    e.Remote,
			UserAgent: e.UserAgent,
			ClientID:  e.ClientID,
			Detail:    map[string]any{"triggeredBy": e.Type},
		})
	}
	return l.emitLocked(e)
}

// hashEvent computes the chain hash over the record with Hash zeroed, encoding
// through the same json.Marshal used to write the line so verification and
// writing agree.
func hashEvent(e Event) string {
	e.Hash = ""
	b, err := json.Marshal(e)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// seenKey builds the first-seen identity. The remote address is reduced to a
// network prefix (/24 for IPv4, /64 for IPv6) so a dynamic-IP user does not
// trip the signal on every reconnect while a different network does.
func seenKey(actor, remote, clientID string) string {
	if actor == "" {
		return ""
	}
	return actor + "|" + networkPrefix(remote) + "|" + clientID
}

func networkPrefix(remote string) string {
	if remote == "" {
		return ""
	}
	ip := net.ParseIP(remote)
	if ip == nil {
		return remote
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// Read returns the events in the log, most recent last. A limit of 0 reads all.
func Read(path string, limit int) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Event
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			// Skip malformed lines rather than discarding everything after them.
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// VerifyResult reports the outcome of a chain check.
type VerifyResult struct {
	Valid    bool   `json:"valid"`
	Count    int    `json:"count"`
	BrokenAt uint64 `json:"brokenAt,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Verify walks the chain and reports the first point at which it breaks: a
// break means records were edited, removed, or reordered after the fact.
func Verify(events []Event) VerifyResult {
	prev := ""
	for i, e := range events {
		if e.Prev != prev {
			return VerifyResult{Count: len(events), BrokenAt: e.Seq,
				Reason: "previous-hash mismatch (a record was removed or altered)"}
		}
		if got := hashEvent(e); got != e.Hash {
			return VerifyResult{Count: len(events), BrokenAt: e.Seq,
				Reason: "record hash mismatch (this record was altered)"}
		}
		if i > 0 && e.Seq != events[i-1].Seq+1 {
			return VerifyResult{Count: len(events), BrokenAt: e.Seq,
				Reason: "sequence gap (a record was removed)"}
		}
		prev = e.Hash
	}
	return VerifyResult{Valid: true, Count: len(events)}
}

// RemoteIP extracts the client IP from a request, using the socket address
// rather than any forwarding header, which is caller-controlled.
func RemoteIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
