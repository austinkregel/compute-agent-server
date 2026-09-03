package state

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// StatsHistoryLimit is the max number of stats samples retained per client.
const StatsHistoryLimit = 25

// OfflineRetention is how long a disconnected agent stays on the roster as an
// offline entry before being forgotten.
const OfflineRetention = 24 * time.Hour

// MaxOfflineClients caps the retained offline roster. Client IDs arrive from
// the network, so an agent that reconnects under a fresh ID on every attempt
// must not be able to grow this map without bound; the oldest drop is evicted
// once the cap is reached.
const MaxOfflineClients = 256

// ClientEntry represents a connected agent in the server's state.
type ClientEntry struct {
	Mu sync.Mutex

	ClientID string
	Conn     *websocket.Conn // nil in tests
	LastPong time.Time
	// RttMs is the most recent ping→pong round-trip in milliseconds, measured
	// from the echoed ping timestamp. 0 until the first pong is received.
	RttMs         int64
	Authenticated bool

	// Populated from stats updates
	Platform     string
	Release      string
	Hostname     string
	Arch         string
	Home         string
	CPUs         string
	AgentVersion string

	// Direct-connection advertisement (from stats.direct); empty when the agent
	// isn't advertising a P2P endpoint.
	DirectAddr        string
	DirectCertSHA256  string
	DirectPinRequired bool

	// Command signing — set during handshake
	SessionNonce string
	// Signer is kept as an opaque interface to avoid import cycles.
	// The ws package will type-assert as needed.
	Signer any

	// Capabilities is the agent's self-reported capability registry snapshot
	// (from stats.capabilities), keyed by capability name (e.g. "docker",
	// "battery", "telephony"). Populated generically — new capabilities never
	// require a server code change.
	Capabilities map[string]CapabilityInfo
}

// CapabilityInfo mirrors the agent's capability.Info wire shape: a tri-state
// availability signal plus optional detail/features/metadata. See
// agent/pkg/capability for the authoritative definition.
type CapabilityInfo struct {
	State     string         `json:"state"`
	Detail    string         `json:"detail,omitempty"`
	Features  []string       `json:"features,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
	LastProbe string         `json:"lastProbe,omitempty"`
}

// PublicClient is the JSON-safe projection of a ClientEntry.
// LastPong is Unix milliseconds (matching the Node.js server's Date.now() format).
type PublicClient struct {
	ClientID string `json:"clientId"`
	LastPong int64  `json:"lastPong"`
	// Connected reports whether the agent's socket is open right now. False
	// entries are retained last-known snapshots (see Store.lastSeen) so the
	// dashboard can render a node as offline instead of having it vanish.
	Connected bool `json:"connected"`
	// DisconnectedAt is Unix milliseconds of the drop, set only when
	// Connected is false.
	DisconnectedAt int64 `json:"disconnectedAt,omitempty"`
	// PingRttMs is the last measured ping→pong round-trip (ms). Omitted until known.
	PingRttMs     int64  `json:"pingRttMs,omitempty"`
	Authenticated bool   `json:"authenticated"`
	Platform      string `json:"platform,omitempty"`
	Release       string `json:"release,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
	Arch          string `json:"arch,omitempty"`
	Home          string `json:"home,omitempty"`
	CPUs          string `json:"cpus,omitempty"`
	AgentVersion  string `json:"agentVersion,omitempty"`
	// Direct-connection advertisement, surfaced so the IDE can attempt P2P.
	DirectAddr        string `json:"directAddr,omitempty"`
	DirectCertSHA256  string `json:"directCertSha256,omitempty"`
	DirectPinRequired bool   `json:"directPinRequired,omitempty"`
	// Capabilities is the agent's self-reported capability snapshot, keyed by
	// capability name. See CapabilityInfo.
	Capabilities map[string]CapabilityInfo `json:"capabilities,omitempty"`
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

	clients map[string]*ClientEntry
	// lastSeen retains a snapshot of agents that have disconnected, so the
	// dashboard keeps showing them as offline rather than dropping them from
	// the roster. Deliberately separate from clients: HasClient/ClientIDs/
	// AllClients are the reachability gates used throughout relay and api, and
	// they must keep meaning "connected right now".
	lastSeen      map[string]PublicClient
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
		lastSeen:        make(map[string]PublicClient),
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
	// It is back; drop the offline snapshot so the roster does not list the
	// same agent twice.
	delete(s.lastSeen, clientID)
	s.clients[clientID] = &ClientEntry{
		ClientID:      clientID,
		Conn:          conn,
		LastPong:      time.Now(),
		Authenticated: true,
	}
}

