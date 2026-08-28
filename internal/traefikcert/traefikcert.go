// Package traefikcert extracts a domain's TLS certificate/key out of a
// Traefik ACME store (acme.json) and keeps a local certDir (fullchain.pem +
// privkey.pem) in sync as Traefik renews it — so a server that terminates
// its own TLS can use a cert Traefik manages, without Traefik terminating
// TLS itself.
package traefikcert

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/austinkregel/compute-agent/pkg/logging"
)

// acmeStore mirrors the shape Traefik writes to its ACME storage file: a map
// of certificate-resolver name to that resolver's account/certificate data.
// Only the fields needed for extraction are declared; unknown fields
// (Account, etc.) are ignored by encoding/json.
type acmeStore map[string]resolverData

type resolverData struct {
	Certificates []certEntry `json:"Certificates"`
}

type certEntry struct {
	Domain      certDomain `json:"domain"`
	Certificate string     `json:"certificate"`
	Key         string     `json:"key"`
}

type certDomain struct {
	Main string   `json:"main"`
	SANs []string `json:"sans"`
}

// ErrDomainNotFound is returned when no resolver in the ACME store has
// issued a certificate for the requested domain yet.
var ErrDomainNotFound = errors.New("traefikcert: domain not found in acme store")

// ExtractDomainCert reads a Traefik acme.json at acmeJSONPath and returns the
// PEM-encoded certificate and key for domain, searching every resolver's
// certificate list and matching against both the main domain and SANs.
func ExtractDomainCert(acmeJSONPath, domain string) (certPEM, keyPEM []byte, err error) {
	raw, err := os.ReadFile(acmeJSONPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read acme store: %w", err)
	}

	var store acmeStore
	if err := json.Unmarshal(raw, &store); err != nil {
		return nil, nil, fmt.Errorf("parse acme store: %w", err)
	}

	for _, resolver := range store {
		for _, entry := range resolver.Certificates {
			if !matchesDomain(entry.Domain, domain) {
				continue
			}
			cert, err := base64.StdEncoding.DecodeString(entry.Certificate)
			if err != nil {
				return nil, nil, fmt.Errorf("decode certificate for %s: %w", domain, err)
			}
			key, err := base64.StdEncoding.DecodeString(entry.Key)
			if err != nil {
				return nil, nil, fmt.Errorf("decode key for %s: %w", domain, err)
			}
			return cert, key, nil
		}
	}

	return nil, nil, fmt.Errorf("%w: %s", ErrDomainNotFound, domain)
}

func matchesDomain(d certDomain, domain string) bool {
	if d.Main == domain {
		return true
	}
	for _, san := range d.SANs {
		if san == domain {
			return true
		}
	}
	return false
}

// WriteCertFiles atomically writes certPEM/keyPEM into certDir as
// fullchain.pem (0644) and privkey.pem (0600) — matching the filenames
// internal/tls.CertManager expects. Atomic (temp file + rename) so a
// concurrent reader (the fsnotify watcher in internal/tls) never observes a
// partially-written file.
func WriteCertFiles(certDir string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}
	if err := atomicWrite(filepath.Join(certDir, "fullchain.pem"), certPEM, 0o644); err != nil {
		return fmt.Errorf("write fullchain.pem: %w", err)
	}
	if err := atomicWrite(filepath.Join(certDir, "privkey.pem"), keyPEM, 0o600); err != nil {
		return fmt.Errorf("write privkey.pem: %w", err)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WatchAndSync extracts domain's cert from acmeJSONPath into certDir once
// immediately, then watches acmeJSONPath's parent directory for changes
// (Traefik renews via temp-file-plus-rename, which would orphan a watch on
// the file itself) and re-extracts whenever it's rewritten, debounced like
// internal/tls's own cert watcher. The returned cancel func stops the
// watcher goroutine.
func WatchAndSync(acmeJSONPath, domain, certDir string, log *logging.Logger) (cancel func(), err error) {
	if err := syncOnce(acmeJSONPath, domain, certDir); err != nil {
		log.Warn("traefik cert sync failed", "domain", domain, "error", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create acme watcher: %w", err)
	}
	dir := filepath.Dir(acmeJSONPath)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch %s: %w", dir, err)
	}
	target := filepath.Base(acmeJSONPath)

	go func() {
		var debounce *time.Timer
		for {
			select {
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) != target {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(300*time.Millisecond, func() {
					if err := syncOnce(acmeJSONPath, domain, certDir); err != nil {
						log.Warn("traefik cert sync failed", "domain", domain, "error", err)
					} else {
						log.Info("traefik cert synced", "domain", domain, "certDir", certDir)
					}
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn("traefik acme watcher error", "error", err)
			}
		}
	}()

	return func() { watcher.Close() }, nil
}

func syncOnce(acmeJSONPath, domain, certDir string) error {
	certPEM, keyPEM, err := ExtractDomainCert(acmeJSONPath, domain)
	if err != nil {
		return err
	}
	return WriteCertFiles(certDir, certPEM, keyPEM)
}
