package relay

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/austinkregel/backup-server/internal/auth"
	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/backup-server/internal/ws"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

// --- Mock DashSender ---

type sentMsg struct {
	Event string
	Data  map[string]any
}

type mockDash struct {
	mu         sync.Mutex
	sent       map[string][]sentMsg // connID → messages
	broadcasts []sentMsg
}

func newMockDash() *mockDash {
	return &mockDash{sent: make(map[string][]sentMsg)}
}

func (m *mockDash) Broadcast(event string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, sentMsg{Event: event, Data: toMap(data)})
}

func (m *mockDash) SendTo(connID string, event string, data any) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent[connID] = append(m.sent[connID], sentMsg{Event: event, Data: toMap(data)})
	return true
}

func (m *mockDash) getSent(connID string) []sentMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMsg{}, m.sent[connID]...)
}

func (m *mockDash) findBroadcast(event string) *sentMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.broadcasts {
		if m.broadcasts[i].Event == event {
			return &m.broadcasts[i]
		}
	}
	return nil
}

func (m *mockDash) findSent(connID, event string) *sentMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.sent[connID] {
		if m.sent[connID][i].Event == event {
			return &m.sent[connID][i]
		}
	}
	return nil
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

// --- Setup ---

func testRelay(t *testing.T) (*Relay, *mockDash, *state.Store) {
	t.Helper()
	store := state.New()
	log, err := logging.New(logging.Options{Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Sync() })

	md := newMockDash()
	r := New(store, log, md, t.TempDir())
	return r, md, store
}

func newMockDC(id string) *ws.DashboardConn {
	return &ws.DashboardConn{
		ID:   id,
		User: &auth.SessionUser{Sub: "test-user", Email: "test@example.com"},
	}
}

func makeMsg(event string, data map[string]any) *ws.Message {
	raw, _ := json.Marshal(data)
	return &ws.Message{Event: event, Data: raw}
}

// --- Shell tests ---

func TestShellStart_Success(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))

	r.shellMu.RLock()
	count := len(r.shellSessions)
	var sess *shellSession
	for _, s := range r.shellSessions {
		sess = s
	}
	r.shellMu.RUnlock()

	if count != 1 {
		t.Fatalf("shell sessions = %d, want 1", count)
	}
	if sess.ClientID != "node-1" {
		t.Errorf("clientID = %q", sess.ClientID)
	}
	if sess.DashConnID != "dash-1" {
		t.Errorf("dashConnID = %q", sess.DashConnID)
	}
}

func TestShellStart_ClientOffline(t *testing.T) {
	r, _, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))

	r.shellMu.RLock()
	count := len(r.shellSessions)
	r.shellMu.RUnlock()
	if count != 0 {
		t.Errorf("shell sessions = %d, want 0 (client offline)", count)
	}
}

func TestShellClose_RemovesSession(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))

	r.shellMu.RLock()
	var sessionID string
	for id := range r.shellSessions {
		sessionID = id
	}
	r.shellMu.RUnlock()

	r.HandleDashboardEvent(dc, makeMsg("shell_close", map[string]any{"session": sessionID}))

	r.shellMu.RLock()
	count := len(r.shellSessions)
	r.shellMu.RUnlock()
	if count != 0 {
		t.Errorf("shell sessions = %d after close, want 0", count)
	}
}

func TestShellOutput_RoutedToOwner(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))

	r.shellMu.RLock()
	var sessionID string
	for id := range r.shellSessions {
		sessionID = id
	}
	r.shellMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("shell_output", map[string]any{
		"session": sessionID, "data": "hello world",
	}))

	msg := md.findSent("dash-1", "shell_output")
	if msg == nil {
		t.Fatal("shell_output not sent to dash-1")
	}
	if msg.Data["data"] != "hello world" {
		t.Errorf("data = %v", msg.Data["data"])
	}
}

