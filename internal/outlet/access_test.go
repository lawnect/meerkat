package outlet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// whoami answers with the access email and cookies the app received, and
// the other headers it got whose names mention meerkat.
func whoami() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var others []string
		for name, values := range r.Header {
			if strings.Contains(strings.ToLower(name), "meerkat") && name != accessEmailHeader && name != accessServiceHeader {
				others = append(others, name+":"+strings.Join(values, ","))
			}
		}
		slices.Sort(others)
		_, _ = fmt.Fprintf(w, "email=%s cookie=%s path=%s service=%s token=%s others=%s", r.Header.Get(accessEmailHeader), r.Header.Get("Cookie"), r.URL.Path, r.Header.Get(accessServiceHeader), r.Header.Get(accessTokenHeader), strings.Join(others, ";"))
	})
}

type accessFixture struct {
	proxyFixture
	idp *fakeIdP
	// binds keeps the browser's binding cookie per address, as a browser
	// would between the challenge and the finish.
	binds map[string]*http.Cookie
}

func newAccessFixture(t *testing.T, policies ...control.AccessPolicy) accessFixture {
	t.Helper()
	f := accessFixture{proxyFixture: newProxyFixture(t, whoami(), whoami()), idp: newFakeIdP(t), binds: map[string]*http.Cookie{}}
	f.route(t, 0, "app.example.com")
	f.route(t, 1, "other.example.com")
	f.store.ssoHTTP = f.idp.server.Client()
	if _, err := f.db.SetAccessProvider(context.Background(), control.AccessProvider{Enabled: true, Issuer: f.idp.server.URL, ClientID: "access", ClientSecret: "access-secret-1234",
		Scopes: []string{"openid", "email", "groups"}, GroupsClaim: "groups", SessionHours: 8}, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.setPolicies(t, policies...)
	return f
}

func (f accessFixture) setPolicies(t *testing.T, policies ...control.AccessPolicy) {
	t.Helper()
	for i := range policies {
		if policies[i].Name == "" {
			policies[i].Name = fmt.Sprintf("policy %d", i)
		}
	}
	if _, err := f.db.SetAccessPolicies(context.Background(), control.AccessPolicies{Policies: policies}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// visit requests host and path as a browser behind Caddy would.
func (f accessFixture) visit(host, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://"+host+path, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-For", "198.51.100.30")
	r.Header.Set("Accept", "text/html")
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// bindCookie is the binding cookie a challenge set.
func bindCookie(w *httptest.ResponseRecorder) *http.Cookie { return cookieNamed(w, accessBindCookie) }

// flight is a sign-in sent to the provider and not back yet: the cookies
// the browser holds for it and the authorization request.
type flight struct {
	host        string
	bind, state *http.Cookie
	query       url.Values
}

// signIn follows a whole sign-in from a request for path on host to the
// response of the provider's callback. claims are the ID token's.
func (f accessFixture) signIn(t *testing.T, host, path string, claims map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	fl := f.depart(t, host, path)
	f.binds[host] = fl.bind
	return f.land(t, fl, claims)
}

// depart sends a browser from path on host to the provider.
func (f accessFixture) depart(t *testing.T, host, path string) flight {
	t.Helper()
	return f.departWith(t, host, path, nil, nil)
}

// departWith is depart for a browser holding cookies on host and on the
// management host.
func (f accessFixture) departWith(t *testing.T, host, path string, hostCookies, adminCookies []*http.Cookie) flight {
	t.Helper()
	challenge := f.visit(host, path, hostCookies...)
	if challenge.Code != http.StatusFound {
		t.Fatalf("challenge: %d %s", challenge.Code, challenge.Body.String())
	}
	start, err := url.Parse(challenge.Header().Get("Location"))
	if err != nil || start.Host != f.adminHost || start.Path != accessStartPath || start.Query().Get("host") != host {
		t.Fatalf("challenge location: %s", challenge.Header().Get("Location"))
	}
	begin := f.visit(f.adminHost, start.RequestURI(), adminCookies...)
	location, err := url.Parse(begin.Header().Get("Location"))
	if begin.Code != http.StatusFound || err != nil || !strings.HasPrefix(location.String(), f.idp.server.URL+"/auth?") {
		t.Fatalf("start: %d %s %s", begin.Code, begin.Header().Get("Location"), begin.Body.String())
	}
	query := location.Query()
	if query.Get("redirect_uri") != "https://"+f.adminHost+accessCallbackPath || query.Get("client_id") != "access" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization request: %s", location)
	}
	return flight{host: host, bind: bindCookie(challenge), state: cookieNamed(begin, accessStateCookie), query: query}
}

// land brings a browser back from the provider, signed in with claims.
func (f accessFixture) land(t *testing.T, fl flight, claims map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	full := map[string]any{"iss": f.idp.server.URL, "aud": "access", "sub": fmt.Sprint("subject-", claims["email"]), "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": fl.query.Get("nonce")}
	for key, value := range claims {
		full[key] = value
	}
	f.idp.mu.Lock()
	f.idp.challenge, f.idp.claims = fl.query.Get("code_challenge"), full
	f.idp.mu.Unlock()
	return f.visit(f.adminHost, accessCallbackPath+"?code=good-code&state="+url.QueryEscape(fl.query.Get("state")), fl.state)
}

// finish swaps the callback's ticket for the session cookie.
func (f accessFixture) finish(t *testing.T, callback *httptest.ResponseRecorder) (*http.Cookie, string) {
	t.Helper()
	if callback.Code != http.StatusSeeOther {
		t.Fatalf("callback: %d %s", callback.Code, callback.Body.String())
	}
	next, err := url.Parse(callback.Header().Get("Location"))
	if err != nil || next.Path != accessPathPrefix+"finish" {
		t.Fatalf("callback location: %s", callback.Header().Get("Location"))
	}
	done := f.visit(next.Host, next.RequestURI(), f.binds[next.Host])
	session := cookieNamed(done, accessCookie)
	if done.Code != http.StatusSeeOther || session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" {
		t.Fatalf("finish: %d %+v %s", done.Code, session, done.Body.String())
	}
	return session, done.Header().Get("Location")
}

var verified = func(email string, extra ...any) map[string]any {
	claims := map[string]any{"email": email, "email_verified": true}
	for i := 0; i+1 < len(extra); i += 2 {
		claims[extra[i].(string)] = extra[i+1]
	}
	return claims
}

// Only covered paths need a sign-in; navigations are sent to sign in,
// other requests get 401; nothing is cached.
func TestAccessChallengesOnlyProtectedPaths(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/admin/"}}, AllowedUsers: []string{"@corp.example.com"}})
	if w := f.visit("app.example.com", "/public"); w.Code != 200 {
		t.Fatalf("unprotected path: %d", w.Code)
	}
	if w := f.visit("other.example.com", "/admin/x"); w.Code != 200 {
		t.Fatalf("unprotected host: %d", w.Code)
	}
	w := f.visit("app.example.com", "/admin/settings?tab=1")
	if w.Code != http.StatusFound || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("navigation: %d %v", w.Code, w.Header())
	}
	start, _ := url.Parse(w.Header().Get("Location"))
	if start.Query().Get("return") != "/admin/settings?tab=1" {
		t.Fatalf("return: %s", start)
	}
	r := httptest.NewRequest("POST", "/admin/api", nil)
	r.Host = "app.example.com"
	r.Header.Set("X-Forwarded-For", "198.51.100.30")
	r.Header.Set("Accept", "application/json")
	api := httptest.NewRecorder()
	f.handler.ServeHTTP(api, r)
	if api.Code != http.StatusUnauthorized {
		t.Fatalf("API request: %d", api.Code)
	}
	upgrade := httptest.NewRequest("GET", "/admin/live", nil)
	upgrade.Host = "app.example.com"
	upgrade.Header.Set("Upgrade", "websocket")
	upgrade.Header.Set("Accept", "text/html")
	ws := httptest.NewRecorder()
	f.handler.ServeHTTP(ws, upgrade)
	if ws.Code != http.StatusUnauthorized {
		t.Fatalf("upgrade: %d", ws.Code)
	}
}

// Path tricks that an app might still read as a protected path are caught.
func TestAccessCatchesPathVariants(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/admin/"}}, AllowedUsers: []string{"@corp.example.com"}})
	for _, path := range []string{"/admin", "/admin/", "/Admin/x", "//admin/x", "/x/../admin/y", "/%61dmin/y", "/admin;jsessionid=1/x", "/admin./x", "/./admin/x",
		// Read as /admin/x by servers that drop parameters first, as /x/admin/x by others.
		"/x/.;/../admin/x", "/admin/.;/../x"} {
		if w := f.visit("app.example.com", path); w.Code != http.StatusFound {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

// A person the policy allows signs in, reaches the app with the email in a
// header, and the app never sees the session cookie.
func TestAccessSignInLetsAllowedPeopleThrough(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	session, back := f.finish(t, f.signIn(t, "app.example.com", "/reports?q=1", verified("Alice@Corp.example.com")))
	if back != "/reports?q=1" {
		t.Fatalf("back to: %s", back)
	}
	w := f.visit("app.example.com", "/reports", session, &http.Cookie{Name: "app", Value: "kept"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "email=alice@corp.example.com") || strings.Contains(w.Body.String(), accessCookie) || !strings.Contains(w.Body.String(), "app=kept") {
		t.Fatalf("app request: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAuditAction(t); event != "access.login" {
		t.Fatalf("audit: %s", event)
	}
}

func (f accessFixture) lastAuditAction(t *testing.T) string {
	t.Helper()
	f.store.journal.flush()
	events, _, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1})
	if err != nil || len(events) == 0 {
		t.Fatalf("audit: %+v %v", events, err)
	}
	return events[0].Action
}

// A client can't hand the app an identity of its own, not even under a
// name some app servers read as the Outlet's header.
func TestAccessEmailHeaderCantBeForged(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	r := httptest.NewRequest("GET", "/x", nil)
	r.Host = "other.example.com"
	r.Header.Set(accessEmailHeader, "boss@corp.example.com")
	for _, name := range []string{"X_Meerkat_Access_Email", "x-meerkat-access-email", "X-MEERKAT-ACCESS_SERVICE", "X-Meerkat-Access-Groups"} {
		r.Header[name] = []string{"forged"}
	}
	r.AddCookie(&http.Cookie{Name: accessCookie, Value: "forged.value"})
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, "email= ") || strings.Contains(body, "forged") || strings.Contains(body, "boss") {
		t.Fatalf("unprotected host: %s", body)
	}
}

// A path longer than apps take is refused before any rule reads it; a long
// query string is not.
func TestPublicPathLimit(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true})
	for _, c := range []struct {
		target string
		code   int
	}{
		{"/" + strings.Repeat("a", maxPublicPath-1), 200},
		{"/" + strings.Repeat("a", maxPublicPath), http.StatusRequestURITooLong},
		{"/" + strings.Repeat("%41", maxPublicPath/3+1), http.StatusRequestURITooLong},
		{"/" + strings.Repeat("%20", maxPublicPath/3+1), http.StatusRequestURITooLong},
		{"/" + strings.Repeat("%C3%A9", maxPublicPath/6+1), http.StatusRequestURITooLong},
		{"/x?q=" + strings.Repeat("a", 4*maxPublicPath), 200},
	} {
		if w := f.visit("app.example.com", c.target); w.Code != c.code {
			t.Errorf("%d-byte target: %d, want %d", len(c.target), w.Code, c.code)
		}
	}
}

// Who may pass: users and groups both, when both are set; administrators
// when the policy says so; verified emails only.
func TestAccessWhoMayPass(t *testing.T) {
	for _, c := range []struct {
		name   string
		policy control.AccessPolicy
		claims map[string]any
		admin  bool
		pass   bool
	}{
		{"user", control.AccessPolicy{AllowedUsers: []string{"alice@corp.example.com"}}, verified("alice@corp.example.com"), false, true},
		{"other user", control.AccessPolicy{AllowedUsers: []string{"alice@corp.example.com"}}, verified("bob@corp.example.com"), false, false},
		{"domain", control.AccessPolicy{AllowedUsers: []string{"@corp.example.com"}}, verified("bob@corp.example.com"), false, true},
		{"subdomain is not the domain", control.AccessPolicy{AllowedUsers: []string{"@corp.example.com"}}, verified("bob@evil.corp.example.com"), false, false},
		{"group", control.AccessPolicy{AllowedGroups: []string{"ops"}}, verified("x@else.example.com", "groups", []string{"ops"}), false, true},
		{"users and groups both", control.AccessPolicy{AllowedUsers: []string{"@corp.example.com"}, AllowedGroups: []string{"ops"}}, verified("bob@corp.example.com", "groups", []string{"dev"}), false, false},
		{"admins", control.AccessPolicy{AllowAdmins: true}, verified("admin@example.com"), true, true},
		{"admins only", control.AccessPolicy{AllowAdmins: true}, verified("bob@corp.example.com"), false, false},
		{"unverified", control.AccessPolicy{AllowedUsers: []string{"@corp.example.com"}}, map[string]any{"email": "bob@corp.example.com"}, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.policy.Enabled, c.policy.Hosts, c.policy.AllPaths = true, []string{"app.example.com"}, true
			f := newAccessFixture(t, c.policy)
			if c.admin {
				if _, err := f.db.SSOAdmin(context.Background(), "https://idp.example.com", "admin-subject", "admin@example.com"); err != nil {
					t.Fatal(err)
				}
			}
			callback := f.signIn(t, "app.example.com", "/", c.claims)
			if !c.pass {
				if callback.Code != http.StatusForbidden || callback.Header().Get("Location") != "" {
					t.Fatalf("callback: %d %s", callback.Code, callback.Header().Get("Location"))
				}
				if event := f.lastAuditAction(t); event != "access.login_failed" {
					t.Fatalf("audit: %s", event)
				}
				return
			}
			session, _ := f.finish(t, callback)
			if w := f.visit("app.example.com", "/", session); w.Code != 200 {
				t.Fatalf("with the session: %d", w.Code)
			}
		})
	}
}

