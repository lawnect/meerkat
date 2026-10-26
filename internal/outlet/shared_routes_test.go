package outlet

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// share puts service on the address another service already serves.
func (f proxyFixture) share(tb testing.TB, service int, host string) string {
	tb.Helper()
	w := request(f.store.Handler(), "POST", "/api/v1/services/"+f.services[service].ID+"/routes", f.admin, map[string]any{"hostname": host, "shared": true})
	if w.Code != 201 {
		tb.Fatalf("share %s: %d %s", host, w.Code, w.Body.String())
	}
	var route control.Route
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil {
		tb.Fatal(err)
	}
	return route.ID
}

func (f proxyFixture) setStatus(service int, status string) {
	f.store.mu.Lock()
	defer f.store.mu.Unlock()
	record := f.store.managed[f.connector.ID]
	statuses := map[string]string{}
	for id, value := range record.ServiceStatuses {
		statuses[id] = value
	}
	statuses[f.services[service].ID] = status
	record.ServiceStatuses = statuses
	f.store.managed[f.connector.ID] = record
}

func (f proxyFixture) from(method, host, client string, body string) (int, string) {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, "/", nil)
	} else {
		r = httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	}
	r.Host = host
	if client != "" {
		r.Header.Set("X-Forwarded-For", client)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// Failover sends everything to the first connected service in priority
// order, moves on when it goes down, and follows a new order.
func TestSharedAddressFailover(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"))
	a := f.route(t, 0, "app.example.com")
	b := f.share(t, 1, "app.example.com")
	for range 3 {
		if code, body := f.get("app.example.com"); code != 200 || !strings.HasPrefix(body, "a ") {
			t.Fatalf("primary: %d %q", code, body)
		}
	}
	f.setStatus(0, "error")
	if code, body := f.get("app.example.com"); code != 200 || !strings.HasPrefix(body, "b ") {
		t.Fatalf("after the primary went down: %d %q", code, body)
	}
	f.setStatus(1, "error")
	if code, _ := f.get("app.example.com"); code != 404 {
		t.Fatalf("with nothing up: %d", code)
	}
	f.setStatus(0, "connected")
	f.setStatus(1, "connected")
	if w := request(f.store.Handler(), "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "failover", "order": []string{b, a}}); w.Code != 204 {
		t.Fatalf("reorder: %d %s", w.Code, w.Body.String())
	}
	if code, body := f.get("app.example.com"); code != 200 || !strings.HasPrefix(body, "b ") {
		t.Fatalf("after reordering: %d %q", code, body)
	}
}

// Balance takes turns, and sticky keeps one client on one service.
func TestSharedAddressBalance(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"))
	a := f.route(t, 0, "app.example.com")
	b := f.share(t, 1, "app.example.com")
	if w := request(f.store.Handler(), "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "balance", "order": []string{a, b}}); w.Code != 204 {
		t.Fatalf("balance: %d %s", w.Code, w.Body.String())
	}
	seen := map[string]int{}
	for range 10 {
		_, body := f.get("app.example.com")
		seen[body[:1]]++
	}
	if seen["a"] != 5 || seen["b"] != 5 {
		t.Fatalf("turns = %v", seen)
	}
	if w := request(f.store.Handler(), "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "balance", "sticky": true, "order": []string{a, b}}); w.Code != 204 {
		t.Fatalf("sticky: %d %s", w.Code, w.Body.String())
	}
	spread := map[string]bool{}
	for i := range 20 {
		client := "203.0.113." + strconv.Itoa(10+i)
		_, first := f.from("GET", "app.example.com", client, "")
		for range 3 {
			if _, again := f.from("GET", "app.example.com", client, ""); again != first {
				t.Fatalf("client %s moved: %q then %q", client, first, again)
			}
		}
		spread[first[:1]] = true
	}
	if len(spread) != 2 {
		t.Fatalf("twenty clients all on %v", spread)
	}
}

// A request that can't reach its service goes to the next one when it has
// no body; one with a body isn't sent twice.
func TestSharedAddressRetriesTheNextService(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"))
	f.route(t, 0, "app.example.com")
	f.share(t, 1, "app.example.com")
	// The first service's tunnel port stops answering before the Outlet
	// notices: its status still says connected.
	f.upstreams[0].Close()
	if code, body := f.get("app.example.com"); code != 200 || !strings.HasPrefix(body, "b ") {
		t.Fatalf("GET: %d %q", code, body)
	}
	if code, body := f.from("DELETE", "app.example.com", "", ""); code != 200 || !strings.HasPrefix(body, "b ") {
		t.Fatalf("DELETE without a body after a refused connection: %d %q", code, body)
	}
	if code, _ := f.from("POST", "app.example.com", "", "payload"); code != 502 {
		t.Fatalf("POST with a body: %d", code)
	}
}

// hangUp accepts the connection and closes it without answering, as a
// tunnel port does while its Connector is away.
func hangUp() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
}

