package outlet

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// Conditions of test policies. A policy covers a request that meets all
// its conditions; values of one condition are alternatives.

func onPaths(operator string, values ...string) control.AccessCondition {
	return control.AccessCondition{Field: control.ConditionPath, Operator: operator, Values: values}
}

func onMethods(methods ...string) control.AccessCondition {
	return control.AccessCondition{Field: control.ConditionMethod, Operator: control.AccessIn, Values: methods}
}

// fromIPs names address book tags and addresses or CIDR ranges.
func fromIPs(values ...string) control.AccessCondition {
	return control.AccessCondition{Field: control.ConditionClientIP, Operator: control.AccessIn, Values: values}
}

// withToken asks for one of the access tokens, by ID, or any valid one.
func withToken(ids ...string) control.AccessCondition {
	if len(ids) == 0 {
		return control.AccessCondition{Field: control.ConditionToken, Operator: control.AccessAnyToken, Values: []string{}}
	}
	return control.AccessCondition{Field: control.ConditionToken, Operator: control.AccessIn, Values: ids}
}

func not(c control.AccessCondition) control.AccessCondition {
	c.Not = true
	return c
}

func conditions(list ...control.AccessCondition) []control.AccessCondition {
	if list == nil {
		return []control.AccessCondition{}
	}
	return list
}

// sendFrom requests host and path with method as a browser behind Caddy
// would, from client address ip ("" for none), with header.
func (f accessFixture) sendFrom(method, ip, host, path string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://"+host+path, nil)
	r.Host = host
	if ip != "" {
		r.Header.Set("X-Forwarded-For", ip)
	}
	r.Header.Set("Accept", "text/html")
	maps.Copy(r.Header, header)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// The policies an administrator writes for an API with an admin console:
// the API paths open to all, the rest only from the admin addresses and
// after a sign-in. Conditions all hold, and a "not" says what happens to
// everyone else.
func TestAccessConditionsAllHold(t *testing.T) {
	f := newAccessFixture(t)
	if _, err := f.db.CreateIPAddress(context.Background(), control.IPAddress{Range: "203.0.113.0/24", Tags: []string{"관리자"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.setPolicies(t,
		control.AccessPolicy{Name: "API", Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: conditions(onPaths(control.BlockStartsWith, "/v1/"))},
		control.AccessPolicy{Name: "Not admins", Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: conditions(not(fromIPs("관리자")))},
		control.AccessPolicy{Name: "Admins", Enabled: true, Hosts: []string{"app.example.com"}, AllowEveryone: true},
	)
	for _, c := range []struct {
		ip, path string
		code     int
	}{
		{"198.51.100.30", "/v1/models", http.StatusOK},
		{"203.0.113.9", "/v1/models", http.StatusOK},
		{"198.51.100.30", "/admin", http.StatusForbidden},
		{"203.0.113.9", "/admin", http.StatusFound},
	} {
		if w := f.visitFrom(c.ip, "app.example.com", c.path); w.Code != c.code {
			t.Errorf("%s %s: %d, want %d", c.ip, c.path, w.Code, c.code)
		}
	}
	// One policy with both conditions means the same for the others.
	f.setPolicies(t,
		control.AccessPolicy{Name: "Admins only", Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onPaths(control.BlockStartsWith, "/v1/")), not(fromIPs("관리자")))},
	)
	for _, c := range []struct {
		ip, path string
		code     int
	}{
		{"198.51.100.30", "/v1/models", http.StatusOK},
		{"198.51.100.30", "/admin", http.StatusForbidden},
		{"203.0.113.9", "/admin", http.StatusOK},
	} {
		if w := f.visitFrom(c.ip, "app.example.com", c.path); w.Code != c.code {
			t.Errorf("one policy, %s %s: %d, want %d", c.ip, c.path, w.Code, c.code)
		}
	}
}

// What a condition leaves out opens it to more: a protective policy's
// "not" path is compared exactly, so a path an app may read as the one
// left out stays protected, and a bypass's "not" path broadly, so it can't
// open the path it leaves out by another spelling.
func TestAccessNotPathsOpenOnlyWhatIsCertain(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Name: "Protect all but public", Enabled: true, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onPaths(control.BlockStartsWith, "/public/"))), AllowEveryone: true},
		control.AccessPolicy{Name: "Bypass all but admin", Enabled: true, Action: control.AccessBypass, Hosts: []string{"other.example.com"}, Conditions: conditions(not(onPaths(control.BlockStartsWith, "/admin/")))},
		control.AccessPolicy{Name: "Everyone else", Enabled: true, Hosts: []string{"other.example.com"}, AllowEveryone: true},
	)
	for _, c := range []struct {
		host, path string
		code       int
	}{
		{"app.example.com", "/public/logo.png", http.StatusOK},
		{"app.example.com", "/Public/logo.png", http.StatusFound},
		{"app.example.com", "/public/%2e%2e/admin", http.StatusFound},
		{"app.example.com", "/admin", http.StatusFound},
		{"other.example.com", "/docs", http.StatusOK},
		{"other.example.com", "/admin/users", http.StatusFound},
		{"other.example.com", "/ADMIN/users", http.StatusFound},
		{"other.example.com", "//admin/users", http.StatusFound},
		{"other.example.com", "/docs/%2e%2e/admin/users", http.StatusFound},
	} {
		if w := f.visit(c.host, c.path); w.Code != c.code {
			t.Errorf("%s%s: %d, want %d", c.host, c.path, w.Code, c.code)
		}
	}
}