// A session is good on its own address only, can't be altered, ends when it
// expires, when the key is replaced, and when a policy no longer allows it.
func TestAccessSessionLimits(t *testing.T) {
	policy := control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com", "other.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}}
	f := newAccessFixture(t, policy)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if w := f.visit("other.example.com", "/", session); w.Code != http.StatusFound {
		t.Fatalf("another address: %d", w.Code)
	}
	payload, signature, _ := strings.Cut(session.Value, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	forged := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), "alice", "boss1", 1))) + "." + signature
	if w := f.visit("app.example.com", "/", &http.Cookie{Name: accessCookie, Value: forged}); w.Code != http.StatusFound {
		t.Fatalf("altered session: %d", w.Code)
	}
	now := time.Now()
	f.store.now = func() time.Time { return now.Add(9 * time.Hour) }
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusFound {
		t.Fatalf("expired session: %d", w.Code)
	}
	f.store.now = time.Now
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("fresh session: %d", w.Code)
	}
	policy.AllowedUsers = []string{"bob@corp.example.com"}
	f.setPolicies(t, policy)
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusForbidden {
		t.Fatalf("after the policy changed: %d", w.Code)
	}
	policy.AllowedUsers = []string{"@corp.example.com"}
	f.setPolicies(t, policy)
	if w := request(f.store.Handler(), "POST", "/api/v1/security/access/sessions/revoke", f.admin, nil); w.Code != 204 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusFound {
		t.Fatalf("after signing everyone out: %d", w.Code)
	}
}

// Policies apply in order: the first enabled one that covers a request
// decides it, so a narrower policy goes above a broader one.
func TestAccessPoliciesApplyInOrder(t *testing.T) {
	finance := control.AccessPolicy{Enabled: true, Hosts: []string{"*.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/finance/"}}, AllowedGroups: []string{"finance"}}
	staff := control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}}
	f := newAccessFixture(t, finance, staff)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("staff policy: %d", w.Code)
	}
	if w := f.visit("app.example.com", "/finance/q3", session); w.Code != http.StatusForbidden {
		t.Fatalf("finance policy first: %d", w.Code)
	}
	carol, _ := f.finish(t, f.signIn(t, "app.example.com", "/finance/q3", verified("carol@else.example.com", "groups", []string{"finance", "unrelated"})))
	if w := f.visit("app.example.com", "/finance/q3", carol); w.Code != 200 {
		t.Fatalf("finance member: %d", w.Code)
	}
	if w := f.visit("app.example.com", "/", carol); w.Code != http.StatusForbidden {
		t.Fatalf("finance member, staff policy: %d", w.Code)
	}
	// Only groups some policy names ride in the cookie.
	payload, _, _ := strings.Cut(carol.Value, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	if strings.Contains(string(raw), "unrelated") {
		t.Fatalf("cookie carries %s", raw)
	}
	// The other way round, the broader policy decides first.
	f.setPolicies(t, staff, finance)
	if w := f.visit("app.example.com", "/finance/q3", session); w.Code != 200 {
		t.Fatalf("staff policy first: %d", w.Code)
	}
}

