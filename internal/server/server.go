package server

import (
	"context"
	cryptotls "crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"nhooyr.io/websocket"

	"github.com/austinkregel/backup-server/internal/allowlist"
	"github.com/austinkregel/backup-server/internal/api"
	"github.com/austinkregel/backup-server/internal/auth"
	servercli "github.com/austinkregel/backup-server/internal/cli"
	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/backup-server/internal/heartbeat"
	"github.com/austinkregel/backup-server/internal/relay"
	"github.com/austinkregel/backup-server/internal/spa"
	"github.com/austinkregel/backup-server/internal/state"
	tlspkg "github.com/austinkregel/backup-server/internal/tls"
	"github.com/austinkregel/backup-server/internal/ws"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// Server is the top-level coordinator for the backup server.
type Server struct {
	cfg   *config.Config
	log   *logging.Logger
	store *state.Store

	httpServer *http.Server
	agents     *ws.AgentHandler
	dashboard  *ws.DashboardHandler
	relayer    *relay.Relay
	oidc       *auth.OIDCProvider
	allowlist  *allowlist.Store

	// EnableCLI controls whether the interactive CLI runs. Defaults to true.
	EnableCLI bool
}

// New constructs a fully wired Server. It does not start listening.
func New(cfg *config.Config, log *logging.Logger) (*Server, error) {
	store := state.New()

	s := &Server{
		cfg:       cfg,
		log:       log,
		store:     store,
		EnableCLI: true,
	}

	// OIDC (optional)
	if cfg.OIDC.Enabled {
		oidcProvider, err := auth.NewOIDCProvider(context.Background(), cfg.OIDC, log)
		if err != nil {
			return nil, fmt.Errorf("oidc init: %w", err)
		}
		s.oidc = oidcProvider
	}

	// Canonical exec allowlist (persisted; seeded from config on first run).
	allowlistPath := os.Getenv("EXEC_ALLOWLIST_STATE_PATH")
	if allowlistPath == "" {
		allowlistPath = "exec-allowlist.json"
	}
	s.allowlist = allowlist.New(allowlistPath, cfg.ExecAllowedCommands, log)

	// Agent handler
	maxSkew := time.Duration(cfg.AgentAuthMaxSkewSec) * time.Second
	s.agents = ws.NewAgentHandler(store, log, cfg.AuthToken, maxSkew)

	// Dashboard handler
	s.dashboard = ws.NewDashboardHandler(store, log, s.oidc, cfg.DashboardAllowedOrigins)

	// Relay (with backup plan persistence directory)
	s.relayer = relay.New(store, log, s.dashboard, "backups")

	// Wire agent callbacks
	s.agents.OnConnect = func(clientID string) {
		s.dashboard.BroadcastClientList()
		// Push the canonical command allowlist to the freshly-connected agent.
		ws.SendSignedCommand(store, clientID, "exec_allowlist",
			map[string]any{"commands": s.allowlist.Commands()}, log)
	}
	s.agents.OnDisconnect = func(clientID string) {
		s.dashboard.BroadcastClientList()
	}
	s.agents.OnMetadataChanged = func(clientID string) {
		s.dashboard.BroadcastClientList()
	}

	s.agents.OnEvent = func(clientID string, msg *ws.Message) {
		// Stats updates are broadcast to dashboards
		if msg.Event == "stats" {
			stats := store.GetStats(clientID)
			s.dashboard.Broadcast("stats", map[string]any{
				"clientId": clientID,
				"data":     stats,
			})

			// Extract and cache OS alerts from stats if present
			if stats != nil {
				if alertsRaw, ok := stats["alerts"]; ok {
					if newAlerts, ok := alertsRaw.(map[string]any); ok {
						prevAlerts := store.GetAlerts(clientID)
						store.SetAlerts(clientID, newAlerts)

						// Broadcast if there are new or changed alerts
						prevCount, _ := prevAlerts["totalCount"].(float64)
						newCount, _ := newAlerts["totalCount"].(float64)
						prevCritical, _ := prevAlerts["hasCritical"].(bool)
						newCritical, _ := newAlerts["hasCritical"].(bool)

						hasNew := prevAlerts == nil ||
							newCount > prevCount ||
							(newCritical && !prevCritical)

						if hasNew {
							if alertList, ok := newAlerts["alerts"].([]any); ok && len(alertList) > 0 {
								s.dashboard.Broadcast("alerts", map[string]any{
									"clientId": clientID,
									"data":     newAlerts,
								})

								// Emit individual os_alert events for critical alerts
								if newCritical {
									count := 0
									for _, a := range alertList {
										if alert, ok := a.(map[string]any); ok {
											if sev, _ := alert["severity"].(string); sev == "critical" {
												s.dashboard.Broadcast("os_alert", map[string]any{
													"clientId": clientID,
													"alert":    alert,
												})
												count++
												if count >= 5 {
													break
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		s.relayer.HandleAgentEvent(clientID, msg)
	}

	// Wire dashboard callbacks
	s.dashboard.OnEvent = func(dc *ws.DashboardConn, msg *ws.Message) {
		s.relayer.HandleDashboardEvent(dc, msg)
	}
	s.dashboard.OnDisconnect = func(dc *ws.DashboardConn) {
		s.relayer.CleanupDashboard(dc)
	}

	// Build HTTP handler
	handler := s.buildHandler()

	s.httpServer = &http.Server{
		Handler: handler,
	}

	return s, nil
}

// Run starts the server and blocks until the context is cancelled.
// It handles TLS if certs are present, otherwise runs plain HTTP.
func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// TLS setup (optional — use certs if present, with auto-reload)
	certDir := "certs"
	var cancelTLSWatch func()
	if tlspkg.CertDirExists(certDir) {
		tlsCfg, cancel, err := tlspkg.NewTLSConfigWithWatcher(certDir, s.log)
		if err != nil {
			s.log.Warn("TLS cert load failed, falling back to plain HTTP", "error", err)
		} else {
			cancelTLSWatch = cancel
			ln = cryptotls.NewListener(ln, tlsCfg)
			s.log.Info("TLS enabled", "certDir", certDir)
		}
	} else {
		s.log.Info("no TLS certs found, running plain HTTP", "certDir", certDir)
	}

	// Start heartbeat
	hb := heartbeat.New(s.newHeartbeatStore(), heartbeat.Config{
		PingInterval: time.Duration(s.cfg.PingIntervalSec) * time.Second,
		PongTimeout:  time.Duration(s.cfg.PongTimeoutSec) * time.Second,
		OnEvict: func(clientID string) {
			s.log.Warn("client timed out; evicting", "clientId", clientID)
			entry := s.store.GetClient(clientID)
			if entry != nil {
				entry.Mu.Lock()
				conn := entry.Conn
				entry.Mu.Unlock()
				if conn != nil {
					conn.Close(websocket.StatusGoingAway, "pong timeout")
				}
			}
		},
	})
	hb.Start(ctx)

	// Start stale file op cleanup ticker (every 60s, removes ops idle > 5min)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.relayer.CleanupStaleFileOps()
			}
		}
	}()

	s.log.Info("server listening", "addr", addr)

	// Start CLI in background (if enabled and stdin is available)
	if s.EnableCLI {
		go s.runCLI(ctx)
	}

	// Run HTTP server
	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	s.log.Info("shutting down...")

	// Stop TLS cert watcher if running
	if cancelTLSWatch != nil {
		cancelTLSWatch()
	}

	// Graceful shutdown: stop accepting, drain with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		s.log.Warn("HTTP shutdown error", "error", err)
	}

	// Wait for serve goroutine to finish
	if err := <-errCh; err != nil {
		return err
	}

	s.log.Info("server stopped")
	return nil
}

// buildHandler creates the combined HTTP handler (API + WebSocket + SPA).
func (s *Server) buildHandler() http.Handler {
	mux := chi.NewMux()

	// API routes
	var authMW func(http.Handler) http.Handler
	if s.oidc != nil {
		authMW = s.oidc.RequireAuth
	}
	deps := api.Deps{
		Store:     s.store,
		Log:       s.log,
		Config:    s.cfg,
		Relay:     s.relayer,
		Allowlist: s.allowlist,
		StartTime: time.Now(),
	}
	if s.oidc != nil {
		deps.AuthStatusHandler = s.oidc.HandleAuthStatus
		deps.AdminMiddleware = s.oidc.RequireAdmin
		if s.cfg.OIDC.AdminGroup == "" {
			s.log.Warn("oidc.adminGroup is not set: admin endpoints (exec-allowlist, restart/shutdown) are NOT role-gated — any authenticated user can call them; set oidc.adminGroup to enforce")
		}
	}
	apiRouter := api.NewRouter(deps, authMW)

	// OIDC auth routes
	if s.oidc != nil {
		mux.Get("/auth/login", s.oidc.HandleLogin)
		mux.Get("/auth/app", s.oidc.HandleAppLogin)
		mux.Get("/auth/callback", s.oidc.HandleCallback)
		mux.Get("/auth/logout", s.oidc.HandleLogout)
		// Legacy /login redirect
		mux.Get("/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
		})
	}

	// Mount API
	mux.Mount("/", apiRouter)

	// WebSocket endpoints
	mux.Handle("/ws/agent", s.agents)
	mux.Handle("/ws/dashboard", s.dashboard)

	// SPA (only if dist directory exists)
	// Try multiple paths: "dist" (deployed layout), "../client/dist" (running from server/),
	// "client/dist" (running from repo root)
	distDir := "dist"
	if _, err := os.Stat(distDir); err != nil {
		distDir = "../client/dist"
	}
	if _, err := os.Stat(distDir); err != nil {
		distDir = "client/dist"
	}
	if _, err := os.Stat(distDir); err == nil {
		s.log.Info("SPA enabled", "distDir", distDir)
		spaHandler := spa.NewHandler(distDir)
		return &spaFallback{
			primary: mux,
			spa:     spaHandler,
			oidc:    s.oidc,
		}
	}

	s.log.Warn("SPA disabled: client dist directory not found", "tried", distDir)
	return mux
}

// spaFallback routes API/auth/WS requests to primary handler, everything else to SPA.
// When oidc is set, unauthenticated requests are redirected to /auth/login.
type spaFallback struct {
	primary http.Handler
	spa     http.Handler
	oidc    *auth.OIDCProvider
}

func (f *spaFallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if isAPIPaths(path) {
		f.primary.ServeHTTP(w, r)
		return
	}
	// Require OIDC auth for SPA routes — redirect unauthenticated users to login
	if f.oidc != nil && f.oidc.GetSessionUser(r) == nil {
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return
	}
	f.spa.ServeHTTP(w, r)
}

func isAPIPaths(path string) bool {
	prefixes := []string{"/api/", "/auth/", "/ws/", "/login", "/github-version-release"}
	for _, p := range prefixes {
		if len(path) >= len(p) && path[:len(p)] == p {
			return true
		}
	}
	return false
}

// runCLI starts the interactive CLI on stdin/stdout.
func (s *Server) runCLI(ctx context.Context) {
	c := servercli.NewCLI(s.newCLIStore(), os.Stdin, os.Stdout)
	c.Run(ctx)
}

// --- Adapter types ---

// heartbeatStoreAdapter adapts the state.Store for the heartbeat package.
type heartbeatStoreAdapter struct {
	store *state.Store
	log   *logging.Logger
}

func (s *Server) newHeartbeatStore() *heartbeatStoreAdapter {
	return &heartbeatStoreAdapter{store: s.store, log: s.log}
}

func (a *heartbeatStoreAdapter) AllPingableClients() []heartbeat.Client {
	entries := a.store.AllClients()
	out := make([]heartbeat.Client, len(entries))
	for i, e := range entries {
		out[i] = &heartbeatClientAdapter{entry: e, log: a.log}
	}
	return out
}

func (a *heartbeatStoreAdapter) EvictClient(clientID string) {
	a.store.RemoveClient(clientID)
}

// heartbeatClientAdapter adapts a state.ClientEntry for the heartbeat.Client interface.
type heartbeatClientAdapter struct {
	entry *state.ClientEntry
	log   *logging.Logger
}

func (c *heartbeatClientAdapter) ClientID() string {
	return c.entry.ClientID
}

func (c *heartbeatClientAdapter) LastPong() time.Time {
	c.entry.Mu.Lock()
	defer c.entry.Mu.Unlock()
	return c.entry.LastPong
}

func (c *heartbeatClientAdapter) Ping() error {
	c.entry.Mu.Lock()
	conn := c.entry.Conn
	c.entry.Mu.Unlock()

	if conn == nil {
		return fmt.Errorf("no connection")
	}

	msg, err := ws.Encode("ping", map[string]any{
		"ts": time.Now().UnixMilli(),
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, msg)
}

// cliStoreAdapter adapts server state for the CLI package.
type cliStoreAdapter struct {
	store *state.Store
	log   *logging.Logger
}

func (s *Server) newCLIStore() *cliStoreAdapter {
	return &cliStoreAdapter{store: s.store, log: s.log}
}

func (a *cliStoreAdapter) ListClients() []servercli.ClientInfo {
	entries := a.store.AllClients()
	out := make([]servercli.ClientInfo, len(entries))
	for i, e := range entries {
		e.Mu.Lock()
		out[i] = servercli.ClientInfo{
			ClientID: e.ClientID,
			Hostname: e.Hostname,
			Platform: e.Platform,
		}
		e.Mu.Unlock()
	}
	return out
}

func (a *cliStoreAdapter) GetClientStats(clientID string) map[string]any {
	return a.store.GetStats(clientID)
}

func (a *cliStoreAdapter) HasClient(clientID string) bool {
	return a.store.HasClient(clientID)
}

func (a *cliStoreAdapter) SendCommand(clientID, event string, payload any) bool {
	return ws.SendSignedCommand(a.store, clientID, event, payload, a.log)
}
