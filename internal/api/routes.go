package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/backup-server/internal/relay"
	"github.com/austinkregel/backup-server/internal/state"
	"github.com/austinkregel/backup-server/internal/ws"
	"github.com/austinkregel/compute-agent/pkg/logging"
)

const isoMillis = "2006-01-02T15:04:05.000Z"

var githubUserRe = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}$`)

// Deps holds dependencies injected into API handlers.
type Deps struct {
	Store  *state.Store
	Log    *logging.Logger
	Config *config.Config
	Relay  *relay.Relay

	// AuthStatusHandler is the handler for /api/auth/status.
	// When OIDC is enabled, this should be OIDCProvider.HandleAuthStatus.
	AuthStatusHandler http.HandlerFunc

	// StartTime is when the server started, used for uptime calculation.
	StartTime time.Time
}

// NewRouter creates the chi router with all API routes.
// The authMiddleware parameter is a middleware that enforces authentication.
// Pass nil to skip auth (for testing without OIDC).
func NewRouter(deps Deps, authMiddleware func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(RecoverPanic)
	r.Use(SecurityHeaders)
	r.Use(NormalizeSlashes)
	r.Use(JSONContentType)
	r.Use(LimitBody(1 << 20)) // 1 MB, matching Node.js express.json limit

	// Public: auth status (no auth required)
	if deps.AuthStatusHandler != nil {
		r.Get("/api/auth/status", deps.AuthStatusHandler)
	} else {
		r.Get("/api/auth/status", handleAuthStatus(deps))
	}

	// Protected API routes
	api := r.Group(func(r chi.Router) {
		if authMiddleware != nil {
			r.Use(authMiddleware)
		}

		// Status
		r.Get("/api/status", handleStatus(deps))

		// Client endpoints
		r.Get("/api/client/{clientId}/stats", handleClientStats(deps))
		r.Get("/api/client/{clientId}/alerts", handleClientAlerts(deps))

		// Admin commands
		r.Post("/api/server/restart", handleRestart(deps))
		r.Post("/api/server/shutdown", handleShutdown(deps))
		r.Post("/api/client/{clientId}/keys/resync", handleKeysResync(deps))

		// Local cron
		r.Get("/api/cron", handleCronGet(deps))
		r.Put("/api/cron", handleCronPut(deps))
		r.Post("/api/cron/validate", handleCronValidate(deps))

		// Remote client cron (sends admin_run to agent, waits for response)
		r.Get("/api/client/{clientId}/cron", handleRemoteCronGet(deps))
		r.Put("/api/client/{clientId}/cron", handleRemoteCronPut(deps))

		// Legacy/deprecated task endpoints
		r.Get("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"tasks": []any{}})
		})
		r.Get("/api/tasks/{clientId}", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"tasks": []any{}})
		})
		r.Post("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusGone, map[string]any{"error": "task queue feature removed"})
		})
		r.Put("/api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusGone, map[string]any{"error": "task queue feature removed"})
		})
		r.Delete("/api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusGone, map[string]any{"error": "task queue feature removed"})
		})

		// Docker / Swarm monitoring endpoints (read-only). Stack deployment
		// and management is owned by a separate tool; the agent only reports.
		r.Get("/api/client/{clientId}/docker", handleClientDocker(deps))
		r.Get("/api/client/{clientId}/containers", handleContainerInventory(deps))
		r.Get("/api/client/{clientId}/containers/{containerId}/logs", handleContainerLogs(deps))
		r.Get("/api/swarm/clusters", handleSwarmClusters(deps))
		r.Get("/api/swarm/cluster/{clusterId}", handleSwarmCluster(deps))
	})
	_ = api

	// GitHub version-release webhook (public, authenticated via HMAC signature)
	r.Post("/github-version-release", handleWebhook(deps))

	// API 404 fallback
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "Not found"})
			return
		}
		http.NotFound(w, r)
	})

	return r
}

// --- Handlers ---

func handleAuthStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: return actual auth state from OIDC session
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"user":          nil,
		})
	}
}

func handleStatus(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clients := deps.Store.PublicClients()
		ids := deps.Store.ClientIDs()
		writeJSON(w, http.StatusOK, map[string]any{
			"connectedClients": deps.Store.ClientCount(),
			"clientIds":        ids,
			"clients":          clients,
			"uptime":           time.Since(deps.StartTime).Seconds(),
			"timestamp":        time.Now().UTC().Format(isoMillis),
		})
	}
}

func handleClientStats(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		stats := deps.Store.GetStats(clientID)
		if stats == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "no stats"})
			return
		}
		resp := map[string]any{"clientId": clientID}
		for k, v := range stats {
			resp[k] = v
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleClientAlerts(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		alerts := deps.Store.GetAlerts(clientID)
		if alerts == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"clientId":    clientID,
				"alerts":      []any{},
				"totalCount":  0,
				"hasCritical": false,
			})
			return
		}
		resp := map[string]any{"clientId": clientID}
		for k, v := range alerts {
			resp[k] = v
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func handleRestart(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClientID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "clientId required in body"})
			return
		}
		if !deps.Store.HasClient(body.ClientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}
		deps.Log.Info("restart request forwarded", "clientId", body.ClientID)
		ws.SendSignedCommand(deps.Store, body.ClientID, "admin_run",
			map[string]any{"cmd": map[string]any{"command": "reboot"}}, deps.Log)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "sent", "action": "restart", "clientId": body.ClientID,
		})
	}
}

func handleShutdown(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientID string `json:"clientId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ClientID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "clientId required in body"})
			return
		}
		if !deps.Store.HasClient(body.ClientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}
		deps.Log.Info("shutdown request forwarded", "clientId", body.ClientID)
		ws.SendSignedCommand(deps.Store, body.ClientID, "admin_run",
			map[string]any{"cmd": map[string]any{"command": "shutdown"}}, deps.Log)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "sent", "action": "shutdown", "clientId": body.ClientID,
		})
	}
}

