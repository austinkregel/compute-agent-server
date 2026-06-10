package tls

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/austinkregel/compute-agent/pkg/logging"
)

// CertManager handles loading and hot-reloading TLS certificates.
type CertManager struct {
	certDir string
	mu      sync.RWMutex
	cert    *tls.Certificate
}

// LoadCertBundle reads the PEM files from the certificate directory and
// returns a tls.Certificate ready for use.
func LoadCertBundle(certDir string) (*tls.Certificate, error) {
	keyPath := filepath.Join(certDir, "privkey.pem")
	certPath := filepath.Join(certDir, "fullchain.pem")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load cert pair: %w", err)
	}
	return &cert, nil
}

// NewCertManager creates a manager that loads certs from certDir.
func NewCertManager(certDir string) (*CertManager, error) {
	mgr := &CertManager{certDir: certDir}
	if err := mgr.loadCert(); err != nil {
		return nil, err
	}
	return mgr, nil
}

// loadCert reads certs from disk and stores them atomically.
func (m *CertManager) loadCert() error {
	cert, err := LoadCertBundle(m.certDir)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cert = cert
	m.mu.Unlock()
	return nil
}

// Reload re-reads certificates from disk. Call this when files change.
func (m *CertManager) Reload() error {
	return m.loadCert()
}

// GetCertificate implements tls.Config.GetCertificate for dynamic cert serving.
func (m *CertManager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.cert == nil {
		return nil, fmt.Errorf("no certificate loaded")
	}
	return m.cert, nil
}

// NewTLSConfig creates a *tls.Config with cert hot-reload support and
// strong defaults (TLS 1.2 minimum).
func NewTLSConfig(certDir string) (*tls.Config, error) {
	mgr, err := NewCertManager(certDir)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		GetCertificate: mgr.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}, nil
}

// NewTLSConfigWithWatcher creates a *tls.Config with automatic cert hot-reload.
// It watches privkey.pem and fullchain.pem for changes using fsnotify, debounces
// 300ms to avoid thrash, then calls CertManager.Reload(). Returns the TLS config
// and a cancel function that stops the watcher goroutine.
func NewTLSConfigWithWatcher(certDir string, log *logging.Logger) (*tls.Config, func(), error) {
	mgr, err := NewCertManager(certDir)
	if err != nil {
		return nil, nil, err
	}

	tlsCfg := &tls.Config{
		GetCertificate: mgr.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, fmt.Errorf("create cert watcher: %w", err)
	}

	// Watch the two cert files
	for _, name := range []string{"privkey.pem", "fullchain.pem"} {
		fp := filepath.Join(certDir, name)
		if err := watcher.Add(fp); err != nil {
			watcher.Close()
			return nil, nil, fmt.Errorf("watch %s: %w", fp, err)
		}
	}

	// Debounced reload goroutine
	go func() {
		var debounce *time.Timer
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}
				// Reset debounce timer (300ms)
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(300*time.Millisecond, func() {
					if err := mgr.Reload(); err != nil {
						log.Warn("TLS cert reload failed", "error", err)
					} else {
						log.Info("TLS cert reloaded", "certDir", certDir)
					}
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn("TLS cert watcher error", "error", err)
			}
		}
	}()

	cancel := func() {
		watcher.Close()
	}

	return tlsCfg, cancel, nil
}

// CertDirExists checks whether the certificate directory contains the required files.
func CertDirExists(certDir string) bool {
	for _, name := range []string{"privkey.pem", "fullchain.pem"} {
		if _, err := os.Stat(filepath.Join(certDir, name)); err != nil {
			return false
		}
	}
	return true
}
