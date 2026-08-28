package traefikcert

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/austinkregel/compute-agent/pkg/logging"
)

const testCertPEM = "-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n"
const testKeyPEM = "-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----\n"

func writeAcmeFixture(t *testing.T, path string, resolvers map[string][]certEntry) {
	t.Helper()
	store := acmeStore{}
	for name, entries := range resolvers {
		store[name] = resolverData{Certificates: entries}
	}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func fixtureEntry(main string, sans []string) certEntry {
	return certEntry{
		Domain:      certDomain{Main: main, SANs: sans},
		Certificate: base64.StdEncoding.EncodeToString([]byte(testCertPEM)),
		Key:         base64.StdEncoding.EncodeToString([]byte(testKeyPEM)),
	}
}

func TestExtractDomainCert_MatchesMain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.json")
	writeAcmeFixture(t, path, map[string][]certEntry{
		"letsencrypt": {fixtureEntry("monitor.kratos.kregel.dev", nil)},
	})

	cert, key, err := ExtractDomainCert(path, "monitor.kratos.kregel.dev")
	if err != nil {
		t.Fatalf("ExtractDomainCert() error = %v", err)
	}
	if string(cert) != testCertPEM {
		t.Errorf("cert = %q, want %q", cert, testCertPEM)
	}
	if string(key) != testKeyPEM {
		t.Errorf("key = %q, want %q", key, testKeyPEM)
	}
}

func TestExtractDomainCert_MatchesSAN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.json")
	writeAcmeFixture(t, path, map[string][]certEntry{
		"letsencrypt": {fixtureEntry("kregel.dev", []string{"monitor.kratos.kregel.dev", "other.kregel.dev"})},
	})

	cert, _, err := ExtractDomainCert(path, "monitor.kratos.kregel.dev")
	if err != nil {
		t.Fatalf("ExtractDomainCert() error = %v", err)
	}
	if string(cert) != testCertPEM {
		t.Errorf("cert = %q, want %q", cert, testCertPEM)
	}
}

func TestExtractDomainCert_MultipleResolvers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.json")
	writeAcmeFixture(t, path, map[string][]certEntry{
		"letsencrypt-staging": {fixtureEntry("unrelated.example.com", nil)},
		"letsencrypt":         {fixtureEntry("monitor.kratos.kregel.dev", nil)},
	})

	_, _, err := ExtractDomainCert(path, "monitor.kratos.kregel.dev")
	if err != nil {
		t.Fatalf("ExtractDomainCert() error = %v", err)
	}
}

func TestExtractDomainCert_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.json")
	writeAcmeFixture(t, path, map[string][]certEntry{
		"letsencrypt": {fixtureEntry("other.kregel.dev", nil)},
	})

	_, _, err := ExtractDomainCert(path, "monitor.kratos.kregel.dev")
	if err == nil {
		t.Fatal("ExtractDomainCert() expected error, got nil")
	}
}

func TestExtractDomainCert_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ExtractDomainCert(path, "monitor.kratos.kregel.dev")
	if err == nil {
		t.Fatal("ExtractDomainCert() expected error for malformed JSON, got nil")
	}
}

func TestExtractDomainCert_MissingFile(t *testing.T) {
	_, _, err := ExtractDomainCert(filepath.Join(t.TempDir(), "missing.json"), "monitor.kratos.kregel.dev")
	if err == nil {
		t.Fatal("ExtractDomainCert() expected error for missing file, got nil")
	}
}

func TestWriteCertFiles_ModesAndContent(t *testing.T) {
	dir := t.TempDir()
	certDir := filepath.Join(dir, "certs")

	if err := WriteCertFiles(certDir, []byte(testCertPEM), []byte(testKeyPEM)); err != nil {
		t.Fatalf("WriteCertFiles() error = %v", err)
	}

	certData, err := os.ReadFile(filepath.Join(certDir, "fullchain.pem"))
	if err != nil {
		t.Fatalf("read fullchain.pem: %v", err)
	}
	if string(certData) != testCertPEM {
		t.Errorf("fullchain.pem content = %q, want %q", certData, testCertPEM)
	}

	keyInfo, err := os.Stat(filepath.Join(certDir, "privkey.pem"))
	if err != nil {
		t.Fatalf("stat privkey.pem: %v", err)
	}
	if mode := keyInfo.Mode().Perm(); mode != 0o600 {
		t.Errorf("privkey.pem mode = %o, want 0600", mode)
	}

	certInfo, err := os.Stat(filepath.Join(certDir, "fullchain.pem"))
	if err != nil {
		t.Fatalf("stat fullchain.pem: %v", err)
	}
	if mode := certInfo.Mode().Perm(); mode != 0o644 {
		t.Errorf("fullchain.pem mode = %o, want 0644", mode)
	}
}

func TestWatchAndSync_InitialExtractAndReload(t *testing.T) {
	dir := t.TempDir()
	acmePath := filepath.Join(dir, "acme.json")
	certDir := filepath.Join(dir, "certs")
	domain := "monitor.kratos.kregel.dev"

	writeAcmeFixture(t, acmePath, map[string][]certEntry{
		"letsencrypt": {fixtureEntry(domain, nil)},
	})

	log, err := logging.New(logging.Options{Level: "error"})
	if err != nil {
		t.Fatalf("logging.New() error = %v", err)
	}
	cancel, err := WatchAndSync(acmePath, domain, certDir, log)
	if err != nil {
		t.Fatalf("WatchAndSync() error = %v", err)
	}
	defer cancel()

	certData, err := os.ReadFile(filepath.Join(certDir, "fullchain.pem"))
	if err != nil {
		t.Fatalf("read fullchain.pem after initial sync: %v", err)
	}
	if string(certData) != testCertPEM {
		t.Errorf("initial fullchain.pem = %q, want %q", certData, testCertPEM)
	}

	// Simulate Traefik renewing: rewrite acme.json with new cert bytes via
	// temp-file-plus-rename, matching how Traefik itself writes it.
	newCert := "-----BEGIN CERTIFICATE-----\nrenewed-cert\n-----END CERTIFICATE-----\n"
	store := acmeStore{"letsencrypt": {Certificates: []certEntry{{
		Domain:      certDomain{Main: domain},
		Certificate: base64.StdEncoding.EncodeToString([]byte(newCert)),
		Key:         base64.StdEncoding.EncodeToString([]byte(testKeyPEM)),
	}}}}
	data, err := json.Marshal(store)
	if err != nil {
		t.Fatal(err)
	}
	tmp := acmePath + ".newtmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, acmePath); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		certData, err := os.ReadFile(filepath.Join(certDir, "fullchain.pem"))
		if err == nil && string(certData) == newCert {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("fullchain.pem was not updated after acme.json rename; last read = %q, err = %v", certData, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
