package traefikroute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRouterConfig_Shape(t *testing.T) {
	dir := t.TempDir()
	domain := "monitor.kratos.kregel.dev"
	backendURL := "https://10.0.1.1:8443"

	if err := WriteRouterConfig(dir, domain, backendURL); err != nil {
		t.Fatalf("WriteRouterConfig() error = %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, routerFileName))
	if err != nil {
		t.Fatalf("read router config: %v", err)
	}

	var got dynamicConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal router config: %v", err)
	}

	r, ok := got.HTTP.Routers["backup-server"]
	if !ok {
		t.Fatal("router \"backup-server\" not present")
	}
	wantRule := "Host(`monitor.kratos.kregel.dev`)"
	if r.Rule != wantRule {
		t.Errorf("rule = %q, want %q", r.Rule, wantRule)
	}
	if r.Service != "backup-server" {
		t.Errorf("service = %q, want %q", r.Service, "backup-server")
	}
	if r.TLS == nil || r.TLS.CertResolver != certResolverName {
		t.Errorf("tls.certResolver = %+v, want %q", r.TLS, certResolverName)
	}

	svc, ok := got.HTTP.Services["backup-server"]
	if !ok {
		t.Fatal("service \"backup-server\" not present")
	}
	if len(svc.LoadBalancer.Servers) != 1 || svc.LoadBalancer.Servers[0].URL != backendURL {
		t.Errorf("servers = %+v, want single server with url %q", svc.LoadBalancer.Servers, backendURL)
	}
}

func TestWriteRouterConfig_RequiresDomainAndBackend(t *testing.T) {
	dir := t.TempDir()

	if err := WriteRouterConfig(dir, "", "https://10.0.1.1:8443"); err == nil {
		t.Error("expected error for empty domain, got nil")
	}
	if err := WriteRouterConfig(dir, "monitor.kratos.kregel.dev", ""); err == nil {
		t.Error("expected error for empty backendURL, got nil")
	}
}

func TestWriteRouterConfig_CreatesDynamicDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dynamic")

	if err := WriteRouterConfig(dir, "monitor.kratos.kregel.dev", "https://10.0.1.1:8443"); err != nil {
		t.Fatalf("WriteRouterConfig() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, routerFileName)); err != nil {
		t.Fatalf("expected router file to exist: %v", err)
	}
}

func TestWriteRouterConfig_Overwrites(t *testing.T) {
	dir := t.TempDir()

	if err := WriteRouterConfig(dir, "old.example.com", "https://10.0.1.1:8443"); err != nil {
		t.Fatal(err)
	}
	if err := WriteRouterConfig(dir, "monitor.kratos.kregel.dev", "https://10.0.1.1:9000"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, routerFileName))
	if err != nil {
		t.Fatal(err)
	}
	var got dynamicConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantRule := "Host(`monitor.kratos.kregel.dev`)"
	if got.HTTP.Routers["backup-server"].Rule != wantRule {
		t.Errorf("rule after overwrite = %q, want %q", got.HTTP.Routers["backup-server"].Rule, wantRule)
	}
}