// A connection that opens and then drops may have carried the request, so
// only methods safe to repeat go on; a WebSocket upgrade is a GET.
func TestSharedAddressRetriesOnlySafeMethodsAfterAConnection(t *testing.T) {
	f := newProxyFixture(t, hangUp(), echo("b"))
	f.route(t, 0, "app.example.com")
	f.share(t, 1, "app.example.com")
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		if code, _ := f.from(method, "app.example.com", "", ""); code != 200 {
			t.Fatalf("%s: %d", method, code)
		}
	}
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if code, _ := f.from(method, "app.example.com", "", ""); code != 502 {
			t.Fatalf("%s after a dropped connection: %d", method, code)
		}
	}
	server := httptest.NewServer(f.handler)
	defer server.Close()
	conn, reader := dial(t, strings.TrimPrefix(server.URL, "http://"))
	_, _ = conn.Write([]byte("GET /live/websocket HTTP/1.1\r\nHost: app.example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"))
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("upgrade moved on: %v %v", res, err)
	}
}

// With sticky balance, only the clients of a service that goes down move.
func TestStickyClientsStayWhenAnotherServiceLeaves(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"), echo("c"))
	a := f.route(t, 0, "app.example.com")
	b := f.share(t, 1, "app.example.com")
	c := f.share(t, 2, "app.example.com")
	if w := request(f.store.Handler(), "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "balance", "sticky": true, "order": []string{a, b, c}}); w.Code != 204 {
		t.Fatalf("sticky: %d %s", w.Code, w.Body.String())
	}
	before := map[string]string{}
	for i := range 60 {
		client := "198.51.100." + strconv.Itoa(i+1)
		_, body := f.from("GET", "app.example.com", client, "")
		before[client] = body[:1]
	}
	f.setStatus(0, "error")
	moved := 0
	for client, was := range before {
		_, body := f.from("GET", "app.example.com", client, "")
		now := body[:1]
		if was != "a" && now != was {
			t.Fatalf("client %s on %s moved to %s when a left", client, was, now)
		}
		if was == "a" {
			moved++
			if now == "a" {
				t.Fatalf("client %s still on the service that left", client)
			}
		}
	}
	if moved == 0 || moved == len(before) {
		t.Fatalf("%d of %d clients were on a", moved, len(before))
	}
	// No client address: turns instead of a crash.
	if code, _ := f.from("GET", "app.example.com", "", ""); code != 200 {
		t.Fatalf("without a client address: %d", code)
	}
}

// Many concurrent requests take turns evenly; run with -race.
func TestBalanceUnderConcurrency(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"))
	a := f.route(t, 0, "app.example.com")
	b := f.share(t, 1, "app.example.com")
	if w := request(f.store.Handler(), "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "balance", "order": []string{a, b}}); w.Code != 204 {
		t.Fatalf("balance: %d", w.Code)
	}
	results := make(chan string, 200)
	for range 200 {
		go func() {
			_, body := f.get("app.example.com")
			results <- body[:1]
		}()
	}
	seen := map[string]int{}
	for range 200 {
		seen[<-results]++
	}
	if seen["a"] != 100 || seen["b"] != 100 {
		t.Fatalf("turns = %v", seen)
	}
}

