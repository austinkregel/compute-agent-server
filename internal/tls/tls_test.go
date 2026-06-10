package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCerts creates a self-signed cert and key in the given directory.
func generateTestCerts(t *testing.T, dir string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	// Write fullchain.pem
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}
	// Write chain.pem (same as cert for self-signed)
	if err := os.WriteFile(filepath.Join(dir, "chain.pem"), certPEM, 0o644); err != nil {
		t.Fatal(err)
	}

	// Write privkey.pem
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCertBundle_Success(t *testing.T) {
	dir := t.TempDir()
	generateTestCerts(t, dir)

	bundle, err := LoadCertBundle(dir)
	if err != nil {
		t.Fatalf("LoadCertBundle() error = %v", err)
	}
	if len(bundle.Certificate) == 0 {
		t.Error("Certificate chain is empty")
	}
}

func TestLoadCertBundle_MissingKey(t *testing.T) {
	dir := t.TempDir()
	generateTestCerts(t, dir)
	os.Remove(filepath.Join(dir, "privkey.pem"))

	_, err := LoadCertBundle(dir)
	if err == nil {
		t.Fatal("LoadCertBundle() should fail when privkey.pem is missing")
	}
}

func TestLoadCertBundle_MissingCert(t *testing.T) {
	dir := t.TempDir()
	generateTestCerts(t, dir)
	os.Remove(filepath.Join(dir, "fullchain.pem"))

	_, err := LoadCertBundle(dir)
	if err == nil {
		t.Fatal("LoadCertBundle() should fail when fullchain.pem is missing")
	}
}

func TestNewTLSConfig(t *testing.T) {
	dir := t.TempDir()
	generateTestCerts(t, dir)

	tlsCfg, err := NewTLSConfig(dir)
	if err != nil {
		t.Fatalf("NewTLSConfig() error = %v", err)
	}
	if tlsCfg.GetCertificate == nil {
		t.Error("GetCertificate callback should be set")
	}
	if tlsCfg.MinVersion == 0 {
		t.Error("MinVersion should be set")
	}
}

func TestReloadCert(t *testing.T) {
	dir := t.TempDir()
	generateTestCerts(t, dir)

	mgr := &CertManager{certDir: dir}
	if err := mgr.loadCert(); err != nil {
		t.Fatalf("initial loadCert() error = %v", err)
	}

	// Regenerate certs (simulates renewal)
	generateTestCerts(t, dir)
	if err := mgr.loadCert(); err != nil {
		t.Fatalf("reload loadCert() error = %v", err)
	}
}
