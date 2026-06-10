package relay

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/backup-server/internal/ws"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// isoMillis matches JavaScript's Date.toISOString() output (with milliseconds).
const isoMillis = "2006-01-02T15:04:05.000Z"

func nowISO() string {
	return time.Now().UTC().Format(isoMillis)
}

// writePlanFile persists a backup plan JSON file to backupsDir.
func (r *Relay) writePlanFile(planID string, data map[string]any) {
	if r.backupsDir == "" {
		return
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		r.log.Warn("failed to marshal backup plan", "planId", planID, "error", err)
		return
	}
	p := filepath.Join(r.backupsDir, planID+".json")
	if err := os.WriteFile(p, b, 0640); err != nil {
		r.log.Warn("failed to write backup plan file", "planId", planID, "error", err)
	}
}

// updatePlanFile merges updates into an existing backup plan JSON file.
func (r *Relay) updatePlanFile(planID string, update map[string]any) {
	if r.backupsDir == "" {
		return
	}
	p := filepath.Join(r.backupsDir, planID+".json")
	prev := map[string]any{}
	if raw, err := os.ReadFile(p); err == nil {
		json.Unmarshal(raw, &prev)
	}
	for k, v := range update {
		prev[k] = v
	}
	b, err := json.MarshalIndent(prev, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(p, b, 0640)
}

// DashSender abstracts the dashboard message sending operations.
// Satisfied by *ws.DashboardHandler and by test mocks.
type DashSender interface {
	Broadcast(event string, data any)
	SendTo(connID string, event string, data any) bool
}

// Relay bridges dashboard events to agents and routes agent responses back.
type Relay struct {
	store      *state.Store
	log        *logging.Logger
	dash       DashSender
	backupsDir string

	// Shell sessions: sessionID → shellSession
	shellMu       sync.RWMutex
	shellSessions map[string]*shellSession

	// Log tail sessions: sessionID → logTailSession
	logMu       sync.RWMutex
	logSessions map[string]*logTailSession

	// Backup jobs: planID → backupJob
	backupMu   sync.RWMutex
	backupJobs map[string]*backupJob

	// Pending file ops: requestID → fileOp
	fileMu  sync.RWMutex
	fileOps map[string]*fileOp

	// Pending cron/admin results: token → channel for synchronous wait
	pendingMu      sync.Mutex
	pendingResults map[string]chan adminResult

	// Generic pending responses: token → channel for arbitrary JSON results
	genericMu      sync.Mutex
	genericPending map[string]chan map[string]any
}

type shellSession struct {
	ClientID   string
	DashConnID string // dashboard connection that owns this session
	CreatedAt  time.Time
}

type logTailSession struct {
	ClientID   string
	DashConnID string
	Lines      int
	CreatedAt  time.Time
}

type backupJob struct {
	ClientID       string
	DashConnID     string
	PlanID         string
	Status         string // planning, planned, running, completed, failed
	FilesCompleted int
	Job            map[string]any
	Plan           map[string]any
	CreatedAt      time.Time
}

type fileOp struct {
	ClientID   string
	DashConnID string
	Type       string // put, delete, chmod
	RequestID  string
	StartedAt  time.Time
	LastSeenAt time.Time
}

type adminResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RegisterPendingResult creates a channel that will receive the admin_result
// matching the given token. Caller must call UnregisterPendingResult when done.
func (r *Relay) RegisterPendingResult(token string) <-chan adminResult {
	ch := make(chan adminResult, 1)
	r.pendingMu.Lock()
	r.pendingResults[token] = ch
	r.pendingMu.Unlock()
	return ch
}

// UnregisterPendingResult removes a pending result channel.
func (r *Relay) UnregisterPendingResult(token string) {
	r.pendingMu.Lock()
	delete(r.pendingResults, token)
	r.pendingMu.Unlock()
}

// RegisterGenericPending creates a channel for receiving an arbitrary JSON
// response keyed by token. Caller must call UnregisterGenericPending when done.
func (r *Relay) RegisterGenericPending(token string) <-chan map[string]any {
	ch := make(chan map[string]any, 1)
	r.genericMu.Lock()
	r.genericPending[token] = ch
	r.genericMu.Unlock()
	return ch
}

// UnregisterGenericPending removes a generic pending channel.
func (r *Relay) UnregisterGenericPending(token string) {
	r.genericMu.Lock()
	delete(r.genericPending, token)
	r.genericMu.Unlock()
}

// ResolveGenericPending delivers a response to a waiting generic pending channel.
func (r *Relay) ResolveGenericPending(token string, data map[string]any) bool {
	r.genericMu.Lock()
	ch, ok := r.genericPending[token]
	r.genericMu.Unlock()
	if ok {
		select {
		case ch <- data:
		default:
		}
	}
	return ok
}

// New creates a new Relay instance.
// backupsDir is the directory for persisting backup plan JSON files (e.g., "backups").
func New(store *state.Store, log *logging.Logger, dash DashSender, backupsDir string) *Relay {
	if backupsDir != "" {
		os.MkdirAll(backupsDir, 0750)
	}
	return &Relay{
		store:          store,
		log:            log,
		dash:           dash,
		backupsDir:     backupsDir,
		shellSessions:  make(map[string]*shellSession),
		logSessions:    make(map[string]*logTailSession),
		backupJobs:     make(map[string]*backupJob),
		fileOps:        make(map[string]*fileOp),
		pendingResults: make(map[string]chan adminResult),
		genericPending: make(map[string]chan map[string]any),
	}
}

// HandleDashboardEvent processes an event from a dashboard connection.
func (r *Relay) HandleDashboardEvent(dc *ws.DashboardConn, msg *ws.Message) {
	var data map[string]any
	json.Unmarshal(msg.Data, &data)

	switch msg.Event {
	// Shell
	case "shell_start", "start_shell":
		r.handleShellStart(dc, data)
	case "shell_input":
		r.handleShellInput(dc, data)
	case "shell_resize":
		r.handleShellResize(dc, data)
	case "shell_close", "close_shell":
		r.handleShellClose(dc, data)

	// Log tail
	case "log_tail_start":
		r.handleLogTailStart(dc, data)
	case "log_tail_stop":
		r.handleLogTailStop(dc, data)

	// Backup
	case "backup_plan_request":
		r.handleBackupPlanRequest(dc, data)
	case "backup_approve":
		r.handleBackupApprove(dc, data)

	// File ops
	case "file_put_start":
		r.handleFilePutStart(dc, data)
	case "file_put_chunk":
		r.handleFilePutChunk(dc, data)
	case "file_put_finish":
		r.handleFilePutFinish(dc, data)
	case "file_delete_request":
		r.handleFileDeleteRequest(dc, data)
	case "file_chmod_request":
		r.handleFileChmodRequest(dc, data)

	// Dir browse
	case "dir_list_request":
		r.handleDirListRequest(dc, data)

	// Kiosk
	case "kiosk_set":
		r.handleKioskSet(dc, data)
	case "kiosk_save_layout":
		r.handleKioskSaveLayout(dc, data)
	case "kiosk_get_layouts":
		r.handleKioskGetLayouts(dc, data)

	// Variant
	case "switch_variant":
		r.handleSwitchVariant(dc, data)

	// Update check
	case "check_updates_request":
		r.handleCheckUpdates(dc, data)

	// Docker / Swarm monitoring + cluster membership (forward to agent).
	// Read-only reporting plus swarm join/leave; stack/service/network/node
	// mutation is owned by a separate management tool, so those events are
	// no longer forwarded (agents no longer handle them).
	case "swarm_info_request",
		"swarm_init_request",
		"swarm_join_request",
		"swarm_leave_request",
		"swarm_node_list_request",
		"swarm_service_list_request",
		"swarm_service_logs_request",
		"swarm_network_list_request",
		"swarm_stack_list_request":
		r.handleDockerDashboardEvent(dc, msg.Event, data)
	}
}

// HandleAgentEvent processes an event from an agent connection.
func (r *Relay) HandleAgentEvent(clientID string, msg *ws.Message) {
	var data map[string]any
	json.Unmarshal(msg.Data, &data)

	switch msg.Event {
	// Shell
	case "shell_output":
		r.handleShellOutput(clientID, data)
	case "shell_closed":
		r.handleShellClosed(clientID, data)

	// Log tail
	case "log_tail_output":
		r.handleLogTailOutput(clientID, data)
	case "log_tail_closed":
		r.handleLogTailClosed(clientID, data)

	// Backup
	case "backup_plan":
		r.handleBackupPlan(clientID, data)
	case "backup_progress":
		r.handleBackupProgress(clientID, data)
	case "backup_complete":
		r.handleBackupComplete(clientID, data)
	case "backup_error":
		r.handleBackupError(clientID, data)

	// File ops
	case "file_put_result":
		r.handleFilePutResult(clientID, data)
	case "file_delete_result":
		r.handleFileDeleteResult(clientID, data)
	case "file_chmod_result":
		r.handleFileChmodResult(clientID, data)

	// Dir browse
	case "dir_list_response":
		r.handleDirListResponse(clientID, data)

	// Kiosk
	case "kiosk_status":
		r.handleKioskStatus(clientID, data)
	case "kiosk_layout_saved":
		r.dash.Broadcast("kiosk_layout_saved", mergeClientID(clientID, data))
	case "kiosk_layouts":
		r.dash.Broadcast("kiosk_layouts", mergeClientID(clientID, data))

	// Variant
	case "variant_status":
		r.handleVariantStatus(clientID, data)
	case "variant_switch_result":
		r.handleVariantSwitchResult(clientID, data)

	// Update check
	case "agent_update_result":
		r.handleAgentUpdateResult(clientID, data)

	// Admin command result
	case "admin_result":
		r.handleAdminResult(clientID, data)

	// Keys sync
	case "keys_sync_result":
		r.handleKeysSyncResult(clientID, data)

	// Legacy shell exit (normalize to shell_closed)
	case "shell_exit":
		r.handleShellExit(clientID, data)

	// Network status (update last pong timestamp)
	case "net_status":
		r.handleNetStatus(clientID, data)

	// Docker / Swarm responses (broadcast to dashboards)
	case "swarm_info_response",
		"swarm_init_result",
		"swarm_join_result",
		"swarm_leave_result",
		"swarm_node_update_result",
		"swarm_node_list_response",
		"swarm_service_list_response",
		"swarm_service_create_result",
		"swarm_service_update_result",
		"swarm_service_remove_result",
		"swarm_service_logs_response",
		"swarm_network_list_response",
		"swarm_network_create_result",
		"swarm_network_remove_result",
		"swarm_stack_list_response",
		"swarm_stack_remove_result":
		r.dash.Broadcast(msg.Event, mergeClientID(clientID, data))

	// Compose import responses: resolve pending REST request if token matches,
	// otherwise broadcast to dashboards
	case "compose_scan_response", "compose_parse_response":
		merged := mergeClientID(clientID, data)
		if token, ok := data["token"].(string); ok && token != "" {
			if r.ResolveGenericPending(token, merged) {
				return
			}
		}
		r.dash.Broadcast(msg.Event, merged)

	// Stack deployment results
	case "stack_deploy_result", "stack_stop_result", "stack_status_response":
		merged := mergeClientID(clientID, data)
		if token, ok := data["token"].(string); ok && token != "" {
			r.ResolveGenericPending(token, merged)
		}
		r.dash.Broadcast(msg.Event, merged)

	// Container lifecycle events from managed containers
	case "container_event":
		r.dash.Broadcast("container_event", mergeClientID(clientID, data))

	// Container inventory response
	case "container_inventory_response":
		merged := mergeClientID(clientID, data)
		if token, ok := data["token"].(string); ok && token != "" {
			r.ResolveGenericPending(token, merged)
		}
		r.dash.Broadcast(msg.Event, merged)

	// Container metrics (per-container CPU/mem)
	case "container_metrics":
		r.dash.Broadcast("container_metrics", mergeClientID(clientID, data))

	// Container log responses
	case "container_logs_response", "container_logs_error":
		merged := mergeClientID(clientID, data)
		if token, ok := data["token"].(string); ok && token != "" {
			r.ResolveGenericPending(token, merged)
		}
		r.dash.Broadcast(msg.Event, merged)
	}
}

// CleanupDashboard removes all sessions owned by a disconnected dashboard.
func (r *Relay) CleanupDashboard(dc *ws.DashboardConn) {
	connID := dc.ID

	// Close shell sessions
	r.shellMu.Lock()
	for sessionID, sess := range r.shellSessions {
		if sess.DashConnID == connID {
			ws.SendSignedCommand(r.store, sess.ClientID, "shell_close",
				map[string]string{"session": sessionID}, r.log)
			delete(r.shellSessions, sessionID)
		}
	}
	r.shellMu.Unlock()

	// Stop log tail sessions
	r.logMu.Lock()
	for sessionID, sess := range r.logSessions {
		if sess.DashConnID == connID {
			ws.SendSignedCommand(r.store, sess.ClientID, "log_tail_stop",
				map[string]string{"session": sessionID}, r.log)
			delete(r.logSessions, sessionID)
		}
	}
	r.logMu.Unlock()

	// Clean up pending file ops
	r.fileMu.Lock()
	for reqID, op := range r.fileOps {
		if op.DashConnID == connID {
			delete(r.fileOps, reqID)
		}
	}
	r.fileMu.Unlock()
}

// CleanupStaleFileOps removes file operations older than 5 minutes.
func (r *Relay) CleanupStaleFileOps() {
	threshold := time.Now().Add(-5 * time.Minute)
	r.fileMu.Lock()
	defer r.fileMu.Unlock()
	for reqID, op := range r.fileOps {
		if op.LastSeenAt.Before(threshold) {
			r.log.Info("cleaning stale file op", "requestId", reqID, "type", op.Type)
			delete(r.fileOps, reqID)
		}
	}
}

// --- Shell handlers ---

func (r *Relay) handleShellStart(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" {
		r.dash.SendTo(dc.ID, "shell_error", map[string]any{"message": "clientId required"})
		return
	}
	if !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "shell_error", map[string]any{"message": "Client offline", "clientId": clientID})
		return
	}

	sessionID := uuid.NewString()

	r.shellMu.Lock()
	r.shellSessions[sessionID] = &shellSession{
		ClientID:   clientID,
		DashConnID: dc.ID,
		CreatedAt:  time.Now(),
	}
	r.shellMu.Unlock()

	ws.SendSignedCommand(r.store, clientID, "shell_start",
		map[string]string{"session": sessionID}, r.log)

	r.dash.SendTo(dc.ID, "shell_started", map[string]any{
		"session":  sessionID,
		"clientId": clientID,
	})
}

