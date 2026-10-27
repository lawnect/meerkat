package outlet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

// requestFrom is request, made from the given client address.
func requestFrom(handler http.Handler, remote, method, path, token string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	r.RemoteAddr = remote
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func guardHeartbeat(response contract.EnrollmentResponse) contract.Heartbeat {
	return contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "healthy", ObservedAt: time.Now().UTC()}
}

func isKnown(s *Store, address string) bool {
	return s.guard.known.has(clientPrefix(netip.MustParseAddr(address)), s.now())
}

// Only an authenticated Connector API request makes an address known: a
// request anyone can send would make every address known and the guard
// pointless.
func TestConnectorRequestsMakeTheirAddressKnown(t *testing.T) {
	f := setup(t)
	h := f.store.Handler()
	credential := registered(t, f)
	hb := guardHeartbeat(credential)
	next := 100 // registered() above came from 192.0.2.1
	from := func() string { next++; return fmt.Sprintf("192.0.2.%d", next) }

	t.Run("junk never does", func(t *testing.T) {
		for name, call := range map[string]func(remote string) int{
			"heartbeat, unknown token":        func(r string) int { return requestFrom(h, r, "POST", "/connector/v1/heartbeat", Token(), hb).Code },
			"heartbeat, no token":             func(r string) int { return requestFrom(h, r, "POST", "/connector/v1/heartbeat", "", hb).Code },
			"heartbeat, the enrollment token": func(r string) int { return requestFrom(h, r, "POST", "/connector/v1/heartbeat", f.tokenB, hb).Code },
			"heartbeat, the admin token":      func(r string) int { return requestFrom(h, r, "POST", "/connector/v1/heartbeat", f.admin, hb).Code },
			"heartbeat, a credential and junk": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/heartbeat", credential.Credential, "junk").Code
			},
			"config, unknown token": func(r string) int { return requestFrom(h, r, "GET", "/connector/v1/config", Token(), nil).Code },
			"config, no token":      func(r string) int { return requestFrom(h, r, "GET", "/connector/v1/config", "", nil).Code },
			"enrollment, unknown token": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/enrollment", Token(), enrollment("connector-b")).Code
			},
			"enrollment, the wrong connector": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/enrollment", f.tokenB, enrollment("connector-a")).Code
			},
			"enrollment, a used token": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a")).Code
			},
			"enrollment, junk body": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/enrollment", f.tokenB, "junk").Code
			},
			"the admin API, a connector's token": func(r string) int {
				return requestFrom(h, r, "GET", "/api/v1/connectors", credential.Credential, nil).Code
			},
		} {
			remote := from()
			if code := call(remote + ":50000"); code == 200 || code == 201 {
				t.Errorf("%s: %d, expected a refusal", name, code)
			}
			if isKnown(f.store, remote) {
				t.Errorf("%s made %s known", name, remote)
			}
		}
	})
	t.Run("a page anyone may read doesn't", func(t *testing.T) {
		remote := from()
		if code := requestFrom(h, remote+":50000", "GET", "/healthz", "", nil).Code; code != 200 || isKnown(f.store, remote) {
			t.Fatalf("health check: %d, known %v", code, isKnown(f.store, remote))
		}
	})
	t.Run("a Connector's own requests do", func(t *testing.T) {
		for name, call := range map[string]func(remote string) int{
			"heartbeat": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/heartbeat", credential.Credential, hb).Code
			},
			"config": func(r string) int {
				return requestFrom(h, r, "GET", "/connector/v1/config", credential.Credential, nil).Code
			},
			"enrollment": func(r string) int {
				return requestFrom(h, r, "POST", "/connector/v1/enrollment", f.tokenB, enrollment("connector-b")).Code
			},
		} {
			remote := from()
			if code := call(remote + ":50000"); code != 200 && code != 201 {
				t.Fatalf("%s: %d", name, code)
			}
			if !isKnown(f.store, remote) {
				t.Errorf("%s didn't make %s known", name, remote)
			}
		}
	})
	t.Run("the client is the one Caddy forwarded, an IPv6 one by /64", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/connector/v1/config", nil)
		r.RemoteAddr = "172.18.0.5:41000" // Caddy
		r.Header.Set("Authorization", "Bearer "+credential.Credential)
		r = r.WithContext(context.WithValue(r.Context(), originKey{}, publicOrigin{ip: "2001:db8:5:6:7:8:9:a"}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("config: %d", w.Code)
		}
		if !isKnown(f.store, "2001:db8:5:6::1") || isKnown(f.store, "172.18.0.5") || isKnown(f.store, "2001:db8:5:7::1") {
			t.Fatalf("known = %+v", f.store.guard.known.list(f.store.now()))
		}
	})
}

