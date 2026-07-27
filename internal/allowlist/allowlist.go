// Package allowlist holds the canonical exec-command allowlist for the control
// plane. It is the single source of truth that gets pushed to every agent.
//
// The store is concurrency-safe (a long-lived agent OnConnect push can race a
// dashboard mutation) and persists changes to disk so operator/app edits survive
// a restart — previously the list lived only in the in-memory config and reverted
// on restart. Each entry carries provenance ("config", "admin", "crucible") so the
// admin UI can distinguish operator policy from app-managed auto-grants.
package allowlist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/austinkregel/compute-agent/pkg/logging"
)

// Source provenance values.
const (
	SourceConfig   = "config"   // seeded from the server config on first run
	SourceAdmin    = "admin"    // added by an operator via the admin UI/API
	SourceCrucible = "crucible" // auto-granted by the desktop app's indexer flow
)

// Entry is a single allowlist command plus where it came from.
type Entry struct {
	Cmd    string `json:"cmd"`
	Source string `json:"source"`
}

// Diff describes what a mutation changed, for audit logging.
type Diff struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Empty reports whether the diff changed nothing.
func (d Diff) Empty() bool { return len(d.Added) == 0 && len(d.Removed) == 0 }

// Store is the concurrency-safe, persisted allowlist.
type Store struct {
	mu      sync.RWMutex
	entries []Entry
	path    string
	log     *logging.Logger
}

// persisted is the on-disk schema.
type persisted struct {
	Entries []Entry `json:"entries"`
}

// New constructs a Store. If a persisted file exists at path it is loaded and
// becomes authoritative; otherwise the store is seeded from `seed` (typically
// config.ExecAllowedCommands) and persisted immediately so subsequent restarts
// read back the same state.
func New(path string, seed []string, log *logging.Logger) *Store {
	s := &Store{path: path, log: log}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var p persisted
			if err := json.Unmarshal(data, &p); err == nil {
				s.entries = sanitize(p.Entries)
				return s
			} else if log != nil {
				log.Warn("exec allowlist state unreadable; reseeding from config", "path", path, "error", err.Error())
			}
		}
	}
	s.entries = seedEntries(seed)
	if err := s.persistLocked(); err != nil && log != nil {
		log.Warn("exec allowlist initial persist failed", "path", path, "error", err.Error())
	}
	return s
}

// Commands returns just the command strings, in order — the wire format pushed
// to agents (which expect a flat []string).
func (s *Store) Commands() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.entries))
	for i, e := range s.entries {
		out[i] = e.Cmd
	}
	return out
}

// Entries returns a copy of the full entries with provenance, for the admin UI.
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out
}

// IsEmpty reports whether the list is empty (which means allow-all on agents).
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries) == 0
}

// Replace sets the entire list, preserving the provenance of entries that
// already existed and classifying new ones. Returns the diff vs the prior state.
func (s *Store) Replace(cmds []string) Diff {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := keySet(s.entries)
	prevSource := make(map[string]string, len(s.entries))
	for _, e := range s.entries {
		prevSource[strings.ToLower(e.Cmd)] = e.Source
	}

	next := make([]Entry, 0, len(cmds))
	seen := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		key := strings.ToLower(c)
		if seen[key] {
			continue
		}
		seen[key] = true
		src := prevSource[key]
		if src == "" {
			src = classifySource(c)
		}
		next = append(next, Entry{Cmd: c, Source: src})
	}
	s.entries = next
	s.persistLocked()
	return diff(prev, seen)
}

// Add appends commands not already present. `source` overrides the classified
// source when non-empty. Returns the entries actually added.
func (s *Store) Add(cmds []string, source string) Diff {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing := keySet(s.entries)
	var added []string
	for _, c := range cmds {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		key := strings.ToLower(c)
		if existing[key] {
			continue
		}
		existing[key] = true
		src := source
		if src == "" {
			src = classifySource(c)
		}
		s.entries = append(s.entries, Entry{Cmd: c, Source: src})
		added = append(added, c)
	}
	if len(added) > 0 {
		s.persistLocked()
	}
	return Diff{Added: added}
}

