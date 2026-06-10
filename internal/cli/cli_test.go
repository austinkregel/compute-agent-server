package cli

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockCLIStore satisfies the Store interface for CLI tests.
type mockCLIStore struct {
	mu      sync.Mutex
	clients []ClientInfo
	stats   map[string]map[string]any
}

func (m *mockCLIStore) ListClients() []ClientInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.clients
}

func (m *mockCLIStore) GetClientStats(clientID string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats[clientID]
}

func (m *mockCLIStore) HasClient(clientID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		if c.ClientID == clientID {
			return true
		}
	}
	return false
}

func (m *mockCLIStore) SendCommand(clientID, event string, payload any) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clients {
		if c.ClientID == clientID {
			return true
		}
	}
	return false
}

func TestParseCommand_Help(t *testing.T) {
	cmd, args := parseCommand("help")
	if cmd != "help" {
		t.Errorf("expected 'help', got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestParseCommand_WithArgs(t *testing.T) {
	cmd, args := parseCommand("stats client-1")
	if cmd != "stats" {
		t.Errorf("expected 'stats', got %q", cmd)
	}
	if len(args) != 1 || args[0] != "client-1" {
		t.Errorf("expected args ['client-1'], got %v", args)
	}
}

func TestParseCommand_WhitespaceHandling(t *testing.T) {
	cmd, args := parseCommand("  admin  c1  echo hello  ")
	if cmd != "admin" {
		t.Errorf("expected 'admin', got %q", cmd)
	}
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %v", args)
	}
}

func TestParseCommand_Empty(t *testing.T) {
	cmd, args := parseCommand("")
	if cmd != "" {
		t.Errorf("expected empty cmd, got %q", cmd)
	}
	if len(args) != 0 {
		t.Errorf("expected no args, got %v", args)
	}
}

func TestExecute_Help(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("help", nil)
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %d", action)
	}
	output := buf.String()
	if !strings.Contains(output, "list") {
		t.Errorf("help output should contain 'list', got: %s", output)
	}
	if !strings.Contains(output, "stats") {
		t.Errorf("help output should contain 'stats', got: %s", output)
	}
	if !strings.Contains(output, "quit") {
		t.Errorf("help output should contain 'quit', got: %s", output)
	}
}

func TestExecute_List(t *testing.T) {
	store := &mockCLIStore{
		clients: []ClientInfo{
			{ClientID: "alpha", Hostname: "host-a", Platform: "linux"},
			{ClientID: "beta", Hostname: "host-b", Platform: "darwin"},
		},
	}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("list", nil)
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %d", action)
	}
	output := buf.String()
	if !strings.Contains(output, "alpha") {
		t.Errorf("list output should contain 'alpha', got: %s", output)
	}
	if !strings.Contains(output, "beta") {
		t.Errorf("list output should contain 'beta', got: %s", output)
	}
}

func TestExecute_ListEmpty(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("list", nil)
	output := buf.String()
	if !strings.Contains(output, "No clients") {
		t.Errorf("expected 'No clients' message, got: %s", output)
	}
}

func TestExecute_Stats(t *testing.T) {
	store := &mockCLIStore{
		clients: []ClientInfo{{ClientID: "c1"}},
		stats: map[string]map[string]any{
			"c1": {"cpu": 42.5, "hostname": "host-1"},
		},
	}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("stats", []string{"c1"})
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %d", action)
	}
	output := buf.String()
	if !strings.Contains(output, "cpu") {
		t.Errorf("stats output should contain 'cpu', got: %s", output)
	}
}

func TestExecute_StatsNoArg(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("stats", nil)
	output := buf.String()
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected usage message, got: %s", output)
	}
}

func TestExecute_StatsNoData(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("stats", []string{"unknown"})
	output := buf.String()
	if !strings.Contains(output, "No stats") {
		t.Errorf("expected 'No stats' message, got: %s", output)
	}
}

func TestExecute_Quit(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("quit", nil)
	if action != ActionQuit {
		t.Errorf("expected ActionQuit, got %d", action)
	}
}

func TestExecute_Exit(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("exit", nil)
	if action != ActionQuit {
		t.Errorf("expected ActionQuit, got %d", action)
	}
}

func TestExecute_Unknown(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	action := c.Execute("foobar", nil)
	if action != ActionContinue {
		t.Errorf("expected ActionContinue, got %d", action)
	}
	output := buf.String()
	if !strings.Contains(output, "Unknown command") {
		t.Errorf("expected unknown command message, got: %s", output)
	}
}

func TestExecute_Admin(t *testing.T) {
	store := &mockCLIStore{
		clients: []ClientInfo{{ClientID: "c1"}},
	}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("admin", []string{"c1", "echo", "hello"})
	output := buf.String()
	if !strings.Contains(output, "sent") && !strings.Contains(output, "Sent") {
		t.Errorf("expected confirmation of send, got: %s", output)
	}
}

func TestExecute_AdminMissingArgs(t *testing.T) {
	store := &mockCLIStore{}
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("admin", nil)
	output := buf.String()
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected usage message, got: %s", output)
	}
}

func TestExecute_AdminClientOffline(t *testing.T) {
	store := &mockCLIStore{} // no clients
	var buf bytes.Buffer
	c := &CLI{store: store, out: &buf}

	c.Execute("admin", []string{"offline-1", "uptime"})
	output := buf.String()
	if !strings.Contains(output, "offline") {
		t.Errorf("expected offline message, got: %s", output)
	}
}

func TestRunLoop_QuitExits(t *testing.T) {
	store := &mockCLIStore{}
	input := strings.NewReader("quit\n")
	var buf bytes.Buffer

	c := NewCLI(store, input, &buf)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.Run(ctx)
	// Should exit cleanly without timeout
}

func TestRunLoop_EmptyLinesContinue(t *testing.T) {
	store := &mockCLIStore{}
	input := strings.NewReader("\n\n\nquit\n")
	var buf bytes.Buffer

	c := NewCLI(store, input, &buf)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c.Run(ctx)
	// Should skip empty lines and quit normally
}

func TestRunLoop_ContextCancel(t *testing.T) {
	store := &mockCLIStore{}
	// Provide a reader that returns EOF immediately
	r := strings.NewReader("")
	var buf bytes.Buffer

	c := NewCLI(store, r, &buf)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c.Run(ctx)
	// Should exit due to context timeout
}
