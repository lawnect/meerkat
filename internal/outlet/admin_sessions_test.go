package outlet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// adminStatus is what the administrator API answers a request carrying cookie.
func (a alertFixture) adminStatus(cookie *http.Cookie) int {
	return cookieRequest(a.store.Handler(), "GET", "/api/v1/alerts", cookie, nil).Code
}

// recoveryBody sets the password of the administrator adminSession creates.
var recoveryBody = map[string]any{"email": "admin@example.com", "password": "an entirely new password"}

// Recovery over SSH sets a new password because the old one may be in someone
// else's hands, so a session that password opened ends with it, even one the
// Outlet has cached.
func TestRecoveryEndsTheAdministratorsSessions(t *testing.T) {
	a := newAlertFixture(t)
	stolen := a.adminSession(t)
	h := a.store.Handler()
	other := sessionCookie(t, request(h, "POST", "/api/v1/session", "", adminLogin))
	for _, cookie := range []*http.Cookie{stolen, other} {
		if code := a.adminStatus(cookie); code != 200 {
			t.Fatalf("session before recovery: %d", code)
		}
	}
	if w := request(h, "POST", "/api/v1/setup/recover", a.admin, recoveryBody); w.Code != 200 {
		t.Fatalf("recovery: %d %s", w.Code, w.Body.String())
	}
	for name, cookie := range map[string]*http.Cookie{"first": stolen, "second": other} {
		if w := cookieRequest(h, "GET", "/api/v1/alerts", cookie, nil); w.Code != 401 || errorCode(t, w) != "invalid_admin_session" {
			t.Fatalf("%s session after recovery: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := request(h, "POST", "/api/v1/session", "", recoveryBody); w.Code != 201 {
		t.Fatalf("signing in with the new password: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
}

func (a alertFixture) revokeAll(cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := jsonRequest("POST", "/api/v1/security/admin/sessions/revoke", nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	a.store.Handler().ServeHTTP(w, r)
	return w
}

func TestAdministratorSessionsCanAllBeEnded(t *testing.T) {
	a := newAlertFixture(t)
	mine := a.adminSession(t)
	h := a.store.Handler()
	other := sessionCookie(t, request(h, "POST", "/api/v1/session", "", adminLogin))
	a.store.background.Wait()
	for _, cookie := range []*http.Cookie{mine, other} {
		if code := a.adminStatus(cookie); code != 200 {
			t.Fatalf("session: %d", code)
		}
	}
	if w := a.revokeAll(nil, nil); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	// A tunneled app is same-site with the console: its form post must not
	// end anyone's session.
	for _, headers := range []map[string]string{{"Sec-Fetch-Site": "cross-site"}, {"Sec-Fetch-Site": "same-site"}, {"Origin": "https://app.example.com"}} {
		if w := a.revokeAll(mine, headers); w.Code != 403 || errorCode(t, w) != "cross_origin_request" {
			t.Fatalf("cross-origin %v: %d %s", headers, w.Code, w.Body.String())
		}
	}
	if code := a.adminStatus(other); code != 200 {
		t.Fatalf("a refused request ended a session: %d", code)
	}
	w := a.revokeAll(mine, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	var cleared bool
	for _, cookie := range w.Result().Cookies() {
		cleared = cleared || (cookie.Name == sessionCookieName && cookie.MaxAge < 0)
	}
	if !cleared {
		t.Fatal("the caller's session cookie was not cleared")
	}
	for name, cookie := range map[string]*http.Cookie{"caller's": mine, "other": other} {
		if code := a.adminStatus(cookie); code != 401 {
			t.Fatalf("%s session after revoking all: %d", name, code)
		}
	}
	// The audit entry names the administrator who did it, though the session
	// that vouched for them has ended.
	var found control.AuditEvent
	for _, event := range a.auditLog(t) {
		if event.Action == "security.admin_sessions_revoke" {
			found = event
		}
	}
	if found.ActorEmail == nil || *found.ActorEmail != "admin@example.com" || found.ResourceType != "security" || !strings.Contains(string(found.Detail), `"sessions":2`) {
		t.Fatalf("audit: %+v %s", found, found.Detail)
	}
	if w := request(h, "POST", "/api/v1/session", "", adminLogin); w.Code != 201 {
		t.Fatalf("signing in again: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
}

// ssoSettingsBody is the saved single sign-on settings as a PUT body, changed
// by change. The secret is left out, which keeps the stored one.
func (a alertFixture) ssoSettingsBody(t *testing.T, cookie *http.Cookie, change func(*control.SSO)) map[string]any {
	t.Helper()
	w := cookieRequest(a.store.Handler(), "GET", "/api/v1/security/sso", cookie, nil)
	var settings control.SSO
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &settings) != nil {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	if change != nil {
		change(&settings)
	}
	return map[string]any{"enabled": settings.Enabled, "label": settings.Label, "issuer": settings.Issuer, "client_id": settings.ClientID, "scopes": settings.Scopes,
		"groups_claim": settings.GroupsClaim, "allowed_users": settings.AllowedUsers, "allowed_groups": settings.AllowedGroups, "password_login": settings.PasswordLogin}
}

func (a alertFixture) saveSSO(t *testing.T, cookie *http.Cookie, change func(*control.SSO)) {
	t.Helper()
	body := a.ssoSettingsBody(t, cookie, change)
	if w := cookieRequest(a.store.Handler(), "PUT", "/api/v1/security/sso", cookie, body); w.Code != 200 {
		t.Fatalf("saving: %d %s", w.Code, w.Body.String())
	}
}

// lastSSOAudit is the detail of the newest security.sso entry.
func (a alertFixture) lastSSOAudit(t *testing.T) string {
	t.Helper()
	var detail string
	for _, event := range a.auditLog(t) {
		if event.Action == "security.sso" {
			detail = string(event.Detail)
		}
	}
	if detail == "" {
		t.Fatal("no security.sso entry")
	}
	return detail
}

// A person removed from the lists that let them in must not keep a session for
// a day: saving the settings ends the sessions of administrators single
// sign-on bound that the new lists no longer name.
func TestSavingSSOSettingsEndsSessionsOfPeopleNoLongerAllowed(t *testing.T) {
	a := newAlertFixture(t)
	admin := a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) {
		s.AllowedUsers, s.AllowedGroups = []string{"alice@corp.example.com", "carol@corp.example.com", "@partner.example"}, nil
	})
	alice := sessionCookie(t, a.ssoSignIn(t, idp, verified("alice@corp.example.com")))
	bob := sessionCookie(t, a.ssoSignIn(t, idp, verified("bob@partner.example")))
	carol := sessionCookie(t, a.ssoSignIn(t, idp, verified("carol@corp.example.com")))
	expect := func(step string, want map[*http.Cookie]int) {
		t.Helper()
		for cookie, code := range want {
			if got := a.adminStatus(cookie); got != code {
				t.Fatalf("%s: %s answers %d, want %d", step, cookie.Value[:6], got, code)
			}
		}
	}
	expect("signed in", map[*http.Cookie]int{admin: 200, alice: 200, bob: 200, carol: 200})

	// Carol leaves the list; her address is named only there.
	a.saveSSO(t, admin, func(s *control.SSO) { s.AllowedUsers = []string{"alice@corp.example.com", "@partner.example"} })
	expect("carol removed", map[*http.Cookie]int{admin: 200, alice: 200, bob: 200, carol: 401})
	if detail := a.lastSSOAudit(t); !strings.Contains(detail, `"sessions_revoked":1`) || !strings.Contains(detail, "not_allowed") {
		t.Fatalf("audit: %s", detail)
	}
	// A whole domain leaves.
	a.saveSSO(t, admin, func(s *control.SSO) { s.AllowedUsers = []string{"alice@corp.example.com"} })
	expect("partner domain removed", map[*http.Cookie]int{admin: 200, alice: 200, bob: 401})
	// A change that names everyone still allowed ends nothing.
	a.saveSSO(t, admin, func(s *control.SSO) { s.Label = "Corp SSO" })
	expect("label changed", map[*http.Cookie]int{admin: 200, alice: 200})
	if detail := a.lastSSOAudit(t); strings.Contains(detail, "sessions_revoked") {
		t.Fatalf("a harmless change audited as revoking: %s", detail)
	}
	a.store.background.Wait()
}

// Group membership can't be read again without the provider, so a change to
// the groups, the provider or turning single sign-on off ends every single
// sign-on session, and the audit entry says why.
func TestSSOSessionsEndWhenGroupsOrProviderChange(t *testing.T) {
	a := newAlertFixture(t)
	admin := a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = nil, []string{"meerkat-admins"} })
	signIn := func() *http.Cookie {
		t.Helper()
		a.clock.advance(loginWindow)
		return sessionCookie(t, a.ssoSignIn(t, idp, verified("alice@corp.example.com", "groups", []string{"meerkat-admins"})))
	}
	for _, c := range []struct {
		name   string
		change func(*control.SSO)
		reason string
	}{
		{"another group", func(s *control.SSO) { s.AllowedGroups = []string{"meerkat-admins", "ops"} }, "allowed_groups_changed"},
		{"another groups claim", func(s *control.SSO) { s.GroupsClaim = "roles" }, "provider_changed"},
		{"another client", func(s *control.SSO) { s.ClientID = "meerkat-2" }, "provider_changed"},
		{"turned off", func(s *control.SSO) { s.Enabled = false }, "sso_disabled"},
	} {
		alice := signIn()
		if code := a.adminStatus(alice); code != 200 {
			t.Fatalf("%s: before: %d", c.name, code)
		}
		a.saveSSO(t, admin, c.change)
		if code := a.adminStatus(alice); code != 401 {
			t.Fatalf("%s: SSO session after the change: %d", c.name, code)
		}
		if code := a.adminStatus(admin); code != 200 {
			t.Fatalf("%s: the password session ended: %d", c.name, code)
		}
		if detail := a.lastSSOAudit(t); !strings.Contains(detail, c.reason) || !strings.Contains(detail, `"sessions_revoked":1`) {
			t.Fatalf("%s: audit: %s", c.name, detail)
		}
		// Turn it back on for the next case.
		a.saveSSO(t, admin, func(s *control.SSO) {
			s.Enabled, s.ClientID, s.GroupsClaim, s.AllowedGroups = true, "meerkat", "groups", []string{"meerkat-admins"}
		})
	}
	a.store.background.Wait()
}

// Settings that change without a save (the environment's, a restore) are
// caught when a cached session is read from the database again: an
// administrator who signs in only with the provider is held to the email and
// domain lists then. One with a password too may be using that, and is not.
func TestSSOOnlySessionsAreHeldToTheListWhenRechecked(t *testing.T) {
	a := newAlertFixture(t)
	password := a.adminSession(t) // admin@example.com, with a password
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) {
		s.AllowedUsers, s.AllowedGroups = []string{"admin@example.com", "alice@corp.example.com", "bob@corp.example.com"}, nil
	})
	alice := sessionCookie(t, a.ssoSignIn(t, idp, verified("alice@corp.example.com")))
	bob := sessionCookie(t, a.ssoSignIn(t, idp, verified("bob@corp.example.com")))
	// The password administrator signs in with the provider once, which binds them.
	if w := a.ssoSignIn(t, idp, verified("admin@example.com", "sub", "owner")); w.Code != 200 {
		t.Fatalf("binding: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, cookie := range []*http.Cookie{password, alice, bob} {
		if code := a.adminStatus(cookie); code != 200 {
			t.Fatalf("before: %d", code)
		}
	}
	// Bob and the password administrator leave the list behind the Outlet's back.
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = []string{"alice@corp.example.com"}, nil })
	// Cached, a session is believed for a minute.
	if code := a.adminStatus(bob); code != 200 {
		t.Fatalf("within the minute: %d", code)
	}
	a.clock.advance(sessionRecheck + time.Second)
	if code := a.adminStatus(bob); code != 401 {
		t.Fatalf("a listed-out SSO-only administrator after the recheck: %d", code)
	}
	if code := a.adminStatus(alice); code != 200 {
		t.Fatalf("still listed: %d", code)
	}
	if code := a.adminStatus(password); code != 200 {
		t.Fatalf("an administrator with a password: %d", code)
	}
	// It ended for good: listing Bob again doesn't bring the session back.
	a.enableSSO(t, idp, func(s *control.SSO) {
		s.AllowedUsers, s.AllowedGroups = []string{"alice@corp.example.com", "bob@corp.example.com"}, nil
	})
	a.clock.advance(sessionRecheck + time.Second)
	if code := a.adminStatus(bob); code != 401 {
		t.Fatalf("revived: %d", code)
	}
	a.store.background.Wait()
}
