package state

import (
	"fmt"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// StatsHistoryLimit is the max number of stats samples retained per client.
const StatsHistoryLimit = 25

// ClientEntry represents a connected agent in the server's state.
type ClientEntry struct {
	Mu sync.Mutex

	ClientID      string
	Conn          *websocket.Conn // nil in tests
	LastPong      time.Time
	Authenticated bool

	// Populated from stats updates
	Platform     string
	Release      string
	Hostname     string
	Arch         string
	CPUs         string
	AgentVersion string

	// Command signing — set during handshake
	SessionNonce string
	// Signer is kept as an opaque interface to avoid import cycles.
	// The ws package will type-assert as needed.
	Signer any
}

// PublicClient is the JSON-safe projection of a ClientEntry.
// LastPong is Unix milliseconds (matching the Node.js server's Date.now() format).
type PublicClient struct {
	ClientID      string `json:"clientId"`
	LastPong      int64  `json:"lastPong"`
	Authenticated bool   `json:"authenticated"`
	Platform      string `json:"platform,omitempty"`
	Release       string `json:"release,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	Arch          string `json:"arch,omitempty"`
	CPUs          string `json:"cpus,omitempty"`
	AgentVersion  string `json:"agentVersion,omitempty"`
}

// ShellSession tracks an active PTY relay.
type ShellSession struct {
	ClientID  string
	DashConn  *websocket.Conn // dashboard socket that initiated this session
	CreatedAt time.Time
}

// LogTailSession tracks an active log tail relay.
type LogTailSession struct {
	ClientID  string
	DashConn  *websocket.Conn
	CreatedAt time.Time
}

// BackupJob tracks a backup plan/execution.
type BackupJob struct {
	ClientID       string         `json:"clientId"`
	PlanID         string         `json:"planId"`
	Job            map[string]any `json:"job,omitempty"`
	Plan           map[string]any `json:"plan,omitempty"`
	Status         string         `json:"status"` // planning, planned, running, completed, failed
	FilesCompleted int            `json:"filesCompleted"`
	CompletedAt    string         `json:"completedAt,omitempty"`
	DurationMs     int64          `json:"durationMs,omitempty"`
	TransferBytes  int64          `json:"transferredBytes,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// PendingFileOp tracks an in-flight file operation.
type PendingFileOp struct {
	ClientID   string
	DashConn   *websocket.Conn
	Type       string // put, delete, chmod
	StartedAt  time.Time
	LastSeenAt time.Time
}

// StatsEntry is a timestamped stats sample.
type StatsEntry struct {
	Stats     map[string]any `json:"stats"`
	UpdatedAt string         `json:"updatedAt"`
}

// Store is the central in-memory state for the server.
type Store struct {
	mu sync.RWMutex

	clients       map[string]*ClientEntry
	statsCache    map[string]map[string]any
	statsHistory  map[string][]StatsEntry
	alertsCache   map[string]map[string]any
	kioskStatus   map[string]map[string]any
	variantStatus map[string]map[string]any

	shellMu       sync.RWMutex
	shellSessions map[string]*ShellSession

	logTailMu       sync.RWMutex
	logTailSessions map[string]*LogTailSession

	backupMu   sync.RWMutex
	backupJobs map[string]*BackupJob

	fileOpMu       sync.RWMutex
	pendingFileOps map[string]*PendingFileOp
}

// New creates an empty Store.
func New() *Store {
	return &Store{
		clients:         make(map[string]*ClientEntry),
		statsCache:      make(map[string]map[string]any),
		statsHistory:    make(map[string][]StatsEntry),
		alertsCache:     make(map[string]map[string]any),
		kioskStatus:     make(map[string]map[string]any),
		variantStatus:   make(map[string]map[string]any),
		shellSessions:   make(map[string]*ShellSession),
		logTailSessions: make(map[string]*LogTailSession),
		backupJobs:      make(map[string]*BackupJob),
		pendingFileOps:  make(map[string]*PendingFileOp),
	}
}

// --- Client management ---

// AddClient registers a new agent connection.
func (s *Store) AddClient(clientID string, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[clientID] = &ClientEntry{
		ClientID:      clientID,
		Conn:          conn,
		LastPong:      time.Now(),
		Authenticated: true,
	}
}

// RemoveClient removes an agent and cleans up associated state.
func (s *Store) RemoveClient(clientID string) {
	s.mu.Lock()
	delete(s.clients, clientID)
	s.mu.Unlock()
}

// HasClient returns true if the client is connected.
func (s *Store) HasClient(clientID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.clients[clientID]
	return ok
}

// GetClient returns the entry for a client. The caller must lock entry.mu for writes.
func (s *Store) GetClient(clientID string) *ClientEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[clientID]
}

// ClientIDs returns the list of connected client IDs.
func (s *Store) ClientIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	return ids
}