// startStoreGuard runs the store's guard on a loopback port in front of
// rathole.
func startStoreGuard(t *testing.T, s *Store, rathole string) {
	t.Helper()
	s.guard.listen, s.guard.internal = "127.0.0.1:0", rathole
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		s.guard.run(ctx)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		s.guard.close()
		<-finished
	})
	select {
	case <-s.guard.bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the guard didn't start listening")
	}
}

// The whole way: a Connector's heartbeat through the real handler lets its
// address past the limits that turn an unknown client away.
func TestKnownConnectorsPassWhereUnknownClientsAreRefused(t *testing.T) {
	f := setup(t)
	rathole := newEchoRathole(t)
	startStoreGuard(t, f.store, rathole.address())
	credential := registered(t, f)
	address := f.store.guard.address().String()
	open := func(n int) []*net.TCPConn {
		var held []*net.TCPConn
		for range n {
			conn, err := net.Dial("tcp", address)
			if err != nil {
				t.Fatal(err)
			}
			held = append(held, conn.(*net.TCPConn))
			t.Cleanup(func() { conn.Close() })
		}
		return held
	}

	// Registering from the same host is not a request to the Connector API:
	// the loopback address is still unknown, and the 33rd connection is cut.
	unknown := open(guardClientConcurrent)
	for _, conn := range unknown {
		echoes(t, conn, framed("x"))
	}
	if !turnedAway(t, address) {
		t.Fatal("an unknown client wasn't refused its 33rd connection")
	}
	for _, conn := range unknown {
		conn.Close()
	}
	eventually(t, "the connections to end", func() bool { return f.store.guard.unknownConns.total.Load() == 0 })

	hb := guardHeartbeat(credential)
	if w := requestFrom(f.store.Handler(), "127.0.0.1:50000", "POST", "/connector/v1/heartbeat", credential.Credential, hb); w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	known := open(3 * guardClientConcurrent)
	for _, conn := range known {
		echoes(t, conn, "x")
	}
	// Nor does a known client's rate count.
	for range 2 * guardTotalBurst {
		conn := open(1)[0]
		echoes(t, conn, "x")
		conn.Close()
	}
	if stats := f.store.guard.stats(); stats.Refused.Busy != 1 || stats.Refused.Rate != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

type guardView struct {
	Settings   control.TunnelGuardSettings `json:"settings"`
	Stats      tunnelGuardStats            `json:"stats"`
	Connectors []tunnelGuardConnector      `json:"connectors"`
}

func decodeGuard(t *testing.T, w *httptest.ResponseRecorder) guardView {
	t.Helper()
	var view guardView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatalf("tunnel guard: %d %s", w.Code, w.Body.String())
	}
	return view
}

