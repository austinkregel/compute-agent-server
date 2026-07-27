package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open opens a GORM connection based on dsn's scheme ("sqlite://path" or
// "postgres://"/"postgresql://...") and runs AutoMigrate for every model this
// package owns.
//
// Uses github.com/glebarez/sqlite (a pure-Go, modernc.org/sqlite-backed
// dialector) rather than gorm.io/driver/sqlite, which wraps mattn/go-sqlite3
// via cgo — that would silently no-op (every query returning a "requires
// cgo" error) in the project's standard CGO_ENABLED=0 static build
// (server/Makefile's `build` target), which is exactly how this binary ships.
func Open(dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch {
	case strings.HasPrefix(dsn, "sqlite://"):
		path := strings.TrimPrefix(dsn, "sqlite://")
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create database dir: %w", err)
			}
		}
		dialector = sqlite.Open(path)
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported database DSN scheme (want sqlite:// or postgres://): %q", dsn)
	}

	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.AutoMigrate(&Thread{}, &Message{}); err != nil {
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return db, nil
}
