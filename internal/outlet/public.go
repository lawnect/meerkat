package outlet

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

const certificateAskPath = "/.well-known/meerkat/cert-ask"

func publicHostname(raw string) string {
	host := strings.ToLower(raw)
	if strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	}
	if !validation.Hostname(host) {
		return ""
	}
	return host
}

// certificateAllowed is an authorization check, not a DNS or health check.
// It prevents an arbitrary SNI from causing an ACME order.
func (s *Store) certificateAllowed(ctx context.Context, hostname string) (bool, error) {
	if s.control == nil || !validation.Hostname(hostname) {
		return false, nil
	}
	outlet, err := s.control.Outlet(ctx)
	if err != nil {
		return false, err
	}
	if outlet.Domain == "" {
		return false, nil
	}
	if hostname == contract.AdminHostname(outlet.Domain) {
		return true, nil
	}
	for _, domain := range outlet.Domains {
		if hostname == domain {
			return true, nil
		}
	}
	_, found, err := s.control.PublicRoute(ctx, hostname)
	return found, err
}

// PublicHandler is served only on the internal Compose network. It does not
// expose the administrator API for arbitrary public hostnames.
func (s *Store) PublicHandler() http.Handler {
	admin := s.Handler()
	transport := &http.Transport{Proxy: nil}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == certificateAskPath && r.Host == "outlet:8080" {
			if r.Method != http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			allowed, err := s.certificateAllowed(r.Context(), r.URL.Query().Get("domain"))
			if err != nil {
				http.Error(w, "certificate authorization unavailable", http.StatusServiceUnavailable)
			} else if !allowed {
				http.NotFound(w, r)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
			return
		}
		host := publicHostname(r.Host)
		if host == "" || s.control == nil {
			http.NotFound(w, r)
			return
		}
		outlet, err := s.control.Outlet(r.Context())
		if err != nil {
			http.Error(w, "route unavailable", http.StatusServiceUnavailable)
			return
		}
		if host == contract.AdminHostname(outlet.Domain) {
			admin.ServeHTTP(w, r)
			return
		}
		if host == outlet.Domain {
			if strings.HasPrefix(r.URL.Path, "/connector/v1/") {
				admin.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		plan, found, err := s.control.PublicRoute(r.Context(), host)
		if err != nil {
			http.Error(w, "route unavailable", http.StatusServiceUnavailable)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		record, ok := s.managed[plan.ConnectorID]
		address := record.probeFor(plan.ServiceID)
		connected := ok && record.Enrolled && managedConnectorStatus(record, s.now()) == "connected" && record.ServiceStatuses[plan.ServiceID] == "connected" && isLoopbackAddress(address)
		s.mu.Unlock()
		if !connected {
			http.NotFound(w, r)
			return
		}
		upstream := &url.URL{Scheme: "http", Host: address}
		proxy := &httputil.ReverseProxy{
			Transport: transport,
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(upstream)
				pr.Out.Host = pr.In.Host
				pr.Out.Header.Set("X-Forwarded-Host", host)
				pr.Out.Header.Set("X-Forwarded-Proto", "https")
				pr.Out.Header.Set("X-Forwarded-Port", "443")
				pr.Out.Header.Del("X-Real-Ip")
				pr.Out.Header.Del("X-Forwarded-Ssl")
				pr.Out.Header.Del("X-Url-Scheme")
				forwardedFor := pr.In.Header.Get("X-Forwarded-For")
				if peer, _, err := net.SplitHostPort(pr.In.RemoteAddr); err == nil {
					if forwardedFor != "" {
						forwardedFor += ", "
					}
					forwardedFor += peer
				}
				if forwardedFor != "" {
					pr.Out.Header.Set("X-Forwarded-For", forwardedFor)
				}
			},
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, "upstream unavailable", http.StatusBadGateway)
			},
		}
		proxy.ServeHTTP(w, r)
	})
}