// RemoveClient marks an agent disconnected. The live entry is dropped so every
// reachability check (HasClient, ClientIDs, AllClients) immediately reports it
// as gone, but a last-known snapshot is retained in lastSeen so PublicClients
// can still render it as offline. Cached stats/alerts are left in place, so an
// offline node keeps showing the values it last reported.
func (s *Store) RemoveClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.clients[clientID]; ok {
		e.Mu.Lock()
		pub := publicClientLocked(e)
		e.Mu.Unlock()
		pub.Connected = false
		pub.DisconnectedAt = time.Now().UnixMilli()
		s.lastSeen[clientID] = pub
	}
	delete(s.clients, clientID)

	s.pruneOfflineLocked()
}

// pruneOfflineLocked drops expired offline entries, then evicts the oldest
// remaining ones until the roster fits MaxOfflineClients. Caller holds s.mu.
func (s *Store) pruneOfflineLocked() {
	cutoff := time.Now().Add(-OfflineRetention).UnixMilli()
	for id, pub := range s.lastSeen {
		if pub.DisconnectedAt < cutoff {
			s.forgetLocked(id)
		}
	}
	for len(s.lastSeen) > MaxOfflineClients {
		oldestID, oldestAt := "", int64(0)
		for id, pub := range s.lastSeen {
			if oldestID == "" || pub.DisconnectedAt < oldestAt {
				oldestID, oldestAt = id, pub.DisconnectedAt
			}
		}
		s.forgetLocked(oldestID)
	}
}

// forgetLocked drops an agent from the offline roster along with the caches
// keyed by its ID. Those caches are only ever written, never cleaned, so
// bounding the roster alone would still leak a stats ring buffer, an alert
// snapshot and two status maps per client ID ever seen. Caller holds s.mu.
func (s *Store) forgetLocked(clientID string) {
	delete(s.lastSeen, clientID)
	delete(s.statsCache, clientID)
	delete(s.statsHistory, clientID)
	delete(s.alertsCache, clientID)
	delete(s.kioskStatus, clientID)
	delete(s.variantStatus, clientID)
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

// ClientIDs returns the list of connected client IDs, sorted. Callers render
// this directly, and Go randomizes map iteration order, so an unsorted result
// reshuffles on every call.
func (s *Store) ClientIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.clients))
	for id := range s.clients {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ClientCount returns the number of connected clients.
func (s *Store) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// publicClientLocked projects an entry into its JSON-safe form. Caller holds
// e.Mu. Connected is left false; the caller sets it, since this same
// projection is used both for live entries and for the offline snapshot taken
// at disconnect.
func publicClientLocked(e *ClientEntry) PublicClient {
	return PublicClient{
		ClientID:          e.ClientID,
		LastPong:          e.LastPong.UnixMilli(),
		PingRttMs:         e.RttMs,
		Authenticated:     e.Authenticated,
		Platform:          e.Platform,
		Release:           e.Release,
		Hostname:          e.Hostname,
		Arch:              e.Arch,
		Home:              e.Home,
		CPUs:              e.CPUs,
		AgentVersion:      e.AgentVersion,
		DirectAddr:        e.DirectAddr,
		DirectCertSHA256:  e.DirectCertSHA256,
		DirectPinRequired: e.DirectPinRequired,
		Capabilities:      e.Capabilities,
	}
}

// PublicClients returns a safe projection of the roster: every connected agent
// (Connected true), plus agents that have dropped within OfflineRetention
// (Connected false, carrying the metadata they last reported). Offline entries
// are included so a node greys out on the dashboard rather than disappearing.
func (s *Store) PublicClients() []PublicClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicClient, 0, len(s.clients)+len(s.lastSeen))
	for _, e := range s.clients {
		e.Mu.Lock()
		pub := publicClientLocked(e)
		e.Mu.Unlock()
		pub.Connected = true
		out = append(out, pub)
	}
	// Filtered on read as well as pruned on write: pruning only runs when some
	// agent disconnects, so without this an expired entry would linger on a
	// quiet server indefinitely.
	cutoff := time.Now().Add(-OfflineRetention).UnixMilli()
	for _, pub := range s.lastSeen {
		// Defensive: a reconnect deletes the snapshot under the same lock, so
		// an ID should never be in both maps.
		if _, live := s.clients[pub.ClientID]; live {
			continue
		}
		if pub.DisconnectedAt < cutoff {
			continue
		}
		out = append(out, pub)
	}

	// Both sources above are maps, and Go randomizes map iteration order, so
	// without this the roster comes back in a different order on every call.
	// The dashboard renders client_list in the order it arrives, which made
	// hosts swap places under the pointer on every connect/disconnect.
	// Sorting by ID (not by connected-ness) keeps a host in the same row
	// across a brief drop, so its position only moves when the fleet's
	// membership actually changes.
	sort.Slice(out, func(i, j int) bool { return out[i].ClientID < out[j].ClientID })
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
		if v, ok := stats["home"].(string); ok && v != "" && v != entry.Home {
			entry.Home = v
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

		// Direct-connection advertisement (stats.direct). Parsed defensively;
		// withdrawn (absent) → fields cleared so a stale endpoint isn't dialed.
		var addr, certSha string
		var pinReq bool
		if direct, ok := stats["direct"].(map[string]any); ok {
			addr, _ = direct["addr"].(string)
			certSha, _ = direct["certSha256"].(string)
			pinReq, _ = direct["pinRequired"].(bool)
		}
		if addr != entry.DirectAddr || certSha != entry.DirectCertSHA256 || pinReq != entry.DirectPinRequired {
			entry.DirectAddr = addr
			entry.DirectCertSHA256 = certSha
			entry.DirectPinRequired = pinReq
			changed = true
		}

		// Capability-registry snapshot (stats.capabilities). Parsed generically —
		// unlike the fields above, this never needs a new hardcoded block when a
		// new capability (e.g. "telephony") is introduced on the agent side.
		if capsRaw, ok := stats["capabilities"].(map[string]any); ok {
			parsed := parseCapabilities(capsRaw)
			if !reflect.DeepEqual(parsed, entry.Capabilities) {
				entry.Capabilities = parsed
				changed = true
			}
		}
		entry.Mu.Unlock()
	}
	return changed
}

