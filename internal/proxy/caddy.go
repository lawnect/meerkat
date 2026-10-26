// Package proxy renders the fixed public proxy configuration. Public routes
// are resolved by Outlet at request time, so route changes never reload Caddy.
package proxy

// caddyGlobal holds the global options both configurations share. Headers
// longer than the Outlet accepts are refused here instead of being buffered
// and forwarded. The Outlet enforces the configurable upload and stall
// limits itself, and Caddy passes the stall on to it; Caddy's own body and
// write idle timeouts sit above the longest the Outlet allows, as a backstop
// that never cuts first.
const caddyGlobal = `	metrics {
		per_host
	}
	servers {
		max_header_size 16KB
		timeouts {
			read_header 10s
			read_body_idle 1h
			write_idle 1h
		}
	}
`

const caddyHeader = "{\n" + caddyGlobal + "}\n\n:9090 {\n\tmetrics /metrics\n}\n"

// EmptyCaddyfile starts Caddy without requesting a public certificate.
// The Outlet replaces it once an administrator registers the first domain.
func EmptyCaddyfile() []byte { return []byte(caddyHeader) }

// DynamicCaddyfile manages per-host certificates with HTTP-01 without
// embedding hostnames or route destinations in Caddy's running configuration.
// Caddy keeps an idle connection to the Outlet for every request in flight,
// up to 4096, instead of its default 32, and closes them before the Outlet's
// 30 s idle timeout does; otherwise it spends its CPU opening connections
// and reuses ones the Outlet has just closed.
func DynamicCaddyfile() []byte {
	return []byte(`{
` + caddyGlobal + `	on_demand_tls {
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
	reverse_proxy http://outlet:8080 {
		transport http {
			keepalive 25s
			keepalive_idle_conns_per_host 4096
		}
	}
}
`)
}
