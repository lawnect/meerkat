package outlet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// visitFrom requests host and path as a browser behind Caddy would, from
// client address ip.
func (f accessFixture) visitFrom(ip, host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://"+host+path, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-For", ip)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// A policy limited to client addresses applies only to requests from them;
// the rest go on to the policies below. Office addresses skip the sign-in,
// everyone else signs in, and a listed range is blocked.
func TestAccessPolicyLimitedToClientAddresses(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	if _, err := f.db.CreateIPAddress(ctx, control.IPAddress{Range: "203.0.113.0/24", Note: "본사", Tags: []string{"사무실"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.setPolicies(t,
		control.AccessPolicy{Name: "blocked", Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, IPRanges: []string{"192.0.2.0/24"}},
		control.AccessPolicy{Name: "office", Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true, IPTags: []string{"사무실"}},
		control.AccessPolicy{Name: "everyone else", Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true},
	)
	if w := f.visitFrom("203.0.113.40", "app.example.com", "/"); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "email= ") {
		t.Fatalf("office: %d %q", w.Code, w.Body.String())
	}
	if w := f.visitFrom("198.51.100.30", "app.example.com", "/"); w.Code != http.StatusFound {
		t.Fatalf("elsewhere: %d", w.Code)
	}
	if w := f.visitFrom("192.0.2.9", "app.example.com", "/"); w.Code != http.StatusForbidden {
		t.Fatalf("blocked range: %d", w.Code)
	}
	// The policy follows the address book at once.
	if _, err := f.db.CreateIPAddress(ctx, control.IPAddress{Range: "2001:db8:5::/48", Tags: []string{"사무실"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccess(ctx); err != nil {
		t.Fatal(err)
	}
	if w := f.visitFrom("2001:db8:5::9", "app.example.com", "/"); w.Code != 200 {
		t.Fatalf("office over IPv6: %d", w.Code)
	}
}

// A policy limited to some client addresses never hides the ones below it,
// which still apply to every other address.
func TestClientLimitedPolicyDoesNotShadow(t *testing.T) {
	limited := &accessRule{all: true, ipLimited: true, policy: control.AccessPolicy{Action: control.AccessAllow}}
	later := &accessRule{all: true, policy: control.AccessPolicy{Action: control.AccessAllow}}
	if limited.shadows(later) {
		t.Fatal("a policy for some addresses shadowed one for all")
	}
	open := &accessRule{all: true, policy: control.AccessPolicy{Action: control.AccessAllow}}
	if !open.shadows(&accessRule{all: true, ipLimited: true, policy: control.AccessPolicy{Action: control.AccessAllow}}) {
		t.Fatal("a policy for every address should shadow a later one for some")
	}
}

func TestIPAddressesAPI(t *testing.T) {
	f := newProxyFixture(t)
	h := f.store.Handler()
	create := func(body map[string]any) control.IPAddress {
		t.Helper()
		w := request(h, "POST", "/api/v1/security/ip-addresses", f.admin, body)
		var e control.IPAddress
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &e) != nil {
			t.Fatalf("create %v: %d %s", body, w.Code, w.Body.String())
		}
		return e
	}
	office := create(map[string]any{"range": "203.0.113.0/24", "note": "본사", "tags": []string{"사무실"}})
	branch := create(map[string]any{"range": "198.51.100.0/24", "note": "지사", "tags": []string{"사무실", "지사"}})
	for _, bad := range []map[string]any{{"range": "nope", "note": "", "tags": []string{}}, {"range": "10.0.0.1", "note": "", "tags": []string{"10.0.0.2"}}, {"range": "10.0.0.1"}} {
		if w := request(h, "POST", "/api/v1/security/ip-addresses", f.admin, bad); w.Code != 400 {
			t.Fatalf("%v: %d", bad, w.Code)
		}
	}
	if w := request(h, "POST", "/api/v1/security/ip-addresses", f.admin, map[string]any{"range": "203.0.113.0/24", "note": "", "tags": []string{}}); w.Code != 409 || errorCode(t, w) != "ip_address_taken" {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body.String())
	}

	// The admin console limit names the tag; from inside the office, a
	// change that would leave this address out of it is refused.
	public := func(method, target, ip string, body any) *httptest.ResponseRecorder {
		r := jsonRequest(method, target, body)
		r.Host = f.adminHost
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set("Authorization", "Bearer "+f.admin)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	if w := public("PUT", "/api/v1/security/admin-access", "203.0.113.5", map[string]any{"restricted": true, "allowed": []string{}, "allowed_tags": []string{"사무실"}}); w.Code != 200 {
		t.Fatalf("limit by tag: %d %s", w.Code, w.Body.String())
	}
	if w := public("GET", "/api/v1/setup/status", "198.51.100.77", nil); w.Code != 200 {
		t.Fatalf("branch office, tagged: %d", w.Code)
	}
	if w := public("GET", "/api/v1/setup/status", "192.0.2.1", nil); w.Code != 404 {
		t.Fatalf("outside: %d", w.Code)
	}
	moved := map[string]any{"range": "192.0.2.0/24", "note": "본사", "tags": []string{"사무실"}}
	if w := public("PUT", "/api/v1/security/ip-addresses/"+office.ID, "203.0.113.5", moved); w.Code != 409 || errorCode(t, w) != "admin_access_locks_out" {
		t.Fatalf("locking oneself out: %d %s", w.Code, w.Body.String())
	}
	// Over SSH it goes through, and the console follows at once.
	if w := request(h, "PUT", "/api/v1/security/ip-addresses/"+office.ID, f.admin, moved); w.Code != 200 {
		t.Fatalf("update over SSH: %d %s", w.Code, w.Body.String())
	}
	if w := public("GET", "/api/v1/setup/status", "203.0.113.5", nil); w.Code != 404 {
		t.Fatalf("old office after the move: %d", w.Code)
	}
	if w := public("GET", "/api/v1/setup/status", "192.0.2.1", nil); w.Code != 200 {
		t.Fatalf("new office after the move: %d", w.Code)
	}

	// The view counts each tag and says what names it.
	var view ipAddressesView
	if w := request(h, "GET", "/api/v1/security/ip-addresses", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatalf("view: %d %s", w.Code, w.Body.String())
	}
	tags := map[string]ipTagUse{}
	for _, tag := range view.Tags {
		tags[tag.Name] = tag
	}
	if len(view.Entries) != 2 || tags["사무실"].Count != 2 || !tags["사무실"].AdminAccess || tags["지사"].Count != 1 || tags["지사"].AdminAccess {
		t.Fatalf("view = %+v", view)
	}
	if w := request(h, "DELETE", "/api/v1/security/ip-addresses/"+branch.ID, f.admin, nil); w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "DELETE", "/api/v1/security/ip-addresses/"+office.ID, f.admin, nil); w.Code != 409 || errorCode(t, w) != "ip_tag_in_use" {
		t.Fatalf("deleting the last 사무실 entry: %d %s", w.Code, w.Body.String())
	}
	if !slices.ContainsFunc(view.Entries, func(e control.IPAddress) bool { return e.Note == "지사" }) {
		t.Fatal("note lost")
	}
}