func (r *Relay) handleShellInput(dc *ws.DashboardConn, data map[string]any) {
	sessionID := str(data, "session")
	r.shellMu.RLock()
	sess, ok := r.shellSessions[sessionID]
	r.shellMu.RUnlock()
	if !ok || sess.DashConnID != dc.ID {
		r.dash.SendTo(dc.ID, "shell_error", map[string]any{"message": "Invalid session", "session": sessionID})
		return
	}

	ws.SendSignedCommand(r.store, sess.ClientID, "shell_input",
		map[string]any{"session": sessionID, "data": data["data"]}, r.log)
}

func (r *Relay) handleShellResize(dc *ws.DashboardConn, data map[string]any) {
	sessionID := str(data, "session")
	r.shellMu.RLock()
	sess, ok := r.shellSessions[sessionID]
	r.shellMu.RUnlock()
	if !ok || sess.DashConnID != dc.ID {
		return
	}

	ws.SendSignedCommand(r.store, sess.ClientID, "shell_resize",
		map[string]any{"session": sessionID, "cols": data["cols"], "rows": data["rows"]}, r.log)
}

func (r *Relay) handleShellClose(dc *ws.DashboardConn, data map[string]any) {
	sessionID := str(data, "session")
	r.shellMu.Lock()
	sess, ok := r.shellSessions[sessionID]
	if ok && sess.DashConnID == dc.ID {
		delete(r.shellSessions, sessionID)
	}
	r.shellMu.Unlock()
	if !ok {
		return
	}

	ws.SendSignedCommand(r.store, sess.ClientID, "shell_close",
		map[string]string{"session": sessionID}, r.log)

	r.dash.SendTo(dc.ID, "shell_closed", map[string]any{
		"session": sessionID,
		"reason":  "operator closed",
	})
}