// ClientCount returns the number of connected clients.
func (s *Store) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// PublicClients returns a safe projection of all connected clients.
func (s *Store) PublicClients() []PublicClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicClient, 0, len(s.clients))
	for _, e := range s.clients {
		e.Mu.Lock()
		pub := PublicClient{
			ClientID:      e.ClientID,
			LastPong:      e.LastPong.UnixMilli(),
			Authenticated: e.Authenticated,
			Platform:      e.Platform,
			Release:       e.Release,
			Hostname:      e.Hostname,
			Arch:          e.Arch,
			CPUs:          e.CPUs,
			AgentVersion:  e.AgentVersion,
		}
		e.Mu.Unlock()
		out = append(out, pub)
	}
	return out
}

// AllClients returns all client entries (for iteration by ws/relay packages).
func (s *Store) AllClients() []*ClientEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ClientEntry, 0, len(s.clients))
	for _, e := range s.clients {
		out = append(out, e)
	}
	return out
}

// --- Stats ---

// UpdateStats caches stats and updates the client entry's metadata fields.
// Returns true if any metadata fields (hostname, platform, etc.) changed,
// signaling that the client list should be re-broadcast.
func (s *Store) UpdateStats(clientID string, stats map[string]any) bool {
	s.mu.Lock()
	s.statsCache[clientID] = stats

	// Bounded history
	history := s.statsHistory[clientID]
	history = append(history, StatsEntry{
		Stats:     stats,
		UpdatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
	})
	if len(history) > StatsHistoryLimit {
		history = history[len(history)-StatsHistoryLimit:]
	}
	s.statsHistory[clientID] = history

	// Populate client entry metadata from stats
	entry := s.clients[clientID]
	s.mu.Unlock()

	changed := false
	if entry != nil {
		entry.Mu.Lock()
		if v, ok := stats["platform"].(string); ok && v != "" && v != entry.Platform {
			entry.Platform = v
			changed = true
		}
		if v, ok := stats["release"].(string); ok && v != "" && v != entry.Release {
			entry.Release = v
			changed = true
		}
		if v, ok := stats["hostname"].(string); ok && v != "" && v != entry.Hostname {
			entry.Hostname = v
			changed = true
		}
		if v, ok := stats["arch"].(string); ok && v != "" && v != entry.Arch {
			entry.Arch = v
			changed = true
		}
		// cpus comes as a JSON number (float64 in Go)
		if v, ok := stats["cpus"].(float64); ok && v > 0 {
			cpuStr := fmt.Sprintf("%d", int(v))
			if cpuStr != entry.CPUs {
				entry.CPUs = cpuStr
				changed = true
			}
		}
		// agentVersion: try top-level first, then nested stats.agent.version
		newVersion := ""
		if v, ok := stats["agentVersion"].(string); ok && v != "" {
			newVersion = v
		} else if agent, ok := stats["agent"].(map[string]any); ok {
			if v, ok := agent["version"].(string); ok && v != "" {
				newVersion = v
			}
		}
		if newVersion != "" && newVersion != entry.AgentVersion {
			entry.AgentVersion = newVersion
			changed = true
		}
		entry.Mu.Unlock()
	}
	return changed
}

// GetStats returns the latest cached stats for a client.
func (s *Store) GetStats(clientID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsCache[clientID]
}

// GetStatsHistory returns the stats history for a client.
func (s *Store) GetStatsHistory(clientID string) []StatsEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsHistory[clientID]
}

// --- Alerts ---

// SetAlerts caches OS alerts for a client.
func (s *Store) SetAlerts(clientID string, alerts map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alertsCache[clientID] = alerts
}

// GetAlerts returns cached alerts for a client.
func (s *Store) GetAlerts(clientID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.alertsCache[clientID]
}

// --- Kiosk Status ---

// SetKioskStatus caches kiosk status for a client.
func (s *Store) SetKioskStatus(clientID string, status map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kioskStatus[clientID] = status
}

// GetKioskStatus returns cached kiosk status.
func (s *Store) GetKioskStatus(clientID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.kioskStatus[clientID]
}

// --- Variant Status ---

// SetVariantStatus caches variant status for a client.
func (s *Store) SetVariantStatus(clientID string, status map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.variantStatus[clientID] = status
}

// GetVariantStatus returns cached variant status.
func (s *Store) GetVariantStatus(clientID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.variantStatus[clientID]
}

// --- Shell Sessions ---

// AddShellSession registers a new shell session.
func (s *Store) AddShellSession(sessionID, clientID string, dashConn *websocket.Conn) {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	s.shellSessions[sessionID] = &ShellSession{
		ClientID:  clientID,
		DashConn:  dashConn,
		CreatedAt: time.Now(),
	}
}

// GetShellSession returns a shell session by ID.
func (s *Store) GetShellSession(sessionID string) *ShellSession {
	s.shellMu.RLock()
	defer s.shellMu.RUnlock()
	return s.shellSessions[sessionID]
}