func TestShellClosed_AgentSide(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))

	r.shellMu.RLock()
	var sessionID string
	for id := range r.shellSessions {
		sessionID = id
	}
	r.shellMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("shell_closed", map[string]any{
		"session": sessionID, "reason": "process exited",
	}))

	r.shellMu.RLock()
	count := len(r.shellSessions)
	r.shellMu.RUnlock()
	if count != 0 {
		t.Errorf("sessions = %d after agent close, want 0", count)
	}

	msg := md.findSent("dash-1", "shell_closed")
	if msg == nil {
		t.Error("shell_closed not sent to dashboard")
	}
}

// --- Log tail tests ---

func TestLogTailStart_Success(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{
		"clientId": "node-1", "lines": float64(50),
	}))

	r.logMu.RLock()
	count := len(r.logSessions)
	var sess *logTailSession
	for _, s := range r.logSessions {
		sess = s
	}
	r.logMu.RUnlock()

	if count != 1 {
		t.Fatalf("log sessions = %d, want 1", count)
	}
	if sess.Lines != 50 {
		t.Errorf("lines = %d, want 50", sess.Lines)
	}
}

func TestLogTailStart_ClampLines(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{
		"clientId": "node-1", "lines": float64(999),
	}))

	r.logMu.RLock()
	var sess *logTailSession
	for _, s := range r.logSessions {
		sess = s
	}
	r.logMu.RUnlock()

	if sess.Lines != 200 {
		t.Errorf("lines = %d, want 200 (clamped)", sess.Lines)
	}
}

func TestLogTailStop_BySession(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{
		"clientId": "node-1", "lines": float64(10),
	}))

	r.logMu.RLock()
	var sessionID string
	for id := range r.logSessions {
		sessionID = id
	}
	r.logMu.RUnlock()

	r.HandleDashboardEvent(dc, makeMsg("log_tail_stop", map[string]any{"session": sessionID}))

	r.logMu.RLock()
	count := len(r.logSessions)
	r.logMu.RUnlock()
	if count != 0 {
		t.Errorf("log sessions = %d after stop, want 0", count)
	}
}

func TestLogTailStop_ByClientID(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{"clientId": "node-1", "lines": float64(10)}))
	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{"clientId": "node-1", "lines": float64(20)}))

	r.logMu.RLock()
	count := len(r.logSessions)
	r.logMu.RUnlock()
	if count != 2 {
		t.Fatalf("expected 2 sessions, got %d", count)
	}

	r.HandleDashboardEvent(dc, makeMsg("log_tail_stop", map[string]any{"clientId": "node-1"}))

	r.logMu.RLock()
	count = len(r.logSessions)
	r.logMu.RUnlock()
	if count != 0 {
		t.Errorf("log sessions = %d after bulk stop, want 0", count)
	}
}

func TestLogTailOutput_Routed(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{"clientId": "node-1", "lines": float64(10)}))

	r.logMu.RLock()
	var sessionID string
	for id := range r.logSessions {
		sessionID = id
	}
	r.logMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("log_tail_output", map[string]any{
		"session": sessionID, "data": "log line", "ts": "2024-01-01T00:00:00Z",
	}))

	msg := md.findSent("dash-1", "log_tail_output")
	if msg == nil {
		t.Fatal("log_tail_output not sent")
	}
	if msg.Data["data"] != "log line" {
		t.Errorf("data = %v", msg.Data["data"])
	}
}

// --- Backup tests ---

func TestBackupPlanRequest_Success(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("backup_plan_request", map[string]any{
		"clientId": "node-1", "destination": "/backups",
	}))

	r.backupMu.RLock()
	count := len(r.backupJobs)
	var job *backupJob
	for _, j := range r.backupJobs {
		job = j
	}
	r.backupMu.RUnlock()

	if count != 1 {
		t.Fatalf("backup jobs = %d, want 1", count)
	}
	if job.Status != "planning" {
		t.Errorf("status = %q, want planning", job.Status)
	}
	if job.ClientID != "node-1" {
		t.Errorf("clientID = %q", job.ClientID)
	}
}