func (r *Relay) handleShellOutput(clientID string, data map[string]any) {
	sessionID := str(data, "session")
	r.shellMu.RLock()
	sess, ok := r.shellSessions[sessionID]
	r.shellMu.RUnlock()
	if !ok {
		return
	}

	r.dash.SendTo(sess.DashConnID, "shell_output", map[string]any{
		"clientId": clientID,
		"session":  sessionID,
		"data":     data["data"],
	})
}

func (r *Relay) handleShellClosed(clientID string, data map[string]any) {
	sessionID := str(data, "session")
	r.shellMu.Lock()
	sess, ok := r.shellSessions[sessionID]
	if ok {
		delete(r.shellSessions, sessionID)
	}
	r.shellMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(sess.DashConnID, "shell_closed", map[string]any{
		"clientId": clientID,
		"session":  sessionID,
		"reason":   data["reason"],
	})
}

// --- Log tail handlers ---

func (r *Relay) handleLogTailStart(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" {
		r.dash.SendTo(dc.ID, "log_tail_error", map[string]any{"message": "clientId required"})
		return
	}
	if !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "log_tail_error", map[string]any{"message": "Client offline", "clientId": clientID})
		return
	}

	lines := intVal(data, "lines", 10)
	if lines < 1 {
		lines = 1
	}
	if lines > 200 {
		lines = 200
	}

	sessionID := uuid.NewString()

	r.logMu.Lock()
	r.logSessions[sessionID] = &logTailSession{
		ClientID:   clientID,
		DashConnID: dc.ID,
		Lines:      lines,
		CreatedAt:  time.Now(),
	}
	r.logMu.Unlock()

	ws.SendSignedCommand(r.store, clientID, "log_tail_start",
		map[string]any{"session": sessionID, "lines": lines}, r.log)

	r.dash.SendTo(dc.ID, "log_tail_started", map[string]any{
		"session":  sessionID,
		"clientId": clientID,
		"lines":    lines,
		"ts":       nowISO(),
	})
}

