package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "server-config.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ValidMinimalConfig(t *testing.T) {
	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "test-secret-token",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "my-client-id",
			"clientSecret": "my-client-secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != 8443 {
		t.Errorf("Port = %d, want 8443", cfg.Port)
	}
	if cfg.AuthToken != "test-secret-token" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "test-secret-token")
	}
	if !cfg.OIDC.Enabled {
		t.Error("OIDC.Enabled = false, want true")
	}
}

func TestLoad_AppliesDefaults(t *testing.T) {
	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.PingIntervalSec != 30 {
		t.Errorf("PingIntervalSec = %d, want 30", cfg.PingIntervalSec)
	}
	if cfg.PongTimeoutSec != 60 {
		t.Errorf("PongTimeoutSec = %d, want 60", cfg.PongTimeoutSec)
	}
	if cfg.AgentAuthMaxSkewSec != 600 {
		t.Errorf("AgentAuthMaxSkewSec = %d, want 600", cfg.AgentAuthMaxSkewSec)
	}
	if cfg.LogFile == "" {
		t.Error("LogFile should have default value")
	}
	if len(cfg.ExecAllowedCommands) != len(DefaultExecAllowedCommands) {
		t.Errorf("ExecAllowedCommands = %v, want defaults", cfg.ExecAllowedCommands)
	}
}

func TestLoad_ExecAllowlistEnvOverride(t *testing.T) {
	t.Setenv("EXEC_ALLOWED_COMMANDS", "git, ls ,  make")
	p := writeTestConfig(t, `{"port":8443,"authToken":"secret"}`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"git", "ls", "make"}
	if len(cfg.ExecAllowedCommands) != len(want) {
		t.Fatalf("ExecAllowedCommands = %v, want %v", cfg.ExecAllowedCommands, want)
	}
	for i := range want {
		if cfg.ExecAllowedCommands[i] != want[i] {
			t.Errorf("ExecAllowedCommands[%d] = %q, want %q", i, cfg.ExecAllowedCommands[i], want[i])
		}
	}
}

func TestLoad_MissingPort(t *testing.T) {
	p := writeTestConfig(t, `{"authToken": "secret"}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load() should fail when port is missing")
	}
}

func TestLoad_MissingAuthToken(t *testing.T) {
	p := writeTestConfig(t, `{"port": 8443}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load() should fail when authToken is missing")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	p := writeTestConfig(t, `{not valid json}`)
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load() should fail on invalid JSON")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("Load() should fail on missing file")
	}
}

func TestLoad_EnvOverrideAuthToken(t *testing.T) {
	t.Setenv("AUTH_TOKEN", "env-override-token")

	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "original-token",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AuthToken != "env-override-token" {
		t.Errorf("AuthToken = %q, want %q", cfg.AuthToken, "env-override-token")
	}
}

func TestLoad_EnvOverridePort(t *testing.T) {
	t.Setenv("PORT", "9999")

	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("Port = %d, want 9999", cfg.Port)
	}
}

