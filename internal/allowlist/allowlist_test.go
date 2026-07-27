package allowlist

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/austinkregel/compute-agent/pkg/logging"
)

func testLog(t *testing.T) *logging.Logger {
	t.Helper()
	l, err := logging.New(logging.Options{Level: "error"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Sync() })
	return l
}

func TestSeedAndCommands(t *testing.T) {
	s := New("", []string{"git", "ls", " git ", ""}, testLog(t))
	cmds := s.Commands()
	if len(cmds) != 2 {
		t.Fatalf("commands = %v, want [git ls] (trimmed, deduped)", cmds)
	}
	for _, e := range s.Entries() {
		if e.Source != SourceConfig {
			t.Errorf("seed entry %q source = %q, want config", e.Cmd, e.Source)
		}
	}
}

func TestClassifyCrucible(t *testing.T) {
	s := New("", nil, testLog(t))
	s.Add([]string{"sha256sum /root/.rebase/bin/rebase-indexer"}, "")
	s.Add([]string{"git status"}, "")
	got := map[string]string{}
	for _, e := range s.Entries() {
		got[e.Cmd] = e.Source
	}
	if got["sha256sum /root/.rebase/bin/rebase-indexer"] != SourceCrucible {
		t.Errorf("indexer command not classified as crucible: %v", got)
	}
	if got["git status"] != SourceAdmin {
		t.Errorf("operator command should be admin: %v", got)
	}
}

func TestAddDedupeAndExplicitSource(t *testing.T) {
	s := New("", []string{"git"}, testLog(t))
	d := s.Add([]string{"git", "ls"}, SourceAdmin) // git already present
	if len(d.Added) != 1 || d.Added[0] != "ls" {
		t.Fatalf("added = %v, want [ls]", d.Added)
	}
	if len(s.Commands()) != 2 {
		t.Fatalf("commands = %v, want 2", s.Commands())
	}
}

func TestRemove(t *testing.T) {
	s := New("", []string{"git", "ls", "cat"}, testLog(t))
	d := s.Remove([]string{"LS", "missing"}) // case-insensitive, ignores missing
	if len(d.Removed) != 1 {
		t.Fatalf("removed = %v, want 1", d.Removed)
	}
	if len(s.Commands()) != 2 {
		t.Fatalf("commands = %v, want 2 remaining", s.Commands())
	}
}

func TestReplacePreservesProvenance(t *testing.T) {
	s := New("", []string{"git"}, testLog(t)) // git -> config
	s.Replace([]string{"git", "ls"})          // git kept (config), ls new (admin)
	srcByCmd := map[string]string{}
	for _, e := range s.Entries() {
		srcByCmd[e.Cmd] = e.Source
	}
	if srcByCmd["git"] != SourceConfig {
		t.Errorf("git source = %q, want config (preserved)", srcByCmd["git"])
	}
	if srcByCmd["ls"] != SourceAdmin {
		t.Errorf("ls source = %q, want admin (new)", srcByCmd["ls"])
	}
}

func TestCountAfterRemove(t *testing.T) {
	s := New("", []string{"git", "ls"}, testLog(t))
	if n := s.CountAfterRemove([]string{"git", "ls"}); n != 0 {
		t.Errorf("CountAfterRemove all = %d, want 0", n)
	}
	if n := s.CountAfterRemove([]string{"git"}); n != 1 {
		t.Errorf("CountAfterRemove one = %d, want 1", n)
	}
}

func TestPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exec-allowlist.json")

	s1 := New(path, []string{"git"}, testLog(t))
	s1.Add([]string{"curl -fSL https://x/rebase-indexer -o /root/.rebase/bin/rebase-indexer"}, "")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	// A fresh store at the same path must load persisted state, not reseed.
	s2 := New(path, []string{"completely", "different", "seed"}, testLog(t))
	cmds := s2.Commands()
	if len(cmds) != 2 {
		t.Fatalf("reloaded commands = %v, want 2 (git + indexer)", cmds)
	}
	var crucible bool
	for _, e := range s2.Entries() {
		if e.Source == SourceCrucible {
			crucible = true
		}
	}
	if !crucible {
		t.Error("crucible provenance lost across reload")
	}
}

func TestValidateCommand(t *testing.T) {
	ok := []string{"git", "curl -fSL https://github.com/x/y -o /tmp/z", "sha256sum /a/b"}
	for _, c := range ok {
		if !ValidateCommand(c) {
			t.Errorf("ValidateCommand(%q) = false, want true", c)
		}
	}
	bad := []string{"git; rm -rf /", "a | b", "a && b", "echo `id`", "x $HOME", "a\nb"}
	for _, c := range bad {
		if ValidateCommand(c) {
			t.Errorf("ValidateCommand(%q) = true, want false", c)
		}
	}
}