func TestBackupApprove_Success(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("backup_plan_request", map[string]any{"clientId": "node-1"}))

	r.backupMu.RLock()
	var planID string
	for id := range r.backupJobs {
		planID = id
	}
	r.backupMu.RUnlock()

	r.HandleDashboardEvent(dc, makeMsg("backup_approve", map[string]any{"planId": planID}))

	r.backupMu.RLock()
	job := r.backupJobs[planID]
	r.backupMu.RUnlock()

	if job.Status != "running" {
		t.Errorf("status = %q, want running", job.Status)
	}

	msg := md.findBroadcast("backup_started")
	if msg == nil {
		t.Error("backup_started not broadcast")
	}
}

func TestBackupComplete(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("backup_plan_request", map[string]any{"clientId": "node-1"}))

	r.backupMu.RLock()
	var planID string
	for id := range r.backupJobs {
		planID = id
	}
	r.backupMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("backup_complete", map[string]any{
		"planId": planID, "ok": true, "ms": float64(1234),
	}))

	r.backupMu.RLock()
	job := r.backupJobs[planID]
	r.backupMu.RUnlock()

	if job.Status != "completed" {
		t.Errorf("status = %q, want completed", job.Status)
	}

	msg := md.findBroadcast("backup_complete")
	if msg == nil {
		t.Error("backup_complete not broadcast")
	}
}

func TestBackupProgress_IncrementsCount(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("backup_plan_request", map[string]any{"clientId": "node-1"}))

	r.backupMu.RLock()
	var planID string
	for id := range r.backupJobs {
		planID = id
	}
	r.backupMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("backup_progress", map[string]any{
		"planId": planID, "file": "test.txt", "percent": float64(50),
	}))
	r.HandleAgentEvent("node-1", makeMsg("backup_progress", map[string]any{
		"planId": planID, "file": "test2.txt", "percent": float64(100),
	}))

	r.backupMu.RLock()
	job := r.backupJobs[planID]
	r.backupMu.RUnlock()
	if job.FilesCompleted != 2 {
		t.Errorf("filesCompleted = %d, want 2", job.FilesCompleted)
	}

	// Last broadcast should have filesCompleted
	msgs := md.broadcasts
	var lastProgress *sentMsg
	for i := range msgs {
		if msgs[i].Event == "backup_progress" {
			lastProgress = &msgs[i]
		}
	}
	if lastProgress == nil {
		t.Fatal("backup_progress not broadcast")
	}
}

// --- File ops tests ---

func TestFilePutStart_CreatesOp(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_put_start", map[string]any{
		"clientId": "node-1", "path": "/tmp/test.txt", "size": float64(1024),
	}))

	r.fileMu.RLock()
	count := len(r.fileOps)
	var op *fileOp
	for _, o := range r.fileOps {
		op = o
	}
	r.fileMu.RUnlock()

	if count != 1 {
		t.Fatalf("file ops = %d, want 1", count)
	}
	if op.Type != "put" {
		t.Errorf("type = %q", op.Type)
	}
	if op.DashConnID != "dash-1" {
		t.Errorf("dashConnID = %q", op.DashConnID)
	}
}

func TestFilePutResult_RoutedToOwner(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_put_start", map[string]any{
		"clientId": "node-1", "path": "/tmp/test.txt",
	}))

	r.fileMu.RLock()
	var reqID string
	for id := range r.fileOps {
		reqID = id
	}
	r.fileMu.RUnlock()

	r.HandleAgentEvent("node-1", makeMsg("file_put_result", map[string]any{
		"requestId": reqID, "ok": true, "path": "/tmp/test.txt",
	}))

	msg := md.findSent("dash-1", "file_put_result")
	if msg == nil {
		t.Fatal("file_put_result not sent to dash-1")
	}
	if msg.Data["ok"] != true {
		t.Errorf("ok = %v", msg.Data["ok"])
	}

	r.fileMu.RLock()
	count := len(r.fileOps)
	r.fileMu.RUnlock()
	if count != 0 {
		t.Errorf("file ops = %d after result, want 0", count)
	}
}

