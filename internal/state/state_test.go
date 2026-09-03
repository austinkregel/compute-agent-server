package state

import (
	"fmt"
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

func TestStore_UpdateStats_ParsesCapabilities(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)

	changed := s.UpdateStats("node-1", map[string]any{
		"capabilities": map[string]any{
			"docker": map[string]any{
				"state": "enabled",
				"meta":  map[string]any{"version": "24.0.0"},
			},
			"telephony": map[string]any{
				"state":    "unavailable",
				"detail":   "companion socket unreachable",
				"features": []any{"sms", "volte_bridge"},
			},
		},
	})
	if !changed {
		t.Error("UpdateStats should report changed when capabilities first appear")
	}

	pub := s.PublicClients()
	if len(pub) != 1 {
		t.Fatalf("PublicClients len = %d", len(pub))
	}
	caps := pub[0].Capabilities
	if caps["docker"].State != "enabled" {
		t.Errorf("docker capability state = %q, want enabled", caps["docker"].State)
	}
	if caps["docker"].Meta["version"] != "24.0.0" {
		t.Errorf("docker capability meta not surfaced: %+v", caps["docker"])
	}
	if caps["telephony"].State != "unavailable" || caps["telephony"].Detail != "companion socket unreachable" {
		t.Errorf("telephony capability not parsed: %+v", caps["telephony"])
	}
	if len(caps["telephony"].Features) != 2 || caps["telephony"].Features[0] != "sms" {
		t.Errorf("telephony features not parsed: %+v", caps["telephony"].Features)
	}

	// A second identical push should NOT report changed (no-op update).
	changed = s.UpdateStats("node-1", map[string]any{
		"capabilities": map[string]any{
			"docker": map[string]any{
				"state": "enabled",
				"meta":  map[string]any{"version": "24.0.0"},
			},
			"telephony": map[string]any{
				"state":    "unavailable",
				"detail":   "companion socket unreachable",
				"features": []any{"sms", "volte_bridge"},
			},
		},
	})
	if changed {
		t.Error("UpdateStats should not report changed for an identical capabilities push")
	}

	// A stats push with no capabilities key at all leaves prior capabilities intact.
	changed = s.UpdateStats("node-1", map[string]any{"cpu": float64(1)})
	if changed {
		t.Error("UpdateStats should not report changed when capabilities key is simply absent")
	}
	if s.PublicClients()[0].Capabilities["docker"].State != "enabled" {
		t.Error("capabilities should persist across stats pushes that omit the key")
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

// A dropped agent must stay on the roster as an offline entry rather than
// vanishing, while every reachability check keeps reporting it as gone.
func TestStore_DisconnectedClientStaysOnRosterAsOffline(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)
	entry := s.GetClient("node-1")
	entry.Mu.Lock()
	entry.Hostname = "box"
	entry.Platform = "linux"
	entry.Mu.Unlock()

	s.RemoveClient("node-1")

	// Reachability is unchanged: relay and api gate command routing on these.
	if s.HasClient("node-1") {
		t.Error("HasClient should be false once the socket is gone")
	}
	if s.ClientCount() != 0 {
		t.Errorf("ClientCount = %d, want 0", s.ClientCount())
	}
	if len(s.ClientIDs()) != 0 {
		t.Errorf("ClientIDs = %v, want empty", s.ClientIDs())
	}
	if len(s.AllClients()) != 0 {
		t.Errorf("AllClients len = %d, want 0", len(s.AllClients()))
	}

	// But the dashboard roster still carries it, flagged offline and keeping
	// the metadata it last reported.
	clients := s.PublicClients()
	if len(clients) != 1 {
		t.Fatalf("PublicClients len = %d, want 1 offline entry", len(clients))
	}
	if clients[0].Connected {
		t.Error("retained entry should be Connected=false")
	}
	if clients[0].Hostname != "box" || clients[0].Platform != "linux" {
		t.Errorf("last-known metadata lost: %+v", clients[0])
	}
	if clients[0].DisconnectedAt == 0 {
		t.Error("DisconnectedAt should be set")
	}
}

func TestStore_ReconnectClearsOfflineEntry(t *testing.T) {
	s := New()
	s.AddClient("node-1", nil)
	s.RemoveClient("node-1")
	s.AddClient("node-1", nil)

	clients := s.PublicClients()
	if len(clients) != 1 {
		t.Fatalf("PublicClients len = %d, want 1 (not a duplicate)", len(clients))
	}
	if !clients[0].Connected {
		t.Error("reconnected client should be Connected=true")
	}
}

// Client IDs come off the network, so an agent reconnecting under a fresh ID
// each time must not grow the offline roster without bound.
func TestStore_OfflineRosterIsBounded(t *testing.T) {
	s := New()
	for i := 0; i < MaxOfflineClients+50; i++ {
		id := fmt.Sprintf("node-%d", i)
		s.AddClient(id, nil)
		s.RemoveClient(id)
	}
	if got := len(s.PublicClients()); got > MaxOfflineClients {
		t.Errorf("offline roster = %d, want <= %d", got, MaxOfflineClients)
	}
}

func TestStore_OfflineEntriesExpire(t *testing.T) {
	s := New()
	s.AddClient("stale", nil)
	s.RemoveClient("stale")

	// Backdate the drop past the retention window.
	s.mu.Lock()
	pub := s.lastSeen["stale"]
	pub.DisconnectedAt = time.Now().Add(-OfflineRetention - time.Minute).UnixMilli()
	s.lastSeen["stale"] = pub
	s.mu.Unlock()

	if got := s.PublicClients(); len(got) != 0 {
		t.Errorf("PublicClients = %+v, want expired entry dropped", got)
	}
}

// Bounding the roster is not enough on its own: the per-client caches are
// write-only, so forgetting an agent must drop those too or they grow without
// bound for every client ID the server has ever seen.
func TestStore_ForgettingClientEvictsCaches(t *testing.T) {
	s := New()
	s.AddClient("gone", nil)
	s.UpdateStats("gone", map[string]any{"cpu": 1.0})
	s.SetAlerts("gone", map[string]any{"totalCount": float64(1)})
	s.SetKioskStatus("gone", map[string]any{"running": true})
	s.SetVariantStatus("gone", map[string]any{"current": "kiosk"})
	s.RemoveClient("gone")

	// Still offline-but-remembered: caches are intentionally kept so the node
	// renders its last-known values.
	if s.GetStats("gone") == nil {
		t.Fatal("stats should survive while the offline entry is retained")
	}

	// Age it out, then force a prune via another disconnect.
	s.mu.Lock()
	pub := s.lastSeen["gone"]
	pub.DisconnectedAt = time.Now().Add(-OfflineRetention - time.Minute).UnixMilli()
	s.lastSeen["gone"] = pub
	s.mu.Unlock()
	s.AddClient("other", nil)
	s.RemoveClient("other")

	if s.GetStats("gone") != nil {
		t.Error("statsCache not evicted for a forgotten client")
	}
	if len(s.GetStatsHistory("gone")) != 0 {
		t.Error("statsHistory not evicted for a forgotten client")
	}
	if s.GetAlerts("gone") != nil {
		t.Error("alertsCache not evicted for a forgotten client")
	}
	if s.GetKioskStatus("gone") != nil {
		t.Error("kioskStatus not evicted for a forgotten client")
	}
	if s.GetVariantStatus("gone") != nil {
		t.Error("variantStatus not evicted for a forgotten client")
	}
}

// The dashboard renders client_list in the order the server sends it. Both
// PublicClients sources are maps, and Go randomizes map iteration, so an
// unsorted roster reshuffled on every connect/disconnect and hosts moved out
// from under the pointer.
func TestStore_PublicClientsIsDeterministicallyOrdered(t *testing.T) {
	ids := []string{"zeta", "alpha", "mike", "bravo", "yankee", "charlie"}

	s := New()
	for _, id := range ids {
		s.AddClient(id, nil)
	}
	// Take one offline so the lastSeen map contributes too.
	s.RemoveClient("mike")

	want := []string{"alpha", "bravo", "charlie", "mike", "yankee", "zeta"}

	// Repeat: a single call can look sorted by luck under map randomization.
	for i := 0; i < 25; i++ {
		got := make([]string, 0, len(want))
		for _, pub := range s.PublicClients() {
			got = append(got, pub.ClientID)
		}
		if len(got) != len(want) {
			t.Fatalf("iteration %d: got %d clients, want %d (%v)", i, len(got), len(want), got)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: PublicClients() order = %v, want %v", i, got, want)
			}
		}
	}
}

