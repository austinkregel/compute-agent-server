package state

import (
	"sync"
	"testing"
	"time"
)

func TestStore_AddRemoveClient(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	if !s.HasClient("node-1") {
		t.Error("HasClient(node-1) = false after Add")
	}

	s.RemoveClient("node-1")
	if s.HasClient("node-1") {
		t.Error("HasClient(node-1) = true after Remove")
	}
}

func TestStore_ClientIDs(t *testing.T) {
	s := New()
	s.AddClient("a", nil)
	s.AddClient("b", nil)

	ids := s.ClientIDs()
	if len(ids) != 2 {
		t.Fatalf("ClientIDs() len = %d, want 2", len(ids))
	}
	found := map[string]bool{}
	for _, id := range ids {
		found[id] = true
	}
	if !found["a"] || !found["b"] {
		t.Errorf("ClientIDs() = %v, missing a or b", ids)
	}
}

func TestStore_PublicClients(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)
	entry := s.GetClient("node-1")
	entry.Mu.Lock()
	entry.Platform = "linux"
	entry.Hostname = "myhost"
	entry.Authenticated = true
	entry.Mu.Unlock()

	clients := s.PublicClients()
	if len(clients) != 1 {
		t.Fatalf("PublicClients() len = %d, want 1", len(clients))
	}
	pub := clients[0]
	if pub.ClientID != "node-1" {
		t.Errorf("ClientID = %q", pub.ClientID)
	}
	if pub.Platform != "linux" {
		t.Errorf("Platform = %q", pub.Platform)
	}
	if pub.Hostname != "myhost" {
		t.Errorf("Hostname = %q", pub.Hostname)
	}
}

func TestStore_UpdateStats(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	stats := map[string]any{
		"platform": "linux",
		"hostname": "testhost",
		"cpus":     float64(4),
		"arch":     "x86_64",
	}
	s.UpdateStats("node-1", stats)

	cached := s.GetStats("node-1")
	if cached == nil {
		t.Fatal("GetStats() = nil after UpdateStats")
	}
	if cached["platform"] != "linux" {
		t.Errorf("cached platform = %v", cached["platform"])
	}
}

func TestStore_UpdateStats_ParsesDirectAdvert(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	changed := s.UpdateStats("node-1", map[string]any{
		"direct": map[string]any{
			"addr":        "100.64.0.5:7420",
			"certSha256":  "abc123",
			"pinRequired": true,
			"scheme":      "wss",
		},
	})
	if !changed {
		t.Error("UpdateStats should report changed when a direct advert appears")
	}

	pub := s.PublicClients()
	if len(pub) != 1 {
		t.Fatalf("PublicClients len = %d", len(pub))
	}
	if pub[0].DirectAddr != "100.64.0.5:7420" || pub[0].DirectCertSHA256 != "abc123" || !pub[0].DirectPinRequired {
		t.Errorf("direct fields not surfaced: %+v", pub[0])
	}

	// Withdrawing the advert clears the fields and reports changed.
	changed = s.UpdateStats("node-1", map[string]any{"cpu": float64(1)})
	if !changed {
		t.Error("UpdateStats should report changed when a direct advert is withdrawn")
	}
	if s.PublicClients()[0].DirectAddr != "" {
		t.Error("direct addr should be cleared when advert withdrawn")
	}
}

func TestStore_StatsHistoryBounded(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	for i := 0; i < 30; i++ {
		s.UpdateStats("node-1", map[string]any{"i": float64(i)})
	}

	history := s.GetStatsHistory("node-1")
	if len(history) != StatsHistoryLimit {
		t.Errorf("history len = %d, want %d", len(history), StatsHistoryLimit)
	}
}

func TestStore_UpdateStatsPopulatesClientFields(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	stats := map[string]any{
		"platform": "linux",
		"hostname": "myhost",
		"arch":     "amd64",
		"release":  "6.1.0",
	}
	s.UpdateStats("node-1", stats)

	entry := s.GetClient("node-1")
	entry.Mu.Lock()
	defer entry.Mu.Unlock()
	if entry.Platform != "linux" {
		t.Errorf("Platform = %q, want linux", entry.Platform)
	}
	if entry.Hostname != "myhost" {
		t.Errorf("Hostname = %q, want myhost", entry.Hostname)
	}
	if entry.Arch != "amd64" {
		t.Errorf("Arch = %q, want amd64", entry.Arch)
	}
}

func TestStore_Alerts(t *testing.T) {
	s := New()
	alerts := map[string]any{
		"totalCount":  float64(3),
		"hasCritical": true,
		"alerts":      []any{},
	}
	s.SetAlerts("node-1", alerts)

	cached := s.GetAlerts("node-1")
	if cached == nil {
		t.Fatal("GetAlerts() = nil after SetAlerts")
	}
}