func (r *Relay) handleLogTailStop(dc *ws.DashboardConn, data map[string]any) {
	sessionID := str(data, "session")
	clientID := str(data, "clientId")

	r.logMu.Lock()
	if sessionID != "" {
		// Stop specific session
		if sess, ok := r.logSessions[sessionID]; ok && sess.DashConnID == dc.ID {
			ws.SendSignedCommand(r.store, sess.ClientID, "log_tail_stop",
				map[string]string{"session": sessionID}, r.log)
			delete(r.logSessions, sessionID)
		}
	} else if clientID != "" {
		// Stop all sessions for this client owned by this dashboard
		for sid, sess := range r.logSessions {
			if sess.ClientID == clientID && sess.DashConnID == dc.ID {
				ws.SendSignedCommand(r.store, clientID, "log_tail_stop",
					map[string]string{"session": sid}, r.log)
				delete(r.logSessions, sid)
			}
		}
	}
	r.logMu.Unlock()
}

func (r *Relay) handleLogTailOutput(clientID string, data map[string]any) {
	sessionID := str(data, "session")
	r.logMu.RLock()
	sess, ok := r.logSessions[sessionID]
	r.logMu.RUnlock()
	if !ok {
		return
	}

	r.dash.SendTo(sess.DashConnID, "log_tail_output", map[string]any{
		"clientId": clientID,
		"session":  sessionID,
		"data":     data["data"],
		"ts":       data["ts"],
	})
}

func (r *Relay) handleLogTailClosed(clientID string, data map[string]any) {
	sessionID := str(data, "session")
	r.logMu.Lock()
	sess, ok := r.logSessions[sessionID]
	if ok {
		delete(r.logSessions, sessionID)
	}
	r.logMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(sess.DashConnID, "log_tail_closed", map[string]any{
		"clientId": clientID,
		"session":  sessionID,
		"reason":   data["reason"],
		"ts":       data["ts"],
	})
}

// --- Backup handlers ---

