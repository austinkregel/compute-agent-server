package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/austinkregel/backup-server/internal/allowlist"
	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

func testDeps(t *testing.T) Deps {
	t.Helper()
	log, err := logging.New(logging.Options{Level: "debug"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Sync() })
	return Deps{
		Store:     state.New(),
		Log:       log,
		Config:    &config.Config{GithubUser: "testuser"},
		Allowlist: allowlist.New("", nil, log), // in-memory (no persistence) for tests
	}
}

func doRequest(t *testing.T, handler http.Handler, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, w.Body.String())
	}
	return result
}

// --- Auth Status (public) ---

func TestAuthStatus(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/auth/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", result["authenticated"])
	}
}

// --- Status ---

func TestStatus_NoClients(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["connectedClients"] != float64(0) {
		t.Errorf("connectedClients = %v, want 0", result["connectedClients"])
	}
	if result["timestamp"] == nil || result["timestamp"] == "" {
		t.Error("timestamp should be set")
	}
}

func TestStatus_WithClients(t *testing.T) {
	deps := testDeps(t)
	deps.Store.AddClient("node-1", nil)
	deps.Store.AddClient("node-2", nil)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["connectedClients"] != float64(2) {
		t.Errorf("connectedClients = %v, want 2", result["connectedClients"])
	}
}

// --- Client Stats ---

func TestClientStats_NotFound(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/node-1/stats", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	result := decodeJSON(t, w)
	if result["error"] != "no stats" {
		t.Errorf("error = %v", result["error"])
	}
}

func TestClientStats_Found(t *testing.T) {
	deps := testDeps(t)
	deps.Store.AddClient("node-1", nil)
	deps.Store.UpdateStats("node-1", map[string]any{
		"platform": "linux",
		"hostname": "testhost",
		"cpus":     float64(8),
	})
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/node-1/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["clientId"] != "node-1" {
		t.Errorf("clientId = %v", result["clientId"])
	}
	if result["platform"] != "linux" {
		t.Errorf("platform = %v", result["platform"])
	}
}

// --- Client Alerts ---

func TestClientAlerts_NoAlerts(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/node-1/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["clientId"] != "node-1" {
		t.Errorf("clientId = %v", result["clientId"])
	}
	if result["hasCritical"] != false {
		t.Errorf("hasCritical = %v, want false", result["hasCritical"])
	}
	count, ok := result["totalCount"].(float64)
	if !ok || count != 0 {
		t.Errorf("totalCount = %v, want 0", result["totalCount"])
	}
}

func TestClientAlerts_WithAlerts(t *testing.T) {
	deps := testDeps(t)
	deps.Store.AddClient("node-1", nil)
	deps.Store.SetAlerts("node-1", map[string]any{
		"alerts":      []any{map[string]any{"category": "oom"}},
		"totalCount":  float64(1),
		"hasCritical": true,
	})
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/client/node-1/alerts", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["clientId"] != "node-1" {
		t.Errorf("clientId = %v", result["clientId"])
	}
	if result["hasCritical"] != true {
		t.Errorf("hasCritical = %v, want true", result["hasCritical"])
	}
}

// --- Restart ---

