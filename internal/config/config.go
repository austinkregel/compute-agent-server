package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config captures all runtime knobs for the Go server.
type Config struct {
	Port                        int      `json:"port"`
	AuthToken                   string   `json:"authToken"`
	LogFile                     string   `json:"logFile"`
	PingIntervalSec             int      `json:"pingIntervalSec"`
	PongTimeoutSec              int      `json:"pongTimeoutSec"`
	AgentAuthMaxSkewSec         int      `json:"agentAuthMaxSkewSec"`
	GithubUser                  string   `json:"githubUser"`
	VersionReleaseWebhookSecret string   `json:"versionReleaseWebhookSecret"`
	ServerURL                   string   `json:"serverUrl"`
	DashboardAllowedOrigins     []string `json:"dashboardAllowedOrigins"`

	// ExecAllowedCommands is the canonical command allowlist pushed to agents
	// (governs exec + admin_run). Managed centrally here, not per-agent.
	ExecAllowedCommands []string `json:"execAllowedCommands"`

	OIDC    OIDCConfig    `json:"oidc"`
	Logging LoggingConfig `json:"logging"`
}

// DefaultExecAllowedCommands is an IDE-friendly allowlist applied when the
// config doesn't specify one. Entries match command-name prefixes on the agent.
var DefaultExecAllowedCommands = []string{
	"git", "ls", "cat", "head", "tail", "wc", "find", "grep", "rg", "which", "env",
	"go", "node", "npm", "npx", "pnpm", "yarn", "python", "python3", "pip", "pip3",
	"make", "cargo", "rustc",
}

// OIDCConfig holds OpenID Connect provider settings.
type OIDCConfig struct {
	Enabled          bool     `json:"enabled"`
	BaseURL          string   `json:"baseURL"`
	Issuer           string   `json:"issuer"`
	ClientID         string   `json:"clientId"`
	ClientSecret     string   `json:"clientSecret"`
	RedirectURI      string   `json:"redirectUri"`
	Scopes           []string `json:"scopes"`
	IDTokenSignAlg   string   `json:"idTokenSigningAlg"`
	ClientAuthMethod string   `json:"clientAuthMethod"`
}

// LoggingConfig describes log destination and verbosity.
type LoggingConfig struct {
	FilePath string `json:"file"`
	Level    string `json:"level"`
}

// DefaultPath returns the config path, honoring SERVER_CONFIG_PATH.
func DefaultPath() string {
	if override := os.Getenv("SERVER_CONFIG_PATH"); override != "" {
		return override
	}
	return filepath.Join(".", "server-config.json")
}

// Load reads the config file, applies env overrides, defaults, and validation.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyEnvOverrides()
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate ensures the minimum viable fields are set.
func (c *Config) Validate() error {
	if c.Port <= 0 {
		return errors.New("port is required")
	}
	if strings.TrimSpace(c.AuthToken) == "" {
		return errors.New("authToken is required")
	}
	if c.OIDC.Enabled {
		switch {
		case strings.TrimSpace(c.OIDC.Issuer) == "":
			return errors.New("oidc.issuer is required when OIDC is enabled")
		case strings.TrimSpace(c.OIDC.ClientID) == "":
			return errors.New("oidc.clientId is required when OIDC is enabled")
		case strings.TrimSpace(c.OIDC.ClientSecret) == "":
			return errors.New("oidc.clientSecret is required when OIDC is enabled")
		case strings.TrimSpace(c.OIDC.RedirectURI) == "":
			return errors.New("oidc.redirectUri is required when OIDC is enabled")
		case strings.TrimSpace(c.OIDC.BaseURL) == "":
			return errors.New("oidc.baseURL is required when OIDC is enabled")
		}
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.LogFile == "" {
		if c.Logging.FilePath != "" {
			c.LogFile = c.Logging.FilePath
		} else {
			c.LogFile = filepath.Join(".", "server.log")
		}
	}
	// Sync LogFile and Logging.FilePath
	if c.Logging.FilePath == "" {
		c.Logging.FilePath = c.LogFile
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.PingIntervalSec <= 0 {
		c.PingIntervalSec = 30
	}
	if c.PongTimeoutSec <= 0 {
		c.PongTimeoutSec = 60
	}
	if c.AgentAuthMaxSkewSec <= 0 {
		c.AgentAuthMaxSkewSec = 600
	}
	// Clamp agent auth skew: min 30, max 86400
	if c.AgentAuthMaxSkewSec < 30 || c.AgentAuthMaxSkewSec > 86400 {
		c.AgentAuthMaxSkewSec = 600
	}
	if c.OIDC.IDTokenSignAlg == "" {
		c.OIDC.IDTokenSignAlg = "RS256"
	}
	if c.OIDC.ClientAuthMethod == "" {
		c.OIDC.ClientAuthMethod = "client_secret_basic"
	}
	if len(c.OIDC.Scopes) == 0 {
		c.OIDC.Scopes = []string{"openid"}
	}
	if len(c.ExecAllowedCommands) == 0 {
		c.ExecAllowedCommands = append([]string(nil), DefaultExecAllowedCommands...)
	}
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.Port = n
		}
	}
	if v := os.Getenv("AUTH_TOKEN"); v != "" {
		c.AuthToken = strings.TrimSpace(v)
	}
	if v := os.Getenv("EXEC_ALLOWED_COMMANDS"); v != "" {
		var cmds []string
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				cmds = append(cmds, p)
			}
		}
		c.ExecAllowedCommands = cmds
	}
	if v := os.Getenv("AGENT_AUTH_MAX_SKEW_SEC"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.AgentAuthMaxSkewSec = n
		}
	}
	if v := os.Getenv("GITHUB_USERNAME"); v != "" {
		c.GithubUser = strings.TrimSpace(v)
	}
	if v := os.Getenv("VERSION_RELEASE_WEBHOOK_SECRET"); v != "" {
		c.VersionReleaseWebhookSecret = strings.TrimSpace(v)
	}

	// OIDC env overrides
	if v := os.Getenv("OIDC_ENABLED"); v != "" {
		c.OIDC.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("OIDC_ISSUER"); v != "" {
		c.OIDC.Issuer = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_CLIENT_ID"); v != "" {
		c.OIDC.ClientID = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_CLIENT_SECRET"); v != "" {
		c.OIDC.ClientSecret = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_REDIRECT_URI"); v != "" {
		c.OIDC.RedirectURI = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_BASE_URL"); v != "" {
		c.OIDC.BaseURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_SCOPES"); v != "" {
		c.OIDC.Scopes = strings.Fields(v)
	}
	if v := os.Getenv("OIDC_ID_TOKEN_SIGNING_ALG"); v != "" {
		c.OIDC.IDTokenSignAlg = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_CLIENT_AUTH_METHOD"); v != "" {
		c.OIDC.ClientAuthMethod = strings.TrimSpace(v)
	}
}
