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
		ExpectContinueTimeout: time.Second,
		// A proxy passes encodings through; transparent gzip would make the
		// Outlet decompress responses the client did not ask to compress.
		DisableCompression:     true,
		MaxResponseHeaderBytes: 1 << 20,
	}
}

// headerTimeoutKey carries a proxied request's response timeout to
// headerDeadline.
type headerTimeoutKey struct{}

// errHeaderTimeout fails a proxied request whose service took longer than
// the response timeout to send its response headers. It is a timeout, so the
// proxy answers 504.
var errHeaderTimeout error = headerTimeoutError{}

type headerTimeoutError struct{}

func (headerTimeoutError) Error() string   { return "timeout awaiting response headers" }
func (headerTimeoutError) Timeout() bool   { return true }
func (headerTimeoutError) Temporary() bool { return true }

// headerDeadline gives the service the response timeout of the request's
// hostname to send its response headers, counted like net/http's
// ResponseHeaderTimeout from when the request is written, again for each
// attempt the transport makes. That one is set per transport, and a
// transport per timeout would split the pool, redialing tunnel channels for
// every change of a timeout and for every hostname of a service with its own.
// Once the headers are in, the body streams for as long as it takes.
type headerDeadline struct{ *http.Transport }

func (t headerDeadline) RoundTrip(r *http.Request) (*http.Response, error) {
	timeout, _ := r.Context().Value(headerTimeoutKey{}).(time.Duration)
	if timeout <= 0 {
		return t.Transport.RoundTrip(r)
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	var mu sync.Mutex
	var timer *time.Timer
	answered := false
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) {
			mu.Lock()
			defer mu.Unlock()
			// A service may answer before it has the whole body; the
			// wait is over then.
			if answered {
				return
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(timeout, func() { cancel(errHeaderTimeout) })
		},
	})
	res, err := t.Transport.RoundTrip(r.WithContext(ctx))
	mu.Lock()
	answered = true
	if timer != nil {
		timer.Stop()
	}
	mu.Unlock()
	// The deadline may have passed as the headers came in; the body would
	// fail on the cancelled context.
	if context.Cause(ctx) == errHeaderTimeout {
		if res != nil {
			res.Body.Close()
		}
		return nil, errHeaderTimeout
	}
	// The context ends with the client's request, after the body is read.
	return res, err
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

// isPlainHTTP reports whether Caddy received r on port 80. Caddy sets
// X-Forwarded-Proto from its own connection and drops the client's value,
// since it trusts no proxy in front of it.
func isPlainHTTP(r *http.Request) bool {
	return r.Header.Get("X-Forwarded-Proto") == "http"
}