func TestFileGetRequest_CreatesOp(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_get_request", map[string]any{
		"clientId": "node-1", "path": "/tmp/read.txt",
	}))

	r.fileMu.RLock()
	count := len(r.fileOps)
	var op *fileOp
	for _, o := range r.fileOps {
		op = o
	}
	r.fileMu.RUnlock()

	if count != 1 {
		t.Fatalf("file ops = %d, want 1", count)
	}
	if op.Type != "get" {
		t.Errorf("type = %q, want get", op.Type)
	}
	if op.DashConnID != "dash-1" {
		t.Errorf("dashConnID = %q", op.DashConnID)
	}
}

func TestFileGetRequest_ClientOffline(t *testing.T) {
	r, md, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_get_request", map[string]any{
		"clientId": "ghost", "path": "/tmp/read.txt", "requestId": "req-1",
	}))

	msg := md.findSent("dash-1", "file_get_result")
	if msg == nil {
		t.Fatal("file_get_result (offline) not sent to dash-1")
	}
	if msg.Data["ok"] != false {
		t.Errorf("ok = %v, want false", msg.Data["ok"])
	}

	r.fileMu.RLock()
	count := len(r.fileOps)
	r.fileMu.RUnlock()
	if count != 0 {
		t.Errorf("file ops = %d, want 0 (client offline)", count)
	}
}

func TestFileGetChunkAndResult_RoutedToOwner(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_get_request", map[string]any{
		"clientId": "node-1", "path": "/tmp/read.txt",
	}))

	r.fileMu.RLock()
	var reqID string
	for id := range r.fileOps {
		reqID = id
	}
	r.fileMu.RUnlock()

	// Streamed chunk routes back to the requesting dashboard, op stays open.
	r.HandleAgentEvent("node-1", makeMsg("file_get_chunk", map[string]any{
		"requestId": reqID, "offset": float64(0), "data": "aGVsbG8=",
	}))
	chunk := md.findSent("dash-1", "file_get_chunk")
	if chunk == nil {
		t.Fatal("file_get_chunk not routed to dash-1")
	}
	if chunk.Data["data"] != "aGVsbG8=" {
		t.Errorf("chunk data = %v", chunk.Data["data"])
	}
	r.fileMu.RLock()
	stillOpen := len(r.fileOps)
	r.fileMu.RUnlock()
	if stillOpen != 1 {
		t.Errorf("file ops = %d after chunk, want 1 (still streaming)", stillOpen)
	}

	// Terminal result routes back and clears the op.
	r.HandleAgentEvent("node-1", makeMsg("file_get_result", map[string]any{
		"requestId": reqID, "ok": true, "path": "/tmp/read.txt", "size": float64(5),
	}))
	result := md.findSent("dash-1", "file_get_result")
	if result == nil {
		t.Fatal("file_get_result not routed to dash-1")
	}
	if result.Data["ok"] != true {
		t.Errorf("ok = %v, want true", result.Data["ok"])
	}

	r.fileMu.RLock()
	count := len(r.fileOps)
	r.fileMu.RUnlock()
	if count != 0 {
		t.Errorf("file ops = %d after result, want 0", count)
	}
}

func TestFileGetChunk_UnknownRequestIgnored(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)

	// A chunk for a requestId with no registered op must be dropped silently.
	r.HandleAgentEvent("node-1", makeMsg("file_get_chunk", map[string]any{
		"requestId": "nope", "offset": float64(0), "data": "eA==",
	}))
	if msg := md.findSent("dash-1", "file_get_chunk"); msg != nil {
		t.Error("orphan file_get_chunk should not be routed anywhere")
	}
}

func TestFileDeleteRequest(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_delete_request", map[string]any{
		"clientId": "node-1", "path": "/tmp/test.txt", "force": true,
	}))

	r.fileMu.RLock()
	count := len(r.fileOps)
	var op *fileOp
	for _, o := range r.fileOps {
		op = o
	}
	r.fileMu.RUnlock()

	if count != 1 {
		t.Fatalf("file ops = %d, want 1", count)
	}
	if op.Type != "delete" {
		t.Errorf("type = %q, want delete", op.Type)
	}
}

func TestFileChmodRequest_MissingMode(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("file_chmod_request", map[string]any{
		"clientId": "node-1", "path": "/tmp/test.txt",
	}))

	r.fileMu.RLock()
	count := len(r.fileOps)
	r.fileMu.RUnlock()
	if count != 0 {
		t.Errorf("file ops = %d, want 0 (missing mode)", count)
	}
}

