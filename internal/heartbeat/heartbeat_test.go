package heartbeat

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockClient implements the Client interface for testing.
type mockClient struct {
	id       string
	lastPong time.Time
	pinged   int
	evicted  bool
	mu       sync.Mutex
}

func (m *mockClient) ClientID() string {
	return m.id
}

func (m *mockClient) LastPong() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPong
}

func (m *mockClient) Ping() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pinged++
	return nil
}

// mockStore implements the ClientStore interface for testing.
type mockStore struct {
	mu      sync.Mutex
	clients []*mockClient
	evicted []string
}

func (s *mockStore) AllPingableClients() []Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Client, len(s.clients))
	for i, c := range s.clients {
		out[i] = c
	}
	return out
}

func (s *mockStore) EvictClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evicted = append(s.evicted, clientID)
}

func TestTicker_PingsActiveClients(t *testing.T) {
	client := &mockClient{id: "c1", lastPong: time.Now()}
	store := &mockStore{clients: []*mockClient{client}}

	tk := New(store, Config{
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  5 * time.Second, // long timeout so no eviction
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tk.Start(ctx)

	// Wait for a few ticks
	time.Sleep(200 * time.Millisecond)
	cancel()

	client.mu.Lock()
	pinged := client.pinged
	client.mu.Unlock()

	if pinged < 2 {
		t.Errorf("expected at least 2 pings, got %d", pinged)
	}
}

func TestTicker_EvictsTimedOutClients(t *testing.T) {
	// Client's last pong was far in the past
	client := &mockClient{
		id:       "stale-1",
		lastPong: time.Now().Add(-10 * time.Second),
	}
	store := &mockStore{clients: []*mockClient{client}}

	tk := New(store, Config{
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  100 * time.Millisecond, // very short timeout
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tk.Start(ctx)

	// Wait for at least one tick
	time.Sleep(150 * time.Millisecond)
	cancel()

	store.mu.Lock()
	evicted := store.evicted
	store.mu.Unlock()

	if len(evicted) == 0 {
		t.Fatal("expected stale client to be evicted")
	}
	if evicted[0] != "stale-1" {
		t.Errorf("expected evicted client 'stale-1', got %q", evicted[0])
	}

	// Stale clients should not be pinged
	client.mu.Lock()
	pinged := client.pinged
	client.mu.Unlock()

	if pinged > 0 {
		t.Errorf("expected 0 pings for evicted client, got %d", pinged)
	}
}

func TestTicker_DoesNotEvictFreshClients(t *testing.T) {
	client := &mockClient{
		id:       "fresh-1",
		lastPong: time.Now(),
	}
	store := &mockStore{clients: []*mockClient{client}}

	tk := New(store, Config{
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tk.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	cancel()

	store.mu.Lock()
	evicted := store.evicted
	store.mu.Unlock()

	if len(evicted) > 0 {
		t.Errorf("expected no evictions, got %v", evicted)
	}
}

func TestTicker_StopsOnContextCancel(t *testing.T) {
	store := &mockStore{clients: []*mockClient{}}

	tk := New(store, Config{
		PingInterval: 10 * time.Millisecond,
		PongTimeout:  5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	tk.Start(ctx)

	// Cancel immediately
	cancel()

	// Give goroutine time to stop
	time.Sleep(50 * time.Millisecond)

	// No way to directly check goroutine stopped, but at least verify no panic
}

func TestTicker_MultipleClients(t *testing.T) {
	fresh := &mockClient{id: "fresh", lastPong: time.Now()}
	stale := &mockClient{id: "stale", lastPong: time.Now().Add(-10 * time.Second)}
	store := &mockStore{clients: []*mockClient{fresh, stale}}

	tk := New(store, Config{
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tk.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	// Fresh should be pinged
	fresh.mu.Lock()
	freshPinged := fresh.pinged
	fresh.mu.Unlock()
	if freshPinged < 1 {
		t.Errorf("expected fresh client to be pinged, got %d pings", freshPinged)
	}

	// Stale should be evicted
	store.mu.Lock()
	evicted := store.evicted
	store.mu.Unlock()

	found := false
	for _, id := range evicted {
		if id == "stale" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected stale client to be evicted, evicted: %v", evicted)
	}
}

func TestTicker_OnEvict_Callback(t *testing.T) {
	stale := &mockClient{id: "bye-1", lastPong: time.Now().Add(-10 * time.Second)}
	store := &mockStore{clients: []*mockClient{stale}}

	var callbackCalled string
	var callbackMu sync.Mutex

	tk := New(store, Config{
		PingInterval: 50 * time.Millisecond,
		PongTimeout:  100 * time.Millisecond,
		OnEvict: func(clientID string) {
			callbackMu.Lock()
			callbackCalled = clientID
			callbackMu.Unlock()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tk.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	cancel()

	callbackMu.Lock()
	called := callbackCalled
	callbackMu.Unlock()

	if called != "bye-1" {
		t.Errorf("expected OnEvict callback with 'bye-1', got %q", called)
	}
}