func TestRestart_MissingBody(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/restart", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRestart_MissingClientId(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/restart", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRestart_ClientOffline(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/restart", `{"clientId":"node-1"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	result := decodeJSON(t, w)
	if result["error"] != "client offline" {
		t.Errorf("error = %v", result["error"])
	}
}

// --- Shutdown ---

func TestShutdown_MissingBody(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/shutdown", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestShutdown_ClientOffline(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/shutdown", `{"clientId":"node-1"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// --- Keys Resync ---

func TestKeysResync_NoGithubUser(t *testing.T) {
	deps := testDeps(t)
	deps.Config.GithubUser = "" // No default
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/client/node-1/keys/resync", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	result := decodeJSON(t, w)
	if !strings.Contains(result["error"].(string), "githubUser required") {
		t.Errorf("error = %v", result["error"])
	}
}

func TestKeysResync_InvalidGithubUser(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/client/node-1/keys/resync", `{"githubUser":"invalid user!@#"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	result := decodeJSON(t, w)
	if result["error"] != "invalid githubUser" {
		t.Errorf("error = %v", result["error"])
	}
}

func TestKeysResync_ClientOffline(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/client/node-1/keys/resync", `{"githubUser":"validuser"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestKeysResync_FallsBackToConfigUser(t *testing.T) {
	deps := testDeps(t)
	deps.Config.GithubUser = "configuser"
	deps.Store.AddClient("node-1", nil)
	r := NewRouter(deps, nil)

	// Send empty body (no githubUser), should use config value
	w := doRequest(t, r, "POST", "/api/client/node-1/keys/resync", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["githubUser"] != "configuser" {
		t.Errorf("githubUser = %v, want configuser", result["githubUser"])
	}
}

// --- Cron Validate ---

func TestCronValidate_ValidCrontab(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	body := `{"crontab":"* * * * * /usr/bin/echo hello\n# comment\nSHELL=/bin/bash\n"}`
	w := doRequest(t, r, "POST", "/api/cron/validate", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["valid"] != true {
		t.Errorf("valid = %v, want true", result["valid"])
	}
}

func TestCronValidate_InvalidCrontab(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	// Only 3 fields — not enough
	body := `{"crontab":"* * *\n"}`
	w := doRequest(t, r, "POST", "/api/cron/validate", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if result["valid"] != false {
		t.Errorf("valid = %v, want false", result["valid"])
	}
	errs, ok := result["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Error("expected errors array")
	}
}

func TestCronValidate_MissingBody(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/cron/validate", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// --- Cron syntax validator unit tests ---

func TestValidateCronSyntax_Valid(t *testing.T) {
	errs := validateCronSyntax("*/5 * * * * /usr/bin/backup\n0 3 * * * /usr/bin/cleanup arg1 arg2\n")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateCronSyntax_Comments(t *testing.T) {
	errs := validateCronSyntax("# This is a comment\n\n# Another comment\n*/5 * * * * /usr/bin/test\n")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateCronSyntax_EnvVars(t *testing.T) {
	errs := validateCronSyntax("SHELL=/bin/bash\nPATH=/usr/bin:/bin\nMAILTO=user@example.com\n*/5 * * * * /usr/bin/test\n")
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateCronSyntax_TooFewFields(t *testing.T) {
	errs := validateCronSyntax("* * *\n")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0], "line 1") {
		t.Errorf("error should reference line 1: %s", errs[0])
	}
	if !strings.Contains(errs[0], "got 3") {
		t.Errorf("error should mention got 3 fields: %s", errs[0])
	}
}

func TestValidateCronSyntax_MultipleErrors(t *testing.T) {
	errs := validateCronSyntax("bad\n# ok\nalso bad\n*/5 * * * * /ok\n")
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(errs), errs)
	}
}

// --- Legacy tasks ---

func TestTasks_GetReturnsEmpty(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	tasks, ok := result["tasks"].([]any)
	if !ok || len(tasks) != 0 {
		t.Errorf("tasks = %v, want empty array", result["tasks"])
	}
}

func TestTasks_PostReturnsGone(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/tasks", `{}`)
	if w.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410", w.Code)
	}
}

// --- 404 fallback ---

func TestNotFound_APIPath(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/nonexistent", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	result := decodeJSON(t, w)
	if result["ok"] != false {
		t.Errorf("ok = %v, want false", result["ok"])
	}
	if result["error"] != "Not found" {
		t.Errorf("error = %v", result["error"])
	}
}

// --- Auth middleware ---

func TestAuthMiddleware_BlocksUnauthenticated(t *testing.T) {
	deps := testDeps(t)

	// Auth middleware that always rejects
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"error": "unauthorized"})
		})
	}

	r := NewRouter(deps, authMW)

	// Protected route should be blocked
	w := doRequest(t, r, "GET", "/api/status", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}

	// Public route should still work
	w = doRequest(t, r, "GET", "/api/auth/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("auth/status = %d, want 200", w.Code)
	}
}

func TestAuthMiddleware_AllowsAuthenticated(t *testing.T) {
	deps := testDeps(t)

	// Auth middleware that always allows
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	r := NewRouter(deps, authMW)

	w := doRequest(t, r, "GET", "/api/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

// --- Admin middleware gating ---

func TestAdminMiddleware_GatesAllowlistButNotStatus(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git"}, deps.Log)

	// Admin middleware that always rejects with 403.
	deps.AdminMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]any{"error": "forbidden"})
		})
	}
	r := NewRouter(deps, nil)

	// Admin-gated route is blocked.
	if w := doRequest(t, r, "GET", "/api/server/exec-allowlist", ""); w.Code != http.StatusForbidden {
		t.Fatalf("exec-allowlist GET = %d, want 403", w.Code)
	}
	if w := doRequest(t, r, "PUT", "/api/server/exec-allowlist", `{"commands":["ls"]}`); w.Code != http.StatusForbidden {
		t.Fatalf("exec-allowlist PUT = %d, want 403", w.Code)
	}
	if w := doRequest(t, r, "POST", "/api/server/restart", `{"clientId":"x"}`); w.Code != http.StatusForbidden {
		t.Fatalf("restart = %d, want 403", w.Code)
	}

	// Non-admin protected route is unaffected.
	if w := doRequest(t, r, "GET", "/api/status", ""); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (not admin-gated)", w.Code)
	}
}

