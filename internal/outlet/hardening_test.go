package outlet

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/securefs"
)

func TestCrossOriginWritesAreRejected(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	send := func(method, path, contentType string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"name":"edge-a"}`))
		r.Host = a.host
		r.Header.Set("Content-Type", contentType)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// A tunneled app is same-site with the management host, so the browser
	// still sends the SameSite=Strict cookie with its form posts.
	for _, headers := range []map[string]string{
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Origin": "https://app.example.com"},
	} {
		for _, path := range []string{"/api/v1/alerts/telegram/test", "/api/v1/connectors/m_any/enrollment-tokens?reset=true"} {
			if w := send("POST", path, "application/x-www-form-urlencoded", headers); w.Code != http.StatusForbidden || errorCode(t, w) != "cross_origin_request" {
				t.Fatalf("%s with %v: %d %s", path, headers, w.Code, w.Body.String())
			}
		}
	}
	if w := send("POST", "/api/v1/connectors", "application/json", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://" + a.host}); w.Code != 201 {
		t.Fatalf("same-origin write: %d %s", w.Code, w.Body.String())
	}
	// Reads stay open to any origin; they change nothing.
	if w := send("GET", "/api/v1/connectors", "application/json", map[string]string{"Sec-Fetch-Site": "cross-site"}); w.Code != 200 {
		t.Fatalf("cross-site read: %d", w.Code)
	}
}

func TestPublicPasswordFailuresShareOneLimit(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	// Thirty wrong passwords from thirty addresses already happened.
	for range publicFailures {
		a.store.loginLimit.failures = append(a.store.loginLimit.failures, a.clock.now())
	}
	w := a.publicRequest("POST", "/api/v1/session", "198.51.100.200", chromeOnMac, adminLogin)
	if w.Code != http.StatusTooManyRequests || errorCode(t, w) != "rate_limited" || w.Header().Get("Retry-After") != "901" {
		t.Fatalf("locked public login: %d %s retry=%q", w.Code, w.Body.String(), w.Header().Get("Retry-After"))
	}
	// The operator still gets in over SSH.
	if w := request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin); w.Code != 201 {
		t.Fatalf("local login: %d %s", w.Code, w.Body.String())
	}
	a.clock.advance(publicFailureWindow)
	if w := a.publicRequest("POST", "/api/v1/session", "198.51.100.200", chromeOnMac, adminLogin); w.Code != 201 {
		t.Fatalf("public login after the window: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
}

func TestPasswordChecksAreCountedAndCapped(t *testing.T) {
	var l attemptLimiter
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for range passwordChecks {
		if _, ok := l.beginPasswordCheck(now, false); !ok {
			t.Fatal("a free slot was refused")
		}
	}
	if wait, ok := l.beginPasswordCheck(now, false); ok || wait != time.Second {
		t.Fatalf("over the cap: %v %v", wait, ok)
	}
	for i := range passwordChecks {
		l.endPasswordCheck(i%2 == 0, now)
	}
	if l.checking != 0 || len(l.failures) != passwordChecks/2 {
		t.Fatalf("checking=%d failures=%d", l.checking, len(l.failures))
	}
	var wg sync.WaitGroup
	for range publicFailures - len(l.failures) {
		wg.Go(func() {
			for {
				if _, ok := l.beginPasswordCheck(now, false); ok {
					l.endPasswordCheck(true, now)
					return
				}
			}
		})
	}
	wg.Wait()
	if _, ok := l.beginPasswordCheck(now, false); ok {
		t.Fatal("the failure limit did not hold")
	}
}

func TestClientKeyGroupsIPv6Prefixes(t *testing.T) {
	if clientKey("2001:db8:1:2:3::1") != clientKey("2001:db8:1:ff:ffff::9") {
		t.Fatal("one /56 got two keys")
	}
	if clientKey("2001:db8:1:200::1") == clientKey("2001:db8:1:300::1") {
		t.Fatal("two /56 prefixes share a key")
	}
	if clientKey("203.0.113.7") != "203.0.113.7" || clientKey("::ffff:203.0.113.7") != "::ffff:203.0.113.7" {
		t.Fatal("IPv4 clients are no longer keyed by address")
	}
}

func TestAttemptLimiterStaysBounded(t *testing.T) {
	var l attemptLimiter
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for i := range 3 * maxLoginKeys {
		l.allow(net.IPv4(10, byte(i>>16), byte(i>>8), byte(i)).String(), now)
		if len(l.hits) > maxLoginKeys {
			t.Fatalf("%d keys after %d addresses", len(l.hits), i+1)
		}
	}
}

func TestAuditKeepsOnlyPlainEmailsAndPrintableAgents(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	for i, email := range []string{"<img src=x onerror=alert(1)>@example.com", "admin@example.com\n[OK] signed in", `"Admin" <admin@example.com>`, strings.Repeat("a", 250) + "@example.com"} {
		ip := net.IPv4(198, 51, 100, byte(i+1)).String()
		if w := a.publicRequest("POST", "/api/v1/session", ip, "curl/8.0\x00\x1b[31m red\r\nX: y", map[string]any{"email": email, "password": "wrong password entirely"}); w.Code != 401 {
			t.Fatalf("login %q: %d", email, w.Code)
		}
		e := a.lastAudit(t)
		if e.Action != "session.login_failed" || e.ActorEmail != nil {
			t.Fatalf("%q stored as %+v", email, e.ActorEmail)
		}
		if e.UserAgent == nil || *e.UserAgent != "curl/8.0[31m redX: y" {
			t.Fatalf("user agent stored as %q", *e.UserAgent)
		}
	}
}

func TestConnectorEnrollmentIsAudited(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	var connector struct {
		ID string `json:"id"`
	}
	if w := cookieRequest(h, "POST", "/api/v1/connectors", cookie, map[string]any{"name": "edge-a"}); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &connector) != nil {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var issued struct {
		Token string `json:"token"`
	}
	if w := cookieRequest(h, "POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", cookie, nil); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &issued) != nil {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	before := len(a.auditLog(t))
	if w := request(h, "POST", "/connector/v1/enrollment", Token(), enrollment("edge-a")); w.Code != 401 {
		t.Fatalf("unknown token: %d", w.Code)
	}
	if len(a.auditLog(t)) != before {
		t.Fatal("a token-less guess was recorded")
	}
	if w := request(h, "POST", "/connector/v1/enrollment", issued.Token, enrollment("edge-b")); w.Code != 403 {
		t.Fatalf("wrong name: %d", w.Code)
	}
	if e := a.lastAudit(t); e.Action != "connector.enroll_failed" || e.ResourceID != connector.ID || string(e.Detail) != `{"name":"edge-b","reason":"connector_mismatch"}` {
		t.Fatalf("mismatch = %+v", e)
	}
	for range 2 {
		if w := request(h, "POST", "/connector/v1/enrollment", issued.Token, enrollment("edge-a")); w.Code != 201 {
			t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
		}
	}
	// A restart enrolls again with the same token; only the first time is news.
	log := a.auditLog(t)[before:]
	if got := actions(log); len(got) != 2 || got[1] != "connector.enroll" {
		t.Fatalf("actions = %v", got)
	}
	e := log[1]
	var detail map[string]string
	if err := json.Unmarshal(e.Detail, &detail); err != nil || e.ResourceID != connector.ID || detail["name"] != "edge-a" || detail["version"] == "" || e.Source != "local" {
		t.Fatalf("enroll = %+v", e)
	}
	everything, _ := json.Marshal(log)
	if strings.Contains(string(everything), issued.Token) {
		t.Fatal("the run token reached the audit log")
	}
}

func TestTunnelCheckNeverDialsLocalAddresses(t *testing.T) {
	f := setup(t)
	store, _ := controlStore(t, f)
	store.lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("169.254.169.254")}, {IP: net.ParseIP("fe80::1")}, {IP: net.ParseIP("203.0.113.10")}}, nil
	}
	var mu sync.Mutex
	var dialed []string
	store.dialTCP = func(_ context.Context, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		client, server := net.Pipe()
		server.Close()
		return client, nil
	}
	w := request(store.Handler(), "POST", "/api/v1/outlet/tunnel/check", f.admin, map[string]any{"address": "tunnel.example.test:2333"})
	var result tunnelCheck
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || len(result.Results) != 4 || !result.Reachable {
		t.Fatalf("check: %d %s", w.Code, w.Body.String())
	}
	for i, want := range []string{"address_not_allowed", "address_not_allowed", "address_not_allowed", ""} {
		if result.Results[i].Error != want {
			t.Fatalf("result %d = %+v", i, result.Results[i])
		}
	}
	if len(dialed) != 1 || dialed[0] != "203.0.113.10:2333" {
		t.Fatalf("dialed %v", dialed)
	}
}

func TestHeartbeatsAreKeptInMemoryAndWrittenBehind(t *testing.T) {
	f := setup(t)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer probe.Close()
	r := f.store.records["connector_a"]
	r.ProbeAddress = strings.TrimPrefix(probe.URL, "http://")
	f.store.records["connector_a"] = r
	response := registered(t, f)
	stored := func() *contract.Heartbeat {
		t.Helper()
		var records map[string]Record
		if err := securefs.ReadJSON(f.store.path, &records); err != nil {
			t.Fatal(err)
		}
		return records["connector_a"].LastHeartbeat
	}
	// The state file cannot be written: the heartbeat is still answered.
	path := f.store.path
	f.store.path = filepath.Join(f.dir, "missing", "state.json")
	hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "healthy", ObservedAt: time.Now().UTC()}
	if w := request(f.store.Handler(), "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	if f.store.records["connector_a"].LastHeartbeat == nil {
		t.Fatal("heartbeat not kept in memory")
	}
	f.store.path = path
	if stored() != nil {
		t.Fatal("the heartbeat waited for the state file")
	}
	f.store.persist()
	if got := stored(); got == nil || !got.ObservedAt.Equal(hb.ObservedAt) {
		t.Fatalf("persisted heartbeat = %+v", got)
	}
}

// A probe that times out on a busy Outlet must not take a service down
// while its proxied traffic is being answered.
func TestProxiedAnswersKeepAServiceConnected(t *testing.T) {
	f := setup(t)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer probe.Close()
	r := f.store.records["connector_a"]
	r.ProbeAddress = strings.TrimPrefix(probe.URL, "http://")
	f.store.records["connector_a"] = r
	response := registered(t, f)
	beat := func() string {
		t.Helper()
		hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "healthy", ObservedAt: time.Now().UTC()}
		if w := request(f.store.Handler(), "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 200 {
			t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
		}
		return f.store.records["connector_a"].ServiceStatuses[response.ServiceID]
	}
	if got := beat(); got != "connected" {
		t.Fatalf("first heartbeat: %s", got)
	}
	probe.Close()
	f.store.noteServed(response.ServiceID)
	if got := beat(); got != "connected" {
		t.Fatalf("failed probe while traffic was answered: %s", got)
	}
	if got := beat(); got != "disconnected" {
		t.Fatalf("failed probe and no traffic since: %s", got)
	}
}

func TestOnlyProxiedAnswersBelow500AreNoted(t *testing.T) {
	failing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	f := newProxyFixture(t, echo("A"), failing)
	f.route(t, 0, "ok.example.com")
	f.route(t, 1, "failing.example.com")
	before := time.Now().Add(-time.Second)
	f.get("ok.example.com")
	f.get("failing.example.com")
	if !f.store.servedSince(f.services[0].ID, before) || f.store.servedSince(f.services[1].ID, before) {
		t.Fatal("proxied answers noted wrongly")
	}
}

// Anyone can fill the shared failure limit with wrong passwords; a browser
// that has signed in before is not locked out by it.
func TestTrustedDevicesSkipTheSharedFailureLimit(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	w := a.publicRequest("POST", "/api/v1/session", "198.51.100.10", chromeOnMac, adminLogin)
	var device *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == deviceCookie {
			device = cookie
		}
	}
	if w.Code != 201 || device == nil || device.Path != "/" || !device.HttpOnly || !device.Secure || device.SameSite != http.SameSiteStrictMode {
		t.Fatalf("public login: %d, device cookie %+v", w.Code, device)
	}
	for range publicFailures {
		a.store.loginLimit.failures = append(a.store.loginLimit.failures, a.clock.now())
	}
	login := func(cookie *http.Cookie) int {
		r := jsonRequest("POST", "/api/v1/session", adminLogin)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", "198.51.100.11")
		r.Header.Set("User-Agent", chromeOnMac)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		return w.Code
	}
	if code := login(nil); code != http.StatusTooManyRequests {
		t.Fatalf("unknown browser during the lockout: %d", code)
	}
	if code := login(device); code != 201 {
		t.Fatalf("trusted browser during the lockout: %d", code)
	}
	stamp, _, _ := strings.Cut(device.Value, ".")
	for _, forged := range []string{stamp + ".AAAA", "9999999999." + strings.SplitN(device.Value, ".", 2)[1], "garbage"} {
		if code := login(&http.Cookie{Name: deviceCookie, Value: forged}); code != http.StatusTooManyRequests {
			t.Fatalf("forged cookie %q: %d", forged, code)
		}
	}
	a.clock.advance(deviceTTL)
	for range publicFailures {
		a.store.loginLimit.failures = append(a.store.loginLimit.failures, a.clock.now())
	}
	if code := login(device); code != http.StatusTooManyRequests {
		t.Fatalf("expired trusted browser: %d", code)
	}
	a.store.background.Wait()
}

// A lookup that read the session before logout revoked it must not cache
// it after.
func TestRevocationWinsOverALookupInFlight(t *testing.T) {
	var c sessionCache
	now := time.Now()
	admin := control.Admin{ID: "admin_1"}
	generation := c.begin()
	c.revoke("token")
	c.put("token", admin, now.Add(time.Hour), now, generation)
	if _, ok := c.get("token", now); ok {
		t.Fatal("revoked session cached again")
	}
	c.put("token", admin, now.Add(time.Hour), now, c.begin())
	if _, ok := c.get("token", now); !ok {
		t.Fatal("lookup with no revocation in between was not cached")
	}
}