// RemoveShellSession removes a shell session.
func (s *Store) RemoveShellSession(sessionID string) {
	s.shellMu.Lock()
	defer s.shellMu.Unlock()
	delete(s.shellSessions, sessionID)
}

// ShellSessionsByClient returns all shell sessions for a given client.
func (s *Store) ShellSessionsByClient(clientID string) []*ShellSession {
	s.shellMu.RLock()
	defer s.shellMu.RUnlock()
	var out []*ShellSession
	for _, sess := range s.shellSessions {
		if sess.ClientID == clientID {
			out = append(out, sess)
		}
	}
	return out
}

// --- Log Tail Sessions ---

// AddLogTailSession registers a new log tail session.
func (s *Store) AddLogTailSession(sessionID, clientID string, dashConn *websocket.Conn) {
	s.logTailMu.Lock()
	defer s.logTailMu.Unlock()
	s.logTailSessions[sessionID] = &LogTailSession{
		ClientID:  clientID,
		DashConn:  dashConn,
		CreatedAt: time.Now(),
	}
}

// GetLogTailSession returns a log tail session by ID.
func (s *Store) GetLogTailSession(sessionID string) *LogTailSession {
	s.logTailMu.RLock()
	defer s.logTailMu.RUnlock()
	return s.logTailSessions[sessionID]
}

// RemoveLogTailSession removes a log tail session.
func (s *Store) RemoveLogTailSession(sessionID string) {
	s.logTailMu.Lock()
	defer s.logTailMu.Unlock()
	delete(s.logTailSessions, sessionID)
}

// --- Backup Jobs ---

// SetBackupJob stores a backup job.
func (s *Store) SetBackupJob(planID string, job *BackupJob) {
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	s.backupJobs[planID] = job
}

// GetBackupJob returns a backup job by plan ID.
func (s *Store) GetBackupJob(planID string) *BackupJob {
	s.backupMu.RLock()
	defer s.backupMu.RUnlock()
	return s.backupJobs[planID]
}

// --- Pending File Operations ---

// SetPendingFileOp registers a file operation.
func (s *Store) SetPendingFileOp(requestID string, op *PendingFileOp) {
	s.fileOpMu.Lock()
	defer s.fileOpMu.Unlock()
	s.pendingFileOps[requestID] = op
}

// GetPendingFileOp returns a pending file op.
func (s *Store) GetPendingFileOp(requestID string) *PendingFileOp {
	s.fileOpMu.RLock()
	defer s.fileOpMu.RUnlock()
	return s.pendingFileOps[requestID]
}

// RemovePendingFileOp removes a pending file op.
func (s *Store) RemovePendingFileOp(requestID string) {
	s.fileOpMu.Lock()
	defer s.fileOpMu.Unlock()
	delete(s.pendingFileOps, requestID)
}

// SwarmClusters groups connected clients by swarm cluster, returning a
// slice of cluster maps with members, manager, and cluster metadata.
func (s *Store) SwarmClusters() []map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type clusterInfo struct {
		members []map[string]any
	}
	clusters := map[string]*clusterInfo{}

	for id, entry := range s.clients {
		stats := s.statsCache[id]
		if stats == nil {
			continue
		}
		swarmActive, _ := stats["swarmActive"].(bool)
		if !swarmActive {
			continue
		}

		clusterID, _ := stats["swarmClusterId"].(string)
		if clusterID == "" {
			clusterID = "default"
		}

		entry.Mu.Lock()
		role, _ := stats["swarmRole"].(string)
		member := map[string]any{
			"clientId": id,
			"hostname": entry.Hostname,
			"role":     role,
		}
		entry.Mu.Unlock()

		ci, ok := clusters[clusterID]
		if !ok {
			ci = &clusterInfo{}
			clusters[clusterID] = ci
		}
		ci.members = append(ci.members, member)
	}

	out := make([]map[string]any, 0, len(clusters))
	for cid, ci := range clusters {
		var manager string
		for _, m := range ci.members {
			if r, _ := m["role"].(string); r == "manager" {
				manager, _ = m["clientId"].(string)
				break
			}
		}
		out = append(out, map[string]any{
			"clusterId": cid,
			"manager":   manager,
			"members":   ci.members,
		})
	}
	return out
}

// CleanStaleFileOps removes file ops older than maxAge.
func (s *Store) CleanStaleFileOps(maxAge time.Duration) {
	s.fileOpMu.Lock()
	defer s.fileOpMu.Unlock()
	now := time.Now()
	for id, op := range s.pendingFileOps {
		lastSeen := op.LastSeenAt
		if lastSeen.IsZero() {
			lastSeen = op.StartedAt
		}
		if now.Sub(lastSeen) > maxAge {
			delete(s.pendingFileOps, id)
		}
	}
}
