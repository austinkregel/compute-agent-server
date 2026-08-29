package traefikroute

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// The backend is addressed by IP while the certificate it presents is the one
// Traefik issued for the domain. Without a serversTransport pinning
// serverName, Traefik's dial to the backend fails x509 hostname verification
// and returns 502 before the WebSocket upgrade completes.
func TestWriteRouterConfig_PinsBackendServerName(t *testing.T) {
	dir := t.TempDir()
	domain := "monitor.kratos.kregel.dev"

	if err := WriteRouterConfig(dir, domain, "https://10.0.1.1:8443"); err != nil {
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

	st, ok := got.HTTP.ServersTransports[resourceName]
	if !ok {
		t.Fatalf("serversTransport %q not present: backend TLS would verify against the IP in the backend URL", resourceName)
	}
	if st.ServerName != domain {
		t.Errorf("serversTransport.serverName = %q, want %q", st.ServerName, domain)
	}

	svc := got.HTTP.Services[resourceName]
	if svc.LoadBalancer.ServersTransport != resourceName {
		t.Errorf("service.loadBalancer.serversTransport = %q, want %q; an unreferenced transport is inert",
			svc.LoadBalancer.ServersTransport, resourceName)
	}
}

// insecureSkipVerify would make the backend hop unauthenticated, defeating the
// point of carrying the edge certificate through to the backend.
func TestWriteRouterConfig_NoInsecureSkipVerify(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRouterConfig(dir, "monitor.kratos.kregel.dev", "https://10.0.1.1:8443"); err != nil {
		t.Fatalf("WriteRouterConfig() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, routerFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "insecureSkipVerify") {
		t.Errorf("router config must not disable backend cert verification:\n%s", data)
	}
}