func TestTunnelGuardAPI(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	const path = "/api/v1/security/tunnel-guard"

	t.Run("defaults", func(t *testing.T) {
		w := request(h, "GET", path, a.admin, nil)
		view := decodeGuard(t, w)
		if view.Settings.Mode != "limited" || view.Settings.Allowed == nil || len(view.Settings.Allowed) != 0 || view.Connectors == nil || view.Stats != (tunnelGuardStats{}) {
			t.Fatalf("defaults: %s", w.Body.String())
		}
		for _, field := range []string{`"settings":{"mode":"limited","allowed":[]}`, `"stats":{"active":0,"accepted":0,"refused":{"rate":0,"busy":0,"idle":0,"strict":0}}`, `"connectors":[]`} {
			if !strings.Contains(w.Body.String(), field) {
				t.Errorf("missing %s in %s", field, w.Body.String())
			}
		}
	})
	t.Run("saving normalizes, applies at once and is audited", func(t *testing.T) {
		w := request(h, "PUT", path, a.admin, map[string]any{"mode": "strict", "allowed": []string{" 203.0.113.7/32", "2001:DB8::/32", "203.0.113.7", "198.51.100.77/24"}})
		want := []string{"203.0.113.7", "2001:db8::/32", "198.51.100.0/24"}
		if view := decodeGuard(t, w); view.Settings.Mode != "strict" || !slices.Equal(view.Settings.Allowed, want) {
			t.Fatalf("saved: %s", w.Body.String())
		}
		if view := decodeGuard(t, request(h, "GET", path, a.admin, nil)); view.Settings.Mode != "strict" || !slices.Equal(view.Settings.Allowed, want) {
			t.Fatalf("read back: %+v", view)
		}
		config := a.store.guard.config.Load()
		if !config.strict || !config.allows(netip.MustParseAddr("198.51.100.200")) || config.allows(netip.MustParseAddr("198.51.101.1")) {
			t.Fatalf("the running guard didn't take the settings: %+v", config)
		}
		if e := a.lastAudit(t); e.Action != "security.tunnel_guard" || e.ResourceType != "security" || e.ResourceID != "tunnel_guard" ||
			string(e.Detail) != `{"allowed":["203.0.113.7","2001:db8::/32","198.51.100.0/24"],"mode":"strict"}` {
			t.Fatalf("audit = %s %s/%s %s", e.Action, e.ResourceType, e.ResourceID, e.Detail)
		}
	})
	t.Run("a bad request changes nothing", func(t *testing.T) {
		before := len(a.auditLog(t))
		many := make([]string, control.MaxTunnelGuardAllowed+1)
		for i := range many {
			many[i] = fmt.Sprintf("10.0.%d.1", i)
		}
		for _, tc := range []struct {
			name string
			body any
			code string
		}{
			{"unknown mode", map[string]any{"mode": "open", "allowed": []string{}}, "invalid_tunnel_guard"},
			{"empty mode", map[string]any{"mode": "", "allowed": []string{}}, "invalid_tunnel_guard"},
			{"not an address", map[string]any{"mode": "limited", "allowed": []string{"example.com"}}, "invalid_tunnel_guard"},
			{"prefix too long", map[string]any{"mode": "limited", "allowed": []string{"10.0.0.0/33"}}, "invalid_tunnel_guard"},
			{"too many", map[string]any{"mode": "limited", "allowed": many}, "invalid_tunnel_guard"},
			{"a wrong type", map[string]any{"mode": "limited", "allowed": []int{1}}, "invalid_request"},
			{"no allowed", map[string]any{"mode": "limited"}, "invalid_request"},
			{"no mode", map[string]any{"allowed": []string{}}, "invalid_request"},
			{"allowed null", map[string]any{"mode": "limited", "allowed": nil}, "invalid_request"},
			{"unknown field", map[string]any{"mode": "limited", "allowed": []string{}, "extra": 1}, "invalid_request"},
		} {
			if w := request(h, "PUT", path, a.admin, tc.body); w.Code != 400 || !strings.Contains(w.Body.String(), tc.code) {
				t.Errorf("%s: %d %s, want 400 %s", tc.name, w.Code, w.Body.String(), tc.code)
			}
		}
		if view := decodeGuard(t, request(h, "GET", path, a.admin, nil)); view.Settings.Mode != "strict" || len(view.Settings.Allowed) != 3 {
			t.Fatalf("settings changed: %+v", view.Settings)
		}
		if after := len(a.auditLog(t)); after != before {
			t.Fatalf("%d audit entries for requests that changed nothing", after-before)
		}
	})
	t.Run("administrators only", func(t *testing.T) {
		credential := a.enroll(t, a.tokenA, "connector-a")
		for _, token := range []string{"", Token(), credential.Credential} {
			for _, method := range []string{"GET", "PUT"} {
				if w := request(h, method, path, token, map[string]any{"mode": "limited", "allowed": []string{}}); w.Code != 401 {
					t.Errorf("%s with %.6q: %d", method, token, w.Code)
				}
			}
		}
		cookie := a.adminSession(t)
		for _, method := range []string{"GET", "PUT"} {
			r := jsonRequest(method, path, map[string]any{"mode": "limited", "allowed": []string{}})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("%s without the session: %d", method, w.Code)
			}
			r = jsonRequest(method, path, map[string]any{"mode": "limited", "allowed": []string{}})
			r.AddCookie(cookie)
			w = httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Errorf("%s with the session: %d", method, w.Code)
			}
		}
	})
}

