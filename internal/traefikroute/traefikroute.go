// Package traefikroute self-registers a Traefik file-provider route (router
// + service) for this server, mirroring how homelab-in-a-box registers its
// own ingress (Homelab.Gateways.Traefik.register_route/3) rather than
// relying on static Docker-label discovery — this server isn't on Traefik's
// discovered network.
package traefikroute

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// certResolverName is the Traefik certificate-resolver configured for ACME
// (DNS-01) issuance on the target deployment. Single-resolver setup — not
// worth a config field.
const certResolverName = "letsencrypt"

const routerFileName = "backup-server.json"

// resourceName is the shared name for the router, service, and
// serversTransport this package registers.
const resourceName = "backup-server"

// WriteRouterConfig renders a Traefik file-provider config registering a
// router for Host(`domain`) with TLS via certResolverName, proxying to a
// single-server service at backendURL, and atomically writes it to
// dynamicDir/backup-server.json.
//
// The service is bound to a serversTransport pinning serverName to domain.
// backendURL normally addresses the backend by IP (the Docker network
// gateway reaching this host), while the certificate this server presents is
// the one Traefik issued for domain and traefikcert syncs into certDir — so
// without serverName the backend hop fails x509 hostname verification and
// Traefik returns 502 before the WebSocket upgrade. Pinning the name keeps
// TLS terminated at the edge *and* re-established to the backend under the
// same certificate, verified against the system roots.
func WriteRouterConfig(dynamicDir, domain, backendURL string) error {
	if domain == "" {
		return fmt.Errorf("traefikroute: domain is required")
	}
	if backendURL == "" {
		return fmt.Errorf("traefikroute: backendURL is required")
	}

	config := dynamicConfig{
		HTTP: httpConfig{
			Routers: map[string]router{
				resourceName: {
					Rule:    fmt.Sprintf("Host(`%s`)", domain),
					Service: resourceName,
					TLS:     &routerTLS{CertResolver: certResolverName},
				},
			},
			Services: map[string]service{
				resourceName: {
					LoadBalancer: loadBalancer{
						Servers:          []server{{URL: backendURL}},
						ServersTransport: resourceName,
					},
				},
			},
			ServersTransports: map[string]serversTransport{
				resourceName: {ServerName: domain},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal router config: %w", err)
	}

	if err := os.MkdirAll(dynamicDir, 0o755); err != nil {
		return fmt.Errorf("create dynamic dir: %w", err)
	}

	path := filepath.Join(dynamicDir, routerFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write router config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename router config: %w", err)
	}
	return nil
}

type dynamicConfig struct {
	HTTP httpConfig `json:"http"`
}

type httpConfig struct {
	Routers           map[string]router           `json:"routers"`
	Services          map[string]service          `json:"services"`
	ServersTransports map[string]serversTransport `json:"serversTransports"`
}

// serversTransport configures Traefik's TLS dial to the backend. ServerName
// sets both the SNI sent and the name verified against the backend's
// certificate. No rootCAs entry: the certificate is publicly issued, so
// Traefik's system trust store already covers it — and no insecureSkipVerify,
// which would silently drop verification on the backend hop.
type serversTransport struct {
	ServerName string `json:"serverName"`
}

type router struct {
	Rule    string     `json:"rule"`
	Service string     `json:"service"`
	TLS     *routerTLS `json:"tls,omitempty"`
}

type routerTLS struct {
	CertResolver string `json:"certResolver"`
}

type service struct {
	LoadBalancer loadBalancer `json:"loadBalancer"`
}

type loadBalancer struct {
	Servers          []server `json:"servers"`
	ServersTransport string   `json:"serversTransport,omitempty"`
}

type server struct {
	URL string `json:"url"`
}
