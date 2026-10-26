package outlet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

// proxyFixture is an Outlet on example.com with one connected Connector and
// one service per upstream, each served by a local HTTP server.
type proxyFixture struct {
	store     *Store
	db        *control.Database
	handler   http.Handler
	admin     string
	adminHost string
	connector control.Connector
	services  []control.Service
	upstreams []*httptest.Server
}

func newProxyFixture(tb testing.TB, upstreams ...http.Handler) proxyFixture {
	tb.Helper()
	ctx := context.Background()
	dir := tb.TempDir()
	db, err := control.Open(ctx, filepath.Join(dir, "control.db"))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { db.Close() })
	f := proxyFixture{db: db, admin: Token()}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(f.admin), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 3600, TunnelRemoteAddress: "outlet:2333", TunnelServerPublicKey: key}
	if f.store, err = OpenWithControl(cfg, dir, db); err != nil {
		tb.Fatal(err)
	}
	outlet, err := db.SetDomain(ctx, "example.com", "")
	if err != nil {
		tb.Fatal(err)
	}
	f.adminHost = outlet.AdminHostname
	if f.connector, err = db.CreateConnector(ctx, "edge"); err != nil {
		tb.Fatal(err)
	}
	record := Record{Seed: Seed{Name: "edge", Configuration: contract.Configuration{ConnectorID: f.connector.ID}}, ProbeAddresses: map[string]string{}, ServiceStatuses: map[string]string{}, Enrolled: true,
		// In the future, so the Connector stays connected for the whole test.
		LastSeen: time.Now().Add(24 * time.Hour)}
	for i, h := range upstreams {
		service, err := db.CreateService(ctx, f.connector.ID, fmt.Sprintf("svc-%d", i), "http", contract.Target{Host: "app", Port: 3000 + i})
		if err != nil {
			tb.Fatal(err)
		}
		server := httptest.NewServer(h)
		tb.Cleanup(server.Close)
		f.services, f.upstreams = append(f.services, service), append(f.upstreams, server)
		record.ProbeAddresses[service.ID] = strings.TrimPrefix(server.URL, "http://")
		record.ServiceStatuses[service.ID] = "connected"
	}
	f.store.mu.Lock()
	f.store.managed[f.connector.ID] = record
	f.store.mu.Unlock()
	f.handler = f.store.PublicHandler()
	return f
}

// echo names the upstream and the host it was asked for, so a response
// routed to the wrong service or host is detectable.
func echo(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, name+" "+r.Host)
	})
}

func (f proxyFixture) get(host string) (int, string) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = host
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

func (f proxyFixture) certificateAsk(domain string) int {
	r := httptest.NewRequest("GET", certificateAskPath+"?domain="+domain, nil)
	r.Host = "outlet:8080"
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Code
}

func (f proxyFixture) route(tb testing.TB, service int, host string) string {
	tb.Helper()
	w := request(f.store.Handler(), "POST", "/api/v1/services/"+f.services[service].ID+"/routes", f.admin, map[string]any{"hostname": host})
	if w.Code != 201 {
		tb.Fatalf("route %s: %d %s", host, w.Code, w.Body.String())
	}
	var route control.Route
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil {
		tb.Fatal(err)
	}
	return route.ID
}

func (f proxyFixture) unroute(tb testing.TB, service int, id string) {
	tb.Helper()
	if w := request(f.store.Handler(), "DELETE", "/api/v1/services/"+f.services[service].ID+"/routes/"+id, f.admin, nil); w.Code != 204 {
		tb.Fatalf("delete route: %d %s", w.Code, w.Body.String())
	}
}

