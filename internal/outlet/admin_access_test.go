package outlet

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// The gate's answer outside the allowed ranges: the plain 404 of an
// address that serves nothing.
const notThere = "404 page not found\n"

// The limit hides the console and admin API on the management host from
// addresses outside the ranges, and nothing else: not the Connector API,
// access protection's sign-in, apps, or the local admin port.
func TestAdminAccessLimitsTheConsoleOnly(t *testing.T) {
	f := newProxyFixture(t, echo("A"))
	f.route(t, 0, "app.example.com")
	send := func(method, host, target, ip string, body any) *httptest.ResponseRecorder {
		t.Helper()
		r := jsonRequest(method, target, body)
		r.Host = host
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set("Authorization", "Bearer "+f.admin)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	console := func(target, ip string) *httptest.ResponseRecorder { return send("GET", f.adminHost, target, ip, nil) }
	save := func(ip string, a control.AdminAccess) *httptest.ResponseRecorder {
		return send("PUT", f.adminHost, "/api/v1/security/admin-access", ip, a)
	}
	if w := console("/api/v1/setup/status", "198.51.100.9"); w.Code != 200 {
		t.Fatalf("unlimited by default: %d", w.Code)
	}
	// From the internet, a limit leaving out the address saving it is refused.
	if w := save("198.51.100.9", control.AdminAccess{Restricted: true, Allowed: []string{"203.0.113.0/24"}}); w.Code != 409 || errorCode(t, w) != "admin_access_locks_out" {
		t.Fatalf("lock-out: %d %s", w.Code, w.Body.String())
	}
	if w := console("/api/v1/setup/status", "198.51.100.9"); w.Code != 200 {
		t.Fatalf("a refused save changed something: %d", w.Code)
	}
	w := save("203.0.113.5", control.AdminAccess{Restricted: true, Allowed: []string{"203.0.113.0/24", " 2001:DB8:1::/48 "}})
	var view adminAccessView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || !view.Restricted || !slices.Equal(view.Allowed, []string{"203.0.113.0/24", "2001:db8:1::/48"}) {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	for _, target := range []string{"/", "/connectors", "/ko/settings", "/api/v1/setup/status", "/api/v1/security/admin-access", "/healthz/",
		"/connector/v1/../api/v1/setup/status", "//api/v1/setup/status", "/connector/v1/heartbeat/../../../api/v1/outlet"} {
		if w := console(target, "198.51.100.9"); w.Code != 404 || w.Body.String() != notThere {
			t.Fatalf("%s from outside: %d %q", target, w.Code, w.Body.String())
		}
	}
	for _, ip := range []string{"203.0.113.77", "2001:db8:1:2::5", "::ffff:203.0.113.8"} {
		if w := console("/api/v1/setup/status", ip); w.Code != 200 {
			t.Fatalf("from %s: %d", ip, w.Code)
		}
	}
	// Port 80 redirects the management host only for those let in.
	for ip, code := range map[string]int{"198.51.100.9": 404, "203.0.113.5": 308} {
		r := httptest.NewRequest("GET", "/api/v1/setup/status", nil)
		r.Host = f.adminHost
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != code || (code == 404) != (w.Header().Get("Location") == "") {
			t.Fatalf("port 80 from %s: %d %q", ip, w.Code, w.Header().Get("Location"))
		}
	}
	// Open whatever the limit: tunnels, visitors of protected addresses and
	// external monitors of /healthz.
	for _, open := range []struct{ method, target string }{
		{"POST", "/connector/v1/heartbeat"}, {"GET", "/connector/v1/config"}, {"POST", "/connector/v1/enrollment"},
		{"GET", accessStartPath}, {"GET", accessCallbackPath}, {"GET", "/healthz"},
	} {
		if w := send(open.method, f.adminHost, open.target, "198.51.100.9", nil); w.Body.String() == notThere {
			t.Fatalf("%s %s refused by the limit: %d", open.method, open.target, w.Code)
		}
	}
	if w := send("GET", "app.example.com", "/", "198.51.100.9", nil); w.Code != 200 || w.Body.String() != "A app.example.com" {
		t.Fatalf("app: %d %q", w.Code, w.Body.String())
	}
	if w := request(f.store.Handler(), "GET", "/api/v1/setup/status", "", nil); w.Code != 200 {
		t.Fatalf("local admin port: %d", w.Code)
	}
	// MEERKAT_ADMIN_IP_LIMIT=false lifts it for a locked-out administrator.
	f.db.SetDefaults(control.Defaults{AdminIPLimitLifted: true})
	if w := console("/api/v1/setup/status", "198.51.100.9"); w.Code != 200 {
		t.Fatalf("lifted: %d", w.Code)
	}
	// Let in that way, they can turn the limit off from where they are,
	// but not save one that still leaves them out.
	if w := save("198.51.100.9", control.AdminAccess{Restricted: true, Allowed: []string{"203.0.113.0/24"}}); w.Code != 409 {
		t.Fatalf("lifted, saving a limit without the saver: %d", w.Code)
	}
	if w := save("198.51.100.9", control.AdminAccess{Restricted: false, Allowed: []string{"203.0.113.0/24"}}); w.Code != 200 {
		t.Fatalf("lifted, turning the limit off: %d %s", w.Code, w.Body.String())
	}
	f.db.SetDefaults(control.Defaults{})
	if w := console("/api/v1/setup/status", "198.51.100.9"); w.Code != 200 {
		t.Fatalf("turned off: %d", w.Code)
	}
	// The local admin port can close the console to the internet entirely.
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/admin-access", f.admin, control.AdminAccess{Restricted: true, Allowed: []string{}}); w.Code != 200 {
		t.Fatalf("close over SSH: %d %s", w.Code, w.Body.String())
	}
	if w := console("/api/v1/setup/status", "203.0.113.5"); w.Code != 404 {
		t.Fatalf("closed: %d", w.Code)
	}
	if w := send("POST", f.adminHost, "/connector/v1/heartbeat", "203.0.113.5", nil); w.Body.String() == notThere {
		t.Fatalf("closed console took the Connector API with it: %d", w.Code)
	}
}

func TestAdminAccessSettingsAPI(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	public := func(method, ip string, body any) *httptest.ResponseRecorder {
		t.Helper()
		r := jsonRequest(method, "/api/v1/security/admin-access", body)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set("Authorization", "Bearer "+a.admin)
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		return w
	}
	read := func(w *httptest.ResponseRecorder) adminAccessView {
		t.Helper()
		var view adminAccessView
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return view
	}
	if view := read(request(h, "GET", "/api/v1/security/admin-access", a.admin, nil)); view.Restricted || view.Allowed == nil || len(view.Allowed) != 0 || view.Client != nil || view.ClientRange != nil || view.Lifted {
		t.Fatalf("default over SSH: %+v", view)
	}
	// "Add current IP" adds the address for IPv4 and its /64 for IPv6.
	for ip, want := range map[string]string{"203.0.113.5": "203.0.113.5", "2001:db8:1:2:aaaa::7": "2001:db8:1:2::/64"} {
		if view := read(public("GET", ip, nil)); view.Client == nil || *view.Client != ip || view.ClientRange == nil || *view.ClientRange != want {
			t.Fatalf("%s: %+v", ip, view)
		}
	}
	before := len(a.auditLog(t))
	many := make([]string, control.MaxAdminAccessAllowed+1)
	for i := range many {
		many[i] = "10.0.0." + string(rune('0'+i%10))
	}
	for _, bad := range []any{
		map[string]any{"restricted": true},
		map[string]any{"restricted": true, "allowed": []string{"not-an-ip"}},
		map[string]any{"restricted": true, "allowed": []string{"203.0.113.0/33"}},
		map[string]any{"restricted": true, "allowed": many},
	} {
		if w := request(h, "PUT", "/api/v1/security/admin-access", a.admin, bad); w.Code != 400 {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body.String())
		}
	}
	if len(a.auditLog(t)) != before {
		t.Fatal("a refused save was audited")
	}
	if view := read(public("PUT", "203.0.113.5", control.AdminAccess{Restricted: true, Allowed: []string{"203.0.113.5/32", "198.51.100.0/24"}})); !slices.Equal(view.Allowed, []string{"203.0.113.5", "198.51.100.0/24"}) {
		t.Fatalf("saved: %+v", view)
	}
	if e := a.lastAudit(t); e.Action != "security.admin_access" || e.ResourceID != "admin_access" || string(e.Detail) != `{"allowed":["203.0.113.5","198.51.100.0/24"],"allowed_tags":[],"restricted":true}` {
		t.Fatalf("audit = %s %s %s", e.Action, e.ResourceID, e.Detail)
	}
	a.db.SetDefaults(control.Defaults{AdminIPLimitLifted: true})
	if view := read(request(h, "GET", "/api/v1/security/admin-access", a.admin, nil)); !view.Lifted {
		t.Fatalf("lifted not shown: %+v", view)
	}
}