// parseCapabilities defensively converts the raw stats.capabilities map (as
// decoded from JSON into map[string]any) into typed CapabilityInfo entries.
// Unknown/malformed entries are skipped rather than erroring, matching this
// package's convention of degrading gracefully on unexpected agent payloads.
func parseCapabilities(raw map[string]any) map[string]CapabilityInfo {
	out := make(map[string]CapabilityInfo, len(raw))
	for name, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		info := CapabilityInfo{}
		info.State, _ = m["state"].(string)
		info.Detail, _ = m["detail"].(string)
		info.LastProbe, _ = m["lastProbe"].(string)
		if featuresRaw, ok := m["features"].([]any); ok {
			for _, f := range featuresRaw {
				if s, ok := f.(string); ok {
					info.Features = append(info.Features, s)
				}
			}
		}
		if meta, ok := m["meta"].(map[string]any); ok {
			info.Meta = meta
		}
		out[name] = info
	}
	return out
}

// GetStats returns the latest cached stats for a client.
func (s *Store) GetStats(clientID string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statsCache[clientID]
}

// GetStatsHistory returns a copy of the retained samples. Copied rather than
// aliased because UpdateStats appends in place, so a caller ranging over the
// live slice outside s.mu would race with the next sample.
func (s *Store) GetStatsHistory(clientID string) []StatsEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.statsHistory[clientID]
	if len(src) == 0 {
		return nil
	}
	out := make([]StatsEntry, len(src))
	copy(out, src)
	return out
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
		// Members accumulate in map-iteration order, so sort before picking a
		// manager: otherwise "the" manager of a multi-manager cluster is
		// whichever one this call happened to visit first, and the answer
		// changes between calls for an unchanged cluster.
		sort.Slice(ci.members, func(i, j int) bool {
			a, _ := ci.members[i]["clientId"].(string)
			b, _ := ci.members[j]["clientId"].(string)
			return a < b
		})

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
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i]["clusterId"].(string)
		b, _ := out[j]["clusterId"].(string)
		return a < b
	})
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