func handleKeysResync(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		var body struct {
			GithubUser string `json:"githubUser"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		user := body.GithubUser
		if user == "" {
			user = deps.Config.GithubUser
		}
		if user == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "githubUser required (body.githubUser or config githubUser)",
			})
			return
		}
		if !githubUserRe.MatchString(user) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid githubUser"})
			return
		}
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}
		deps.Log.Info("dispatching GitHub key sync", "clientId", clientID, "githubUser", user)
		ws.SendSignedCommand(deps.Store, clientID, "sync_keys",
			map[string]any{"user": user}, deps.Log)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "sent", "clientId": clientID, "githubUser": user,
		})
	}
}

func handleCronGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		text, _ := getCurrentCrontab()
		writeJSON(w, http.StatusOK, map[string]any{"crontab": text})
	}
}

func handleCronPut(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Crontab string `json:"crontab"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "crontab string required"})
			return
		}
		errs := validateCronSyntax(body.Crontab)
		if len(errs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation_failed", "details": errs})
			return
		}
		cmd := exec.Command("crontab", "-")
		cmd.Stdin = strings.NewReader(body.Crontab)
		if out, err := cmd.CombinedOutput(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "crontab_apply_failed", "stderr": string(out),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "updated"})
	}
}

func handleCronValidate(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Crontab string `json:"crontab"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "crontab string required"})
			return
		}
		errs := validateCronSyntax(body.Crontab)
		if len(errs) > 0 {
			writeJSON(w, http.StatusOK, map[string]any{"valid": false, "errors": errs})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"valid": true})
	}
}

// --- GitHub webhook ---

func handleWebhook(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := deps.Config.VersionReleaseWebhookSecret
		if secret == "" {
			deps.Log.Warn("github-version-release webhook received but secret not configured")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": "webhook secret not configured"})
			return
		}

		// Read raw body for signature verification
		rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "read body failed"})
			return
		}

		// Verify HMAC-SHA256 signature
		sigHeader := r.Header.Get("X-Kratos-Signature-256")
		if sigHeader == "" {
			sigHeader = r.Header.Get("X-Kratos-Signature")
		}
		sigHeader = strings.TrimSpace(sigHeader)
		if !strings.HasPrefix(sigHeader, "sha256=") || len(sigHeader) != 71 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "missing/invalid signature header"})
			return
		}
		providedHex := strings.ToLower(sigHeader[7:])
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(rawBody)
		expectedHex := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(providedHex), []byte(expectedHex)) {
			deps.Log.Warn("github-version-release webhook rejected: bad signature")
			writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "error": "bad signature"})
			return
		}

		// Parse payload
		var payload map[string]any
		json.Unmarshal(rawBody, &payload)
		if payload == nil {
			payload = map[string]any{}
		}

		tag := strVal(payload, "tag", strVal(payload, "version", strVal(payload, "release", "")))
		source := strVal(payload, "source", strVal(payload, "repo", strVal(payload, "repository", "unknown")))

		deps.Log.Info("github-version-release webhook accepted", "source", source, "tag", tag)

		// Notify all connected agents to self-update
		updatePayload := map[string]any{
			"repo": "austinkregel/compute-agent",
			"tag":  tag,
			"at":   time.Now().UTC().Format(isoMillis),
		}
		sent := 0
		for _, clientID := range deps.Store.ClientIDs() {
			if ws.SendSignedCommand(deps.Store, clientID, "agent_update", updatePayload, deps.Log) {
				sent++
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "notifiedClients": sent})
	}
}

// --- Remote client cron ---

func handleRemoteCronGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}

		token := fmt.Sprintf("cron-get-%s-%d", clientID, time.Now().UnixMilli())
		ch := deps.Relay.RegisterPendingResult(token)
		defer deps.Relay.UnregisterPendingResult(token)

		ws.SendSignedCommand(deps.Store, clientID, "admin_run",
			map[string]any{"token": token, "cmd": map[string]any{"command": "crontab -l"}}, deps.Log)

		select {
		case res := <-ch:
			if res.ExitCode != 0 {
				errMsg := res.Stderr
				if errMsg == "" {
					errMsg = "crontab read failed"
				}
				writeJSON(w, http.StatusOK, map[string]any{"crontab": "", "error": errMsg})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{"crontab": res.Stdout})
			}
		case <-time.After(8 * time.Second):
			writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "timeout waiting for agent response"})
		case <-r.Context().Done():
			return
		}
	}
}

func handleRemoteCronPut(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		var body struct {
			Crontab string `json:"crontab"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Crontab == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "crontab string required"})
			return
		}
		errs := validateCronSyntax(body.Crontab)
		if len(errs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "validation_failed", "details": errs})
			return
		}
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}

		b64 := base64.StdEncoding.EncodeToString([]byte(body.Crontab))
		remoteCmd := fmt.Sprintf("echo %s | base64 -d | crontab -", b64)
		token := fmt.Sprintf("cron-set-%s-%d", clientID, time.Now().UnixMilli())
		ch := deps.Relay.RegisterPendingResult(token)
		defer deps.Relay.UnregisterPendingResult(token)

		ws.SendSignedCommand(deps.Store, clientID, "admin_run",
			map[string]any{"token": token, "cmd": map[string]any{"command": remoteCmd, "timeoutSec": 6}}, deps.Log)

		select {
		case res := <-ch:
			if res.ExitCode != 0 {
				errMsg := res.Stderr
				if errMsg == "" {
					errMsg = "crontab write failed"
				}
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": errMsg})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			}
		case <-time.After(8 * time.Second):
			writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "timeout waiting for agent response"})
		case <-r.Context().Done():
			return
		}
	}
}

// --- Helpers ---

func strVal(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func getCurrentCrontab() (string, error) {
	out, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n"), nil
}

// validateCronSyntax checks that each non-empty, non-comment line has at least
// 6 fields (5 schedule fields + command).
func validateCronSyntax(text string) []string {
	var errs []string
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Special cron variables like SHELL=, PATH=, MAILTO=
		if strings.Contains(trimmed, "=") && !strings.HasPrefix(trimmed, "*") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 6 {
			errs = append(errs, cronLineError(i+1, len(fields)))
		}
	}
	return errs
}

func cronLineError(line, fields int) string {
	return "line " + strconv.Itoa(line) + ": expected at least 6 fields (5 schedule + command), got " + strconv.Itoa(fields)
}