// --- Dir browse tests ---

func TestDirListRequest_Success(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("dir_list_request", map[string]any{
		"clientId": "node-1", "path": "/home",
	}))
	// Should not panic
}

func TestDirListResponse_Broadcast(t *testing.T) {
	r, md, _ := testRelay(t)

	r.HandleAgentEvent("node-1", makeMsg("dir_list_response", map[string]any{
		"requestId": "req-1", "path": "/home", "entries": []any{},
	}))

	msg := md.findBroadcast("dir_list_response")
	if msg == nil {
		t.Fatal("dir_list_response not broadcast")
	}
	if msg.Data["clientId"] != "node-1" {
		t.Errorf("clientId = %v", msg.Data["clientId"])
	}
}

// --- Kiosk tests ---

func TestKioskSet_ValidMessage(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "message", "title": "Hello", "text": "World"},
	}))
}

func TestKioskSet_ValidBlank(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "blank"},
	}))
}

func TestKioskSet_ValidURL(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "url", "url": "https://example.com"},
	}))
}

func TestKioskSet_ValidDashboard(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "dashboard"},
	}))
}

func TestKioskSet_InvalidKind(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "invalid"},
	}))
}

func TestKioskSet_URLNotHTTP(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "url", "url": "ftp://evil.com"},
	}))
}

func TestKioskStatus_BroadcastAndStore(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)

	r.HandleAgentEvent("node-1", makeMsg("kiosk_status", map[string]any{
		"kiosk": map[string]any{"running": true, "connected": true},
	}))

	msg := md.findBroadcast("kiosk_status")
	if msg == nil {
		t.Fatal("kiosk_status not broadcast")
	}
	if msg.Data["clientId"] != "node-1" {
		t.Errorf("clientId = %v", msg.Data["clientId"])
	}

	status := store.GetKioskStatus("node-1")
	if status == nil {
		t.Fatal("kiosk status not in store")
	}
}

func TestKioskSet_ValidPage(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "page", "layout": "ultrawide"},
	}))

	// Should not get an error (no kiosk_error sent back to dashboard)
	// Just verify no panic occurred
}

func TestKioskSet_PageMissingLayout(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content":  map[string]any{"kind": "page"},
	}))
}

func TestKioskSet_PageWithWidgets(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_set", map[string]any{
		"clientId": "node-1",
		"content": map[string]any{
			"kind":   "page",
			"layout": "custom",
			"widgets": []map[string]any{
				{"type": "stats-primary", "col": 1, "row": 1, "w": 1, "h": 1},
			},
		},
	}))
}

func TestKioskSaveLayout(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_save_layout", map[string]any{
		"clientId": "node-1",
		"layout":   "my-layout",
		"cols":     3,
		"rows":     3,
		"widgets": []map[string]any{
			{"type": "stats-primary", "col": 1, "row": 1, "w": 1, "h": 1},
		},
	}))
}

func TestKioskSaveLayout_ClientOffline(t *testing.T) {
	r, _, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_save_layout", map[string]any{
		"clientId": "missing",
		"layout":   "test",
	}))
}

func TestKioskGetLayouts(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("kiosk_get_layouts", map[string]any{
		"clientId": "node-1",
	}))
}

func TestKioskLayoutSaved_Broadcast(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)

	r.HandleAgentEvent("node-1", makeMsg("kiosk_layout_saved", map[string]any{
		"layout": "ultrawide",
		"ok":     true,
	}))

	msg := md.findBroadcast("kiosk_layout_saved")
	if msg == nil {
		t.Fatal("kiosk_layout_saved not broadcast")
	}
	if msg.Data["clientId"] != "node-1" {
		t.Errorf("clientId = %v", msg.Data["clientId"])
	}
}

func TestKioskLayouts_Broadcast(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)

	r.HandleAgentEvent("node-1", makeMsg("kiosk_layouts", map[string]any{
		"layouts": map[string]any{"ultrawide": map[string]any{"cols": 5, "rows": 3}},
	}))

	msg := md.findBroadcast("kiosk_layouts")
	if msg == nil {
		t.Fatal("kiosk_layouts not broadcast")
	}
}