func (r *Relay) handleBackupPlanRequest(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" {
		r.dash.SendTo(dc.ID, "backup_error", map[string]any{"error": "clientId required"})
		return
	}
	if !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "backup_error", map[string]any{"error": "client offline", "clientId": clientID})
		return
	}

	planID := uuid.NewString()

	r.backupMu.Lock()
	r.backupJobs[planID] = &backupJob{
		ClientID:   clientID,
		DashConnID: dc.ID,
		PlanID:     planID,
		Status:     "planning",
		Job:        data,
		CreatedAt:  time.Now(),
	}
	r.backupMu.Unlock()

	// Forward to agent with planID
	payload := copyMap(data)
	payload["planId"] = planID
	delete(payload, "clientId")
	ws.SendSignedCommand(r.store, clientID, "backup_plan", payload, r.log)

	r.dash.SendTo(dc.ID, "backup_plan_dispatched", map[string]any{
		"clientId": clientID,
		"planId":   planID,
	})
}

func (r *Relay) handleBackupApprove(dc *ws.DashboardConn, data map[string]any) {
	planID := str(data, "planId")
	r.backupMu.Lock()
	job, ok := r.backupJobs[planID]
	if ok {
		job.Status = "running"
	}
	r.backupMu.Unlock()
	if !ok {
		r.dash.SendTo(dc.ID, "backup_error", map[string]any{"error": "unknown planId", "planId": planID})
		return
	}

	payload := copyMap(job.Job)
	payload["planId"] = planID
	delete(payload, "clientId")
	ws.SendSignedCommand(r.store, job.ClientID, "backup_start", payload, r.log)

	r.dash.Broadcast("backup_started", map[string]any{
		"clientId": job.ClientID,
		"planId":   planID,
	})
}

func (r *Relay) handleBackupPlan(clientID string, data map[string]any) {
	planID := str(data, "planId")
	r.backupMu.Lock()
	job, ok := r.backupJobs[planID]
	if ok {
		job.Status = "planned"
		job.Plan = data
	}
	r.backupMu.Unlock()
	if !ok {
		return
	}

	r.writePlanFile(planID, map[string]any{
		"clientId": clientID,
		"planId":   planID,
		"job":      job.Job,
		"plan":     data,
		"status":   "planned",
	})

	r.dash.Broadcast("backup_plan", map[string]any{
		"clientId": clientID,
		"planId":   planID,
		"job":      job.Job,
		"plan":     data,
	})
}

func (r *Relay) handleBackupProgress(clientID string, data map[string]any) {
	planID := str(data, "planId")
	r.backupMu.Lock()
	job, ok := r.backupJobs[planID]
	if ok {
		job.FilesCompleted++
	}
	r.backupMu.Unlock()
	if !ok {
		return
	}

	file := str(data, "file")
	op := str(data, "op")
	if file != "" && op != "" {
		r.updatePlanFile(planID, map[string]any{
			"filesCompleted": job.FilesCompleted,
			"lastFile":       map[string]any{"file": file, "op": op, "ts": nowISO()},
		})
	}

	payload := mergeClientID(clientID, data)
	payload["filesCompleted"] = job.FilesCompleted
	r.dash.Broadcast("backup_progress", payload)
}

func (r *Relay) handleBackupComplete(clientID string, data map[string]any) {
	planID := str(data, "planId")
	r.backupMu.Lock()
	job, ok := r.backupJobs[planID]
	if ok {
		if boolVal(data, "ok") {
			job.Status = "completed"
		} else {
			job.Status = "failed"
		}
	}
	r.backupMu.Unlock()
	if !ok {
		return
	}

	r.updatePlanFile(planID, map[string]any{
		"status":           job.Status,
		"completedAt":      nowISO(),
		"durationMs":       data["ms"],
		"transferredBytes": data["transferredBytes"],
	})

	r.dash.Broadcast("backup_complete", mergeClientID(clientID, data))
}

func (r *Relay) handleBackupError(clientID string, data map[string]any) {
	planID := str(data, "planId")
	r.backupMu.Lock()
	job, ok := r.backupJobs[planID]
	if ok {
		job.Status = "failed"
	}
	r.backupMu.Unlock()

	if planID != "" {
		r.updatePlanFile(planID, map[string]any{
			"status":   "failed",
			"error":    data["error"],
			"failedAt": nowISO(),
		})
	}

	r.dash.Broadcast("backup_error", mergeClientID(clientID, data))
}

// --- File operation handlers ---

func (r *Relay) handleFilePutStart(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "file_put_result", map[string]any{"error": "client offline"})
		return
	}

	reqID := str(data, "requestId")
	if reqID == "" {
		reqID = uuid.NewString()
	}

	now := time.Now()
	r.fileMu.Lock()
	r.fileOps[reqID] = &fileOp{
		ClientID:   clientID,
		DashConnID: dc.ID,
		Type:       "put",
		RequestID:  reqID,
		StartedAt:  now,
		LastSeenAt: now,
	}
	r.fileMu.Unlock()

	payload := copyMap(data)
	payload["requestId"] = reqID
	ws.SendSignedCommand(r.store, clientID, "file_put_start", payload, r.log)

	r.dash.SendTo(dc.ID, "file_put_dispatched", map[string]any{
		"clientId":  clientID,
		"requestId": reqID,
		"path":      data["path"],
	})
}

