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

// WriteRouterConfig renders a Traefik file-provider config registering a
// router for Host(`domain`) with TLS via certResolverName, proxying to a
// single-server service at backendURL, and atomically writes it to
// dynamicDir/backup-server.json.
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
				"backup-server": {
					Rule:    fmt.Sprintf("Host(`%s`)", domain),
					Service: "backup-server",
					TLS:     &routerTLS{CertResolver: certResolverName},
				},
			},
			Services: map[string]service{
				"backup-server": {
					LoadBalancer: loadBalancer{
						Servers: []server{{URL: backendURL}},
					},
				},
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
	Routers  map[string]router  `json:"routers"`
	Services map[string]service `json:"services"`
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
	Servers []server `json:"servers"`
}

type server struct {
	URL string `json:"url"`
}
