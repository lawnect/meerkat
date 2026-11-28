package outlet

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// Addresses enough to fill the limiter's table, one empty login each, don't
// keep an administrator at another address out.
func TestFullLoginTableStillAdmitsAnAdministrator(t *testing.T) {
	a := newAlertFixture(t)
	for i := range maxLoginKeys {
		ip := fmt.Sprintf("198.%d.%d.%d", 18+(i>>16), (i>>8)&255, i&255)
		w := a.publicRequest("POST", "/api/v1/session", ip, chromeOnMac, map[string]any{})
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("flood request %d refused early: %s", i, w.Body.String())
		}
		a.clock.t = a.clock.t.Add(5 * time.Millisecond)
	}
	w := a.publicRequest("POST", "/api/v1/session", "203.0.113.200", chromeOnMac, adminLogin)
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("a correct password from another address: %d %s", w.Code, w.Body.String())
	}
}

// Nor do that many addresses starting one access sign-in each keep a
// visitor at another address from starting theirs.
func TestFullStartTableStillAdmitsAVisitor(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	for i := range maxLoginKeys {
		ip := fmt.Sprintf("198.%d.%d.%d", 18+(i>>16), (i>>8)&255, i&255)
		w := f.visitFrom(ip, f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash(fmt.Sprint(i)))
		if w.Code != http.StatusFound {
			t.Fatalf("flood start %d: %d", i, w.Code)
		}
	}
	challenge := f.visit("app.example.com", "/")
	if challenge.Code != http.StatusFound {
		t.Fatalf("challenge: %d", challenge.Code)
	}
	begin := f.visitFrom("203.0.113.201", f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash("victim"))
	if begin.Code != http.StatusFound {
		t.Fatalf("a visitor from another address cannot start a sign-in: %d", begin.Code)
	}
}

// An administrator who signs in only through the provider follows an email
// the provider changed: the recheck holds the new one to the list, not the
// one stored when the account was bound.
func TestSSOAdministratorFollowsAChangedEmail(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = []string{"@old.example.com"}, nil })
	first := a.ssoSignIn(t, idp, map[string]any{"email": "alice@old.example.com", "email_verified": true, "sub": "alice"})
	if first.Code != 200 {
		t.Fatalf("first: %d", first.Code)
	}
	// The company moves to a new domain at the provider and in the list.
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = []string{"@new.example.com"}, nil })
	w := a.ssoSignIn(t, idp, map[string]any{"email": "alice@new.example.com", "email_verified": true, "sub": "alice"})
	if w.Code != 200 {
		t.Fatalf("sign-in with new email: %d %s", w.Code, w.Header().Get("Location"))
	}
	cookie := sessionCookie(t, w)
	if code := a.adminStatus(cookie); code != 200 {
		t.Fatalf("right after: %d", code)
	}
	a.clock.advance(sessionRecheck + time.Second)
	if code := a.adminStatus(cookie); code != 200 {
		t.Fatalf("the session ended at the recheck: %d", code)
	}
	a.store.background.Wait()
}

// HEAD runs the GET handlers, so it is refused from another site as GET is.
func TestCrossSiteHeadIsRefused(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	for _, path := range []string{"/api/v1/connectors", "/api/v1/outlet", "/api/v1/observe/request-log/top"} {
		r := jsonRequest("HEAD", path, nil)
		r.AddCookie(cookie)
		r.Header.Set("Sec-Fetch-Site", "same-site")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("HEAD %s from same-site ran the handler: %d", path, w.Code)
		}
	}
}

// The administrator who changes the settings keeps their session, bound to
// the provider or not: signed out by a mistyped group, they couldn't sign in
// through the provider to correct it. Everyone else bound to it is signed out.
func TestSavingSSOSettingsKeepsTheSaversSession(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = nil, []string{"meerkat-admins"} })
	alice := sessionCookie(t, a.ssoSignIn(t, idp, verified("alice@corp.example.com", "groups", []string{"meerkat-admins"})))
	a.clock.advance(loginWindow)
	bob := sessionCookie(t, a.ssoSignIn(t, idp, verified("bob@corp.example.com", "sub", "bob", "groups", []string{"meerkat-admins"})))
	a.saveSSO(t, alice, func(s *control.SSO) { s.AllowedGroups = []string{"meerkat-admin"} })
	if code := a.adminStatus(alice); code != 200 {
		t.Fatalf("the administrator who saved was signed out: %d", code)
	}
	if code := a.adminStatus(bob); code != 401 {
		t.Fatalf("another bound administrator kept the session: %d", code)
	}
	a.store.background.Wait()
}