func (r *Relay) handleFilePutChunk(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	reqID := str(data, "requestId")

	r.fileMu.Lock()
	op, ok := r.fileOps[reqID]
	if ok {
		op.LastSeenAt = time.Now()
	}
	r.fileMu.Unlock()
	if !ok {
		return
	}

	ws.SendSignedCommand(r.store, clientID, "file_put_chunk",
		map[string]any{"requestId": reqID, "offset": data["offset"], "data": data["data"]}, r.log)

	_ = op // used above
}

func (r *Relay) handleFilePutFinish(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	reqID := str(data, "requestId")

	ws.SendSignedCommand(r.store, clientID, "file_put_finish",
		map[string]any{"requestId": reqID, "checksum": data["checksum"]}, r.log)
}

func (r *Relay) handleFilePutResult(clientID string, data map[string]any) {
	reqID := str(data, "requestId")
	r.fileMu.Lock()
	op, ok := r.fileOps[reqID]
	if ok {
		delete(r.fileOps, reqID)
	}
	r.fileMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(op.DashConnID, "file_put_result", mergeClientID(clientID, data))
}

func (r *Relay) handleFileDeleteRequest(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "file_delete_result", map[string]any{"error": "client offline"})
		return
	}

	reqID := str(data, "requestId")
	if reqID == "" {
		reqID = uuid.NewString()
	}

	now := time.Now()
	r.fileMu.Lock()
	r.fileOps[reqID] = &fileOp{
		ClientID:   clientID,
		DashConnID: dc.ID,
		Type:       "delete",
		RequestID:  reqID,
		StartedAt:  now,
		LastSeenAt: now,
	}
	r.fileMu.Unlock()

	payload := copyMap(data)
	payload["requestId"] = reqID
	ws.SendSignedCommand(r.store, clientID, "file_delete_request", payload, r.log)

	r.dash.SendTo(dc.ID, "file_delete_dispatched", map[string]any{
		"clientId":  clientID,
		"requestId": reqID,
		"path":      data["path"],
	})
}

func (r *Relay) handleFileDeleteResult(clientID string, data map[string]any) {
	reqID := str(data, "requestId")
	r.fileMu.Lock()
	op, ok := r.fileOps[reqID]
	if ok {
		delete(r.fileOps, reqID)
	}
	r.fileMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(op.DashConnID, "file_delete_result", mergeClientID(clientID, data))
}

func (r *Relay) handleFileChmodRequest(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "file_chmod_result", map[string]any{"error": "client offline"})
		return
	}

	if str(data, "mode") == "" {
		r.dash.SendTo(dc.ID, "file_chmod_result", map[string]any{"error": "mode required"})
		return
	}

	reqID := str(data, "requestId")
	if reqID == "" {
		reqID = uuid.NewString()
	}

	now := time.Now()
	r.fileMu.Lock()
	r.fileOps[reqID] = &fileOp{
		ClientID:   clientID,
		DashConnID: dc.ID,
		Type:       "chmod",
		RequestID:  reqID,
		StartedAt:  now,
		LastSeenAt: now,
	}
	r.fileMu.Unlock()

	payload := copyMap(data)
	payload["requestId"] = reqID
	ws.SendSignedCommand(r.store, clientID, "file_chmod_request", payload, r.log)

	r.dash.SendTo(dc.ID, "file_chmod_dispatched", map[string]any{
		"clientId":  clientID,
		"requestId": reqID,
		"path":      data["path"],
		"mode":      data["mode"],
	})
}

func (r *Relay) handleFileChmodResult(clientID string, data map[string]any) {
	reqID := str(data, "requestId")
	r.fileMu.Lock()
	op, ok := r.fileOps[reqID]
	if ok {
		delete(r.fileOps, reqID)
	}
	r.fileMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(op.DashConnID, "file_chmod_result", mergeClientID(clientID, data))
}

// --- Dir browse handlers ---

func (r *Relay) handleDirListRequest(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" {
		r.dash.SendTo(dc.ID, "dir_list_response", map[string]any{"error": "clientId required", "entries": []any{}})
		return
	}
	if !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "dir_list_response", map[string]any{"error": "client offline", "clientId": clientID, "entries": []any{}})
		return
	}

	reqID := str(data, "requestId")
	if reqID == "" {
		reqID = uuid.NewString()
	}

	mode := str(data, "mode")
	if mode == "" {
		mode = "local"
	}

	payload := copyMap(data)
	payload["requestId"] = reqID
	payload["mode"] = mode
	if data["port"] == nil {
		payload["port"] = 22
	}
	ws.SendSignedCommand(r.store, clientID, "dir_list_request", payload, r.log)

	r.dash.SendTo(dc.ID, "dir_list_dispatched", map[string]any{
		"clientId":  clientID,
		"requestId": reqID,
		"path":      data["path"],
		"mode":      mode,
	})
}

func (r *Relay) handleDirListResponse(clientID string, data map[string]any) {
	r.dash.Broadcast("dir_list_response", mergeClientID(clientID, data))
}

// --- Kiosk handlers ---

