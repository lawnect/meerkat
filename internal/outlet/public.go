package outlet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

const certificateAskPath = "/.well-known/meerkat/cert-ask"

// maxPublicPath is the longest path a public request may have, escaped as
// the app gets it:
// what nginx, Apache and Tomcat take in a whole request line by default.
// Block rules and access policies compare the path in full, each regular
// expression in time proportional to its length.
const maxPublicPath = 8192

// upstreamHeaderTimeout bounds the wait for an application's response
// headers, like nginx's proxy_read_timeout; the body may stream for longer.
// Tests shorten it before building a handler.
var upstreamHeaderTimeout = 60 * time.Second

// upstreamIdlePerService keeps an idle connection for every request that was
// in flight at once, so a burst never closes connections it will need again.
const upstreamIdlePerService = 16384

// upstreamTransport reaches services through their tunnel ports on loopback.
// Each new connection there opens a tunnel channel to the Connector. A pool
// smaller than the concurrency (net/http keeps two per host by default)
// closes and redials on almost every request, and the closed sockets wait in
// TIME_WAIT until loopback runs out of ports and requests fail with 502.
func upstreamTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConnsPerHost:   upstreamIdlePerService,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: upstreamHeaderTimeout,
		ExpectContinueTimeout: time.Second,
		// A proxy passes encodings through; transparent gzip would make the
		// Outlet decompress responses the client did not ask to compress.
		DisableCompression:     true,
		MaxResponseHeaderBytes: 1 << 20,
	}
}

type bufferPool struct{ pool sync.Pool }

func (b *bufferPool) Get() []byte {
	if buf, ok := b.pool.Get().(*[]byte); ok {
		return *buf
	}
	return make([]byte, 32<<10)
}

func (b *bufferPool) Put(buf []byte) { b.pool.Put(&buf) }

// proxyBuffers reuses the body copy buffers of proxied requests.
var proxyBuffers = &bufferPool{}

// noteServed records that a service answered proxied traffic. A heartbeat
// probe can time out on a busy Outlet while real requests flow, and such
// answers prove the tunnel works without another round trip.
func (s *Store) noteServed(serviceID string) {
	now := s.now().UnixNano()
	v, ok := s.served.Load(serviceID)
	if !ok {
		v, _ = s.served.LoadOrStore(serviceID, new(atomic.Int64))
	}
	// Once a second is precise enough, and spares the shared cache line.
	if last := v.(*atomic.Int64); now-last.Load() >= int64(time.Second) {
		last.Store(now)
	}
}

// servedSince reports whether the service answered proxied traffic after t.
func (s *Store) servedSince(serviceID string, t time.Time) bool {
	v, ok := s.served.Load(serviceID)
	return ok && !t.IsZero() && v.(*atomic.Int64).Load() > t.UnixNano()
}

// spoofableClientHeaders name a client address. Behind Caddy only
// X-Forwarded-For's last entry is real, so these are removed before a
// request reaches an app; X-Real-IP is then set from that entry.
var spoofableClientHeaders = []string{"X-Real-Ip", "Cf-Connecting-Ip", "Cf-Connecting-Ipv6", "True-Client-Ip", "X-Client-Ip", "Forwarded"}

// isCertificateAsk reports whether r is Caddy's own on-demand TLS check.
// Caddy calls it directly, without X-Forwarded-For; every request Caddy
// proxies for a client carries that header, so a client that sets
// Host: outlet:8080 (the upstream) still cannot ask which names the Outlet
// would certify.
func (s *Store) isCertificateAsk(r *http.Request) bool {
	return r.URL.Path == certificateAskPath && r.Host == s.config.CaddyUpstream && len(r.Header.Values("X-Forwarded-For")) == 0
}

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
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return false, err
	}
	outlet := routing.Outlet
	if outlet.Domain == "" {
		return false, nil
	}
	if hostname == outlet.AdminHostname {
		return true, nil
	}
	for _, domain := range outlet.Domains {
		if hostname == domain {
			return true, nil
		}
	}
	_, found := routing.Route(hostname)
	return found, nil
}