// enroll registers a seeded Connector on the store and returns its answer.
func (a alertFixture) enroll(t *testing.T, token, name string) contract.EnrollmentResponse {
	t.Helper()
	w := request(a.store.Handler(), "POST", "/connector/v1/enrollment", token, enrollment(name))
	var enrolled contract.EnrollmentResponse
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &enrolled) != nil {
		t.Fatalf("enrollment: %d %s", w.Code, w.Body.String())
	}
	return enrolled
}

// The console's list: the addresses learned from the Connector API, newest
// first, and the guard's counters.
func TestTunnelGuardAPIListsKnownConnectorsAndCounts(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	// Enrolling is the first request, from 192.0.2.1 at 14:00.
	enrolled := a.enroll(t, a.tokenB, "connector-b")
	for _, remote := range []string{"192.0.2.1:1", "[2001:db8:0:9:1:2:3:4]:1"} {
		a.clock.advance(time.Minute)
		if w := requestFrom(h, remote, "GET", "/connector/v1/config", enrolled.Credential, nil); w.Code != 200 {
			t.Fatalf("config: %d", w.Code)
		}
	}
	rathole := newEchoRathole(t)
	startStoreGuard(t, a.store, rathole.address())
	if request(h, "PUT", "/api/v1/security/tunnel-guard", a.admin, map[string]any{"mode": "strict", "allowed": []string{}}).Code != 200 {
		t.Fatal("strict")
	}
	for range 3 {
		if !turnedAway(t, a.store.guard.address().String()) {
			t.Fatal("a strict guard kept an unknown client")
		}
	}
	view := decodeGuard(t, request(h, "GET", "/api/v1/security/tunnel-guard", a.admin, nil))
	want := []tunnelGuardConnector{
		{Client: "2001:db8:0:9::/64", LastSeen: "2026-10-26T14:02:00Z"},
		{Client: "192.0.2.1", LastSeen: "2026-10-26T14:01:00Z"},
	}
	if !slices.Equal(view.Connectors, want) {
		t.Fatalf("connectors = %+v, want %+v", view.Connectors, want)
	}
	if view.Stats.Refused.Strict != 3 || view.Stats.Accepted != 0 || view.Stats.Active != 0 {
		t.Fatalf("stats = %+v", view.Stats)
	}
}