func TestPublicRoutingFollowsAdminChanges(t *testing.T) {
	f := newProxyFixture(t, echo("A"), echo("B"))
	h := f.store.Handler()
	expect := func(host string, code int, body string) {
		t.Helper()
		if gotCode, gotBody := f.get(host); gotCode != code || (body != "" && gotBody != body) {
			t.Fatalf("%s: %d %q, want %d %q", host, gotCode, gotBody, code, body)
		}
	}
	expect("app.example.com", 404, "")
	app := f.route(t, 0, "app.example.com")
	expect("app.example.com", 200, "A app.example.com")
	api := f.route(t, 1, "api.example.com")
	expect("api.example.com", 200, "B api.example.com")
	if f.certificateAsk("api.example.com") != 204 {
		t.Fatal("certificate refused for a new route")
	}
	f.unroute(t, 1, api)
	expect("api.example.com", 404, "")
	if f.certificateAsk("api.example.com") != 404 {
		t.Fatal("certificate allowed for a deleted route")
	}
	if w := request(h, "POST", "/api/v1/outlet/domains", f.admin, map[string]any{"domain": "example.org", "mode": "hosts"}); w.Code != 201 {
		t.Fatalf("add domain: %d %s", w.Code, w.Body.String())
	}
	if f.certificateAsk("example.org") != 204 {
		t.Fatal("certificate refused for an added domain")
	}
	org := f.route(t, 1, "app.example.org")
	expect("app.example.org", 200, "B app.example.org")
	if w := request(h, "PUT", "/api/v1/outlet/domains/example.org/mode", f.admin, map[string]any{"mode": "wildcard"}); w.Code != 200 {
		t.Fatalf("mode: %d %s", w.Code, w.Body.String())
	}
	expect("app.example.org", 200, "B app.example.org")
	f.unroute(t, 1, org)
	if w := request(h, "DELETE", "/api/v1/outlet/domains/example.org", f.admin, nil); w.Code != 200 {
		t.Fatalf("remove domain: %d %s", w.Code, w.Body.String())
	}
	if f.certificateAsk("example.org") != 404 {
		t.Fatal("certificate allowed for a removed domain")
	}
	f.unroute(t, 0, app)
	expect("app.example.com", 404, "")
	f.route(t, 0, "app.example.com")
	expect("app.example.com", 200, "A app.example.com")
	if w := request(h, "DELETE", "/api/v1/connectors/"+f.connector.ID, f.admin, nil); w.Code != 204 {
		t.Fatalf("delete connector: %d %s", w.Code, w.Body.String())
	}
	expect("app.example.com", 404, "")
	if f.certificateAsk("app.example.com") != 404 {
		t.Fatal("certificate allowed for a deleted Connector's route")
	}
}

// Run with -race: public traffic while routes, domain modes, rate limits and
// Connector state change must never reach the wrong service or a deleted route.
func TestPublicRoutingUnderConfigChurn(t *testing.T) {
	f := newProxyFixture(t, echo("A"), echo("B"))
	f.route(t, 0, "a.example.com")
	f.route(t, 1, "b.example.com")
	h := f.store.Handler()
	var stop atomic.Bool
	var served atomic.Int64
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := 0; !stop.Load(); i++ {
				host, want := "a.example.com", "A a.example.com"
				if (g+i)%2 == 1 {
					host, want = "b.example.com", "B b.example.com"
				}
				r := httptest.NewRequest("GET", "/", nil)
				r.Host = host
				r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.%d.%d", g, i%250+1))
				w := httptest.NewRecorder()
				f.handler.ServeHTTP(w, r)
				if w.Code != 200 || w.Body.String() != want {
					t.Errorf("%s: %d %q", host, w.Code, w.Body.String())
					return
				}
				served.Add(1)
			}
		})
	}
	modes := []string{"hosts", "wildcard"}
	for i := range 40 {
		host := fmt.Sprintf("c%d.example.com", i)
		id := f.route(t, i%2, host)
		want := []string{"A ", "B "}[i%2] + host
		if code, body := f.get(host); code != 200 || body != want {
			t.Fatalf("new route %s: %d %q", host, code, body)
		}
		f.unroute(t, i%2, id)
		if code, _ := f.get(host); code != 404 {
			t.Fatalf("deleted route %s: %d", host, code)
		}
		if w := request(h, "PUT", "/api/v1/outlet/domains/example.com/mode", f.admin, map[string]any{"mode": modes[i%2]}); w.Code != 200 {
			t.Fatalf("mode: %d %s", w.Code, w.Body.String())
		}
		if w := request(h, "PUT", "/api/v1/security/rate-limit", f.admin, rateLimitBody(map[string]any{"enabled": i%3 != 0})); w.Code != 200 {
			t.Fatalf("rate limit: %d %s", w.Code, w.Body.String())
		}
		// What a heartbeat does to the Connector record.
		f.store.mu.Lock()
		record := f.store.managed[f.connector.ID]
		record.LastHeartbeat = &contract.Heartbeat{ObservedAt: time.Now()}
		if err := f.store.saveManaged(f.connector.ID, record); err != nil {
			t.Error(err)
		}
		f.store.mu.Unlock()
	}
	stop.Store(true)
	wg.Wait()
	if served.Load() == 0 {
		t.Fatal("no public request completed")
	}
}

