package outlet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// whoami answers with the access email and cookies the app received.
func whoami() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "email=%s cookie=%s path=%s", r.Header.Get(accessEmailHeader), r.Header.Get("Cookie"), r.URL.Path)
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
	for _, path := range []string{"/admin", "/admin/", "/Admin/x", "//admin/x", "/x/../admin/y", "/%61dmin/y", "/admin;jsessionid=1/x", "/admin./x", "/./admin/x"} {
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

// A client can't hand the app an identity of its own.
func TestAccessEmailHeaderCantBeForged(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}})
	r := httptest.NewRequest("GET", "/x", nil)
	r.Host = "other.example.com"
	r.Header.Set(accessEmailHeader, "boss@corp.example.com")
	r.AddCookie(&http.Cookie{Name: accessCookie, Value: "forged.value"})
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "email= ") || strings.Contains(w.Body.String(), "forged") {
		t.Fatalf("unprotected host: %s", w.Body.String())
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

// Every policy covering a request must allow it.
func TestAccessPoliciesAllApply(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"*.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/finance/"}}, AllowedGroups: []string{"finance"}},
	)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if w := f.visit("app.example.com", "/", session); w.Code != 200 {
		t.Fatalf("first policy only: %d", w.Code)
	}
	if w := f.visit("app.example.com", "/finance/q3", session); w.Code != http.StatusForbidden {
		t.Fatalf("both policies: %d", w.Code)
	}
	session, _ = f.finish(t, f.signIn(t, "app.example.com", "/finance/q3", verified("carol@corp.example.com", "groups", []string{"finance", "unrelated"})))
	if w := f.visit("app.example.com", "/finance/q3", session); w.Code != 200 {
		t.Fatalf("both satisfied: %d", w.Code)
	}
	// Only groups some policy names ride in the cookie.
	payload, _, _ := strings.Cut(session.Value, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(payload)
	if strings.Contains(string(raw), "unrelated") {
		t.Fatalf("cookie carries %s", raw)
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
	go func() {
		policy := control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}}
		f.setPolicies(t, policy)
	}()
	wg.Wait()
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
		// A NUL, which ends the path for some.
		"/public%00/../admin/x",
	} {
		if w := f.visit("app.example.com", path); w.Code != http.StatusFound {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/", "/public/page", "/administrator", "/secrets", "/100%25", "/%EA%B2%8C%EC%8B%9C%ED%8C%90/1", "/a..b/c", "/a/../public", "/admin/../public",
		// Decoded once more, these name nothing protected.
		"/x%252fy", "/files/100%25ab.pdf", "/data/a%253Fb%253Dc"} {
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
		"/api/v1/security/block-rules":       map[string]any{"rules": []any{}},
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
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: []string{"@corp.example.com"}},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/finance/"}}, AllowedGroups: []string{"finance"}},
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
	if event := f.lastAudit(t); event.Action != "security.access_policies" || string(event.Detail) != `{"policies":[{"name":"Staff","enabled":true,"hosts":["app.example.com"],"allowed_users":["@corp.example.com"],"allowed_groups":["staff"],"allow_admins":true}]}` {
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
