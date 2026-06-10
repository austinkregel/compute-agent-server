package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

func main() {
	// Load .env file if present (silent on missing file, matching Node.js dotenv behavior)
	_ = godotenv.Load()

	var cfgPath string
	var showVersion bool
	flag.StringVar(&cfgPath, "config", config.DefaultPath(), "Path to server-config.json")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if handleVersionFlag(showVersion) {
		return
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
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

	srv, err := server.New(cfg, log)
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