// A new setting reaches the next connection through the real handler.
func TestTunnelGuardSettingsReachTheListener(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	rathole := newEchoRathole(t)
	startStoreGuard(t, a.store, rathole.address())
	connect := func() *net.TCPConn {
		conn, err := net.Dial("tcp", a.store.guard.address().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn.(*net.TCPConn)
	}
	put := func(mode string, allowed ...string) {
		t.Helper()
		if w := request(h, "PUT", "/api/v1/security/tunnel-guard", a.admin, map[string]any{"mode": mode, "allowed": append([]string{}, allowed...)}); w.Code != 200 {
			t.Fatalf("%s: %d %s", mode, w.Code, w.Body.String())
		}
	}
	echoes(t, connect(), framed("limited admits an unknown client"))
	put("strict")
	if !turnedAway(t, a.store.guard.address().String()) {
		t.Fatal("strict kept an unknown client")
	}
	put("strict", "127.0.0.0/8")
	echoes(t, connect(), framed("an allowed range is admitted in strict mode"))
	put("limited")
	echoes(t, connect(), framed("and back"))
}

func TestTunnelAddressesAreValidated(t *testing.T) {
	for _, tc := range []struct {
		listen, internal string
		ok               bool
	}{
		{"0.0.0.0:2333", "127.0.0.1:2334", true},
		{"0.0.0.0:2333", "127.0.0.1:2444", true},
		{"0.0.0.0:2333", "[::1]:2334", true},
		{"0.0.0.0:2333", "127.0.0.2:2334", true},
		{"0.0.0.0:2333", "127.0.0.1:2333", false},
		{"127.0.0.1:2333", "127.0.0.1:2333", false},
		{"[::]:2333", "[::1]:2333", false},
		{"0.0.0.0:2333", "0.0.0.0:2334", false},
		{"0.0.0.0:2333", "192.0.2.1:2334", false},
		{"0.0.0.0:2333", "10.0.0.5:2334", false},
		{"0.0.0.0:2333", "localhost:2334", false},
		{"0.0.0.0:2333", "outlet:2334", false},
		{"0.0.0.0:2333", "127.0.0.1", false},
		{"0.0.0.0:2333", "127.0.0.1:0", false},
		{"0.0.0.0:2333", "127.0.0.1:70000", false},
		{"0.0.0.0:2333", ":2334", false},
		{"0.0.0.0:2333", "127.0.0.1:02334", false},
		{"0.0.0.0", "127.0.0.1:2334", false},
	} {
		if err := validateTunnelAddresses(tc.listen, tc.internal); (err == nil) != tc.ok {
			t.Errorf("listen %q internal %q: %v, want ok=%v", tc.listen, tc.internal, err, tc.ok)
		}
	}
}

// Without a tunnel_internal_address rathole listens on the loopback address
// one port after the tunnel port (the one before it for the last port), so a
// tunnel port of 2334 can't collide with the default.
func TestDefaultTunnelInternalAddress(t *testing.T) {
	for listen, want := range map[string]string{
		"0.0.0.0:2333":        "127.0.0.1:2334",
		"0.0.0.0:2334":        "127.0.0.1:2335",
		"[::]:2334":           "127.0.0.1:2335",
		"127.0.0.1:2333":      "127.0.0.1:2334",
		"0.0.0.0:443":         "127.0.0.1:444",
		"0.0.0.0:1":           "127.0.0.1:2",
		"0.0.0.0:65534":       "127.0.0.1:65535",
		"0.0.0.0:65535":       "127.0.0.1:65534",
		"203.0.113.5:65535":   "127.0.0.1:65534",
		"[2001:db8::1]:65535": "127.0.0.1:65534",
	} {
		got := defaultTunnelInternalAddress(listen)
		if got != want {
			t.Errorf("listen %q: default internal address %q, want %q", listen, got, want)
		}
		if err := validateTunnelAddresses(listen, got); err != nil {
			t.Errorf("listen %q: its default internal address is refused: %v", listen, err)
		}
	}
	// What validation refuses as a listen address gets the constant, so the
	// error that follows is about the listen address.
	for _, listen := range []string{"2333", "0.0.0.0", "0.0.0.0:0", "0.0.0.0:65536", "0.0.0.0:-1", "0.0.0.0:abc", ""} {
		if got := defaultTunnelInternalAddress(listen); got != DefaultTunnelInternalAddress {
			t.Errorf("listen %q: %q, want %q", listen, got, DefaultTunnelInternalAddress)
		}
		if err := validateTunnelAddresses(listen, defaultTunnelInternalAddress(listen)); err == nil || err.Error() != "invalid tunnel_listen_address" {
			t.Errorf("listen %q: %v, want the listen address refused", listen, err)
		}
	}
}

func TestTunnelInternalAddressInTheConfiguration(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, tc := range []struct {
		name, listen, internal string
		bind                   string // the server.toml bind_addr; empty: the Store doesn't open
	}{
		{"default", "0.0.0.0:2333", "", "127.0.0.1:2334"},
		{"default beside the default internal port", "0.0.0.0:2334", "", "127.0.0.1:2335"},
		{"default at the last port", "0.0.0.0:65535", "", "127.0.0.1:65534"},
		{"the default isn't taken when one is chosen", "0.0.0.0:2334", "127.0.0.1:2444", "127.0.0.1:2444"},
		{"a chosen address on the public port is still refused", "0.0.0.0:2334", "127.0.0.1:2334", ""},
		{"chosen", "0.0.0.0:2333", "127.0.0.1:2444", "127.0.0.1:2444"},
		{"IPv6", "0.0.0.0:2333", "[::1]:2444", "[::1]:2444"},
		{"not loopback", "0.0.0.0:2333", "0.0.0.0:2334", ""},
		{"the public port", "0.0.0.0:2333", "127.0.0.1:2333", ""},
		{"a name", "0.0.0.0:2333", "localhost:2334", ""},
		{"a bad listen address", "2333", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			f.cfg.TunnelListenAddress, f.cfg.TunnelInternalAddress, f.cfg.TunnelServerPrivateKey = tc.listen, tc.internal, key
			store, err := Open(f.cfg, f.dir)
			if tc.bind == "" {
				if err == nil {
					store.Close()
					t.Fatal("opened with an invalid configuration")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			toml, err := os.ReadFile(store.tunnelPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(toml), "[server]\nbind_addr = \""+tc.bind+"\"\n") || strings.Contains(string(toml), tc.listen) {
				t.Fatalf("server.toml binds the wrong address:\n%s", toml)
			}
			if store.guard.listen != tc.listen || store.guard.internal != tc.bind {
				t.Fatalf("guard listens on %s for %s", store.guard.listen, store.guard.internal)
			}
		})
	}
	t.Run("without a tunnel_listen_address nothing is validated or written", func(t *testing.T) {
		f := setup(t)
		f.cfg.TunnelInternalAddress = "not an address"
		store, err := Open(f.cfg, f.dir)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		store.RunTunnelGuard(context.Background()) // returns at once
		if _, err := os.Stat(store.tunnelPath); !os.IsNotExist(err) {
			t.Fatalf("server.toml: %v", err)
		}
	})
}

// The Outlet starts and stops the guard: Close cuts proxied connections and
// leaves no goroutine behind.
func TestStoreCloseStopsTheGuard(t *testing.T) {
	before := runtime.NumGoroutine()
	rathole := newEchoRathole(t)
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := free.Addr().String()
	free.Close()
	f := setup(t)
	f.cfg.TunnelListenAddress, f.cfg.TunnelInternalAddress = listen, rathole.address()
	f.cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, err := Open(f.cfg, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan struct{})
	go func() {
		store.RunTunnelGuard(context.Background())
		close(ran)
	}()
	select {
	case <-store.guard.bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the guard didn't start listening")
	}
	conn, err := net.Dial("tcp", listen)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	echoes(t, conn.(*net.TCPConn), framed("before"))

	within(t, "closing the store", store.Close)
	if !closedByGuard(conn.(*net.TCPConn), 3*time.Second) {
		t.Fatal("Close left a proxied connection open")
	}
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("RunTunnelGuard didn't return after Close")
	}
	if c, err := net.DialTimeout("tcp", listen, time.Second); err == nil {
		c.Close()
		t.Fatal("the tunnel port is still open")
	}
	conn.Close()
	rathole.close()
	f.store.Close()
	settled(t, before)
}