// A restarted Outlet applies the saved limit from its first request.
func TestAdminAccessHoldsAcrossRestarts(t *testing.T) {
	f := newProxyFixture(t)
	if _, err := f.db.SetAdminAccess(context.Background(), control.AdminAccess{Restricted: true, Allowed: []string{"203.0.113.0/24"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(f.admin), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 3600, TunnelRemoteAddress: "outlet:2333", TunnelServerPublicKey: key}
	restarted, err := OpenWithControl(cfg, t.TempDir(), f.db)
	if err != nil {
		t.Fatal(err)
	}
	for ip, code := range map[string]int{"198.51.100.9": 404, "203.0.113.5": 200} {
		r := httptest.NewRequest("GET", "/api/v1/setup/status", nil)
		r.Host = f.adminHost
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		restarted.PublicHandler().ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("after a restart, from %s: %d, want %d", ip, w.Code, code)
		}
	}
}

// The card warns when the address the Outlet sees is likely shared by many
// clients: allowing it would let all of them in.
func TestAdminAccessFlagsSharedClientAddresses(t *testing.T) {
	a := newAlertFixture(t)
	for _, tc := range []struct {
		ip, header string
		shared     bool
	}{{"203.0.113.5", "", false}, {"2001:db8::5", "", false}, {"10.0.0.5", "", true}, {"192.168.215.1", "", true}, {"fd00::1", "", true}, {"100.64.1.1", "", true}, {"127.0.0.1", "", true}, {"203.0.113.5", "Cf-Ray", true}, {"203.0.113.5", "Cdn-Loop", true}} {
		r := jsonRequest("GET", "/api/v1/security/admin-access", nil)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", tc.ip)
		r.Header.Set("Authorization", "Bearer "+a.admin)
		if tc.header != "" {
			r.Header.Set(tc.header, "x")
		}
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		var view adminAccessView
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.ClientShared != tc.shared {
			t.Fatalf("%s %s: %d %s", tc.ip, tc.header, w.Code, w.Body.String())
		}
	}
}

// Stored settings the Outlet can't read close the console on public
// addresses instead of opening it; the local admin port says why.
func TestAdminAccessFailsClosedOnDamagedSettings(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := control.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	outlet, err := db.SetDomain(ctx, "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(`INSERT INTO outlet_settings(key, value, updated_at) VALUES ('admin_access', '{"restricted":true,"allowed":["nope"]}', '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	admin := Token()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(admin), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 3600, TunnelRemoteAddress: "outlet:2333", TunnelServerPublicKey: key}
	store, err := OpenWithControl(cfg, t.TempDir(), db)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/setup/status", nil)
	r.Host = outlet.AdminHostname
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	w := httptest.NewRecorder()
	store.PublicHandler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("damaged settings opened the console: %d", w.Code)
	}
	var view adminAccessView
	if w := request(store.Handler(), "GET", "/api/v1/security/admin-access", admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || !view.Damaged || !view.Restricted {
		t.Fatalf("local port: %d %s", w.Code, w.Body.String())
	}
}
