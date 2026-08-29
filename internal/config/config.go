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

	// DatabaseDSN is the persistence backend for features that need it (SMS
	// history today). "sqlite://path" or "postgres://..."/"postgresql://...".
	DatabaseDSN string `json:"databaseDsn"`
	// SMSEncryptionKeyFile stores the AES-256 key used to encrypt SMS bodies
	// at rest, generated on first run if it doesn't exist (see
	// internal/database.LoadOrCreateKey). Keep this out of version control
	// and backups of the config file — it's the only thing standing between
	// a DB leak and readable message content.
	SMSEncryptionKeyFile string `json:"smsEncryptionKeyFile"`

	// TLSDomain is the public hostname this server presents on its TLS
	// listener and registers with Traefik (e.g. monitor.kratos.kregel.dev).
	// Empty disables the Traefik cert-sync/self-registration integration
	// below entirely; the server falls back to whatever's in certDir already.
	TLSDomain string `json:"tlsDomain"`
	// TraefikACMEPath points at a (read-only mounted) Traefik acme.json. When
	// set alongside TLSDomain, the server extracts TLSDomain's cert/key from
	// it into certDir and keeps them in sync as Traefik renews. See
	// internal/traefikcert.
	TraefikACMEPath string `json:"traefikAcmePath"`
	// TraefikDynamicDir points at a (read-write mounted) Traefik file-provider
	// directory. When set alongside TLSDomain, the server writes its own
	// router/service config there on startup so Traefik requests a cert for
	// TLSDomain and proxies to TraefikBackendURL. See internal/traefikroute.
	TraefikDynamicDir string `json:"traefikDynamicDir"`
	// TraefikBackendURL is the address Traefik should proxy TLSDomain traffic
	// to (e.g. https://10.0.1.1:8443 — the Docker network gateway reaching
	// this host). Only used when TraefikDynamicDir is set.
	TraefikBackendURL string `json:"traefikBackendUrl"`

	// AuditLogFile is the append-only, hash-chained security audit trail
	// (internal/audit). Separate from Logging.FilePath so operational log
	// rotation and filtering cannot affect it.
	AuditLogFile string `json:"auditLogFile"`

	// InsecureAllowUnauthenticated permits running with oidc.enabled false.
	// Without OIDC there is no authentication middleware, so every /api/*
	// route — including server shutdown and exec-allowlist mutation, which
	// pushes to every connected agent — is reachable by anyone who can reach
	// the port. Startup refuses that configuration unless this flag is set.
	InsecureAllowUnauthenticated bool `json:"insecureAllowUnauthenticated"`

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

	// AdminGroup is the OIDC groups/roles claim value that grants administrative
	// access: managing the exec allowlist, restart/shutdown, and every
	// privileged dashboard event (remote shell, exec, file mutation).
	//
	// Empty denies all admin actions. There is no default because the value is
	// whatever the IdP emits; Authentik emits numeric team IDs rather than
	// group names. The "groups" array in /api/auth/status reports the value for
	// the current session. The IdP must be asked for the claim via Scopes.
	AdminGroup string `json:"adminGroup"`

	// PermissionsEndpoint is an absolute URL that returns the signed-in user's
	// entitlement and permissions FOR THIS CLIENT, called once per login with
	// the access token from the code exchange (aut.hair's
	// GET /api/client-permissions). Empty disables the lookup entirely.
	//
	// This exists because an IdP can approve a user for one application and not
	// another, and a bare "authenticated" answer cannot express that. The
	// endpoint takes no parameters: the access token names both the user and
	// the client, so a compromised relying party cannot probe another client's
	// entitlements.
	PermissionsEndpoint string `json:"permissionsEndpoint"`

	// AdminPermission is the permission that grants administrative access when
	// returned by PermissionsEndpoint. Empty disables permission-based admin.
	//
	// Preferred over AdminGroup: it is scoped to this client by construction,
	// so it cannot be satisfied by membership that entitles a user to some
	// other application, and it needs no groups claim.
	AdminPermission string `json:"adminPermission"`

	// RequireEntitlement refuses a login outright when PermissionsEndpoint
	// reports the user is not entitled to this client (HTTP 403 not_entitled),
	// rather than merely denying admin. A transport failure is never treated as
	// "not entitled" — that denies admin and is logged, but does not lock the
	// fleet dashboard out during an IdP outage.
	RequireEntitlement bool `json:"requireEntitlement"`
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
	if !c.OIDC.Enabled && !c.InsecureAllowUnauthenticated {
		return errors.New("oidc.enabled is false: the REST API and dashboard would serve with no authentication at all " +
			"(including POST /api/server/shutdown and PUT /api/server/exec-allowlist). " +
			"Enable OIDC, or set insecureAllowUnauthenticated:true to accept that risk explicitly")
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
	if c.DatabaseDSN == "" {
		c.DatabaseDSN = "sqlite://./data/app.db"
	}
	if c.SMSEncryptionKeyFile == "" {
		c.SMSEncryptionKeyFile = "sms-encryption.key"
	}
	if c.AuditLogFile == "" {
		c.AuditLogFile = filepath.Join(".", "audit.jsonl")
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
	if v := os.Getenv("SERVER_URL"); v != "" {
		c.ServerURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("PING_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.PingIntervalSec = n
		}
	}
	if v := os.Getenv("PONG_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			c.PongTimeoutSec = n
		}
	}
	if v := os.Getenv("DASHBOARD_ALLOWED_ORIGINS"); v != "" {
		var origins []string
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				origins = append(origins, p)
			}
		}
		c.DashboardAllowedOrigins = origins
	}
	if v := os.Getenv("LOG_FILE"); v != "" {
		c.LogFile = strings.TrimSpace(v)
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.Logging.Level = strings.TrimSpace(v)
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
	if v := os.Getenv("DATABASE_DSN"); v != "" {
		c.DatabaseDSN = strings.TrimSpace(v)
	}
	if v := os.Getenv("SMS_ENCRYPTION_KEY_FILE"); v != "" {
		c.SMSEncryptionKeyFile = strings.TrimSpace(v)
	}
	if v := os.Getenv("TLS_DOMAIN"); v != "" {
		c.TLSDomain = strings.TrimSpace(v)
	}
	if v := os.Getenv("TRAEFIK_ACME_PATH"); v != "" {
		c.TraefikACMEPath = strings.TrimSpace(v)
	}
	if v := os.Getenv("TRAEFIK_DYNAMIC_DIR"); v != "" {
		c.TraefikDynamicDir = strings.TrimSpace(v)
	}
	if v := os.Getenv("TRAEFIK_BACKEND_URL"); v != "" {
		c.TraefikBackendURL = strings.TrimSpace(v)
	}

	if v := os.Getenv("AUDIT_LOG_FILE"); v != "" {
		c.AuditLogFile = strings.TrimSpace(v)
	}
	if v := os.Getenv("INSECURE_ALLOW_UNAUTHENTICATED"); v != "" {
		c.InsecureAllowUnauthenticated = v == "true" || v == "1"
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
	if v := os.Getenv("OIDC_ADMIN_GROUP"); v != "" {
		c.OIDC.AdminGroup = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_PERMISSIONS_ENDPOINT"); v != "" {
		c.OIDC.PermissionsEndpoint = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_ADMIN_PERMISSION"); v != "" {
		c.OIDC.AdminPermission = strings.TrimSpace(v)
	}
	if v := os.Getenv("OIDC_REQUIRE_ENTITLEMENT"); v != "" {
		c.OIDC.RequireEntitlement = strings.EqualFold(strings.TrimSpace(v), "true") || v == "1"
	}
}