func (r *Relay) handleKioskSet(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "client offline"})
		return
	}

	content, ok := data["content"].(map[string]any)
	if !ok {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content required"})
		return
	}

	kind := str(content, "kind")
	if kind != "blank" && kind != "dashboard" && kind != "message" && kind != "url" && kind != "page" {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.kind must be blank, dashboard, message, url, or page"})
		return
	}

	// Validate kind-specific fields
	switch kind {
	case "message":
		text := str(content, "text")
		title := str(content, "title")
		if len(text) > 10000 {
			r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.text exceeds 10,000 chars"})
			return
		}
		if len(title) > 500 {
			r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.title exceeds 500 chars"})
			return
		}
	case "url":
		u := str(content, "url")
		if len(u) > 2048 {
			r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.url exceeds 2,048 chars"})
			return
		}
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.url must be http or https"})
			return
		}
	case "page":
		layout := str(content, "layout")
		if layout == "" || len(layout) > 64 {
			r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "content.layout required (max 64 chars)"})
			return
		}
	}

	reqID := str(data, "requestId")
	if reqID == "" {
		reqID = uuid.NewString()
	}

	// Build deterministic payload with only kind-specific fields
	cleanContent := map[string]any{"kind": kind}
	switch kind {
	case "message":
		cleanContent["text"] = str(content, "text")
		cleanContent["title"] = str(content, "title")
	case "url":
		cleanContent["url"] = str(content, "url")
	case "page":
		cleanContent["layout"] = str(content, "layout")
		if widgets, ok := content["widgets"]; ok {
			cleanContent["widgets"] = widgets
		}
	}
	if units := str(content, "units"); units == "imperial" || units == "metric" {
		cleanContent["units"] = units
	}

	ws.SendSignedCommand(r.store, clientID, "kiosk_set",
		map[string]any{"requestId": reqID, "content": cleanContent}, r.log)

	r.dash.SendTo(dc.ID, "kiosk_set_dispatched", map[string]any{
		"clientId":  clientID,
		"requestId": reqID,
		"content":   cleanContent,
	})
}

func (r *Relay) handleKioskStatus(clientID string, data map[string]any) {
	kiosk, _ := data["kiosk"].(map[string]any)
	if kiosk != nil {
		r.store.SetKioskStatus(clientID, kiosk)
	}
	r.dash.Broadcast("kiosk_status", map[string]any{
		"clientId": clientID,
		"kiosk":    kiosk,
		"ts":       nowISO(),
	})
}

func (r *Relay) handleKioskSaveLayout(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "client offline"})
		return
	}

	layout := str(data, "layout")
	if layout == "" || len(layout) > 64 {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "layout name required (max 64 chars)"})
		return
	}

	payload := map[string]any{
		"layout": layout,
	}
	if cols, ok := data["cols"]; ok {
		payload["cols"] = cols
	}
	if rows, ok := data["rows"]; ok {
		payload["rows"] = rows
	}
	if widgets, ok := data["widgets"]; ok {
		payload["widgets"] = widgets
	}
	if units := str(data, "units"); units == "imperial" || units == "metric" {
		payload["units"] = units
	}

	ws.SendSignedCommand(r.store, clientID, "kiosk_save_layout", payload, r.log)
}

func (r *Relay) handleKioskGetLayouts(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "kiosk_error", map[string]any{"error": "client offline"})
		return
	}

	ws.SendSignedCommand(r.store, clientID, "kiosk_get_layouts", map[string]any{}, r.log)
}

// --- Variant handlers ---

func (r *Relay) handleSwitchVariant(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "variant_error", map[string]any{"error": "client offline"})
		return
	}

	variant := strings.ToLower(str(data, "variant"))
	if variant != "headless" && variant != "kiosk" {
		r.dash.SendTo(dc.ID, "variant_error", map[string]any{"error": "variant must be headless or kiosk"})
		return
	}

	tag := str(data, "tag")

	ws.SendSignedCommand(r.store, clientID, "switch_variant",
		map[string]any{
			"variant": variant,
			"repo":    "austinkregel/compute-agent",
			"tag":     tag,
		}, r.log)

	r.dash.SendTo(dc.ID, "variant_switch_dispatched", map[string]any{
		"clientId": clientID,
		"variant":  variant,
		"tag":      tag,
		"ts":       nowISO(),
	})
}

func (r *Relay) handleVariantStatus(clientID string, data map[string]any) {
	r.store.SetVariantStatus(clientID, data)
	payload := mergeClientID(clientID, data)
	payload["ts"] = nowISO()
	r.dash.Broadcast("variant_status", payload)
}

func (r *Relay) handleVariantSwitchResult(clientID string, data map[string]any) {
	payload := mergeClientID(clientID, data)
	payload["ts"] = nowISO()
	r.dash.Broadcast("variant_switch_result", payload)
}

// --- Update check handlers ---

func (r *Relay) handleCheckUpdates(dc *ws.DashboardConn, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" || !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "check_updates_error", map[string]any{"message": "client offline"})
		return
	}

	ws.SendSignedCommand(r.store, clientID, "check_updates",
		map[string]any{"at": nowISO()}, r.log)

	r.dash.SendTo(dc.ID, "check_updates_dispatched", map[string]any{
		"clientId": clientID,
		"ts":       nowISO(),
	})
}

func (r *Relay) handleAgentUpdateResult(clientID string, data map[string]any) {
	r.log.Info("agent update result",
		"clientId", clientID,
		"ok", data["ok"],
		"tag", data["tag"],
		"error", data["error"],
	)
	payload := mergeClientID(clientID, data)
	payload["ts"] = nowISO()
	r.dash.Broadcast("agent_update_result", payload)
}