// Methods, tokens and client addresses take "not" too. A client address
// that isn't known is in no range: a "not" holds for it, except in a
// bypass, which it never opens.
func TestAccessNotMethodsTokensAndAddresses(t *testing.T) {
	f := newAccessFixture(t)
	token, value, err := f.db.CreateAccessToken(context.Background(), control.AccessTokenScope{Name: "CI", Hosts: []string{"app.example.com"}}, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccessTokens(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.setPolicies(t,
		control.AccessPolicy{Name: "Read only", Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onMethods("GET")), not(withToken(token.ID)))},
		control.AccessPolicy{Name: "Outsiders", Enabled: true, Action: control.AccessBlock, Hosts: []string{"other.example.com"}, Conditions: conditions(not(fromIPs("203.0.113.0/24")))},
		control.AccessPolicy{Name: "Insiders", Enabled: true, Action: control.AccessBypass, Hosts: []string{"other.example.com"}, Conditions: conditions(not(fromIPs("192.0.2.0/24")))},
	)
	withValue := http.Header{accessTokenHeader: {value}}
	for _, c := range []struct {
		method, ip, host string
		header           http.Header
		code             int
	}{
		{"GET", "198.51.100.30", "app.example.com", nil, http.StatusOK},
		{"HEAD", "198.51.100.30", "app.example.com", nil, http.StatusOK},
		{"POST", "198.51.100.30", "app.example.com", nil, http.StatusForbidden},
		{"POST", "198.51.100.30", "app.example.com", withValue, http.StatusOK},
		{"GET", "198.51.100.30", "other.example.com", nil, http.StatusForbidden},
		{"GET", "203.0.113.9", "other.example.com", nil, http.StatusOK},
		{"GET", "", "other.example.com", nil, http.StatusForbidden},
	} {
		if w := f.sendFrom(c.method, c.ip, c.host, "/", c.header); w.Code != c.code {
			t.Errorf("%s %s from %q: %d, want %d", c.method, c.host, c.ip, w.Code, c.code)
		}
	}
	rule := f.store.access.config.Load().matching("other.example.com", "GET", &url.URL{Path: "/"}, "", "")
	if rule == nil || rule.policy.Name != "Outsiders" {
		t.Fatalf("unknown client: %+v", rule)
	}
	insiders := compileAccess(control.AccessPolicies{Policies: []control.AccessPolicy{
		{Name: "Insiders", Enabled: true, Action: control.AccessBypass, Hosts: []string{"other.example.com"}, Conditions: conditions(not(fromIPs("192.0.2.0/24")))},
	}}, control.IPAddresses{}, false)
	if rule := insiders.matching("other.example.com", "GET", &url.URL{Path: "/"}, "", ""); rule != nil {
		t.Fatal("a bypass with a \"not\" client IP opened to an unknown client")
	}
}

