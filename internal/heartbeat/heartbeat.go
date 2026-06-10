package heartbeat

import (
	"context"
	"time"
)

// Client is the interface that each connected agent must satisfy for heartbeat.
type Client interface {
	ClientID() string
	LastPong() time.Time
	Ping() error
}

// ClientStore provides access to all connected clients and eviction.
type ClientStore interface {
	AllPingableClients() []Client
	EvictClient(clientID string)
}

// Config controls heartbeat behavior.
type Config struct {
	PingInterval time.Duration
	PongTimeout  time.Duration

	// OnEvict is an optional callback fired when a client is evicted due to pong timeout.
	OnEvict func(clientID string)
}

// Ticker periodically pings connected agents and evicts those that haven't responded.
type Ticker struct {
	store  ClientStore
	config Config
}

// New creates a heartbeat Ticker.
func New(store ClientStore, cfg Config) *Ticker {
	return &Ticker{
		store:  store,
		config: cfg,
	}
}

// Start begins the heartbeat loop in a goroutine. It stops when ctx is cancelled.
func (t *Ticker) Start(ctx context.Context) {
	go t.loop(ctx)
}

func (t *Ticker) loop(ctx context.Context) {
	ticker := time.NewTicker(t.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tick()
		}
	}
}

func (t *Ticker) tick() {
	now := time.Now()
	clients := t.store.AllPingableClients()

	for _, c := range clients {
		if now.Sub(c.LastPong()) > t.config.PongTimeout {
			t.store.EvictClient(c.ClientID())
			if t.config.OnEvict != nil {
				t.config.OnEvict(c.ClientID())
			}
			continue
		}
		// Ping active clients (ignore errors — the next tick will evict if no pong)
		_ = c.Ping()
	}
}
