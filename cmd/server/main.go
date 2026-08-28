package main

import (
	"context"
	cryptotls "crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/austinkregel/backup-server/internal/config"
	"github.com/austinkregel/backup-server/internal/server"
	"github.com/austinkregel/compute-agent/pkg/logging"
	"github.com/austinkregel/compute-agent/pkg/version"
)

// printVersion prints the version information.
func printVersion() {
	fmt.Printf("backup-server %s (%s) built=%s\n", version.Version, version.Commit, version.BuildDate)
}

// handleVersionFlag returns true if the program should exit after printing version.
func handleVersionFlag(showVersion bool) bool {
	if showVersion {
		printVersion()
		return true
	}
	return false
}

// runHealthcheck probes this server's own /api/status over loopback and
// returns true if it responded successfully. Used as a Docker HEALTHCHECK —
// re-invokes this same binary rather than shelling out, since the distroless
// runtime image has no shell/curl. Tries plain HTTP first (the common case),
// then HTTPS with certificate verification skipped (a loopback self-check
// has no MITM exposure, and the server's own cert isn't necessarily trusted
// by this process regardless).
func runHealthcheck(port int) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/status", port)
	client := &http.Client{Timeout: 3 * time.Second}
	if resp, err := client.Get(url); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return true
		}
	}

	httpsClient := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &cryptotls.Config{InsecureSkipVerify: true}, //nolint:gosec // loopback self-check only
		},
	}
	resp, err := httpsClient.Get(fmt.Sprintf("https://127.0.0.1:%d/api/status", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func main() {
	// Load .env file if present (silent on missing file, matching Node.js dotenv behavior)
	_ = godotenv.Load()

	var cfgPath string
	var showVersion bool
	var healthcheck bool
	flag.StringVar(&cfgPath, "config", config.DefaultPath(), "Path to server-config.json")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&healthcheck, "healthcheck", false, "Check server health and exit (for Docker HEALTHCHECK)")
	flag.Parse()

	if handleVersionFlag(showVersion) {
		return
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	if healthcheck {
		if runHealthcheck(cfg.Port) {
			os.Exit(0)
		}
		os.Exit(1)
	}

	log, err := logging.New(logging.Options{
		File:  cfg.Logging.FilePath,
		Level: cfg.Logging.Level,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	log.Info("backup-server starting",
		"version", version.Short(),
		"port", cfg.Port,
		"oidc_enabled", cfg.OIDC.Enabled,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv, err := server.New(ctx, cfg, log)
	if err != nil {
		log.Error("startup failed", "error", err)
		os.Exit(1)
	}

	if err := srv.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("server terminated with error", "error", err)
		os.Exit(1)
	}

	log.Info("backup-server stopped")
}