// --- Variant tests ---

func TestSwitchVariant_Valid(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("switch_variant", map[string]any{
		"clientId": "node-1", "variant": "kiosk", "tag": "v1.0.0",
	}))
}

func TestSwitchVariant_InvalidVariant(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("switch_variant", map[string]any{
		"clientId": "node-1", "variant": "invalid",
	}))
}

func TestVariantStatus_BroadcastAndStore(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)

	r.HandleAgentEvent("node-1", makeMsg("variant_status", map[string]any{
		"current": "headless", "desired": "kiosk", "kioskAvailable": true,
	}))

	msg := md.findBroadcast("variant_status")
	if msg == nil {
		t.Fatal("variant_status not broadcast")
	}
	if msg.Data["clientId"] != "node-1" {
		t.Errorf("clientId = %v", msg.Data["clientId"])
	}

	status := store.GetVariantStatus("node-1")
	if status == nil {
		t.Fatal("variant status not in store")
	}
}

func TestVariantSwitchResult_Broadcast(t *testing.T) {
	r, md, _ := testRelay(t)

	r.HandleAgentEvent("node-1", makeMsg("variant_switch_result", map[string]any{
		"ok": true, "variant": "kiosk",
	}))

	msg := md.findBroadcast("variant_switch_result")
	if msg == nil {
		t.Fatal("variant_switch_result not broadcast")
	}
}

// --- Update check tests ---

func TestCheckUpdates_Success(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("check_updates_request", map[string]any{
		"clientId": "node-1",
	}))
}

func TestCheckUpdates_ClientOffline(t *testing.T) {
	r, _, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("check_updates_request", map[string]any{
		"clientId": "node-offline",
	}))
}

func TestAgentUpdateResult_Broadcast(t *testing.T) {
	r, md, _ := testRelay(t)

	r.HandleAgentEvent("node-1", makeMsg("agent_update_result", map[string]any{
		"ok": true, "tag": "v2.0.0",
	}))

	msg := md.findBroadcast("agent_update_result")
	if msg == nil {
		t.Fatal("agent_update_result not broadcast")
	}
	if msg.Data["tag"] != "v2.0.0" {
		t.Errorf("tag = %v", msg.Data["tag"])
	}
}

// --- Cleanup tests ---

func TestCleanupDashboard(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("shell_start", map[string]any{"clientId": "node-1"}))
	r.HandleDashboardEvent(dc, makeMsg("log_tail_start", map[string]any{"clientId": "node-1", "lines": float64(10)}))
	r.HandleDashboardEvent(dc, makeMsg("file_put_start", map[string]any{"clientId": "node-1", "path": "/tmp/x"}))

	r.shellMu.RLock()
	shells := len(r.shellSessions)
	r.shellMu.RUnlock()
	r.logMu.RLock()
	logs := len(r.logSessions)
	r.logMu.RUnlock()
	r.fileMu.RLock()
	files := len(r.fileOps)
	r.fileMu.RUnlock()

	if shells != 1 || logs != 1 || files != 1 {
		t.Fatalf("before cleanup: shells=%d, logs=%d, files=%d", shells, logs, files)
	}

	r.CleanupDashboard(dc)

	r.shellMu.RLock()
	shells = len(r.shellSessions)
	r.shellMu.RUnlock()
	r.logMu.RLock()
	logs = len(r.logSessions)
	r.logMu.RUnlock()
	r.fileMu.RLock()
	files = len(r.fileOps)
	r.fileMu.RUnlock()

	if shells != 0 || logs != 0 || files != 0 {
		t.Errorf("after cleanup: shells=%d, logs=%d, files=%d", shells, logs, files)
	}
}