// redirectToHTTPS answers a request Caddy received on port 80 (Caddy has
// already answered ACME HTTP-01 challenges there). Only a host the Outlet
// would certify is sent on to HTTPS, so neither a forged Host nor an
// absolute-form request target, which Go puts in r.Host, can make it
// redirect elsewhere. Nothing is cached: a host's registration can change.
func (s *Store) redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// The body is never read. Without this the server would wait to drain
	// it after answering, and a client trickling a chunked body would hold
	// the connection with none of the HTTPS path's transfer limits.
	if r.ContentLength != 0 {
		w.Header().Set("Connection", "close")
	}
	host := strings.TrimSuffix(strings.ToLower(r.Host), ":80")
	// Origin-form only: "*" (OPTIONS) and CONNECT's authority have no path.
	target := r.URL.RequestURI()
	if r.Method == http.MethodConnect || !strings.HasPrefix(target, "/") {
		http.NotFound(w, r)
		return
	}
	allowed, err := s.certificateAllowed(r.Context(), host)
	if err != nil {
		http.Error(w, "route unavailable", http.StatusServiceUnavailable)
		return
	}
	// The management host redirects only those its IP limit lets in, so
	// port 80 doesn't show it to the rest.
	if allowed && s.isAdminHost(r.Context(), host) && !s.adminReachable(r) {
		allowed = false
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Location", "https://"+host+target)
	w.WriteHeader(http.StatusPermanentRedirect)
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
	transport := headerDeadline{upstreamTransport()}
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
		if isPlainHTTP(r) {
			s.redirectToHTTPS(w, r)
			return
		}
		// Every answer from here on is HTTPS, the Outlet's own (limits,
		// access sign-in, block rules) and an app's.
		s.setHSTS(w)
		began := time.Now()
		limits := s.connectionLimits()
		w, body := guardTransfer(w, r, limits)
		defer finishTransfer(w, body)
		host := publicHostname(r.Host)
		if host == "" || s.control == nil {
			http.NotFound(w, r)
			return
		}
		if s.limitRequest(w, r, host) {
			s.logTurnedAway(r, host, began, failRateLimited, http.StatusTooManyRequests)
			return
		}
		client, ok := s.admitClient(w, r, limits)
		if !ok {
			s.logTurnedAway(r, host, began, failBusy, http.StatusTooManyRequests)
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
			// Outside the allowed ranges the console isn't there at all.
			if !s.adminReachable(r) {
				http.NotFound(w, r)
				return
			}
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
		// The request log keeps every request to a routed address from here,
		// whatever becomes of it.
		w, r, logged := s.recordRequest(w, r, host, began)
		if logged != nil {
			defer logged()
		}
		// Rules compare the whole path, so one longer than apps take (see
		// maxPublicPath) would only make them slow.
		if len(r.URL.EscapedPath()) > maxPublicPath {
			http.Error(w, "request path too long", http.StatusRequestURITooLong)
			s.logFailure(r, host, began, failTooLong, http.StatusRequestURITooLong, "")
			return
		}
		// A banned client gets nothing from a routed address, not even the
		// sign-in of access protection. The management host and the Outlet
		// domain's connector API answered above and are never affected.
		origin, _ := requestOrigin(r)
		if s.refuseBanned(w, r, host, began, origin) {
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
			cause := failBlocked
			if rule.ban && !fromABrowser(r) {
				s.banClient(origin, rule, host, r.URL.Path)
				cause = failTrapped
			}
			blocked(w)
			s.logFailure(r, host, began, cause, http.StatusForbidden, "")
			return
		}
		backends := s.orderBackends(host, origin, s.liveBackends(plans), policy)
		if len(backends) == 0 {
			http.NotFound(w, r)
			s.logFailure(r, host, began, failNoService, http.StatusNotFound, "")
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
				s.logFailure(r, host, began, failTooLarge, http.StatusRequestEntityTooLarge, "")
				return
			}
			if body != nil {
				body.limit = limit
			}
		}
		held, ok := s.admitProxy(w, r, limits)
		if !ok {
			s.logFailure(r, host, began, failBusy, http.StatusServiceUnavailable, "")
			return
		}
		if held {
			defer s.connections.inflight.total.Add(-1)
		}
		// A request that can't reach one service goes on to the next, when
		// it can be sent again (see retryable). The next one is chosen afresh:
		// by then its route may be gone or the service down.
		headerTimeout := limits.responseTimeout(host)
		tried := map[string]bool{}
		// waited is the time spent on attempts that failed, the services'.
		var waited time.Duration
		var serve func(target backend)
		record := recordOf(r)
		serve = func(target backend) {
			tried[target.serviceID] = true
			if record != nil {
				record.service = target.service
			}
			// wrote says whether any attempt, the transport's own retries
			// included, put the request on a connection; responded, whether
			// the service answered. Either limits what may go on. timed
			// splits the time to the answer: the Outlet's, and the service's.
			var wrote, responded atomic.Bool
			attempt := time.Now()
			timed := &timing{began: began, waited: waited}
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
					outbound := context.WithValue(pr.Out.Context(), headerTimeoutKey{}, headerTimeout)
					pr.Out = pr.Out.WithContext(httptrace.WithClientTrace(outbound, &httptrace.ClientTrace{
						GotConn: func(httptrace.GotConnInfo) { timed.ready.Store(time.Now().UnixNano()) },
						WroteHeaders: func() {
							timed.sent.Store(time.Now().UnixNano())
							wrote.Store(true)
						},
						WroteRequest: func(httptrace.WroteRequestInfo) { timed.written.Store(time.Now().UnixNano()) },
					}))
				},
				ModifyResponse: func(res *http.Response) error {
					responded.Store(true)
					if outlet, upstream, ok := timed.latencies(time.Now()); ok {
						s.countLatencies(outlet, upstream)
						if record != nil {
							record.outletUS, record.upstreamUS = outlet.Microseconds(), upstream.Microseconds()
						}
					}
					s.answered(target.serviceID)
					dropAccessSetCookie(res.Header)
					s.applyHSTS(w, res)
					if res.StatusCode < 500 {
						s.noteServed(target.serviceID)
					} else {
						s.logFailure(r, host, began, failAppError, res.StatusCode, target.service)
					}
					return nil
				},
				ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
					// An upload that went wrong is the client's doing: the service
					// isn't passed over for it.
					upload := body != nil && (body.tooLarge.Load() || body.timedOut.Load() || body.failed.Load())
					if !responded.Load() && r.Context().Err() == nil && !upload {
						s.failed(target.serviceID)
					}
					// After an answer the response is under way, or an
					// upgraded connection taken over: never another try.
					if !responded.Load() && retryable(r, err, wrote.Load()) {
						if next, ok := s.nextBackend(host, origin, tried); ok {
							waited += time.Since(attempt)
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
						s.logFailure(r, host, began, failTooLarge, http.StatusRequestEntityTooLarge, target.service)
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
						s.logFailure(r, host, began, failTimeout, http.StatusGatewayTimeout, target.service)
						return
					}
					http.Error(w, "upstream unavailable", http.StatusBadGateway)
					s.logFailure(r, host, began, failUnreachable, http.StatusBadGateway, target.service)
				},
			}
			proxy.ServeHTTP(w, r)
		}
		serve(backends[0])
	})
}