// A bypass policy opens what it covers to anyone without a sign-in, and a
// block policy shuts it to everyone, sessions included; above a policy
// protecting every path they carve out exceptions. A path an app may read
// differently is never opened.
func TestAccessBypassAndBlock(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/v1/"}, {Operator: control.BlockEquals, Value: "/health"}}},
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/internal/"}}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	for _, path := range []string{"/v1/chat/completions", "/v1/", "/health", "/v1/%EA%B2%8C%EC%8B%9C%ED%8C%90", "/v1/items(1)", "/v1/files/*", "/v1/a,b:c@d", "/v1/x%20y", "/v1/게시판"} {
		if w := f.visit("app.example.com", path); w.Code != 200 || !strings.Contains(w.Body.String(), "email= ") {
			t.Errorf("open %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	// Bypass opens only what it plainly names, as the app gets it: not a
	// path an app could route elsewhere.
	for _, path := range []string{"/", "/login", "/workspace", "/healthz", "/v2/x", "/v1", "/V1/models", "/v1./models", "/v1%20/models", "/v1;private/models", "/v1%5cmodels", "/v1%2fmodels", "/v%31/models", "/v1//models", "/health/", "/HEALTH", "/v1/%ea%b2%8c", "/v1/files/%2A", "/v1/%7Eitems", "/v1/x%2a", "/v1/%61dmin"} {
		if w := f.visit("app.example.com", path); w.Code != http.StatusFound {
			t.Errorf("protected %s: %d", path, w.Code)
		}
	}
	// Not opened, an unclear path meets the first policy that shuts paths:
	// here the block policy.
	for _, path := range []string{"/v1/%2e%2e/admin", "/v1/x%2f..%2f..%2fadmin", "/v1%5c..%5cadmin", "/v1/.../x", "/v1/x%00/../../admin", "/v1/%252e%252e/admin",
		"/v1/%2500private", "/v1/%250aprivate", "/v1/%25c0%25ae%25c0%25ae/private", "/v1/%25ff/private"} {
		if w := f.visit("app.example.com", path); w.Code != http.StatusForbidden {
			t.Errorf("unclear %s: %d", path, w.Code)
		}
	}
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	for _, cookies := range [][]*http.Cookie{nil, {session}} {
		if w := f.visit("app.example.com", "/internal/x", cookies...); w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "<form") {
			t.Fatalf("blocked: %d %s", w.Code, w.Body.String())
		}
	}
	if w := f.visit("app.example.com", "/workspace", session); w.Code != 200 {
		t.Fatalf("signed in: %d", w.Code)
	}
	// A sign-in starts only for addresses an allow policy covers.
	f.setPolicies(t, control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true})
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusNotFound {
		t.Fatalf("start for a blocked address: %d", w.Code)
	}
}

// A policy limited to some methods leaves the others to the policies below:
// GET takes HEAD along, and a method no policy limits to falls to one that
// names none.
func TestAccessPoliciesByMethod(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/v1/"}}, Methods: []string{"GET"}},
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, Methods: []string{"DELETE", "PUT"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	send := func(method, path string) int {
		r := httptest.NewRequest(method, "https://app.example.com"+path, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.30")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	for _, c := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/v1/models", 200},
		{"HEAD", "/v1/models", 200},
		{"POST", "/v1/models", http.StatusUnauthorized},
		{"OPTIONS", "/v1/models", http.StatusUnauthorized},
		{"DELETE", "/v1/models", http.StatusForbidden},
		{"PUT", "/workspace", http.StatusForbidden},
		{"PATCH", "/workspace", http.StatusUnauthorized},
		{"PROPFIND", "/v1/models", http.StatusUnauthorized},
		{"get", "/v1/models", 200},
		{"delete", "/workspace", http.StatusForbidden},
		{"Put", "/workspace", http.StatusForbidden},
	} {
		if code := send(c.method, c.path); code != c.code {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, code, c.code)
		}
	}
	// A policy limited to some methods hides none below it from a sign-in.
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, Methods: []string{"GET", "POST"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true},
	)
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusFound {
		t.Fatalf("start behind a block for some methods: %d", w.Code)
	}
	// One for every method a later policy names does.
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, Methods: []string{"GET", "POST"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true, Methods: []string{"POST"}},
	)
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusNotFound {
		t.Fatalf("start behind a block for its methods: %d", w.Code)
	}
	// Saved through the API, methods come back in one order and are audited.
	h := f.store.Handler()
	policy := map[string]any{"name": "API", "enabled": true, "action": "bypass", "hosts": []string{"app.example.com"}, "all_paths": true, "methods": []string{"post", "GET"}}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": []any{policy}}); w.Code != 200 || !strings.Contains(w.Body.String(), `"methods":["GET","POST"]`) {
		t.Fatalf("save methods: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAudit(t); event.Action != "security.access_policies" || !strings.Contains(string(event.Detail), `"methods":["GET","POST"]`) {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	policy["methods"] = []string{"HEAD"}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": []any{policy}}); w.Code != 400 {
		t.Fatalf("save HEAD: %d", w.Code)
	}
}

// What each policy decides is counted, by policy and hour, and the console
// reads it, requests just made included.
func TestAccessMetrics(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/v1/"}}},
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/internal/"}}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	policies, err := f.db.AccessPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	api, shut, staff := policies.Policies[0].ID, policies.Policies[1].ID, policies.Policies[2].ID
	// Signing in starts with a visit sent to sign in: one challenge.
	alice, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	f.visit("app.example.com", "/v1/models")
	f.visit("app.example.com", "/v1/chat")
	f.visit("app.example.com", "/internal/x", alice)
	f.visit("app.example.com", "/workspace")
	f.visit("app.example.com", "/workspace", alice)
	f.visit("app.example.com", "/workspace", &http.Cookie{Name: accessCookie, Value: "forged.value"})
	f.visit("other.example.com", "/")
	var metrics control.AccessSeries
	now := time.Now().UTC()
	day := now.Truncate(24 * time.Hour)
	query := url.Values{"unit": {"hour"}, "from": {day.Format(time.RFC3339)}, "to": {day.Add(24 * time.Hour).Format(time.RFC3339)}, "tz": {"UTC"}}.Encode()
	w := request(f.store.Handler(), "GET", "/api/v1/observe/access-metrics?"+query, f.admin, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &metrics) != nil {
		t.Fatalf("metrics: %d %s", w.Code, w.Body.String())
	}
	want := map[string]control.AccessCounts{api: {Passed: 2}, shut: {Denied: 1}, staff: {Passed: 1, Challenged: 3}}
	for id, counts := range want {
		if metrics.Policies[id] != counts {
			t.Errorf("policy %s: %+v, want %+v", id, metrics.Policies[id], counts)
		}
	}
	if len(metrics.Policies) != len(want) || len(metrics.Buckets) != 24 || metrics.Buckets[now.Hour()].AccessCounts != (control.AccessCounts{Passed: 3, Challenged: 3, Denied: 1}) {
		t.Fatalf("metrics: %+v", metrics)
	}
	// Counted once: read again, by day, nothing doubles.
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	query = url.Values{"unit": {"day"}, "from": {month.Format(time.RFC3339)}, "to": {month.AddDate(0, 1, 0).Format(time.RFC3339)}, "tz": {"UTC"}}.Encode()
	w = request(f.store.Handler(), "GET", "/api/v1/observe/access-metrics?"+query, f.admin, nil)
	if json.Unmarshal(w.Body.Bytes(), &metrics) != nil || metrics.Unit != "day" || metrics.Policies[api] != want[api] || metrics.Buckets[now.Day()-1].Passed != 3 {
		t.Fatalf("again: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{"unit=week&" + query, "unit=hour&from=x&to=y", "unit=hour&tz=Local&" + query} {
		if w := request(f.store.Handler(), "GET", "/api/v1/observe/access-metrics?"+bad, f.admin, nil); w.Code != 400 {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/access-metrics?"+query, "", nil); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
}

// An allow policy can let in everyone the access provider signs in.
func TestAccessAllowEveryone(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true})
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("anyone@elsewhere.example.org")))
	if w := f.visit("app.example.com", "/", session); w.Code != 200 || !strings.Contains(w.Body.String(), "email=anyone@elsewhere.example.org") {
		t.Fatalf("everyone: %d %s", w.Code, w.Body.String())
	}
	if w := f.signIn(t, "app.example.com", "/", map[string]any{"email": "unverified@elsewhere.example.org"}); w.Code != http.StatusForbidden {
		t.Fatalf("unverified email: %d", w.Code)
	}
}

// A ticket works once, on the address it was issued for.
func TestAccessTicketsAreSingleUse(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com", "other.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	callback := f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com"))
	next, _ := url.Parse(callback.Header().Get("Location"))
	if w := f.visit("other.example.com", next.RequestURI()); w.Code != http.StatusBadRequest || cookieNamed(w, accessCookie) != nil {
		t.Fatalf("ticket on another address: %d", w.Code)
	}
	// The wrong address used the ticket up.
	if w := f.visit("app.example.com", next.RequestURI()); w.Code != http.StatusBadRequest {
		t.Fatalf("ticket again: %d", w.Code)
	}
	callback = f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com"))
	next, _ = url.Parse(callback.Header().Get("Location"))
	if w := f.visit("app.example.com", next.RequestURI(), f.binds["app.example.com"]); w.Code != http.StatusSeeOther {
		t.Fatalf("first use: %d", w.Code)
	}
	if w := f.visit("app.example.com", next.RequestURI(), f.binds["app.example.com"]); w.Code != http.StatusBadRequest {
		t.Fatalf("second use: %d", w.Code)
	}
}

// A ticket from an attacker's own sign-in can't sign a victim's browser in:
// it only works in the browser the address sent to sign in.
func TestAccessTicketsBelongToTheirBrowser(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	callback := f.signIn(t, "app.example.com", "/", verified("mallory@corp.example.com"))
	next, _ := url.Parse(callback.Header().Get("Location"))
	victim := f.visit("app.example.com", "/")
	for _, cookies := range [][]*http.Cookie{nil, {bindCookie(victim)}} {
		if w := f.visit("app.example.com", next.RequestURI(), cookies...); w.Code != http.StatusBadRequest || cookieNamed(w, accessCookie) != nil {
			t.Fatalf("ticket in another browser: %d", w.Code)
		}
	}
	// Without a binding the start refuses.
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/"); w.Code != http.StatusNotFound {
		t.Fatalf("start without a binding: %d", w.Code)
	}
}

// The sign-in start only serves protected addresses, on public addresses,
// and can't send anyone off the address.
func TestAccessStartStaysOnProtectedAddresses(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	for _, host := range []string{"other.example.com", "evil.example.org", "", f.adminHost} {
		if w := f.visit(f.adminHost, accessStartPath+"?host="+url.QueryEscape(host)+"&return=/&bind="+Hash("browser")); w.Code != http.StatusNotFound {
			t.Fatalf("start for %q: %d", host, w.Code)
		}
	}
	local := request(f.store.Handler(), "GET", accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser"), "", nil)
	if local.Code != http.StatusNotFound {
		t.Fatalf("start on the local port: %d", local.Code)
	}
	for _, back := range []string{"//evil.example.org/", "https://evil.example.org/", "/\\evil", "/x#frag", "/x\x00y", "/x\x7f", "/x\ny"} {
		if got := accessReturn(back); got != "/" {
			t.Fatalf("return %q kept as %q", back, got)
		}
	}
	// A binding that isn't a hash, which would name a cookie, is refused.
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+strings.Repeat("a;", 32)); w.Code != http.StatusNotFound {
		t.Fatalf("start with a bad binding: %d", w.Code)
	}
	// With the provider off, protected addresses stay closed.
	f.provider(t, false)
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("start with the provider off: %d", w.Code)
	}
	if w := f.visit("app.example.com", "/"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("protected address with the provider off: %d", w.Code)
	}
}

// provider turns access SSO on or off through the API, as the console does.
func (f accessFixture) provider(t *testing.T, enabled bool) {
	t.Helper()
	body := map[string]any{"enabled": enabled, "issuer": f.idp.server.URL, "client_id": "access", "scopes": []string{"openid", "email", "groups"}, "groups_claim": "groups", "session_hours": 8}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/access-provider", f.admin, body); w.Code != 200 {
		t.Fatalf("provider: %d %s", w.Code, w.Body.String())
	}
}

// The callback needs the state of the browser that started.
func TestAccessCallbackNeedsItsState(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	challenge := f.visit("app.example.com", "/")
	start, _ := url.Parse(challenge.Header().Get("Location"))
	begin := f.visit(f.adminHost, start.RequestURI())
	location, _ := url.Parse(begin.Header().Get("Location"))
	state := location.Query().Get("state")
	if w := f.visit(f.adminHost, accessCallbackPath+"?code=good-code&state="+url.QueryEscape(state)); w.Code != http.StatusBadRequest {
		t.Fatalf("without the state cookie: %d", w.Code)
	}
	if w := f.visit(f.adminHost, accessCallbackPath+"?code=good-code&state=other", cookieNamed(begin, accessStateCookie)); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown state: %d", w.Code)
	}
	// Another browser, with a state cookie of its own, can't use this
	// browser's sign-in.
	other := f.depart(t, "app.example.com", "/")
	if w := f.land(t, flight{host: "app.example.com", state: other.state, query: location.Query()}, verified("mallory@corp.example.com")); w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
		t.Fatalf("state in another browser: %d", w.Code)
	}
}

// Logging out drops the session; the Outlet's own paths pass the default
// block rules, which refuse other paths starting with "/.".
func TestAccessLogoutAndOwnPaths(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	w := f.visit("app.example.com", accessPathPrefix+"logout")
	if w.Code != 200 || cookieNamed(w, accessCookie) != nil || !strings.Contains(w.Body.String(), `<form method="post" action="/.meerkat/access/logout">`) {
		t.Fatalf("logout: %d %s", w.Code, w.Body.String())
	}
	if w := f.visit("app.example.com", accessPathPrefix+"other"); w.Code != http.StatusNotFound || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unknown own path: %d %v", w.Code, w.Header())
	}
	if w := f.visit("other.example.com", "/.env"); w.Code != http.StatusForbidden {
		t.Fatalf("block rules still apply: %d", w.Code)
	}
}

// Many requests with a session at once; run with -race.
func TestAccessConcurrentRequests(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}, AllowAdmins: true})
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	var wg sync.WaitGroup
	failures := make(chan int, 100)
	for range 100 {
		wg.Go(func() {
			if w := f.visit("app.example.com", "/", session); w.Code != 200 {
				failures <- w.Code
			}
		})
	}
	// The save runs alongside the requests and is waited for: a Fatal from
	// it must not come after the test ends.
	saved := make(chan error, 1)
	go func() {
		policy := control.AccessPolicy{Name: "staff", Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}}
		_, err := f.db.SetAccessPolicies(context.Background(), control.AccessPolicies{Policies: []control.AccessPolicy{policy}}, time.Now())
		if err == nil {
			err = f.store.reloadAccess(context.Background())
		}
		saved <- err
	}()
	wg.Wait()
	if err := <-saved; err != nil {
		t.Fatalf("save: %v", err)
	}
	close(failures)
	for code := range failures {
		t.Fatalf("request failed: %d", code)
	}
}

// The settings API is for administrators, never shows the secret, and
// audits changes.
func TestAccessSettingsAPI(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	h := f.store.Handler()
	for _, path := range []string{"/api/v1/security/access-provider", "/api/v1/security/access-policies"} {
		if w := request(h, "GET", path, "", nil); w.Code != 401 {
			t.Fatalf("%s without a session: %d", path, w.Code)
		}
	}
	w := request(h, "GET", "/api/v1/security/access-provider", f.admin, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "access-secret-1234") || !strings.Contains(w.Body.String(), `"client_secret_hint":"1234"`) || !strings.Contains(w.Body.String(), `"redirect_uri":"https://`+f.adminHost+accessCallbackPath+`"`) {
		t.Fatalf("provider view: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "PUT", "/api/v1/security/access-provider", f.admin, map[string]any{"enabled": true, "issuer": "http://x", "client_id": "a", "scopes": []string{}, "groups_claim": "", "session_hours": 8}); w.Code != 400 {
		t.Fatalf("bad provider: %d", w.Code)
	}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": []map[string]any{{"name": "x", "enabled": true, "hosts": []string{"app.example.com"}, "all_paths": true}}}); w.Code != 400 {
		t.Fatalf("policy without anyone allowed: %d", w.Code)
	}
	w = request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": []map[string]any{{"name": "Staff", "enabled": true, "hosts": []string{"other.example.com"}, "all_paths": true, "allowed_users": []string{"@corp.example.com"}}}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"addresses":["app.example.com","other.example.com"]`) {
		t.Fatalf("policies: %d %s", w.Code, w.Body.String())
	}
	// Applied at once: app is open now, other is protected.
	if w := f.visit("app.example.com", "/"); w.Code != 200 {
		t.Fatalf("app after the change: %d", w.Code)
	}
	if w := f.visit("other.example.com", "/"); w.Code != http.StatusFound {
		t.Fatalf("other after the change: %d", w.Code)
	}
	if event := f.lastAuditAction(t); event != "security.access_policies" {
		t.Fatalf("audit: %s", event)
	}
}

// With access SSO off, protected addresses let nobody in, sessions from
// before included, and a ticket issued before can't start one.
func TestAccessClosedWhileProviderOff(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	callback := f.signIn(t, "app.example.com", "/", verified("bob@corp.example.com"))
	f.provider(t, false)
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "email=") {
		t.Fatalf("session with the provider off: %d %s", w.Code, w.Body.String())
	}
	next, _ := url.Parse(callback.Header().Get("Location"))
	if w := f.visit("app.example.com", next.RequestURI(), f.binds["app.example.com"]); w.Code != http.StatusServiceUnavailable || cookieNamed(w, accessCookie) != nil {
		t.Fatalf("ticket with the provider off: %d", w.Code)
	}
	if w := f.visit("other.example.com", "/"); w.Code != 200 {
		t.Fatalf("unprotected address: %d", w.Code)
	}
	f.provider(t, true)
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("session with the provider back on: %d", w.Code)
	}
}

// Saves that cross leave the saved policies in force; run with -race.
func TestAccessPolicySavesLeaveTheSavedOneInForce(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			body := map[string]any{"policies": []map[string]any{{"name": fmt.Sprint("policy ", i), "enabled": true, "hosts": []string{"app.example.com"}, "all_paths": true, "allowed_users": []string{"@corp.example.com"}}}}
			if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, body); w.Code != 200 {
				t.Errorf("save %d: %d %s", i, w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	saved, err := f.db.AccessPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rules := f.store.access.config.Load().rules; len(rules) != 1 || rules[0].policy.Name != saved.Policies[0].Name {
		t.Fatalf("in force %+v, saved %+v", rules, saved.Policies)
	}
}

// A condition protects the path it names as requests are compared, a
// pattern holds with or without the trailing slash, and a path an app may
// read differently counts as every path.
func TestAccessPathsAppsMayReadDifferently(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{
		{Operator: control.BlockEquals, Value: "/secret."},
		{Operator: control.BlockStartsWith, Value: "/private./"},
		{Operator: control.BlockMatches, Value: "^/(admin|reports)/"},
	}, AllowedUsers: []string{"@corp.example.com"}})
	for _, path := range []string{
		"/secret", "/secret.", "/private/x", "/private./x", "/admin", "/admin/", "/ADMIN", "/admin.", "/reports", "/admin/users",
		// Decoded again, overlong, or dots and spaces servers read their own ways.
		"/%2561dmin/x", "/public/..%20/admin/x", "/admin%c0%afx", "/a/.../b", "/a/%20/b", "/a/.%20/b",
		// Dot segments that came escaped or next to a backslash, which many apps keep whole.
		"/admin/%2e%2e/public", "/admin/a%2f..%2f..%2fpublic", "/admin/.%2E/x", "/admin%5c..%5cpublic", "/%252e%252e/admin/x",
		// A NUL, which ends the path for some, also once decoded again, as
		// are bytes that aren't UTF-8.
		"/public%00/../admin/x", "/public/%2500/../admin", "/files/100%25ab.pdf",
	} {
		if w := f.visit("app.example.com", path); w.Code != http.StatusFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/public/page", "/administrator", "/secrets", "/100%25", "/%EA%B2%8C%EC%8B%9C%ED%8C%90/1", "/a..b/c", "/a/../public", "/admin/../public",
		// Decoded once more, these name nothing protected.
		"/x%252fy", "/data/a%253Fb%253Dc"} {
		if w := f.visit("app.example.com", path); w.Code != 200 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

// The Outlet's cookies never reach the app, however the name is spaced or
// cased.
func TestAccessCookiesNeverReachTheApp(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	for _, name := range []string{accessCookie + " ", accessCookie + "\t", " " + accessCookie} {
		r := httptest.NewRequest("GET", "https://app.example.com/", nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.30")
		r.Header.Set("Cookie", "app=kept; "+name+"="+session.Value+"; "+strings.ToUpper(accessBindCookie)+"_x=bind; "+strings.ToLower(accessCookie)+"=other")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, "email=alice@corp.example.com") || !strings.Contains(body, "cookie=app=kept path") {
			t.Fatalf("%q: %d %s", name, w.Code, body)
		}
	}
	header := http.Header{"Cookie": {"a=1; " + accessCookie + " =x", accessBindCookie + "_1234=y; b=2"}}
	if stripAccessCookie(header); strings.Join(header.Values("Cookie"), "|") != "a=1|b=2" {
		t.Fatalf("stripped: %q", header.Values("Cookie"))
	}
}

// A session carries the groups its address's policies name and, when they
// don't fit in a cookie, one that each policy allows, so the person still
// gets in.
func TestAccessSessionGroupsFitTheAddress(t *testing.T) {
	var many []string
	for i := range 30 {
		many = append(many, fmt.Sprintf("team-%02d-%s", i, strings.Repeat("x", 150)))
	}
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedGroups: many},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/finance/"}}, AllowedGroups: []string{"finance"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"other.example.com"}, AllPaths: true, AllowedGroups: []string{"elsewhere"}},
	)
	groups := append(append([]string{}, many...), "finance", "elsewhere")
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("carol@corp.example.com", "groups", groups)))
	for _, path := range []string{"/", "/finance/q3"} {
		if w := f.visit("app.example.com", path, session); w.Code != 200 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	payload, _, _ := strings.Cut(session.Value, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	if len(session.Value) > maxAccessCookie || strings.Contains(string(raw), "elsewhere") || strings.Count(string(raw), "team-") != 1 {
		t.Fatalf("cookie of %d bytes carries %s", len(session.Value), raw)
	}
	// Few enough groups all ride along.
	session, _ = f.finish(t, f.signIn(t, "app.example.com", "/", verified("dan@corp.example.com", "groups", []string{many[0], many[1], "finance"})))
	payload, _, _ = strings.Cut(session.Value, ".")
	if raw, _ = base64.RawURLEncoding.DecodeString(payload); strings.Count(string(raw), "team-") != 2 {
		t.Fatalf("cookie carries %s", raw)
	}
}

// One client address can begin only so many sign-ins a minute.
func TestAccessStartsAreLimited(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	for i := range accessStarts + 1 {
		w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash(fmt.Sprint("browser ", i)))
		if want := http.StatusFound; i == accessStarts {
			if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
				t.Fatalf("start %d: %d", i, w.Code)
			}
		} else if w.Code != want {
			t.Fatalf("start %d: %d", i, w.Code)
		}
	}
}

// Sign-ins started at once in several tabs of one browser don't undo each
// other, whichever comes back first. The browser keeps the newest cookie of
// each name, per host.
func TestAccessSignInsInFlightTogether(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com", "other.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	jar := map[string]map[string]*http.Cookie{}
	keep := func(host string, cookies ...*http.Cookie) {
		if jar[host] == nil {
			jar[host] = map[string]*http.Cookie{}
		}
		for _, cookie := range cookies {
			if cookie.MaxAge < 0 {
				delete(jar[host], cookie.Name)
			} else {
				jar[host][cookie.Name] = cookie
			}
		}
	}
	held := func(host string) []*http.Cookie {
		var out []*http.Cookie
		for _, cookie := range jar[host] {
			out = append(out, cookie)
		}
		return out
	}
	var flights []flight
	for i := range 6 {
		host := []string{"app.example.com", "other.example.com"}[i%2]
		fl := f.departWith(t, host, fmt.Sprint("/tab/", i), held(host), held(f.adminHost))
		keep(fl.host, fl.bind)
		keep(f.adminHost, fl.state)
		flights = append(flights, fl)
	}
	for i := len(flights) - 1; i >= 0; i-- {
		fl := flights[i]
		f.idp.mu.Lock()
		f.idp.challenge, f.idp.claims = fl.query.Get("code_challenge"), map[string]any{"iss": f.idp.server.URL, "aud": "access", "sub": "alice", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": fl.query.Get("nonce"), "email": "alice@corp.example.com", "email_verified": true}
		f.idp.mu.Unlock()
		callback := f.visit(f.adminHost, accessCallbackPath+"?code=good-code&state="+url.QueryEscape(fl.query.Get("state")), held(f.adminHost)...)
		keep(f.adminHost, callback.Result().Cookies()...)
		next, _ := url.Parse(callback.Header().Get("Location"))
		if callback.Code != http.StatusSeeOther || next.Host != fl.host {
			t.Fatalf("tab %d callback: %d %s", i, callback.Code, callback.Body.String())
		}
		done := f.visit(fl.host, next.RequestURI(), held(fl.host)...)
		keep(fl.host, done.Result().Cookies()...)
		if done.Code != http.StatusSeeOther || done.Header().Get("Location") != fmt.Sprint("/tab/", i) || cookieNamed(done, accessCookie) == nil {
			t.Fatalf("tab %d finish: %d %s", i, done.Code, done.Body.String())
		}
	}
	// The browser holds one state cookie and one binding per address,
	// however many sign-ins it made.
	if len(jar[f.adminHost]) != 1 || jar[f.adminHost][accessStateCookie] == nil {
		t.Fatalf("management host keeps %v", jar[f.adminHost])
	}
	for _, host := range []string{"app.example.com", "other.example.com"} {
		if len(jar[host]) != 2 || jar[host][accessCookie] == nil || jar[host][accessBindCookie] == nil {
			t.Fatalf("%s keeps %v", host, jar[host])
		}
	}
}

// Saved policies that no longer pass the checks keep closed what they may
// have protected, without keeping the Outlet down, and show in the console
// to be fixed.
func TestAccessSavedPoliciesThatFailTheChecksStayClosed(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	store := func(raw string) {
		t.Helper()
		if _, err := f.db.SQL().ExecContext(ctx, "UPDATE outlet_settings SET value = ? WHERE key = 'access_policies'", raw); err != nil {
			t.Fatal(err)
		}
		if err := f.store.reloadAccess(ctx); err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	store(`{"policies":[{"id":"old","name":"Old","enabled":true,"hosts":["App.Example.com"],"conditions":[{"operator":"matches","value":"("}],"allowed_users":["@Corp.example.com"]}]}`)
	if w := f.visit("app.example.com", "/anything"); w.Code != http.StatusFound {
		t.Fatalf("named address: %d", w.Code)
	}
	if w := f.visit("other.example.com", "/"); w.Code != 200 {
		t.Fatalf("other address: %d", w.Code)
	}
	// Whom it names still gets in.
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("allowed person: %d", w.Code)
	}
	if w := request(f.store.Handler(), "GET", "/api/v1/security/access-policies", f.admin, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Old"`) || !strings.Contains(w.Body.String(), `"damaged":true`) {
		t.Fatalf("console: %d %s", w.Code, w.Body.String())
	}
	// A damaged bypass policy saved above a damaged allow policy lets nobody
	// in, but doesn't shut out whom the allow policy names.
	store(`{"policies":[{"id":"open","name":"Open","enabled":true,"action":"bypass","hosts":["app.example.com"],"all_paths":true},` +
		`{"id":"old","name":"Old","enabled":true,"hosts":["app.example.com"],"conditions":[{"operator":"matches","value":"("}],"allowed_users":["@Corp.example.com"]}]}`)
	if w := f.visit("app.example.com", "/"); w.Code != http.StatusFound {
		t.Fatalf("damaged bypass: %d", w.Code)
	}
	session, _ = f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("named person behind a damaged bypass: %d", w.Code)
	}
	// Damage elsewhere keeps the saved order: a block above an allow policy
	// still shuts those it names out.
	store(`{"policies":[{"id":"shut","name":"Shut","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":true},` +
		`{"id":"staff","name":"Staff","enabled":true,"hosts":["app.example.com"],"all_paths":true,"allowed_users":["@corp.example.com"]},` +
		`{"id":"bad","name":"Bad","enabled":true,"hosts":["other.example.com"],"conditions":[{"operator":"matches","value":"("}],"allowed_users":["@corp.example.com"]}]}`)
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusForbidden {
		t.Fatalf("block above an allow policy, damaged elsewhere: %d", w.Code)
	}
	// Damaged, an allow policy that also required a token lets nobody in:
	// without its token, it can't tell whom it let in.
	store(`{"policies":[{"id":"ci","name":"CI","enabled":true,"hosts":["app.example.com"],"all_paths":true,"require_token":true,"tokens":["nosuchtoken"],"allowed_users":["@corp.example.com"]},` +
		`{"id":"shut","name":"Shut","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":true},` +
		`{"id":"bad","name":"Bad","enabled":true,"hosts":["other.example.com"],"conditions":[{"operator":"matches","value":"("}],"allowed_users":["@corp.example.com"]}]}`)
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusForbidden {
		t.Fatalf("damaged allow policy that required a token: %d", w.Code)
	}
	// Damaged, a block limited to some methods shuts every method.
	store(`{"policies":[{"id":"shut","name":"Shut","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":true,"methods":["DELETE","TRACE"]},` +
		`{"id":"staff","name":"Staff","enabled":true,"hosts":["app.example.com"],"all_paths":true,"allowed_users":["@corp.example.com"]}]}`)
	if w := f.visit("app.example.com", "/", session); w.Code != http.StatusForbidden {
		t.Fatalf("damaged block for some methods: %d", w.Code)
	}
	for _, raw := range []string{`not json`, `null`, `{}`, `{"policies":null}`, `{"policies":[null]}`, `{"policies":[{"name":"No hosts","enabled":true,"all_paths":true}]}`} {
		store(raw)
		if w := f.visit("other.example.com", "/"); w.Code != http.StatusFound {
			t.Fatalf("%s: every address: %d", raw, w.Code)
		}
		if w := request(f.store.Handler(), "GET", "/api/v1/security/access-policies", f.admin, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"damaged":true`) || strings.Contains(w.Body.String(), "null") {
			t.Fatalf("%s: console: %d %s", raw, w.Code, w.Body.String())
		}
	}
	f.setPolicies(t)
	if w := f.visit("other.example.com", "/"); w.Code != 200 {
		t.Fatalf("after saving again: %d", w.Code)
	}
	if w := request(f.store.Handler(), "GET", "/api/v1/security/access-policies", f.admin, nil); strings.Contains(w.Body.String(), "damaged") {
		t.Fatalf("console after saving again: %s", w.Body.String())
	}
}

// What the console shows of damaged policies can be saved back as it is
// once the damage is fixed: every list there, valid IDs, none twice.
func TestAccessDamagedPoliciesSaveBack(t *testing.T) {
	f := newAccessFixture(t)
	ctx := context.Background()
	raw := `{"policies":[{"id":"bad id!","name":"One","enabled":true,"hosts":["app.example.com"],"all_paths":true,"allowed_users":["@corp.example.com"],"allowed_groups":null},` +
		`{"id":"same","name":"Two","enabled":false,"hosts":["other.example.com"],"all_paths":true,"allow_admins":true},{"id":"same","name":"Three","enabled":false,"hosts":null,"all_paths":true,"allow_admins":true}]}`
	if _, err := f.db.SQL().ExecContext(ctx, "UPDATE outlet_settings SET value = ? WHERE key = 'access_policies'", raw); err != nil {
		t.Fatal(err)
	}
	h := f.store.Handler()
	var view accessPoliciesView
	if w := request(h, "GET", "/api/v1/security/access-policies", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || !view.Damaged || len(view.Policies) != 3 {
		t.Fatalf("console: %d %s", w.Code, w.Body.String())
	}
	ids := map[string]bool{}
	for _, policy := range view.Policies {
		if ids[policy.ID] || policy.ID == "bad id!" || policy.Hosts == nil || policy.AllowedGroups == nil || policy.AllowedUsers == nil || policy.Conditions == nil {
			t.Fatalf("policy %+v", policy)
		}
		ids[policy.ID] = true
	}
	view.Policies[2].Hosts = []string{"app.example.com"}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": view.Policies}); w.Code != 200 || strings.Contains(w.Body.String(), "damaged") {
		t.Fatalf("save back: %d %s", w.Code, w.Body.String())
	}
}

// Many people behind one address can sign in at once; a client is held to
// its share with 429 and a link back, and its sign-ins in flight are never
// replaced by newer ones; its failures fill the audit log only so fast.
func TestAccessOfficeBehindOneAddress(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	codes := map[int]int{}
	for i := range accessStarts + 5 {
		w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=%2Fdocs&bind="+Hash(fmt.Sprint("browser ", i)))
		codes[w.Code]++
		if w.Code == http.StatusTooManyRequests && !strings.Contains(w.Body.String(), `<a href="https://app.example.com/docs">`) {
			t.Fatalf("busy page: %s", w.Body.String())
		}
	}
	if codes[http.StatusFound] != accessStarts || codes[http.StatusTooManyRequests] != 5 {
		t.Fatalf("starts: %v", codes)
	}
	var st ssoState
	st.perClient, st.keep = 2, true
	now := time.Now()
	for i, want := range []bool{true, true, false} {
		if got := st.put(fmt.Sprint("state ", i), ssoPending{client: "office", expires: now.Add(time.Minute)}, now); got != want {
			t.Fatalf("sign-in %d admitted %v", i, got)
		}
	}
	if _, ok := st.take("state 0", now); !ok {
		t.Fatal("the first sign-in was replaced")
	}
	if !st.put("elsewhere", ssoPending{client: "home", expires: now.Add(time.Minute)}, now) {
		t.Fatal("another client was refused")
	}
}

// One client address gets only so many failed sign-ins a minute into the
// audit log.
func TestAccessFailuresAuditedOnlySoFast(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	for range loginAttempts + 5 {
		fl := f.depart(t, "app.example.com", "/")
		if w := f.visit(f.adminHost, accessCallbackPath+"?error=access_denied&state="+url.QueryEscape(fl.query.Get("state")), fl.state); w.Code != http.StatusForbidden {
			t.Fatalf("callback: %d", w.Code)
		}
	}
	f.store.journal.flush()
	if _, total, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1, Action: "access.login_failed"}); err != nil || total != loginAttempts {
		t.Fatalf("audited %d failures, %v", total, err)
	}
}

// Groups that don't fit in a cookie are packed by how many policies each
// satisfies, so one shared group can stand for many.
func TestAccessFewestGroups(t *testing.T) {
	var rules []*accessRule
	var groups []string
	for i := range 24 {
		long := fmt.Sprintf("team-%02d-%s", i, strings.Repeat("x", 200))
		rules = append(rules, &accessRule{policy: control.AccessPolicy{AllowedGroups: []string{long, "shared"}}})
		groups = append(groups, long)
	}
	rules = append(rules, &accessRule{policy: control.AccessPolicy{AllowedGroups: []string{"finance"}}}, &accessRule{policy: control.AccessPolicy{AllowedUsers: []string{"@corp.example.com"}}})
	groups = append(groups, "shared", "finance", "unused")
	if got := fewestGroups(rules, groups); fmt.Sprint(got) != "[shared finance]" {
		t.Fatalf("kept %v", got)
	}
	// A group taken first that later ones make unneeded is dropped: "a"
	// satisfies the most at first, but "b" and "c" are needed anyway and
	// satisfy all of its policies too.
	rules = []*accessRule{
		{policy: control.AccessPolicy{AllowedGroups: []string{"a", "b"}}},
		{policy: control.AccessPolicy{AllowedGroups: []string{"a", "b"}}},
		{policy: control.AccessPolicy{AllowedGroups: []string{"a", "c"}}},
		{policy: control.AccessPolicy{AllowedGroups: []string{"b"}}},
		{policy: control.AccessPolicy{AllowedGroups: []string{"c"}}},
	}
	if got := fewestGroups(rules, []string{"a", "b", "c"}); fmt.Sprint(got) != "[b c]" {
		t.Fatalf("kept %v", got)
	}
}

// Packing groups by the rules each satisfies keeps what weighing every
// group against every rule kept, ties included.
func TestAccessFewestGroupsAsBefore(t *testing.T) {
	// slowly is how groups were packed before.
	slowly := func(rules []*accessRule, groups []string) []string {
		var sets []map[string]bool
		for _, rule := range rules {
			set := map[string]bool{}
			for _, group := range rule.policy.AllowedGroups {
				set[group] = true
			}
			if slices.ContainsFunc(groups, func(group string) bool { return set[group] }) {
				sets = append(sets, set)
			}
		}
		var kept []string
		for open := slices.Clone(sets); len(open) > 0; {
			best, most := "", 0
			for _, group := range groups {
				n := 0
				for _, set := range open {
					if set[group] {
						n++
					}
				}
				if n > most || n == most && n > 0 && len(group) < len(best) {
					best, most = group, n
				}
			}
			kept = append(kept, best)
			open = slices.DeleteFunc(open, func(set map[string]bool) bool { return set[best] })
		}
		for i := len(kept) - 1; i >= 0; i-- {
			without := slices.Delete(slices.Clone(kept), i, i+1)
			if !slices.ContainsFunc(sets, func(set map[string]bool) bool {
				return !slices.ContainsFunc(without, func(group string) bool { return set[group] })
			}) {
				kept = without
			}
		}
		return kept
	}
	random := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		name := func() string { return strings.Repeat("g", 1+random.IntN(3)) + fmt.Sprint(random.IntN(12)) }
		var rules []*accessRule
		for range random.IntN(140) {
			var named []string
			for range random.IntN(5) {
				named = append(named, name())
			}
			rules = append(rules, &accessRule{policy: control.AccessPolicy{AllowedGroups: named}})
		}
		var groups []string
		for range random.IntN(16) {
			groups = append(groups, name())
		}
		if got, want := fewestGroups(rules, groups), slowly(rules, groups); !slices.Equal(got, want) && len(got)+len(want) > 0 {
			t.Fatalf("groups %q: kept %q, before %q", groups, got, want)
		}
	}
}

// An app can't set the Outlet's cookies on its address.
func TestAccessAppCantSetOutletCookies(t *testing.T) {
	header := http.Header{"Set-Cookie": {"app=1; Path=/", "__Host-meerkat_access=junk; Path=/; Secure", " __HOST-MEERKAT_ACCESS_BIND =x; Path=/", "other=2"}}
	if dropAccessSetCookie(header); strings.Join(header.Values("Set-Cookie"), "|") != "app=1; Path=/|other=2" {
		t.Fatalf("kept %q", header.Values("Set-Cookie"))
	}
}

// Saving a setting the Outlet keeps in memory lets go of Store.saving before
// answering, so a slow client can't hold up other saves; changing the
// primary domain renames the management host's rate limits under it.
func TestSettingSavesReleaseTheLockBeforeAnswering(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	current := func(path string, keys ...string) map[string]any {
		var all map[string]any
		_ = json.Unmarshal(request(h, "GET", path, f.admin, nil).Body.Bytes(), &all)
		body := map[string]any{}
		for _, key := range keys {
			body[key] = all[key]
		}
		return body
	}
	for path, body := range map[string]any{
		"/api/v1/security/rate-limit":        current("/api/v1/security/rate-limit", "enabled", "per_ip", "per_subnet", "per_address", "endpoints"),
		"/api/v1/security/connection-limits": current("/api/v1/security/connection-limits", "max_upload_mb", "per_ip_concurrent", "total_concurrent", "idle_timeout_seconds", "min_upload_kb_per_second", "hosts"),
		"/api/v1/security/tunnel-guard":      map[string]any{"mode": "limited", "allowed": []string{}},
		"/api/v1/security/block-rules":       map[string]any{"rules": []any{}},
		"/api/v1/security/client-bans":       map[string]any{"minutes": 60, "exempt": []string{}},
		"/api/v1/security/access-policies":   map[string]any{"policies": []any{}},
		"/api/v1/security/access-provider":   map[string]any{"enabled": true, "issuer": f.idp.server.URL, "client_id": "access", "scopes": []string{"openid"}, "groups_claim": "groups", "session_hours": 8},
	} {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest("PUT", path, strings.NewReader(string(raw)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.admin)
		w := &stalledWriter{ResponseRecorder: httptest.NewRecorder(), writing: make(chan struct{}), release: make(chan struct{})}
		done := make(chan struct{})
		go func() { h.ServeHTTP(w, r); close(done) }()
		select {
		case <-w.writing:
		case <-done:
			t.Fatalf("%s answered without writing: %d %s", path, w.Code, w.Body.String())
		}
		locked := !f.store.saving.TryLock()
		if !locked {
			f.store.saving.Unlock()
		}
		close(w.release)
		<-done
		if locked || w.Code != 200 {
			t.Fatalf("%s: lock held while answering %v, %d %s", path, locked, w.Code, w.Body.String())
		}
	}
	// The domain change waits for a save in progress.
	f.store.saving.Lock()
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"}) }()
	select {
	case w := <-done:
		f.store.saving.Unlock()
		t.Fatalf("domain change didn't wait: %d %s", w.Code, w.Body.String())
	case <-time.After(200 * time.Millisecond):
	}
	f.store.saving.Unlock()
	if w := <-done; w.Code != 200 {
		t.Fatalf("domain change: %d %s", w.Code, w.Body.String())
	}
}

// stalledWriter holds a response's body until released.
type stalledWriter struct {
	*httptest.ResponseRecorder
	once             sync.Once
	writing, release chan struct{}
}

func (w *stalledWriter) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.writing) })
	<-w.release
	return w.ResponseRecorder.Write(b)
}

// The pages a protected address shows are never stored; only a POST from
// the address itself signs out, and a denied person can sign in again, as
// someone with groups a policy named after they signed in must.
func TestAccessPagesAndSignOut(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("carol@corp.example.com", "groups", []string{"finance"})))
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/finance/"}}, AllowedGroups: []string{"finance"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	denied := f.visit("app.example.com", "/finance/q3?y=1", session)
	again := `<form method="post" action="/.meerkat/access/logout?return=%2Ffinance%2Fq3%3Fy%3D1">`
	if denied.Code != http.StatusForbidden || denied.Header().Get("Cache-Control") != "no-store" || !strings.Contains(denied.Body.String(), again) {
		t.Fatalf("denied: %d %v %s", denied.Code, denied.Header(), denied.Body.String())
	}
	send := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://app.example.com"+path, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.30")
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		r.AddCookie(session)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for name, c := range map[string]struct {
		method  string
		headers map[string]string
	}{
		"a link":                      {"GET", nil},
		"another site":                {"POST", map[string]string{"Sec-Fetch-Site": "cross-site"}},
		"a sibling address":           {"POST", map[string]string{"Sec-Fetch-Site": "same-site"}},
		"an older browser, elsewhere": {"POST", map[string]string{"Origin": "https://evil.example.org"}},
		"an older browser, sibling":   {"POST", map[string]string{"Origin": "https://other.example.com"}},
	} {
		if w := send(c.method, accessPathPrefix+"logout", c.headers); w.Code != 200 || cookieNamed(w, accessCookie) != nil || !strings.Contains(w.Body.String(), `<form method="post"`) {
			t.Fatalf("sign-out from %s: %d %s", name, w.Code, w.Body.String())
		}
	}
	w := send("POST", accessPathPrefix+"logout", map[string]string{"Sec-Fetch-Site": "same-origin"})
	if cookie := cookieNamed(w, accessCookie); w.Code != 200 || cookie == nil || cookie.MaxAge >= 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("sign-out: %d %+v %v", w.Code, cookie, w.Header())
	}
	// The denied page's button signs out and goes back, to sign in again.
	back := send("POST", "/.meerkat/access/logout?return=%2Ffinance%2Fq3%3Fy%3D1", map[string]string{"Sec-Fetch-Site": "same-origin"})
	if cookie := cookieNamed(back, accessCookie); back.Code != http.StatusSeeOther || back.Header().Get("Location") != "/finance/q3?y=1" || cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("sign in again: %d %s", back.Code, back.Header().Get("Location"))
	}
	session, _ = f.finish(t, f.signIn(t, "app.example.com", "/finance/q3", verified("carol@corp.example.com", "groups", []string{"finance"})))
	if w := f.visit("app.example.com", "/finance/q3", session); w.Code != 200 {
		t.Fatalf("after signing in again: %d", w.Code)
	}
}

// Administrators pass a policy that lets them in, by their email; the
// database is asked only when the session alone doesn't settle it.
func TestAccessAdminsByEmail(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}, AllowAdmins: true})
	if _, err := f.db.SSOAdmin(context.Background(), "https://idp.example.com", "admin-subject", "Boss@Example.com"); err != nil {
		t.Fatal(err)
	}
	admin, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("boss@example.com")))
	staff, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	f.db.Close()
	if w := f.visit("app.example.com", "/", staff); w.Code != 200 {
		t.Fatalf("staff without the database: %d", w.Code)
	}
	// An administrator looked up lately gets in without asking again; once
	// that answer is stale, a lookup that fails lets nobody in.
	if w := f.visit("app.example.com", "/", admin); w.Code != 200 {
		t.Fatalf("administrator looked up lately: %d", w.Code)
	}
	f.store.now = func() time.Time { return time.Now().Add(adminEmailTTL) }
	if w := f.visit("app.example.com", "/", admin); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("administrator without the database: %d", w.Code)
	}
}

// The audit log says who could get in after a change, and as whom a
// person signed in.
func TestAccessAuditDetail(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}, AllowedGroups: []string{"staff"}, AllowAdmins: true})
	f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com", "groups", []string{"staff"})))
	var login map[string]any
	if event := f.lastAudit(t); event.Action != "access.login" || json.Unmarshal(event.Detail, &login) != nil || login["subject"] != "subject-alice@corp.example.com" || login["issuer"] != f.idp.server.URL {
		t.Fatalf("login: %s %s", event.Action, event.Detail)
	}
	body := map[string]any{"policies": []map[string]any{{"name": "Staff", "enabled": true, "hosts": []string{"app.example.com"}, "all_paths": true, "allowed_users": []string{"@corp.example.com"}, "allowed_groups": []string{"staff"}, "allow_admins": true}}}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/access-policies", f.admin, body); w.Code != 200 {
		t.Fatalf("save: %d", w.Code)
	}
	if event := f.lastAudit(t); event.Action != "security.access_policies" || string(event.Detail) != `{"policies":[{"name":"Staff","enabled":true,"action":"allow","hosts":["app.example.com"],"allowed_users":["@corp.example.com"],"allowed_groups":["staff"],"allow_admins":true}]}` {
		t.Fatalf("policies: %s %s", event.Action, event.Detail)
	}
}

func (f accessFixture) lastAudit(t *testing.T) control.AuditEvent {
	t.Helper()
	f.store.journal.flush()
	events, _, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1})
	if err != nil || len(events) == 0 {
		t.Fatalf("audit: %+v %v", events, err)
	}
	return events[0]
}

// An access token lets a client that can't sign in through the allow
// policies that name it, without a provider; the app gets the token's name
// and never the token. A wrong, unknown or expired token, or one the
// policy doesn't name, gets nowhere.
func TestAccessTokens(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	create := func(name string, days int) (accessTokenView, string) {
		t.Helper()
		var created struct {
			Token accessTokenView `json:"token"`
			Value string          `json:"value"`
		}
		w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": name, "hosts": []string{"app.example.com", "other.example.com"}, "ranges": []string{}, "expires_days": days})
		if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Value == "" {
			t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
		}
		return created.Token, created.Value
	}
	deploy, deployValue := create("Deploy bot", 0)
	monitor, monitorValue := create("Monitor", 30)
	if monitor.ExpiresAt == "" || deploy.ExpiresAt != "" {
		t.Fatalf("expiry: %+v %+v", deploy, monitor)
	}
	if w := request(h, "GET", "/api/v1/security/access-tokens", f.admin, nil); w.Code != 200 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), deployValue[strings.Index(deployValue, ".")+1:]) || !strings.Contains(w.Body.String(), `"name":"Monitor"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []map[string]any{
		{"name": " ", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0},
		{"name": "x", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": -1},
		{"name": "x", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 3651},
		{"name": strings.Repeat("x", 65), "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0},
		{"name": "x", "hosts": []string{}, "ranges": []string{}, "expires_days": 0},
		{"name": "x", "hosts": []string{"app.example.com"}, "ranges": []string{"not a range"}, "expires_days": 0},
	} {
		if w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, body); w.Code != 400 {
			t.Fatalf("create %v: %d", body, w.Code)
		}
	}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, map[string]any{"policies": []map[string]any{{"name": "x", "enabled": true, "hosts": []string{"app.example.com"}, "all_paths": true, "action": "bypass", "require_token": true, "tokens": []string{"nosuchtoken"}}}}); w.Code != 400 {
		t.Fatalf("policy naming an unknown token: %d", w.Code)
	}
	deployPath := []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/deploy/"}}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: deployPath, RequireToken: true, Tokens: []string{deploy.ID}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: deployPath, AllowedUsers: []string{"@corp.example.com"}},
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true, Tokens: []string{monitor.ID}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	send := func(host, path string, headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://"+host+path, nil)
		r.Host = host
		r.Header.Set("X-Forwarded-For", "198.51.100.30")
		for key, value := range headers {
			r.Header.Set(key, value)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	token := func(value string) map[string]string {
		return map[string]string{accessTokenHeader: value, accessServiceHeader: "forged", accessEmailHeader: "boss@corp.example.com"}
	}
	// The app sees the token's name, and neither the token nor forged
	// identity headers.
	if w := send("app.example.com", "/deploy/run", token(deployValue)); w.Code != 200 || !strings.Contains(w.Body.String(), "email= ") || !strings.HasSuffix(w.Body.String(), "service=Deploy bot token= others=") || strings.Contains(w.Body.String(), "forged") || strings.Contains(w.Body.String(), deployValue) {
		t.Fatalf("deploy token: %d %s", w.Code, w.Body.String())
	}
	if w := send("app.example.com", "/", token(monitorValue)); w.Code != 200 {
		t.Fatalf("monitor token: %d", w.Code)
	}
	// The policy that decides /deploy/ for it doesn't require the monitor
	// token.
	if w := send("app.example.com", "/deploy/run", token(monitorValue)); w.Code != http.StatusForbidden {
		t.Fatalf("token the policy doesn't name: %d", w.Code)
	}
	id, _, _ := strings.Cut(deployValue, ".")
	for _, value := range []string{"x", id, id + ".wrong", "nosuchtoken." + deployValue[len(id)+1:], deployValue + "x", deployValue + " x"} {
		if w := send("app.example.com", "/deploy/run", map[string]string{accessTokenHeader: value}); w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("token %q: %d", value, w.Code)
		}
	}
	// Spaces around the value don't matter; a refused token leaves the
	// request to a session it also carries, and is audited either way.
	if w := send("app.example.com", "/deploy/run", map[string]string{accessTokenHeader: " " + deployValue + " "}); w.Code != 200 {
		t.Fatalf("token with spaces: %d", w.Code)
	}
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	// The deploy token is valid but no policy for / requires it. Another client
	// sends these, so their audits leave this one's allowance alone.
	for value, reason := range map[string]string{"junk.secret": "invalid", deployValue: "not_allowed"} {
		r := httptest.NewRequest("GET", "https://app.example.com/", nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.31")
		r.Header.Set(accessTokenHeader, value)
		r.AddCookie(session)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "email=alice@corp.example.com") {
			t.Fatalf("session with token %q: %d %s", value, w.Code, w.Body.String())
		}
		if event := f.lastAudit(t); event.Action != "access.token_failed" || !strings.Contains(string(event.Detail), `"reason":"`+reason+`"`) {
			t.Fatalf("session with token %q, audit: %s %s", value, event.Action, event.Detail)
		}
	}
	if w := send("app.example.com", "/deploy/run", map[string]string{accessTokenHeader: "junk.secret"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("junk token: %d", w.Code)
	}
	if event := f.lastAudit(t); event.Action != "access.token_failed" || !strings.Contains(string(event.Detail), `"reason":"invalid"`) {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	if w := send("app.example.com", "/deploy/run", token(monitorValue)); w.Code != http.StatusForbidden {
		t.Fatalf("token the policy doesn't name: %d", w.Code)
	}
	if event := f.lastAudit(t); event.Action != "access.token_failed" || !strings.Contains(string(event.Detail), `"reason":"not_allowed"`) || !strings.Contains(string(event.Detail), monitor.ID) {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	// Tokens need no provider.
	f.provider(t, false)
	if w := send("app.example.com", "/deploy/run", token(deployValue)); w.Code != 200 {
		t.Fatalf("token with the provider off: %d", w.Code)
	}
	f.provider(t, true)
	// An expired token stops working.
	now := time.Now()
	f.store.now = func() time.Time { return now.AddDate(0, 0, 31) }
	if w := send("app.example.com", "/", token(monitorValue)); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: %d", w.Code)
	}
	if event := f.lastAudit(t); event.Action != "access.token_failed" || !strings.Contains(string(event.Detail), `"reason":"expired"`) || !strings.Contains(string(event.Detail), monitor.ID) {
		t.Fatalf("expired token audit: %s %s", event.Action, event.Detail)
	}
	f.store.now = time.Now
	// So has one whose saved expiry can't be read.
	var saved string
	if err := f.db.SQL().QueryRowContext(context.Background(), "SELECT value FROM outlet_settings WHERE key = 'access_tokens'").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(saved, monitor.ExpiresAt, "soon", 1), strings.Replace(saved, monitor.ExpiresAt, "0001-01-01T00:00:00Z", 1), saved} {
		if _, err := f.db.SQL().ExecContext(context.Background(), "UPDATE outlet_settings SET value = ? WHERE key = 'access_tokens'", raw); err != nil {
			t.Fatal(err)
		}
		if err := f.store.reloadAccessTokens(context.Background()); err != nil {
			t.Fatal(err)
		}
		if w := send("app.example.com", "/", token(monitorValue)); (raw == saved) != (w.Code == 200) {
			t.Fatalf("saved expiry %q: %d", raw, w.Code)
		}
	}
	// Two saved tokens with one ID can't be told apart, so neither counts:
	// not the deploy token, nor another saved under its ID that a value
	// matches.
	var tokens []control.AccessToken
	if err := json.Unmarshal([]byte(saved), &tokens); err != nil {
		t.Fatal(err)
	}
	secret := monitorValue[strings.Index(monitorValue, ".")+1:]
	twin := tokens[1]
	twin.ID, twin.Name, twin.SecretHash = deploy.ID, "Twin", control.AccessTokenHash(deploy.ID+"."+secret)
	raw, _ := json.Marshal(append(slices.Clone(tokens), twin))
	if _, err := f.db.SQL().ExecContext(context.Background(), "UPDATE outlet_settings SET value = ? WHERE key = 'access_tokens'", string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccessTokens(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{deployValue, deploy.ID + "." + secret} {
		if w := send("app.example.com", "/deploy/run", token(value)); w.Code == 200 {
			t.Fatalf("token sharing its ID: %d %s", w.Code, w.Body.String())
		}
	}
	if _, err := f.db.SQL().ExecContext(context.Background(), "UPDATE outlet_settings SET value = ? WHERE key = 'access_tokens'", saved); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccessTokens(context.Background()); err != nil {
		t.Fatal(err)
	}
	// On an address no policy covers, the headers still never reach the app.
	if w := send("other.example.com", "/", token(deployValue)); w.Code != 200 || strings.Contains(w.Body.String(), "forged") || !strings.HasSuffix(w.Body.String(), "service= token= others=") {
		t.Fatalf("unprotected address: %d %s", w.Code, w.Body.String())
	}
	// A token a policy names can't be deleted; once out of the policies it
	// can, and stops working at once.
	if w := request(h, "DELETE", "/api/v1/security/access-tokens/"+deploy.ID, f.admin, nil); w.Code != http.StatusConflict {
		t.Fatalf("delete a token in use: %d", w.Code)
	}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true, Tokens: []string{monitor.ID}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	if w := request(h, "DELETE", "/api/v1/security/access-tokens/"+deploy.ID, f.admin, nil); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	// (The expired-token check above logged a refusal dated a month ahead,
	// so look the deletion up by its action.)
	f.store.journal.flush()
	if events, _, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1, Action: "security.access_token_delete"}); err != nil || len(events) != 1 || !strings.Contains(string(events[0].Detail), "Deploy bot") {
		t.Fatalf("audit: %+v %v", events, err)
	}
	if w := request(h, "DELETE", "/api/v1/security/access-tokens/"+deploy.ID, f.admin, nil); w.Code != http.StatusNotFound {
		t.Fatalf("delete again: %d", w.Code)
	}
	if w := send("app.example.com", "/", token(deployValue)); w.Code != http.StatusUnauthorized {
		t.Fatalf("deleted token: %d", w.Code)
	}
	// Tokens survive a restart: they are loaded with the rest.
	if err := f.store.reloadAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w := send("app.example.com", "/", token(monitorValue)); w.Code != 200 {
		t.Fatalf("after reloading: %d", w.Code)
	}
	for _, path := range []string{"/api/v1/security/access-tokens"} {
		if w := request(h, "GET", path, "", nil); w.Code != 401 {
			t.Fatalf("%s without a session: %d", path, w.Code)
		}
	}
}

// A token works only on the addresses and from the client addresses it is
// limited to, whatever the policies allow; its scope can change without a
// new value. The console sees which policies allow it and when it was last
// used, across restarts.
func TestAccessTokenScope(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	var created struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{"198.51.100.0/24"}, "expires_days": 90})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"*.example.com"}, AllPaths: true, RequireToken: true, Tokens: []string{created.Token.ID}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"*.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	send := func(host, client string) int {
		r := httptest.NewRequest("GET", "https://"+host+"/", nil)
		r.Host = host
		r.Header.Set("X-Forwarded-For", client)
		r.Header.Set(accessTokenHeader, created.Value)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := send("app.example.com", "198.51.100.30"); code != 200 {
		t.Fatalf("in scope: %d", code)
	}
	if code := send("other.example.com", "198.51.100.30"); code != http.StatusForbidden {
		t.Fatalf("another address the policy covers: %d", code)
	}
	if code := send("app.example.com", "203.0.113.9"); code != http.StatusForbidden {
		t.Fatalf("from outside its ranges: %d", code)
	}
	var list accessTokensView
	if w := request(h, "GET", "/api/v1/security/access-tokens", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Tokens) != 1 ||
		list.Tokens[0].LastUsedAt == "" || len(list.Tokens[0].Policies) != 1 || list.Tokens[0].Policies[0].Name != "policy 0" || !slices.Contains(list.Addresses, "app.example.com") {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	// The scope changes in place; the value keeps working where it now may.
	w = request(h, "PUT", "/api/v1/security/access-tokens/"+created.Token.ID, f.admin, map[string]any{"name": "CI", "hosts": []string{"*.example.com"}, "ranges": []string{}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"hosts":["*.example.com"]`) {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if code := send("other.example.com", "203.0.113.9"); code != 200 {
		t.Fatalf("after widening: %d", code)
	}
	if event := f.lastAudit(t); event.Action != "security.access_token_update" || !strings.Contains(string(event.Detail), "*.example.com") {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	for _, body := range []map[string]any{{"name": "CI", "hosts": []string{}, "ranges": []string{}}, {"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{"10.0.0.0/40"}}} {
		if w := request(h, "PUT", "/api/v1/security/access-tokens/"+created.Token.ID, f.admin, body); w.Code != 400 {
			t.Fatalf("update %v: %d", body, w.Code)
		}
	}
	if w := request(h, "PUT", "/api/v1/security/access-tokens/nosuchtoken", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}}); w.Code != 404 {
		t.Fatalf("update a missing token: %d", w.Code)
	}
	// Last use is saved and survives a restart.
	f.store.flushAccessTokenUse(context.Background())
	f.store.access.used.Clear()
	if used, err := f.db.AccessTokenUse(context.Background()); err != nil || used[created.Token.ID].IsZero() {
		t.Fatalf("saved use: %v %v", used, err)
	}
	if w := request(h, "GET", "/api/v1/security/access-tokens", f.admin, nil); !strings.Contains(w.Body.String(), `"last_used_at":"20`) {
		t.Fatalf("list after a restart: %s", w.Body.String())
	}
	// The policy audit names the tokens it requires.
	if event := f.lastAuditAction(t); event != "security.access_token_update" {
		t.Fatalf("audit: %s", event)
	}
	body := map[string]any{"policies": []map[string]any{{"name": "CI", "enabled": true, "action": "bypass", "hosts": []string{"app.example.com"}, "all_paths": true, "require_token": true, "tokens": []string{created.Token.ID}}}}
	if w := request(h, "PUT", "/api/v1/security/access-policies", f.admin, body); w.Code != 200 {
		t.Fatalf("policies: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAudit(t); !strings.Contains(string(event.Detail), `"require_token":true,"tokens":["`+created.Token.ID+`"]`) {
		t.Fatalf("policy audit: %s", event.Detail)
	}
	// A policy taking any token on an address the token shares lists it too;
	// one on other addresses doesn't.
	f.setPolicies(t,
		control.AccessPolicy{Name: "Any", Enabled: true, Action: control.AccessBypass, Hosts: []string{"api.example.com"}, AllPaths: true, RequireToken: true},
		control.AccessPolicy{Name: "Elsewhere", Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.org"}, AllPaths: true, RequireToken: true},
	)
	if w := request(h, "GET", "/api/v1/security/access-tokens", f.admin, nil); json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Tokens[0].Policies) != 1 || list.Tokens[0].Policies[0].Name != "Any" {
		t.Fatalf("policies taking any token: %s", w.Body.String())
	}
}

// A policy that requires an access token covers only requests that send
// one valid for them; others go on to the policies below. With bypass the
// token alone lets them in; with allow, a session must as well.
func TestAccessTokenCondition(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	create := func(name string) (string, string) {
		t.Helper()
		var created struct {
			Token accessTokenView `json:"token"`
			Value string          `json:"value"`
		}
		w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": name, "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
		if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
			t.Fatalf("create %s: %d %s", name, w.Code, w.Body.String())
		}
		return created.Token.ID, created.Value
	}
	ci, ciValue := create("CI")
	_, otherValue := create("Other")
	on := func(prefix string) []control.BlockCondition {
		return []control.BlockCondition{{Operator: control.BlockStartsWith, Value: prefix}}
	}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: on("/both/"), RequireToken: true, Tokens: []string{ci}, AllowedUsers: []string{"@corp.example.com"}},
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: on("/any/"), RequireToken: true},
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: on("/shut/"), RequireToken: true, Tokens: []string{ci}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	send := func(path, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://app.example.com"+path, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.40")
		if token != "" {
			r.Header.Set(accessTokenHeader, token)
		}
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	for _, c := range []struct {
		name, path, token string
		session           bool
		code              int
		body              string
	}{
		{"any token", "/any/x", ciValue, false, 200, "service=CI "},
		{"another token", "/any/x", otherValue, false, 200, "service=Other "},
		{"no token falls through", "/any/x", "", false, http.StatusUnauthorized, ""},
		{"no token, a session below", "/any/x", "", true, 200, "email=alice@corp.example.com"},
		{"token and session", "/both/x", ciValue, true, 200, "service=CI "},
		{"token without a session", "/both/x", ciValue, false, http.StatusUnauthorized, ""},
		{"session without the token", "/both/x", otherValue, true, 200, "service= "},
		{"blocked token", "/shut/x", ciValue, false, http.StatusForbidden, ""},
		{"another token where one is blocked", "/shut/x", otherValue, false, http.StatusForbidden, ""},
		{"no token where one is blocked", "/shut/x", "", true, 200, "email=alice@corp.example.com"},
	} {
		var cookies []*http.Cookie
		if c.session {
			cookies = append(cookies, session)
		}
		if w := send(c.path, c.token, cookies...); w.Code != c.code || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body.String())
		}
	}
	// A policy requiring a token hides from a sign-in none that don't.
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true},
	)
	if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusFound {
		t.Fatalf("start behind a block for tokens: %d", w.Code)
	}
	// Nor does one requiring some tokens hide one taking any; one requiring
	// any hides one requiring some.
	for _, c := range []struct {
		first, then []string
		code        int
	}{
		{[]string{ci}, nil, http.StatusFound},
		{nil, []string{ci}, http.StatusNotFound},
	} {
		f.setPolicies(t,
			control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true, Tokens: c.first},
			control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true, Tokens: c.then, AllowEveryone: true},
		)
		if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != c.code {
			t.Fatalf("start behind a block for tokens %q, allowing %q: %d", c.first, c.then, w.Code)
		}
	}
}

// A policy below one that takes every path of an address never applies
// there, so it neither lets anyone sign in nor puts its groups in the
// session.
func TestAccessShadowedPoliciesSignNobodyIn(t *testing.T) {
	var shadowed []control.AccessPolicy
	everyone := control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true}
	shadowed = append(shadowed, everyone)
	var groups []string
	for i := range 30 {
		group := fmt.Sprintf("team-%02d-%s", i, strings.Repeat("x", 200))
		groups = append(groups, group)
		shadowed = append(shadowed, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: fmt.Sprintf("/team-%02d/", i)}}, AllowedGroups: []string{group}})
	}
	f := newAccessFixture(t, shadowed...)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@else.example.org", "groups", groups)))
	payload, _, _ := strings.Cut(session.Value, ".")
	if raw, _ := base64.RawURLEncoding.DecodeString(payload); strings.Contains(string(raw), "team-") {
		t.Fatalf("session carries shadowed groups: %s", raw)
	}
	// Nor does a policy whose paths an earlier one starts with: its groups
	// stay out too, so the session fits.
	shadowed = []control.AccessPolicy{{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/team/"}}, AllowEveryone: true}}
	for i, group := range groups {
		shadowed = append(shadowed, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: fmt.Sprintf("/team/x-%02d/", i)}, {Operator: control.BlockEquals, Value: fmt.Sprintf("/Team/y-%02d", i)}}, AllowedGroups: []string{group}})
	}
	f.setPolicies(t, append(shadowed, everyone)...)
	session, _ = f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@else.example.org", "groups", groups)))
	payload, _, _ = strings.Cut(session.Value, ".")
	if raw, _ := base64.RawURLEncoding.DecodeString(payload); strings.Contains(string(raw), "team-") {
		t.Fatalf("session carries groups of shadowed conditions: %s", raw)
	}
	// A shadowed allow policy alone doesn't let a sign-in start, behind a
	// block for every path or for every path under "/".
	for _, block := range []control.AccessPolicy{
		{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, AllPaths: true},
		{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/"}}},
	} {
		f.setPolicies(t, block, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true})
		if w := f.visit(f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("browser")); w.Code != http.StatusNotFound {
			t.Fatalf("start behind %+v: %d", block.Conditions, w.Code)
		}
	}
}

// A token whose change was saved but couldn't be read back stops at once,
// rather than keep letting requests in as before (see forgetAccessToken).
func TestForgottenAccessTokenLetsNothingIn(t *testing.T) {
	f := newAccessFixture(t)
	var created struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(f.store.Handler(), "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true, RequireToken: true, Tokens: []string{created.Token.ID}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
	)
	send := func() int {
		r := httptest.NewRequest("GET", "https://app.example.com/x", nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.30")
		r.Header.Set("Accept", "application/json")
		r.Header.Set(accessTokenHeader, created.Value)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := send(); code != 200 {
		t.Fatalf("with the token: %d", code)
	}
	f.store.forgetAccessToken(created.Token.ID)
	if code := send(); code == 200 {
		t.Fatal("a forgotten token still let a request in")
	}
}