func TestCleanupStaleFileOps(t *testing.T) {
	r, _, store := testRelay(t)
	store.AddClient("node-1", nil)

	r.fileMu.Lock()
	r.fileOps["stale-1"] = &fileOp{
		ClientID:   "node-1",
		DashConnID: "dash-1",
		Type:       "put",
		RequestID:  "stale-1",
		StartedAt:  time.Now().Add(-10 * time.Minute),
		LastSeenAt: time.Now().Add(-10 * time.Minute),
	}
	r.fileOps["fresh-1"] = &fileOp{
		ClientID:   "node-1",
		DashConnID: "dash-1",
		Type:       "put",
		RequestID:  "fresh-1",
		StartedAt:  time.Now(),
		LastSeenAt: time.Now(),
	}
	r.fileMu.Unlock()

	r.CleanupStaleFileOps()

	r.fileMu.RLock()
	_, hasStale := r.fileOps["stale-1"]
	_, hasFresh := r.fileOps["fresh-1"]
	r.fileMu.RUnlock()

	if hasStale {
		t.Error("stale op should have been cleaned up")
	}
	if !hasFresh {
		t.Error("fresh op should still exist")
	}
}

// --- Docker/Swarm relay tests ---

func TestDockerDashboardEvent_ForwardsToAgent(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("swarm_info_request", map[string]any{
		"clientId": "node-1",
	}))

	dispatched := md.findSent("dash-1", "swarm_info_request_dispatched")
	if dispatched == nil {
		t.Fatal("expected swarm_info_request_dispatched to be sent to dashboard")
	}
	if dispatched.Data["clientId"] != "node-1" {
		t.Errorf("clientId = %v", dispatched.Data["clientId"])
	}
}

func TestDockerDashboardEvent_ClientOffline(t *testing.T) {
	r, md, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("swarm_info_request", map[string]any{
		"clientId": "offline-node",
	}))

	errMsg := md.findSent("dash-1", "docker_error")
	if errMsg == nil {
		t.Fatal("expected docker_error for offline client")
	}
	if errMsg.Data["error"] != "client offline" {
		t.Errorf("error = %v", errMsg.Data["error"])
	}
}

func TestDockerDashboardEvent_MissingClientId(t *testing.T) {
	r, md, _ := testRelay(t)
	dc := newMockDC("dash-1")

	r.HandleDashboardEvent(dc, makeMsg("swarm_init_request", map[string]any{}))

	errMsg := md.findSent("dash-1", "docker_error")
	if errMsg == nil {
		t.Fatal("expected docker_error for missing clientId")
	}
	if errMsg.Data["error"] != "clientId required" {
		t.Errorf("error = %v", errMsg.Data["error"])
	}
}

func TestDockerAgentResponse_BroadcastsToDashboards(t *testing.T) {
	r, md, _ := testRelay(t)

	events := []string{
		"swarm_info_response",
		"swarm_init_result",
		"swarm_join_result",
		"swarm_leave_result",
		"swarm_node_list_response",
		"swarm_service_list_response",
		"swarm_network_list_response",
		"swarm_stack_list_response",
	}

	for _, event := range events {
		r.HandleAgentEvent("node-1", makeMsg(event, map[string]any{
			"success": true,
		}))

		found := md.findBroadcast(event)
		if found == nil {
			t.Errorf("expected broadcast of %q", event)
			continue
		}
		if found.Data["clientId"] != "node-1" {
			t.Errorf("%s: clientId = %v, want node-1", event, found.Data["clientId"])
		}
	}
}

func TestDockerDashboardEvent_AllEventTypes(t *testing.T) {
	r, md, store := testRelay(t)
	store.AddClient("node-1", nil)
	dc := newMockDC("dash-1")

	events := []string{
		"swarm_info_request",
		"swarm_init_request",
		"swarm_join_request",
		"swarm_leave_request",
		"swarm_node_list_request",
		"swarm_service_list_request",
		"swarm_service_logs_request",
		"swarm_network_list_request",
		"swarm_stack_list_request",
	}

	for _, event := range events {
		r.HandleDashboardEvent(dc, makeMsg(event, map[string]any{
			"clientId": "node-1",
		}))

		dispatched := md.findSent("dash-1", event+"_dispatched")
		if dispatched == nil {
			t.Errorf("expected %s_dispatched to be sent", event)
		}
	}
}