func TestLoad_EnvOverrideOIDC(t *testing.T) {
	t.Setenv("OIDC_ENABLED", "true")
	t.Setenv("OIDC_ISSUER", "https://env-issuer.example.com")
	t.Setenv("OIDC_CLIENT_ID", "env-client-id")
	t.Setenv("OIDC_CLIENT_SECRET", "env-client-secret")
	t.Setenv("OIDC_REDIRECT_URI", "https://env.example.com/auth/callback")
	t.Setenv("OIDC_BASE_URL", "https://env.example.com")

	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret"
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.OIDC.Enabled {
		t.Error("OIDC.Enabled = false, want true from env")
	}
	if cfg.OIDC.Issuer != "https://env-issuer.example.com" {
		t.Errorf("OIDC.Issuer = %q, want env value", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "env-client-id" {
		t.Errorf("OIDC.ClientID = %q, want env value", cfg.OIDC.ClientID)
	}
}

func TestLoad_EnvOverrideAgentAuthMaxSkew(t *testing.T) {
	t.Setenv("AGENT_AUTH_MAX_SKEW_SEC", "120")

	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentAuthMaxSkewSec != 120 {
		t.Errorf("AgentAuthMaxSkewSec = %d, want 120", cfg.AgentAuthMaxSkewSec)
	}
}

func TestLoad_AgentAuthMaxSkewClamped(t *testing.T) {
	// Too low — should clamp to 600
	t.Setenv("AGENT_AUTH_MAX_SKEW_SEC", "5")

	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AgentAuthMaxSkewSec != 600 {
		t.Errorf("AgentAuthMaxSkewSec = %d, want 600 (clamped)", cfg.AgentAuthMaxSkewSec)
	}
}

func TestLoad_ExplicitValues(t *testing.T) {
	p := writeTestConfig(t, `{
		"port": 9443,
		"authToken": "secret",
		"logFile": "/tmp/custom.log",
		"pingIntervalSec": 15,
		"pongTimeoutSec": 45,
		"agentAuthMaxSkewSec": 300,
		"githubUser": "testuser",
		"versionReleaseWebhookSecret": "webhook-secret",
		"serverUrl": "https://server.example.com",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com",
			"scopes": ["openid", "profile", "email"],
			"idTokenSigningAlg": "RS256",
			"clientAuthMethod": "client_secret_basic"
		},
		"dashboardAllowedOrigins": ["https://dashboard.example.com"]
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != 9443 {
		t.Errorf("Port = %d, want 9443", cfg.Port)
	}
	if cfg.LogFile != "/tmp/custom.log" {
		t.Errorf("LogFile = %q, want /tmp/custom.log", cfg.LogFile)
	}
	if cfg.PingIntervalSec != 15 {
		t.Errorf("PingIntervalSec = %d, want 15", cfg.PingIntervalSec)
	}
	if cfg.PongTimeoutSec != 45 {
		t.Errorf("PongTimeoutSec = %d, want 45", cfg.PongTimeoutSec)
	}
	if cfg.AgentAuthMaxSkewSec != 300 {
		t.Errorf("AgentAuthMaxSkewSec = %d, want 300", cfg.AgentAuthMaxSkewSec)
	}
	if cfg.GithubUser != "testuser" {
		t.Errorf("GithubUser = %q, want testuser", cfg.GithubUser)
	}
	if cfg.VersionReleaseWebhookSecret != "webhook-secret" {
		t.Errorf("VersionReleaseWebhookSecret = %q", cfg.VersionReleaseWebhookSecret)
	}
	if cfg.ServerURL != "https://server.example.com" {
		t.Errorf("ServerURL = %q", cfg.ServerURL)
	}
	if len(cfg.DashboardAllowedOrigins) != 1 || cfg.DashboardAllowedOrigins[0] != "https://dashboard.example.com" {
		t.Errorf("DashboardAllowedOrigins = %v", cfg.DashboardAllowedOrigins)
	}
	if len(cfg.OIDC.Scopes) != 3 {
		t.Errorf("OIDC.Scopes = %v, want 3 entries", cfg.OIDC.Scopes)
	}
}

func TestValidate_OIDCRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		cfg  string
	}{
		{"missing issuer", `{"port":8443,"authToken":"s","oidc":{"enabled":true,"clientId":"id","clientSecret":"s","redirectUri":"https://x/auth/callback","baseURL":"https://x"}}`},
		{"missing clientId", `{"port":8443,"authToken":"s","oidc":{"enabled":true,"issuer":"https://x","clientSecret":"s","redirectUri":"https://x/auth/callback","baseURL":"https://x"}}`},
		{"missing clientSecret", `{"port":8443,"authToken":"s","oidc":{"enabled":true,"issuer":"https://x","clientId":"id","redirectUri":"https://x/auth/callback","baseURL":"https://x"}}`},
		{"missing redirectUri", `{"port":8443,"authToken":"s","oidc":{"enabled":true,"issuer":"https://x","clientId":"id","clientSecret":"s","baseURL":"https://x"}}`},
		{"missing baseURL", `{"port":8443,"authToken":"s","oidc":{"enabled":true,"issuer":"https://x","clientId":"id","clientSecret":"s","redirectUri":"https://x/auth/callback"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := writeTestConfig(t, tt.cfg)
			_, err := Load(p)
			if err == nil {
				t.Fatal("Load() should fail with missing OIDC field")
			}
		})
	}
}

func TestLoad_Logging(t *testing.T) {
	p := writeTestConfig(t, `{
		"port": 8443,
		"authToken": "secret",
		"oidc": {
			"enabled": true,
			"issuer": "https://accounts.google.com",
			"clientId": "id",
			"clientSecret": "secret",
			"redirectUri": "https://example.com/auth/callback",
			"baseURL": "https://example.com"
		},
		"logging": {
			"file": "/var/log/server.log",
			"level": "debug"
		}
	}`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Logging.FilePath != "/var/log/server.log" {
		t.Errorf("Logging.FilePath = %q", cfg.Logging.FilePath)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q", cfg.Logging.Level)
	}
}