// Concurrent requests reuse upstream connections: each new one would open
// another tunnel channel, and closing them exhausts ports under load.
func TestProxyReusesUpstreamConnections(t *testing.T) {
	var mu sync.Mutex
	peers := map[string]bool{}
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers[r.RemoteAddr] = true
		mu.Unlock()
	}))
	f.route(t, 0, "app.example.com")
	const clients, each = 20, 25
	var wg sync.WaitGroup
	for range clients {
		wg.Go(func() {
			for range each {
				if code, _ := f.get("app.example.com"); code != 200 {
					t.Errorf("status %d", code)
					return
				}
			}
		})
	}
	wg.Wait()
	if n := len(peers); n > clients+5 {
		t.Fatalf("%d upstream connections for %d concurrent clients", n, clients)
	}
}

// A second burst as large as the first must find every connection the first
// one opened still idle, even when the burst is larger than a fixed pool.
func TestProxyKeepsConnectionsForTheWholeBurst(t *testing.T) {
	const burst = 600
	var mu sync.Mutex
	peers := map[string]bool{}
	arrived := make(chan struct{}, burst)
	release := make(chan struct{})
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers[r.RemoteAddr] = true
		mu.Unlock()
		arrived <- struct{}{}
		<-release
	}))
	f.route(t, 0, "app.example.com")
	wave := func() {
		var wg sync.WaitGroup
		for range burst {
			wg.Go(func() {
				if code, _ := f.get("app.example.com"); code != 200 {
					t.Errorf("status %d", code)
				}
			})
		}
		// All requests are in flight at once before any is answered.
		for range burst {
			<-arrived
		}
		for range burst {
			release <- struct{}{}
		}
		wg.Wait()
	}
	wave()
	first := len(peers)
	wave()
	if first < burst || len(peers) > first+5 {
		t.Fatalf("first burst opened %d connections, the second %d more", first, len(peers)-first)
	}
}

func TestUpstreamFailuresAnswerQuickly(t *testing.T) {
	previous := upstreamHeaderTimeout
	upstreamHeaderTimeout = 200 * time.Millisecond
	defer func() { upstreamHeaderTimeout = previous }()
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	reset := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.(*net.TCPConn).SetLinger(0)
			conn.Close()
		}
	})
	f := newProxyFixture(t, hang, reset, echo("C"))
	f.route(t, 0, "hang.example.com")
	f.route(t, 1, "reset.example.com")
	f.route(t, 2, "gone.example.com")
	start := time.Now()
	if code, _ := f.get("hang.example.com"); code != http.StatusGatewayTimeout {
		t.Fatalf("hanging upstream: %d", code)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hanging upstream took %s", elapsed)
	}
	if code, _ := f.get("reset.example.com"); code != http.StatusBadGateway {
		t.Fatalf("reset upstream: %d", code)
	}
	if code, body := f.get("gone.example.com"); code != 200 || body != "C gone.example.com" {
		t.Fatalf("live upstream: %d %q", code, body)
	}
	address := strings.TrimPrefix(f.upstreams[2].URL, "http://")
	f.upstreams[2].Close()
	if code, _ := f.get("gone.example.com"); code != http.StatusBadGateway {
		t.Fatalf("stopped upstream: %d", code)
	}
	// The upstream comes back on the same port.
	ln, err := net.Listen("tcp", address)
	if err != nil {
		t.Skipf("port not reusable: %v", err)
	}
	back := &http.Server{Handler: echo("C")}
	go back.Serve(ln)
	defer back.Close()
	if code, body := f.get("gone.example.com"); code != 200 || body != "C gone.example.com" {
		t.Fatalf("recovered upstream: %d %q", code, body)
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("invalid byte in chunk length") }

// A request the client got wrong is a 4xx, not an upstream failure that
// would show up in error-rate alerts.
func TestClientErrorsAreNotUpstreamErrors(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	f.route(t, 0, "app.example.com")
	r := httptest.NewRequest("POST", "/", io.NopCloser(brokenBody{}))
	r.Host, r.ContentLength = "app.example.com", -1
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("broken body: %d %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest("CONNECT", "/", nil)
	r.Host = "app.example.com"
	w = httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("CONNECT: %d", w.Code)
	}
	if code, _ := f.get("app.example.com"); code != 200 {
		t.Fatalf("ordinary request: %d", code)
	}
}
