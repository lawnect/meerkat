// Package proxy renders the fixed public proxy configuration. Public routes
// are resolved by Outlet at request time, so route changes never reload Caddy.
package proxy

const caddyHeader = "{\n\tmetrics {\n\t\tper_host\n\t}\n}\n\n:9090 {\n\tmetrics /metrics\n}\n"

// EmptyCaddyfile starts Caddy without requesting a public certificate.
// The Outlet replaces it once an administrator registers the first domain.
func EmptyCaddyfile() []byte { return []byte(caddyHeader) }

// DynamicCaddyfile manages per-host certificates with HTTP-01 without
// embedding hostnames or route destinations in Caddy's running configuration.
func DynamicCaddyfile() []byte {
	return []byte(`{
	metrics {
		per_host
	}
	on_demand_tls {
		ask http://outlet:8080/.well-known/meerkat/cert-ask
	}
}

:9090 {
	metrics /metrics
}

http:// {
	redir https://{host}{uri} permanent
}

https:// {
	tls {
		on_demand
		issuer acme {
			disable_tlsalpn_challenge
		}
	}
	reverse_proxy http://outlet:8080
}
`)
}