func TestAdminMiddleware_NilAllowsAuthenticated(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git", "ls"}, deps.Log)
	// AdminMiddleware nil -> no role gating (existing behavior).
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/server/exec-allowlist", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 when AdminMiddleware nil", w.Code)
	}
	result := decodeJSON(t, w)
	cmds, ok := result["commands"].([]any)
	if !ok || len(cmds) != 2 {
		t.Errorf("commands = %v, want 2 entries", result["commands"])
	}
}

// --- Exec allowlist GET/PUT/POST ---

func TestExecAllowlistGet_ReturnsEntriesAndProvenance(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git"}, deps.Log)
	deps.Allowlist.Add([]string{"sha256sum /root/.rebase/bin/rebase-indexer"}, "")
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/server/exec-allowlist", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	result := decodeJSON(t, w)
	entries, ok := result["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries = %v, want 2", result["entries"])
	}
	if result["empty"] != false {
		t.Errorf("empty = %v, want false", result["empty"])
	}
}

func TestExecAllowlistPut_RejectsForbiddenChars(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)
	w := doRequest(t, r, "PUT", "/api/server/exec-allowlist", `{"commands":["git; rm -rf /"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for forbidden chars", w.Code)
	}
}

func TestExecAllowlistPut_EmptyRequiresConfirm(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git"}, deps.Log)
	r := NewRouter(deps, nil)

	// Without confirmEmpty -> blocked.
	if w := doRequest(t, r, "PUT", "/api/server/exec-allowlist", `{"commands":[]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when clearing without confirm", w.Code)
	}
	// With confirmEmpty -> allowed.
	if w := doRequest(t, r, "PUT", "/api/server/exec-allowlist", `{"commands":[],"confirmEmpty":true}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with confirmEmpty", w.Code)
	}
	if !deps.Allowlist.IsEmpty() {
		t.Error("allowlist should be empty after confirmed clear")
	}
}

func TestExecAllowlistPost_AddRemove(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git", "ls"}, deps.Log)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "POST", "/api/server/exec-allowlist", `{"add":["cat"],"remove":["ls"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	result := decodeJSON(t, w)
	if added, _ := result["added"].([]any); len(added) != 1 {
		t.Errorf("added = %v, want [cat]", result["added"])
	}
	if removed, _ := result["removed"].([]any); len(removed) != 1 {
		t.Errorf("removed = %v, want [ls]", result["removed"])
	}
	cmds := deps.Allowlist.Commands()
	if len(cmds) != 2 { // git + cat
		t.Fatalf("commands = %v, want [git cat]", cmds)
	}
}

func TestExecAllowlistPost_RemoveAllRequiresConfirm(t *testing.T) {
	deps := testDeps(t)
	deps.Allowlist = allowlist.New("", []string{"git"}, deps.Log)
	r := NewRouter(deps, nil)

	if w := doRequest(t, r, "POST", "/api/server/exec-allowlist", `{"remove":["git"]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 when remove clears list", w.Code)
	}
	if w := doRequest(t, r, "POST", "/api/server/exec-allowlist", `{"remove":["git"],"confirmEmpty":true}`); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with confirmEmpty", w.Code)
	}
}

func TestExecAllowlistPost_EmptyBodyRejected(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)
	if w := doRequest(t, r, "POST", "/api/server/exec-allowlist", `{}`); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty add/remove", w.Code)
	}
}

// --- Security headers on API responses ---

func TestSecurityHeaders_OnAPIRoutes(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/status", "")
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options: DENY")
	}
	if w.Header().Get("Referrer-Policy") != "same-origin" {
		t.Error("missing Referrer-Policy: same-origin")
	}
}

// --- JSON Content-Type ---

func TestJSONContentType_OnAPI(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "/api/status", "")
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// --- Double-slash normalization ---

func TestDoubleSlash_Normalized(t *testing.T) {
	deps := testDeps(t)
	r := NewRouter(deps, nil)

	w := doRequest(t, r, "GET", "//api//status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after slash normalization", w.Code)
	}
}