// The API joins an address only on purpose, and checks policies.
func TestSharedAddressAPI(t *testing.T) {
	f := newProxyFixture(t, echo("a"), echo("b"))
	a := f.route(t, 0, "app.example.com")
	h := f.store.Handler()
	w := request(h, "POST", "/api/v1/services/"+f.services[1].ID+"/routes", f.admin, map[string]any{"hostname": "app.example.com"})
	if w.Code != 409 || errorCode(t, w) != "hostname_taken" {
		t.Fatalf("join without shared: %d %s", w.Code, w.Body.String())
	}
	b := f.share(t, 1, "app.example.com")
	if w := request(h, "POST", "/api/v1/services/"+f.services[1].ID+"/routes", f.admin, map[string]any{"hostname": "app.example.com", "shared": true}); w.Code != 409 {
		t.Fatalf("same service twice: %d", w.Code)
	}
	for _, c := range []struct {
		host string
		body map[string]any
		code int
		err  string
	}{
		{"app.example.com", map[string]any{"mode": "random", "order": []string{a, b}}, 400, "invalid_address_policy"},
		{"app.example.com", map[string]any{"mode": "balance", "order": []string{a}}, 400, "invalid_address_policy"},
		{"app.example.com", map[string]any{"mode": "balance"}, 400, "invalid_request"},
		{"none.example.com", map[string]any{"mode": "balance", "order": []string{}}, 404, "address_not_found"},
		{"APP.example.com", map[string]any{"mode": "balance", "order": []string{a, b}}, 400, "invalid_address_policy"},
	} {
		w := request(h, "PUT", "/api/v1/addresses/"+c.host, f.admin, c.body)
		if w.Code != c.code || errorCode(t, w) != c.err {
			t.Fatalf("%s %v: %d %s", c.host, c.body, w.Code, w.Body.String())
		}
	}
	if w := request(h, "PUT", "/api/v1/addresses/app.example.com", "", map[string]any{"mode": "balance", "order": []string{a, b}}); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	if w := request(h, "PUT", "/api/v1/addresses/app.example.com", f.admin, map[string]any{"mode": "balance", "sticky": true, "order": []string{b, a}}); w.Code != 204 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Routes []control.Route `json:"routes"`
	}
	w = request(h, "GET", "/api/v1/services/"+f.services[0].ID+"/routes", f.admin, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Routes) != 1 {
		t.Fatalf("list: %s", w.Body.String())
	}
	if route := listed.Routes[0]; route.Priority != 1 || route.AddressMode != "balance" || !route.AddressSticky || route.AddressBackends != 2 {
		t.Fatalf("route = %+v", route)
	}
	// The certificate check and block rules work per address as before.
	if code := f.certificateAsk("app.example.com"); code != 204 {
		t.Fatalf("certificate: %d", code)
	}
	// Removing one service leaves the address to the other.
	f.unroute(t, 0, a)
	for range 4 {
		if code, body := f.get("app.example.com"); code != 200 || !strings.HasPrefix(body, "b ") {
			t.Fatalf("after removing a: %d %q", code, body)
		}
	}
}

// Caddy counts per host: a shared address is counted on its first service in
// failover order, and a disabled route counts for nothing.
func TestSharedAddressUsageGoesToTheFirstService(t *testing.T) {
	plans := []control.HTTPRoutePlan{
		{Route: control.Route{ID: "r1", Hostname: "app.example.com", Enabled: true, Priority: 0}},
		{Route: control.Route{ID: "r2", Hostname: "app.example.com", Enabled: true, Priority: 1}},
		{Route: control.Route{ID: "r3", Hostname: "off.example.com", Enabled: false}},
		{Route: control.Route{ID: "r4", Hostname: "off.example.com", Enabled: true, Priority: 1}},
	}
	hosts := usageHosts(plans)
	if len(hosts) != 2 || hosts["app.example.com"] != "r1" || hosts["off.example.com"] != "r4" {
		t.Fatalf("hosts = %v", hosts)
	}
}