func TestStore_ClientIDsIsSorted(t *testing.T) {
	s := New()
	for _, id := range []string{"delta", "alpha", "charlie", "bravo"} {
		s.AddClient(id, nil)
	}
	want := []string{"alpha", "bravo", "charlie", "delta"}

	for i := 0; i < 25; i++ {
		got := s.ClientIDs()
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: ClientIDs() = %v, want %v", i, got, want)
			}
		}
	}
}

// A cluster's manager was whichever manager the map iteration reached first,
// so an unchanged two-manager cluster could report a different manager on
// consecutive calls — and its member list reordered under the same randomness.
func TestStore_SwarmClustersIsDeterministic(t *testing.T) {
	s := New()
	members := map[string]string{
		"node-c": "manager",
		"node-a": "manager",
		"node-b": "worker",
		"node-d": "worker",
	}
	for id, role := range members {
		s.AddClient(id, nil)
		s.UpdateStats(id, map[string]any{
			"swarmActive":    true,
			"swarmClusterId": "cluster-1",
			"swarmRole":      role,
		})
	}

	for i := 0; i < 25; i++ {
		clusters := s.SwarmClusters()
		if len(clusters) != 1 {
			t.Fatalf("iteration %d: got %d clusters, want 1", i, len(clusters))
		}
		if mgr, _ := clusters[0]["manager"].(string); mgr != "node-a" {
			t.Fatalf("iteration %d: manager = %q, want the lowest-ID manager node-a", i, mgr)
		}
		list, _ := clusters[0]["members"].([]map[string]any)
		want := []string{"node-a", "node-b", "node-c", "node-d"}
		if len(list) != len(want) {
			t.Fatalf("iteration %d: got %d members, want %d", i, len(list), len(want))
		}
		for j, id := range want {
			if got, _ := list[j]["clientId"].(string); got != id {
				t.Fatalf("iteration %d: member %d = %q, want %q", i, j, got, id)
			}
		}
	}
}