// --- Admin result handler ---

func (r *Relay) handleAdminResult(clientID string, data map[string]any) {
	r.log.Debug("admin result received", "clientId", clientID)

	result, _ := data["result"].(map[string]any)
	command := str(data, "command")
	if command == "" {
		command = "admin"
	}

	var stdout, stderr string
	var exitCode int
	if result != nil {
		stdout = str(result, "stdout")
		stderr = str(result, "stderr")
		if stderr == "" {
			stderr = str(result, "error")
		}
		summary, _ := result["summary"].(map[string]any)
		if summary != nil {
			exitCode = intVal(summary, "code", 0)
		}
	}
	success := exitCode == 0

	// Resolve any pending synchronous waiter (e.g. remote cron HTTP handler)
	token := str(data, "token")
	if token != "" {
		r.pendingMu.Lock()
		ch, ok := r.pendingResults[token]
		if ok {
			delete(r.pendingResults, token)
		}
		r.pendingMu.Unlock()
		if ok {
			ch <- adminResult{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}
		}
	}

	ts := nowISO()

	r.dash.Broadcast("command_output", map[string]any{
		"clientId":  clientID,
		"command":   command,
		"output":    stdout,
		"error":     stderr,
		"exitCode":  exitCode,
		"success":   success,
		"timestamp": ts,
	})
	r.dash.Broadcast("command_complete", map[string]any{
		"clientId":  clientID,
		"command":   command,
		"exitCode":  exitCode,
		"success":   success,
		"timestamp": ts,
	})
}

// --- Keys sync result handler ---

func (r *Relay) handleKeysSyncResult(clientID string, data map[string]any) {
	// Broadcast raw result
	r.dash.Broadcast("keys_sync_result", mergeClientID(clientID, data))

	// Also emit command_output/command_complete for unified command display
	success := boolVal(data, "ok")
	var output, errMsg string
	if success {
		added := intVal(data, "added", 0)
		user := str(data, "user")
		if user == "" {
			user = "unknown"
		}
		ms := intVal(data, "ms", 0)
		output = fmt.Sprintf("Synced %d key(s) for %s in %dms", added, user, ms)
	} else {
		errMsg = str(data, "error")
		if errMsg == "" {
			errMsg = "Keys sync failed"
		}
	}

	exitCode := 0
	if !success {
		exitCode = 1
	}
	ts := nowISO()

	r.dash.Broadcast("command_output", map[string]any{
		"clientId":  clientID,
		"command":   "keys_sync",
		"output":    output,
		"error":     errMsg,
		"exitCode":  exitCode,
		"success":   success,
		"timestamp": ts,
	})
	r.dash.Broadcast("command_complete", map[string]any{
		"clientId":  clientID,
		"command":   "keys_sync",
		"exitCode":  exitCode,
		"success":   success,
		"timestamp": ts,
	})
}

// --- Legacy shell_exit normalization ---

func (r *Relay) handleShellExit(clientID string, data map[string]any) {
	sessionID := str(data, "session")
	code := intVal(data, "code", -1)
	reason := str(data, "reason")
	if reason == "" {
		if code == 0 {
			reason = "exit 0"
		} else if code >= 0 {
			reason = fmt.Sprintf("exit %d", code)
		} else {
			reason = "closed"
		}
	}

	r.shellMu.Lock()
	sess, ok := r.shellSessions[sessionID]
	if ok {
		delete(r.shellSessions, sessionID)
	}
	r.shellMu.Unlock()
	if !ok {
		return
	}

	r.dash.SendTo(sess.DashConnID, "shell_closed", map[string]any{
		"clientId": clientID,
		"session":  sessionID,
		"code":     code,
		"reason":   reason,
		"signal":   data["signal"],
	})
}

// --- Docker/Swarm dashboard → agent forwarding ---

func (r *Relay) handleDockerDashboardEvent(dc *ws.DashboardConn, event string, data map[string]any) {
	clientID := str(data, "clientId")
	if clientID == "" {
		r.dash.SendTo(dc.ID, "docker_error", map[string]any{"error": "clientId required", "event": event})
		return
	}
	if !r.store.HasClient(clientID) {
		r.dash.SendTo(dc.ID, "docker_error", map[string]any{"error": "client offline", "clientId": clientID, "event": event})
		return
	}

	payload := copyMap(data)
	delete(payload, "clientId")

	agentCommand := strings.TrimSuffix(event, "_request")
	ws.SendSignedCommand(r.store, clientID, agentCommand, payload, r.log)

	r.dash.SendTo(dc.ID, event+"_dispatched", map[string]any{
		"clientId": clientID,
		"ts":       nowISO(),
	})
}

// --- Network status handler ---

func (r *Relay) handleNetStatus(clientID string, data map[string]any) {
	entry := r.store.GetClient(clientID)
	if entry != nil {
		entry.Mu.Lock()
		entry.LastPong = time.Now()
		entry.Mu.Unlock()
	}
}

// --- Helpers ---

func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func intVal(m map[string]any, key string, def int) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, err := v.Int64()
		if err == nil {
			return int(n)
		}
	}
	return def
}

func boolVal(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mergeClientID(clientID string, data map[string]any) map[string]any {
	out := copyMap(data)
	out["clientId"] = clientID
	return out
}