func TestStore_ShellSessions(t *testing.T) {
	s := New()
	s.AddShellSession("sess-1", "node-1", nil)

	sess := s.GetShellSession("sess-1")
	if sess == nil {
		t.Fatal("GetShellSession() = nil after Add")
	}
	if sess.ClientID != "node-1" {
		t.Errorf("ClientID = %q", sess.ClientID)
	}

	s.RemoveShellSession("sess-1")
	if s.GetShellSession("sess-1") != nil {
		t.Error("GetShellSession() should be nil after Remove")
	}
}

func TestStore_ShellSessionsByClient(t *testing.T) {
	s := New()
	s.AddShellSession("sess-1", "node-1", nil)
	s.AddShellSession("sess-2", "node-1", nil)
	s.AddShellSession("sess-3", "node-2", nil)

	sessions := s.ShellSessionsByClient("node-1")
	if len(sessions) != 2 {
		t.Errorf("ShellSessionsByClient(node-1) len = %d, want 2", len(sessions))
	}
}

func TestStore_BackupJobs(t *testing.T) {
	s := New()
	job := &BackupJob{
		ClientID: "node-1",
		PlanID:   "plan-123",
		Status:   "planning",
	}
	s.SetBackupJob("plan-123", job)

	got := s.GetBackupJob("plan-123")
	if got == nil {
		t.Fatal("GetBackupJob() = nil after Set")
	}
	if got.Status != "planning" {
		t.Errorf("Status = %q", got.Status)
	}
}

func TestStore_PendingFileOps(t *testing.T) {
	s := New()
	op := &PendingFileOp{
		ClientID:  "node-1",
		Type:      "put",
		StartedAt: time.Now(),
	}
	s.SetPendingFileOp("req-1", op)

	got := s.GetPendingFileOp("req-1")
	if got == nil {
		t.Fatal("GetPendingFileOp() = nil after Set")
	}
	if got.Type != "put" {
		t.Errorf("Type = %q", got.Type)
	}

	s.RemovePendingFileOp("req-1")
	if s.GetPendingFileOp("req-1") != nil {
		t.Error("GetPendingFileOp() should be nil after Remove")
	}
}

func TestStore_CleanStaleFileOps(t *testing.T) {
	s := New()
	stale := &PendingFileOp{
		ClientID:  "node-1",
		Type:      "put",
		StartedAt: time.Now().Add(-10 * time.Minute),
	}
	fresh := &PendingFileOp{
		ClientID:  "node-2",
		Type:      "delete",
		StartedAt: time.Now(),
	}
	s.SetPendingFileOp("stale-1", stale)
	s.SetPendingFileOp("fresh-1", fresh)

	s.CleanStaleFileOps(5 * time.Minute)

	if s.GetPendingFileOp("stale-1") != nil {
		t.Error("stale op should have been cleaned")
	}
	if s.GetPendingFileOp("fresh-1") == nil {
		t.Error("fresh op should not have been cleaned")
	}
}

func TestStore_KioskStatus(t *testing.T) {
	s := New()
	status := map[string]any{
		"running":   true,
		"connected": true,
		"content":   map[string]any{"kind": "blank"},
	}
	s.SetKioskStatus("node-1", status)

	got := s.GetKioskStatus("node-1")
	if got == nil {
		t.Fatal("GetKioskStatus() = nil")
	}
}

func TestStore_VariantStatus(t *testing.T) {
	s := New()
	status := map[string]any{
		"current":        "headless",
		"desired":        "kiosk",
		"kioskAvailable": false,
	}
	s.SetVariantStatus("node-1", status)

	got := s.GetVariantStatus("node-1")
	if got == nil {
		t.Fatal("GetVariantStatus() = nil")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := New()
	var wg sync.WaitGroup

	// Concurrent add/remove/read
	for i := 0; i < 100; i++ {
		wg.Add(3)
		id := "node-" + string(rune('A'+i%26))
		go func() {
			defer wg.Done()
			s.AddClient(id, nil)
		}()
		go func() {
			defer wg.Done()
			_ = s.ClientIDs()
		}()
		go func() {
			defer wg.Done()
			s.UpdateStats(id, map[string]any{"i": float64(1)})
		}()
	}
	wg.Wait()
}

func TestStore_LogTailSessions(t *testing.T) {
	s := New()
	s.AddLogTailSession("lt-1", "node-1", nil)

	sess := s.GetLogTailSession("lt-1")
	if sess == nil {
		t.Fatal("GetLogTailSession() = nil after Add")
	}
	if sess.ClientID != "node-1" {
		t.Errorf("ClientID = %q", sess.ClientID)
	}

	s.RemoveLogTailSession("lt-1")
	if s.GetLogTailSession("lt-1") != nil {
		t.Error("GetLogTailSession() should be nil after Remove")
	}
}