// PublicHandler is served only on the internal Compose network. It does not
// expose the administrator API for arbitrary public hostnames.
func (s *Store) PublicHandler() http.Handler {
	admin := s.Handler()
	transport := upstreamTransport()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), originKey{}, publicOrigin{ip: forwardedClient(r)}))
		if s.isCertificateAsk(r) {
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
		limits := s.connectionLimits()
		w, body := guardTransfer(w, r, limits)
		defer finishTransfer(w, body)
		host := publicHostname(r.Host)
		if host == "" || s.control == nil {
			http.NotFound(w, r)
			return
		}
		if s.limitRequest(w, r, host) {
			return
		}
		client, ok := s.admitClient(w, r, limits)
		if !ok {
			return
		}
		if client != "" {
			defer s.connections.inflight.release(client)
		}
		routing, err := s.control.Routing(r.Context())
		if err != nil {
			http.Error(w, "route unavailable", http.StatusServiceUnavailable)
			return
		}
		outlet := routing.Outlet
		if host == outlet.AdminHostname {
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
		plans, policy := routing.Routes(host)
		if len(plans) == 0 {
			http.NotFound(w, r)
			return
		}
		// Rules compare the whole path, so one longer than apps take (see
		// maxPublicPath) would only make them slow.
		if len(r.URL.EscapedPath()) > maxPublicPath {
			http.Error(w, "request path too long", http.StatusRequestURITooLong)
			return
		}
		// Access protection first: its own paths start with "/.", which the
		// default block rules refuse.
		next, pass := s.guardAccess(w, r, host, outlet.AdminHostname)
		if !pass {
			return
		}
		r = next
		if rule := s.blockedBy(host, r.URL.Path); rule != nil {
			rule.hits.Add(1)
			blocked(w)
			return
		}
		origin, _ := requestOrigin(r)
		backends := s.orderBackends(host, origin, s.liveBackends(plans), policy)
		if len(backends) == 0 {
			http.NotFound(w, r)
			return
		}
		// A tunnel is not an HTTP proxy for other destinations.
		if r.Method == http.MethodConnect {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if limit := limits.uploadLimit(host); limit > 0 {
			if r.ContentLength > limit {
				w.Header().Set("Connection", "close")
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			if body != nil {
				body.limit = limit
			}
		}
		held, ok := s.admitProxy(w, r, limits)
		if !ok {
			return
		}
		if held {
			defer s.connections.inflight.total.Add(-1)
		}
		// A request that can't reach one service goes on to the next, when
		// it can be sent again (see retryable). The next one is chosen afresh:
		// by then its route may be gone or the service down.
		tried := map[string]bool{}
		var serve func(target backend)
		serve = func(target backend) {
			tried[target.serviceID] = true
			// wrote says whether any attempt, the transport's own retries
			// included, put the request on a connection; responded, whether
			// the service answered. Either limits what may go on.
			var wrote, responded atomic.Bool
			upstream := &url.URL{Scheme: "http", Host: target.address}
			proxy := &httputil.ReverseProxy{
				Transport:  transport,
				BufferPool: proxyBuffers,
				Rewrite: func(pr *httputil.ProxyRequest) {
					pr.SetURL(upstream)
					pr.Out.Host = pr.In.Host
					pr.Out.Header.Set("X-Forwarded-Host", host)
					pr.Out.Header.Set("X-Forwarded-Proto", "https")
					pr.Out.Header.Set("X-Forwarded-Port", "443")
					pr.Out.Header.Del("X-Forwarded-Ssl")
					pr.Out.Header.Del("X-Url-Scheme")
					// Apps get one client address they can trust: X-Real-IP is
					// the address Caddy saw, never a value the client sent.
					// Headers a CDN would set, which an app moved off one might
					// still trust, are dropped since a client could forge them.
					for _, name := range spoofableClientHeaders {
						pr.Out.Header.Del(name)
					}
					if client := forwardedClient(pr.In); client != "" {
						pr.Out.Header.Set("X-Real-Ip", client)
					}
					// The person or access token access protection let in, never a
					// value the client sent; the session cookie and the token stay
					// with the Outlet.
					dropAccessHeaders(pr.Out.Header)
					if email, _ := pr.In.Context().Value(accessIdentityKey{}).(string); email != "" {
						pr.Out.Header.Set(accessEmailHeader, email)
					}
					if service, _ := pr.In.Context().Value(accessServiceKey{}).(string); service != "" {
						pr.Out.Header.Set(accessServiceHeader, service)
					}
					stripAccessCookie(pr.Out.Header)
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
					pr.Out = pr.Out.WithContext(httptrace.WithClientTrace(pr.Out.Context(), &httptrace.ClientTrace{WroteHeaders: func() { wrote.Store(true) }}))
				},
				ModifyResponse: func(res *http.Response) error {
					responded.Store(true)
					s.answered(target.serviceID)
					dropAccessSetCookie(res.Header)
					if res.StatusCode < 500 {
						s.noteServed(target.serviceID)
					}
					return nil
				},
				ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
					if !responded.Load() && r.Context().Err() == nil {
						s.failed(target.serviceID)
					}
					// After an answer the response is under way, or an
					// upgraded connection taken over: never another try.
					if !responded.Load() && retryable(r, err, wrote.Load()) {
						if next, ok := s.nextBackend(host, origin, tried); ok {
							serve(next)
							return
						}
					}
					// A malformed, abandoned, stalled or oversized upload is the
					// client's error, and must not count as an upstream failure in
					// error alerts. A failed read of the client's connection
					// cancels the request before the body records why, so a
					// cancelled request with its upload unfinished is the client's
					// too.
					clientGone := r.Context().Err() != nil
					switch {
					case body != nil && body.tooLarge.Load():
						w.Header().Set("Connection", "close")
						http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
						return
					case body != nil && (body.timedOut.Load() || clientGone && !body.complete.Load()):
						w.Header().Set("Connection", "close")
						http.Error(w, "request body timed out", http.StatusRequestTimeout)
						return
					case body != nil && body.failed.Load():
						http.Error(w, "invalid request body", http.StatusBadRequest)
						return
					case clientGone:
						// nginx's "client closed request"; nobody reads it.
						w.WriteHeader(499)
						return
					}
					var timeout net.Error
					if errors.As(err, &timeout) && timeout.Timeout() {
						http.Error(w, "upstream timed out", http.StatusGatewayTimeout)
						return
					}
					http.Error(w, "upstream unavailable", http.StatusBadGateway)
				},
			}
			proxy.ServeHTTP(w, r)
		}
		serve(backends[0])
	})
}
