package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	log, err := logging.New(logging.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	return log
}

func minimalConfig() *config.Config {
	return &config.Config{
		Port:                8443,
		AuthToken:           "test-token",
		PingIntervalSec:     30,
		PongTimeoutSec:      90,
		AgentAuthMaxSkewSec: 600,
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
}

func TestNew_NoOIDC(t *testing.T) {
	cfg := minimalConfig()
	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.oidc != nil {
		t.Error("expected nil oidc when not enabled")
	}
	if srv.agents == nil {
		t.Error("expected non-nil agents handler")
	}
	if srv.dashboard == nil {
		t.Error("expected non-nil dashboard handler")
	}
	if srv.relayer == nil {
		t.Error("expected non-nil relay")
	}
}

func TestRun_ListensAndShutdown(t *testing.T) {
	cfg := minimalConfig()
	cfg.Port = 0 // let OS pick a port

	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv.EnableCLI = false

	ctx, cancel := context.WithCancel(context.Background())

	// Use a free port by finding one
	cfg.Port = findFreePort(t)

	// Recreate with the correct port
	srv, err = New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv.EnableCLI = false

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()

	// Wait a bit for the server to start
	time.Sleep(200 * time.Millisecond)

	// Verify server is listening by making an HTTP request
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/status", cfg.Port))
	if err != nil {
		cancel()
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Trigger graceful shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() returned error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown timed out")
	}
}

func TestRun_AgentWebSocket(t *testing.T) {
	cfg := minimalConfig()
	cfg.Port = findFreePort(t)

	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv.EnableCLI = false

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(200 * time.Millisecond)

	// Attempt agent WebSocket connection without auth - should fail
	_, _, err = websocket.Dial(ctx, fmt.Sprintf("ws://localhost:%d/ws/agent", cfg.Port), nil)
	if err == nil {
		t.Error("expected WebSocket dial to fail without auth")
	}
}

func TestBuildHandler_APIPaths(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/status", true},
		{"/api/client/foo/stats", true},
		{"/auth/login", true},
		{"/auth/callback", true},
		{"/ws/agent", true},
		{"/ws/dashboard", true},
		{"/", false},
		{"/client/foo", false},
		{"/some/page", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isAPIPaths(tt.path)
			if got != tt.expected {
				t.Errorf("isAPIPaths(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestHeartbeatAdapters(t *testing.T) {
	cfg := minimalConfig()
	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Add a client to state
	srv.store.AddClient("test-client", nil)

	// Test heartbeat store adapter
	hbStore := srv.newHeartbeatStore()
	clients := hbStore.AllPingableClients()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if clients[0].ClientID() != "test-client" {
		t.Errorf("expected clientId 'test-client', got %q", clients[0].ClientID())
	}

	// Test eviction
	hbStore.EvictClient("test-client")
	if srv.store.HasClient("test-client") {
		t.Error("expected client to be evicted")
	}
}

func TestCLIAdapters(t *testing.T) {
	cfg := minimalConfig()
	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Add a client with some stats
	srv.store.AddClient("cli-client", nil)
	srv.store.UpdateStats("cli-client", map[string]any{
		"hostname": "test-host",
		"cpu":      42.0,
	})

	cliStore := srv.newCLIStore()

	// ListClients
	clients := cliStore.ListClients()
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if clients[0].ClientID != "cli-client" {
		t.Errorf("expected 'cli-client', got %q", clients[0].ClientID)
	}

	// GetClientStats
	stats := cliStore.GetClientStats("cli-client")
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats["cpu"] != 42.0 {
		t.Errorf("expected cpu=42.0, got %v", stats["cpu"])
	}

	// HasClient
	if !cliStore.HasClient("cli-client") {
		t.Error("expected HasClient to return true")
	}
	if cliStore.HasClient("nonexistent") {
		t.Error("expected HasClient to return false for nonexistent")
	}
}

func TestRun_StatsRetrievalAPI(t *testing.T) {
	cfg := minimalConfig()
	cfg.Port = findFreePort(t)

	log := testLogger(t)

	srv, err := New(cfg, log)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	srv.EnableCLI = false

	// Pre-populate state
	srv.store.AddClient("agent-1", nil)
	srv.store.UpdateStats("agent-1", map[string]any{
		"hostname": "my-host",
		"cpu":      55.0,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Run(ctx)
	time.Sleep(200 * time.Millisecond)

	// GET /api/client/agent-1/stats
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/api/client/agent-1/stats", cfg.Port))
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["hostname"] != "my-host" {
		t.Errorf("expected hostname 'my-host', got %v", body["hostname"])
	}
}

func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