// Remove deletes the named commands (case-insensitive). Returns those removed.
func (s *Store) Remove(cmds []string) Diff {
	s.mu.Lock()
	defer s.mu.Unlock()

	drop := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		drop[strings.ToLower(strings.TrimSpace(c))] = true
	}
	var removed []string
	kept := s.entries[:0:0]
	for _, e := range s.entries {
		if drop[strings.ToLower(e.Cmd)] {
			removed = append(removed, e.Cmd)
			continue
		}
		kept = append(kept, e)
	}
	if len(removed) > 0 {
		s.entries = kept
		s.persistLocked()
	}
	return Diff{Removed: removed}
}

// CountAfterRemove returns how many entries would remain after removing cmds —
// lets the API guard against accidentally clearing the list (empty = allow-all).
func (s *Store) CountAfterRemove(cmds []string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drop := make(map[string]bool, len(cmds))
	for _, c := range cmds {
		drop[strings.ToLower(strings.TrimSpace(c))] = true
	}
	remaining := 0
	for _, e := range s.entries {
		if !drop[strings.ToLower(e.Cmd)] {
			remaining++
		}
	}
	return remaining
}

// persistLocked writes the current state to disk. Caller must hold the lock.
func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(persisted{Entries: s.entries}, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}
	// Write atomically via a temp file + rename so a crash mid-write can't
	// truncate the canonical policy.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		if s.log != nil {
			s.log.Warn("exec allowlist persist failed", "path", s.path, "error", err.Error())
		}
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		if s.log != nil {
			s.log.Warn("exec allowlist persist rename failed", "path", s.path, "error", err.Error())
		}
		return err
	}
	return nil
}

// --- helpers ---

// ValidateCommand rejects entries containing shell metacharacters the agent
// would refuse anyway, so the API can fail fast with a clear message.
func ValidateCommand(cmd string) bool {
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "||") {
		return false
	}
	for _, r := range cmd {
		switch r {
		case ';', '|', '`', '$', '\n', '\r':
			return false
		}
	}
	return true
}

// classifySource heuristically tags an entry. Every command Crucible auto-grants
// references the indexer binary name, so that substring is a reliable marker.
func classifySource(cmd string) string {
	if strings.Contains(strings.ToLower(cmd), "rebase-indexer") {
		return SourceCrucible
	}
	return SourceAdmin
}

func seedEntries(seed []string) []Entry {
	out := make([]Entry, 0, len(seed))
	seen := make(map[string]bool, len(seed))
	for _, c := range seed {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		key := strings.ToLower(c)
		if seen[key] {
			continue
		}
		seen[key] = true
		src := classifySource(c)
		if src == SourceAdmin {
			src = SourceConfig // seed defaults are config-provenance, not operator-added
		}
		out = append(out, Entry{Cmd: c, Source: src})
	}
	return out
}

// sanitize trims and de-dupes loaded entries and drops empties/invalid sources.
func sanitize(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, e := range in {
		cmd := strings.TrimSpace(e.Cmd)
		if cmd == "" {
			continue
		}
		key := strings.ToLower(cmd)
		if seen[key] {
			continue
		}
		seen[key] = true
		src := e.Source
		if src != SourceConfig && src != SourceAdmin && src != SourceCrucible {
			src = classifySource(cmd)
		}
		out = append(out, Entry{Cmd: cmd, Source: src})
	}
	return out
}

func keySet(entries []Entry) map[string]bool {
	m := make(map[string]bool, len(entries))
	for _, e := range entries {
		m[strings.ToLower(e.Cmd)] = true
	}
	return m
}

// diff computes added/removed command keys between a prior key set and the new
// key set. It reports lowercased keys, which is sufficient for audit logging.
func diff(prev map[string]bool, next map[string]bool) Diff {
	var d Diff
	for k := range next {
		if !prev[k] {
			d.Added = append(d.Added, k)
		}
	}
	for k := range prev {
		if !next[k] {
			d.Removed = append(d.Removed, k)
		}
	}
	return d
}
