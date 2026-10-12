// Package proxy renders the generated public proxy configuration. User input
// never reaches a shell; values are validated before Caddyfile text is made.
package proxy

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/lawnect/meerkat/internal/validation"
)

type HTTPRoute struct {
	Hostname string
	Upstream string
}

func Caddyfile(domain string, routes []HTTPRoute) ([]byte, error) {
	if !validation.Hostname(domain) {
		return nil, errors.New("invalid outlet domain")
	}
	copyRoutes := append([]HTTPRoute(nil), routes...)
	sort.Slice(copyRoutes, func(i, j int) bool { return copyRoutes[i].Hostname < copyRoutes[j].Hostname })
	seen := map[string]bool{}
	var b strings.Builder
	fmt.Fprintf(&b, "{\n\temail {env.CADDY_EMAIL}\n}\n\n%s {\n", domain)
	b.WriteString("\ttls {\n\t\tdns cloudflare {env.CF_API_TOKEN}\n\t}\n\n")
	b.WriteString("\treverse_proxy https://outlet:8443 {\n\t\ttransport http {\n\t\t\ttls_trust_pool file /etc/caddy/root-ca.pem\n\t\t\ttls_server_name outlet\n\t\t}\n\t}\n}\n")
	for _, route := range copyRoutes {
		if !validation.Hostname(route.Hostname) || (route.Hostname != domain && !strings.HasSuffix(route.Hostname, "."+domain)) || seen[route.Hostname] {
			return nil, errors.New("invalid or duplicate route hostname")
		}
		if route.Hostname == domain {
			return nil, errors.New("outlet domain is reserved for the dashboard")
		}
		if err := validation.Endpoint(route.Upstream); err != nil {
			return nil, errors.New("invalid route upstream")
		}
		host, _, _ := net.SplitHostPort(route.Upstream)
		if host != "outlet" {
			return nil, errors.New("route upstream must use the Outlet network alias")
		}
		seen[route.Hostname] = true
		fmt.Fprintf(&b, "\n%s {\n\ttls {\n\t\tdns cloudflare {env.CF_API_TOKEN}\n\t}\n\treverse_proxy http://%s\n}\n", route.Hostname, route.Upstream)
	}
	return []byte(b.String()), nil
}
