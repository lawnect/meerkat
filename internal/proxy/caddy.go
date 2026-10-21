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

const caddyHeader = "{\n\tmetrics {\n\t\tper_host\n\t}\n}\n\n:9090 {\n\tmetrics /metrics\n}\n\n"

// EmptyCaddyfile starts Caddy without requesting a public certificate.
// The Outlet replaces it after an administrator registers a domain.
func EmptyCaddyfile() []byte { return []byte(caddyHeader) }

func Caddyfile(domain string, routes []HTTPRoute) ([]byte, error) {
	return CaddyfileDomains(domain, []string{domain}, routes)
}

func CaddyfileDomains(primary string, domains []string, routes []HTTPRoute) ([]byte, error) {
	if !validation.Hostname(primary) || len(domains) == 0 || domains[0] != primary {
		return nil, errors.New("invalid outlet domain")
	}
	for i, domain := range domains {
		if !validation.Hostname(domain) {
			return nil, errors.New("invalid outlet domain")
		}
		for _, earlier := range domains[:i] {
			if domain == earlier || strings.HasSuffix(domain, "."+earlier) || strings.HasSuffix(earlier, "."+domain) {
				return nil, errors.New("overlapping outlet domains")
			}
		}
	}
	copyRoutes := append([]HTTPRoute(nil), routes...)
	sort.Slice(copyRoutes, func(i, j int) bool { return copyRoutes[i].Hostname < copyRoutes[j].Hostname })
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteString(caddyHeader)
	fmt.Fprintf(&b, "%s {\n", primary)
	writeHTTP01TLS(&b)
	b.WriteString("\treverse_proxy https://outlet:8443 {\n\t\ttransport http {\n\t\t\ttls_trust_pool file /etc/caddy/root-ca.pem\n\t\t\ttls_server_name outlet\n\t\t}\n\t}\n}\n")
	for _, domain := range domains {
		if domain != primary {
			fmt.Fprintf(&b, "\n%s {\n", domain)
			writeHTTP01TLS(&b)
			b.WriteString("\trespond 404\n}\n")
		}
	}
	for _, route := range copyRoutes {
		allowed := false
		for _, domain := range domains {
			if strings.HasSuffix(route.Hostname, "."+domain) {
				allowed = true
				break
			}
		}
		if !validation.Hostname(route.Hostname) || !allowed || seen[route.Hostname] {
			return nil, errors.New("invalid or duplicate route hostname")
		}
		if err := validation.Endpoint(route.Upstream); err != nil {
			return nil, errors.New("invalid route upstream")
		}
		host, _, _ := net.SplitHostPort(route.Upstream)
		if host != "outlet" {
			return nil, errors.New("route upstream must use the Outlet network alias")
		}
		seen[route.Hostname] = true
		fmt.Fprintf(&b, "\n%s {\n", route.Hostname)
		writeHTTP01TLS(&b)
		fmt.Fprintf(&b, "\treverse_proxy http://%s\n}\n", route.Upstream)
	}
	return []byte(b.String()), nil
}

func writeHTTP01TLS(b *strings.Builder) {
	b.WriteString("\ttls {\n\t\tissuer acme {\n\t\t\tdisable_tlsalpn_challenge\n\t\t}\n\t}\n")
}