// An earlier policy shadows a later one only where each of its conditions
// holds whenever the later's do.
func TestAccessConditionsShadow(t *testing.T) {
	rule := func(action string, list ...control.AccessCondition) *accessRule {
		config := compileAccess(control.AccessPolicies{Policies: []control.AccessPolicy{{Enabled: true, Action: action, Hosts: []string{"app.example.com"}, Conditions: conditions(list...)}}}, control.IPAddresses{}, false)
		return &config.rules[0]
	}
	allow := control.AccessAllow
	for _, c := range []struct {
		name           string
		earlier, later *accessRule
		shadows        bool
	}{
		{"everything", rule(allow), rule(allow, onPaths(control.BlockStartsWith, "/x/")), true},
		{"all paths", rule(allow, onPaths(control.BlockStartsWith, "/")), rule(allow, onMethods("POST")), true},
		{"prefix", rule(allow, onPaths(control.BlockStartsWith, "/team/")), rule(allow, onPaths(control.BlockStartsWith, "/team/a/")), true},
		{"equals under prefix", rule(allow, onPaths(control.BlockStartsWith, "/team/")), rule(allow, onPaths(control.BlockEquals, "/team")), true},
		{"narrower prefix", rule(allow, onPaths(control.BlockStartsWith, "/team/a/")), rule(allow, onPaths(control.BlockStartsWith, "/team/")), false},
		{"methods", rule(allow, onMethods("GET", "POST")), rule(allow, onMethods("POST")), true},
		{"not methods", rule(allow, not(onMethods("GET"))), rule(allow, onMethods("POST")), true},
		{"not methods overlap", rule(allow, not(onMethods("GET"))), rule(allow, onMethods("GET", "POST")), false},
		{"client IPs", rule(allow, fromIPs("192.0.2.0/24")), rule(allow, fromIPs("192.0.2.8/29")), true},
		{"client IPs elsewhere", rule(allow, fromIPs("192.0.2.0/24")), rule(allow), false},
		{"not client IPs", rule(allow, not(fromIPs("192.0.2.8/29"))), rule(allow, not(fromIPs("192.0.2.0/24"))), true},
		{"any token", rule(allow, withToken()), rule(allow, withToken("tok1")), true},
		{"one token", rule(allow, withToken("tok1")), rule(allow, withToken()), false},
		{"no token", rule(allow, not(withToken("tok1"))), rule(allow, not(withToken())), true},
		{"bypass prefix", rule(control.AccessBypass, onPaths(control.BlockStartsWith, "/")), rule(allow, onPaths(control.BlockStartsWith, "/x/")), false},
		// A client whose address isn't known meets the allow's "not", not the bypass's.
		{"bypass not client IPs", rule(control.AccessBypass, not(fromIPs("192.0.2.0/24"))), rule(allow, not(fromIPs("192.0.2.0/24"))), false},
		{"not client IPs over a bypass", rule(allow, not(fromIPs("192.0.2.0/24"))), rule(control.AccessBypass, not(fromIPs("192.0.2.0/24"))), true},
		// A method the console doesn't list meets the allow's "not", not the bypass's.
		{"bypass not methods", rule(control.AccessBypass, not(onMethods("POST"))), rule(allow, not(onMethods("POST"))), false},
	} {
		if got := c.earlier.shadows(c.later); got != c.shadows {
			t.Errorf("%s: shadows = %v, want %v", c.name, got, c.shadows)
		}
	}
}

// A bypass leaving out some methods opens only the other methods the
// console lists: "not POST, PUT, PATCH, DELETE" is read only, not every
// method an app may take.
func TestAccessBypassNotMethodsOpenOnlyKnownMethods(t *testing.T) {
	config := compileAccess(control.AccessPolicies{Policies: []control.AccessPolicy{
		{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onMethods("POST", "PUT", "PATCH", "DELETE")))},
	}}, control.IPAddresses{}, false)
	for method, opens := range map[string]bool{"GET": true, "head": true, "OPTIONS": true, "POST": false, "post": false, "PROPFIND": false, "MOVE": false, "PURGE": false} {
		if got := config.matching("app.example.com", method, &url.URL{Path: "/"}, "", "198.51.100.30") != nil; got != opens {
			t.Errorf("%s: bypassed = %v, want %v", method, got, opens)
		}
	}
	// A block leaving out some methods still blocks the ones it doesn't know.
	config = compileAccess(control.AccessPolicies{Policies: []control.AccessPolicy{
		{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onMethods("GET")))},
	}}, control.IPAddresses{}, false)
	if config.matching("app.example.com", "PROPFIND", &url.URL{Path: "/"}, "", "198.51.100.30") == nil {
		t.Error("a block leaving out GET let PROPFIND through")
	}
}

// Saved policies that fail the checks keep an allow policy with conditions
// closed to everyone: letting whom it names in to all of its addresses
// would let them where its conditions kept them out.
func TestAccessDamagedPoliciesWithConditionsLetNobodyIn(t *testing.T) {
	config := closedAccess(control.AccessPolicies{Policies: []control.AccessPolicy{
		{ID: "public", Enabled: true, Action: control.AccessAllow, Hosts: []string{"app.example.com"}, Conditions: conditions(not(onPaths(control.BlockStartsWith, "/admin/"))), AllowEveryone: true},
		{ID: "admins", Enabled: true, Action: control.AccessAllow, Hosts: []string{"other.example.com"}, AllowedUsers: []string{"admin@example.com"}},
	}})
	rule := config.matching("app.example.com", "GET", &url.URL{Path: "/admin/users"}, "", "198.51.100.30")
	if rule == nil || rule.policy.Allows(control.AccessIdentity{Email: "anyone@example.com"}, false) {
		t.Fatalf("damaged allow with conditions: %+v", rule)
	}
	if rule := config.matching("other.example.com", "GET", &url.URL{Path: "/"}, "", "198.51.100.30"); rule == nil || !rule.policy.Allows(control.AccessIdentity{Email: "admin@example.com"}, false) {
		t.Fatalf("damaged allow without conditions: %+v", rule)
	}
}
