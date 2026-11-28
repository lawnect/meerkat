package outlet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// legacyIDTokenCookie is what releases before the __Host- prefix called it.
const legacyIDTokenCookie = "meerkat_sso_id_token"

func cookiesNamed(w *httptest.ResponseRecorder, name string) []*http.Cookie {
	var found []*http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			found = append(found, cookie)
		}
	}
	return found
}

// The ID token cookie carries the __Host- prefix, as the session cookie does:
// an app on a sibling hostname can't plant it, and the Outlet never reads the
// plain name.
func TestSSOIDTokenCookieIsHostOnly(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)
	w := a.ssoSignIn(t, idp, verified("alice@corp.example.com", "groups", []string{"meerkat-admins"}))
	if len(cookiesNamed(w, legacyIDTokenCookie)) != 0 {
		t.Fatal("the ID token is still set under the plain name")
	}
	set := cookiesNamed(w, "__Host-meerkat_sso_id_token")
	if len(set) != 1 || set[0].Path != "/" || set[0].Domain != "" || !set[0].Secure || !set[0].HttpOnly || set[0].SameSite != http.SameSiteStrictMode || set[0].Value == "" {
		t.Fatalf("ID token cookie: %+v", set)
	}
	if ssoIDTokenCookie != "__Host-meerkat_sso_id_token" {
		t.Fatalf("cookie name = %q", ssoIDTokenCookie)
	}
}

func TestLogoutOffersTheProviderOnlyToAnActiveSession(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)
	signedIn := a.ssoSignIn(t, idp, verified("alice@corp.example.com", "groups", []string{"meerkat-admins"}))
	session, idToken := sessionCookie(t, signedIn), cookiesNamed(signedIn, ssoIDTokenCookie)[0]
	logout := func(cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := jsonRequest("DELETE", "/api/v1/session", nil)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", "198.51.100.20")
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		return w
	}
	// Anyone who can send a cookie to the console could otherwise have the
	// Outlet build a provider logout address around a token of their choice.
	planted := *idToken
	if w := logout(&planted); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("no session: %d %s", w.Code, w.Body.String())
	}
	if w := logout(sessionFor(t, a), idToken); w.Code != http.StatusOK {
		t.Fatalf("session and ID token cookie: %d %s", w.Code, w.Body.String())
	}
	// The plain name is never believed, but a logout clears it, so what an
	// upgrade left behind doesn't stay.
	legacy := &http.Cookie{Name: legacyIDTokenCookie, Value: idToken.Value}
	w := logout(sessionFor(t, a), legacy)
	if w.Code != http.StatusNoContent {
		t.Fatalf("plain-named cookie: %d %s", w.Code, w.Body.String())
	}
	for _, name := range []string{legacyIDTokenCookie, ssoIDTokenCookie} {
		if cleared := cookiesNamed(w, name); len(cleared) != 1 || cleared[0].MaxAge >= 0 || cleared[0].Value != "" {
			t.Fatalf("%s after logout: %+v", name, cleared)
		}
	}
	if cleared := cookiesNamed(w, ssoIDTokenCookie)[0]; cleared.Path != "/" || !cleared.Secure {
		t.Fatalf("the cleared ID token cookie must match its prefix rules: %+v", cleared)
	}
	// The session the provider started ends with its own logout.
	var body struct {
		LogoutURL string `json:"logout_url"`
	}
	out := logout(session, idToken)
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &body) != nil || body.LogoutURL == "" {
		t.Fatalf("the SSO session's own logout: %d %s", out.Code, out.Body.String())
	}
	// A password sign-in clears whatever ID token cookie the browser sent.
	login := jsonRequest("POST", "/api/v1/session", adminLogin)
	login.Host = a.host
	login.Header.Set("X-Forwarded-For", "198.51.100.21")
	login.AddCookie(idToken)
	login.AddCookie(&http.Cookie{Name: legacyIDTokenCookie, Value: idToken.Value})
	signIn := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(signIn, login)
	if signIn.Code != 201 {
		t.Fatalf("password sign-in: %d %s", signIn.Code, signIn.Body.String())
	}
	for _, name := range []string{legacyIDTokenCookie, ssoIDTokenCookie} {
		if cleared := cookiesNamed(signIn, name); len(cleared) != 1 || cleared[0].MaxAge >= 0 {
			t.Fatalf("%s after a password sign-in: %+v", name, cleared)
		}
	}
	a.store.background.Wait()
}

// sessionFor signs another administrator in with a password, so a logout has
// a session of its own to end while the SSO one is kept for later.
func sessionFor(t *testing.T, a alertFixture) *http.Cookie {
	t.Helper()
	w := a.publicRequest("POST", "/api/v1/session", "198.51.100.22", chromeOnMac, adminLogin)
	return sessionCookie(t, w)
}
