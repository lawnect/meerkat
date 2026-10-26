package outlet

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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

	"github.com/go-jose/go-jose/v4"

	"github.com/lawnect/meerkat/internal/control"
)

// fakeIdP is an OpenID provider over TLS: discovery, keys, and a token
// endpoint that checks the PKCE verifier and signs the next ID token.
type fakeIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey

	mu        sync.Mutex
	challenge string         // code_challenge of the last authorization request
	claims    map[string]any // claims of the next ID token
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key}
	mux := http.NewServeMux()
	idp.server = httptest.NewTLSServer(mux)
	t.Cleanup(idp.server.Close)
	issuer := idp.server.URL
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/auth", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks", "end_session_endpoint": issuer + "/logout",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		idp.mu.Lock()
		defer idp.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != idp.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": idp.sign(t, idp.claims)})
	})
	return idp
}

func (idp *fakeIdP) sign(t *testing.T, claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return compact
}

func (a alertFixture) enableSSO(t *testing.T, idp *fakeIdP, change func(*control.SSO)) {
	t.Helper()
	a.store.ssoHTTP = idp.server.Client()
	settings := control.SSO{Enabled: true, Label: "Corp", Issuer: idp.server.URL, ClientID: "meerkat", ClientSecret: "client-secret-1234",
		Scopes: []string{"openid", "email", "groups"}, GroupsClaim: "groups", AllowedUsers: []string{"@corp.example.com"}, AllowedGroups: []string{"meerkat-admins"}, PasswordLogin: true}
	if change != nil {
		change(&settings)
	}
	if _, err := a.db.SetSSO(context.Background(), settings, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// ssoSignIn runs a whole sign-in: the start redirect, the provider (which
// signs claims, with the nonce from the start unless claims set one), and the
// callback. It returns the callback's response.
func (a alertFixture) ssoSignIn(t *testing.T, idp *fakeIdP, claims map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	start := a.publicRequest("GET", ssoPath+"/start?return=/ko/", "198.51.100.20", chromeOnMac, nil)
	location, err := url.Parse(start.Header().Get("Location"))
	if start.Code != http.StatusFound || err != nil || !strings.HasPrefix(location.String(), idp.server.URL+"/auth?") {
		t.Fatalf("start: %d %s", start.Code, start.Header().Get("Location"))
	}
	query := location.Query()
	if query.Get("redirect_uri") != "https://"+a.host+ssoCallbackPath || query.Get("code_challenge_method") != "S256" || query.Get("client_id") != "meerkat" {
		t.Fatalf("authorization request: %s", location)
	}
	// Each address is its own provider account unless the claims say otherwise.
	full := map[string]any{"iss": idp.server.URL, "aud": "meerkat", "sub": fmt.Sprint("subject-", claims["email"]), "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": query.Get("nonce")}
	for key, value := range claims {
		full[key] = value
	}
	idp.mu.Lock()
	idp.challenge, idp.claims = query.Get("code_challenge"), full
	idp.mu.Unlock()
	r := httptest.NewRequest("GET", ssoCallbackPath+"?code=good-code&state="+url.QueryEscape(query.Get("state")), nil)
	r.Host = a.host
	r.Header.Set("X-Forwarded-For", "198.51.100.20")
	for _, cookie := range start.Result().Cookies() {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(w, r)
	a.store.background.Wait()
	return w
}

func ssoError(w *httptest.ResponseRecorder) string {
	if w.Code != http.StatusSeeOther {
		return ""
	}
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		return ""
	}
	return location.Query().Get("sso_error")
}

func TestSSOSignsInOnlyAllowedPeople(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)

	w := a.ssoSignIn(t, idp, map[string]any{"email": "Alice@Corp.Example.com", "email_verified": true, "groups": []string{"staff", "meerkat-admins"}})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `content="0;url=/ko/"`) {
		t.Fatalf("allowed domain: %d %s %s", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	session := sessionCookie(t, w)
	r := httptest.NewRequest("GET", "/api/v1/alerts", nil)
	r.AddCookie(session)
	check := httptest.NewRecorder()
	a.store.Handler().ServeHTTP(check, r)
	if check.Code != 200 {
		t.Fatalf("SSO session: %d", check.Code)
	}
	if event := a.lastAudit(t); event.Action != "session.login" || event.ActorEmail == nil || *event.ActorEmail != "alice@corp.example.com" || !strings.Contains(string(event.Detail), `"method":"sso"`) {
		t.Fatalf("login audit: %+v", event)
	}
	// The administrator single sign-on created has no password.
	if w := request(a.store.Handler(), "POST", "/api/v1/session", "", map[string]any{"email": "alice@corp.example.com", "password": "anything at all here"}); w.Code != 401 {
		t.Fatalf("password for an SSO administrator: %d", w.Code)
	}

	// Both lists are set, so a person needs both: the domain and a group.
	for name, claims := range map[string]map[string]any{
		"group but other domain": {"email": "bob@partner.example", "groups": []string{"meerkat-admins"}},
		"domain but no group":    {"email": "carol@corp.example.com", "groups": []string{"staff"}},
		"domain, groups missing": {"email": "dave@corp.example.com"},
		"neither":                {"email": "eve@other.example", "groups": []string{"staff"}},
		"unverified email":       {"email": "mallory@corp.example.com", "email_verified": false, "groups": []string{"meerkat-admins"}},
		"no email":               {"groups": []string{"meerkat-admins"}},
	} {
		if w := a.ssoSignIn(t, idp, claims); ssoError(w) != "not_allowed" || !strings.HasPrefix(w.Header().Get("Location"), "/ko/?") {
			t.Fatalf("%s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
	}
	// One address may start ten sign-ins a minute.
	a.clock.advance(loginWindow)
	for name, claims := range map[string]map[string]any{
		"wrong nonce":    {"email": "alice@corp.example.com", "nonce": "replayed"},
		"wrong audience": {"email": "alice@corp.example.com", "aud": "another-app"},
		"wrong issuer":   {"email": "alice@corp.example.com", "iss": "https://evil.example"},
		"expired":        {"email": "alice@corp.example.com", "exp": time.Now().Add(-time.Hour).Unix()},
	} {
		if w := a.ssoSignIn(t, idp, claims); ssoError(w) != "token_failed" {
			t.Fatalf("%s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestSSOListsThatAreSetMustAllMatch(t *testing.T) {
	settings := func(users, groups []string) control.SSO {
		return control.SSO{AllowedUsers: users, AllowedGroups: groups}
	}
	for _, c := range []struct {
		name          string
		users, groups []string
		email         string
		memberOf      []string
		want          bool
	}{
		{"users only, listed email", []string{"a@x.example"}, nil, "a@x.example", nil, true},
		{"users only, listed domain", []string{"@x.example"}, nil, "b@x.example", nil, true},
		{"users only, other email", []string{"a@x.example"}, nil, "b@x.example", []string{"admins"}, false},
		{"groups only, member", nil, []string{"admins"}, "z@y.example", []string{"staff", "admins"}, true},
		{"groups only, not a member", nil, []string{"admins"}, "z@y.example", []string{"staff"}, false},
		{"both, both match", []string{"@x.example"}, []string{"admins"}, "a@x.example", []string{"admins"}, true},
		{"both, user only", []string{"@x.example"}, []string{"admins"}, "a@x.example", nil, false},
		{"both, group only", []string{"@x.example"}, []string{"admins"}, "a@y.example", []string{"admins"}, false},
		{"neither list", nil, nil, "a@x.example", []string{"admins"}, false},
	} {
		if got := ssoAllowed(settings(c.users, c.groups), c.email, c.memberOf); got != c.want {
			t.Errorf("%s: allowed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSSOStateIsBoundToTheBrowserAndUsedOnce(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)
	start := a.publicRequest("GET", ssoPath+"/start", "198.51.100.21", chromeOnMac, nil)
	location, _ := url.Parse(start.Header().Get("Location"))
	state := location.Query().Get("state")
	callback := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", ssoCallbackPath+"?code=good-code&state="+url.QueryEscape(state), nil)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", "198.51.100.21")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		return w
	}
	if w := callback(nil); ssoError(w) != "invalid_state" {
		t.Fatalf("callback without the browser's cookie: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := callback(&http.Cookie{Name: ssoCookie, Value: "someone-else"}); ssoError(w) != "invalid_state" {
		t.Fatalf("callback with another state: %d", w.Code)
	}
	var cookie *http.Cookie
	for _, c := range start.Result().Cookies() {
		if c.Name == ssoCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.SameSite != http.SameSiteLaxMode || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/" {
		t.Fatalf("state cookie: %+v", cookie)
	}
	// The provider reports an error; the state is spent either way.
	r := httptest.NewRequest("GET", ssoCallbackPath+"?error=access_denied&state="+url.QueryEscape(state), nil)
	r.Host = a.host
	r.Header.Set("X-Forwarded-For", "198.51.100.21")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(w, r)
	if ssoError(w) != "provider_denied" {
		t.Fatalf("provider error: %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := callback(cookie); ssoError(w) != "invalid_state" {
		t.Fatalf("state used twice: %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestSSOCanReplacePasswordsOnPublicAddresses(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	status := func() map[string]any {
		var body struct {
			SignIn map[string]any `json:"sign_in"`
		}
		w := a.publicRequest("GET", "/api/v1/setup/status", "198.51.100.22", chromeOnMac, nil)
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body.SignIn
	}
	if got := status(); got["sso"] != false || got["password"] != true {
		t.Fatalf("sign-in methods without SSO: %v", got)
	}
	a.enableSSO(t, idp, func(s *control.SSO) { s.PasswordLogin = false })
	if got := status(); got["sso"] != true || got["password"] != false || got["sso_label"] != "Corp" {
		t.Fatalf("sign-in methods with SSO only: %v", got)
	}
	if w := a.publicRequest("POST", "/api/v1/session", "198.51.100.22", chromeOnMac, adminLogin); w.Code != http.StatusForbidden || errorCode(t, w) != "password_login_disabled" {
		t.Fatalf("public password login: %d %s", w.Code, w.Body.String())
	}
	if w := request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin); w.Code != 201 {
		t.Fatalf("password over SSH: %d %s", w.Code, w.Body.String())
	}
	// Single sign-on only works on the public management host.
	if w := request(a.store.Handler(), "GET", ssoPath+"/start", "", nil); ssoError(w) != "sso_unavailable" {
		t.Fatalf("SSO on the local port: %d %s", w.Code, w.Header().Get("Location"))
	}
	a.store.background.Wait()
}

func TestSSOSettingsNeverShowTheSecret(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		r := jsonRequest(method, path, body)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		a.store.Handler().ServeHTTP(w, r)
		return w
	}
	settings := map[string]any{"enabled": true, "label": "Okta", "issuer": "https://login.example.com/", "client_id": "meerkat", "client_secret": "s3cret-0123456789",
		"scopes": []string{"email"}, "groups_claim": "", "allowed_users": []string{}, "allowed_groups": []string{}, "password_login": false}
	if w := call("PUT", "/api/v1/security/sso", settings); w.Code != 400 || errorCode(t, w) != "invalid_sso" {
		t.Fatalf("enabled with nobody allowed: %d %s", w.Code, w.Body.String())
	}
	settings["allowed_groups"] = []string{"Meerkat Admins"}
	w := call("PUT", "/api/v1/security/sso", settings)
	if w.Code != 200 || strings.Contains(w.Body.String(), "s3cret") {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var view map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &view)
	if view["issuer"] != "https://login.example.com" || view["client_secret_hint"] != "6789" || view["client_secret_set"] != true || view["groups_claim"] != "groups" ||
		view["redirect_uri"] != "https://"+a.host+ssoCallbackPath || len(view["scopes"].([]any)) != 2 {
		t.Fatalf("view: %v", view)
	}
	// Saving without a secret keeps the stored one.
	delete(settings, "client_secret")
	settings["label"] = "Okta SSO"
	if w := call("PUT", "/api/v1/security/sso", settings); w.Code != 200 {
		t.Fatalf("save without secret: %d %s", w.Code, w.Body.String())
	}
	if stored, _ := a.db.SSO(context.Background()); stored.ClientSecret != "s3cret-0123456789" || stored.Label != "Okta SSO" {
		t.Fatalf("stored: %+v", stored)
	}
	if w := call("GET", "/api/v1/security/sso", nil); strings.Contains(w.Body.String(), "s3cret") {
		t.Fatalf("secret in GET: %s", w.Body.String())
	}
	if event := a.lastAudit(t); event.Action != "security.sso" || strings.Contains(string(event.Detail), "s3cret") || !strings.Contains(string(event.Detail), `"secret_changed":false`) {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	for _, issuer := range []string{"http://login.example.com", "https://login.example.com/?a=1", "not a url"} {
		settings["issuer"] = issuer
		if w := call("PUT", "/api/v1/security/sso", settings); w.Code != 400 {
			t.Fatalf("issuer %q: %d", issuer, w.Code)
		}
	}
}

func TestSafeReturnStaysInTheApp(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/ko/": "/ko/", "//evil.example": "/", "https://evil.example": "/", "/\\evil.example": "/", "/ko/?x=1": "/", "/a\nb": "/"} {
		if got := safeReturn(in); got != want {
			t.Errorf("safeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

// With SSO from the environment, the console needs no setup: the first
// allowed person to sign in becomes an administrator. Saving from the
// console overrides the environment until the saved settings are reset.
func TestSSOFromTheEnvironment(t *testing.T) {
	a := newAlertFixture(t)
	idp := newFakeIdP(t)
	a.store.ssoHTTP = idp.server.Client()
	env, _ := control.SSO{Enabled: true, Label: "Env", Issuer: idp.server.URL, ClientID: "meerkat", ClientSecret: "client-secret-1234",
		Scopes: []string{"openid", "email"}, AllowedUsers: []string{"admin@corp.example.com"}, PasswordLogin: true}.Normalize()
	a.db.SetDefaults(control.Defaults{SSO: &env})

	var status struct {
		SetupComplete bool           `json:"setup_complete"`
		SignIn        map[string]any `json:"sign_in"`
	}
	w := a.publicRequest("GET", "/api/v1/setup/status", "198.51.100.30", chromeOnMac, nil)
	if json.Unmarshal(w.Body.Bytes(), &status) != nil || !status.SetupComplete || status.SignIn["sso"] != true || status.SignIn["sso_label"] != "Env" {
		t.Fatalf("setup status: %s", w.Body.String())
	}
	w = a.ssoSignIn(t, idp, map[string]any{"email": "admin@corp.example.com", "email_verified": true})
	if w.Code != 200 {
		t.Fatalf("first administrator through SSO: %d %s", w.Code, w.Header().Get("Location"))
	}
	cookie := sessionCookie(t, w)
	call := func(method, path string, body any) map[string]any {
		r := jsonRequest(method, path, body)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		a.store.Handler().ServeHTTP(w, r)
		var view map[string]any
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return view
	}
	if view := call("GET", "/api/v1/security/sso", nil); view["source"] != "environment" || view["environment"] != true || view["client_secret_hint"] != "1234" {
		t.Fatalf("environment view: %v", view)
	}
	view := call("PUT", "/api/v1/security/sso", map[string]any{"enabled": true, "label": "Console", "issuer": idp.server.URL, "client_id": "meerkat",
		"scopes": []string{"openid"}, "groups_claim": "groups", "allowed_users": []string{"admin@corp.example.com"}, "allowed_groups": []string{}, "password_login": true})
	if view["source"] != "database" || view["label"] != "Console" {
		t.Fatalf("saved view: %v", view)
	}
	if view := call("DELETE", "/api/v1/security/sso", nil); view["source"] != "environment" || view["label"] != "Env" {
		t.Fatalf("reset view: %v", view)
	}
	if event := a.lastAudit(t); event.Action != "security.sso_reset" {
		t.Fatalf("audit: %s", event.Action)
	}
}

// Logging out of a single sign-on session ends the provider's session too;
// a password session just ends.
func TestSSOLogoutEndsTheProviderSession(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)
	w := a.ssoSignIn(t, idp, map[string]any{"email": "alice@corp.example.com", "email_verified": true, "groups": []string{"meerkat-admins"}})
	var idToken *http.Cookie
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == ssoIDTokenCookie {
			idToken = cookie
		}
	}
	if idToken == nil || !idToken.HttpOnly || !idToken.Secure || idToken.SameSite != http.SameSiteStrictMode || idToken.Path != "/api/v1/session" {
		t.Fatalf("ID token cookie: %+v", idToken)
	}
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
	out := logout(sessionCookie(t, w), idToken)
	var body struct {
		LogoutURL string `json:"logout_url"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &body) != nil {
		t.Fatalf("SSO logout: %d %s", out.Code, out.Body.String())
	}
	target, err := url.Parse(body.LogoutURL)
	if err != nil || !strings.HasPrefix(body.LogoutURL, idp.server.URL+"/logout?") || target.Query().Get("client_id") != "meerkat" ||
		target.Query().Get("id_token_hint") != idToken.Value || target.Query().Get("post_logout_redirect_uri") != "https://"+a.host+"/" {
		t.Fatalf("logout URL: %s", body.LogoutURL)
	}
	login := a.publicRequest("POST", "/api/v1/session", "198.51.100.21", chromeOnMac, adminLogin)
	if out := logout(sessionCookie(t, login)); out.Code != http.StatusNoContent {
		t.Fatalf("password logout: %d %s", out.Code, out.Body.String())
	}
	a.store.background.Wait()
}

// Only a provider-vouched, plain-ASCII email identifies an administrator.
func TestSSORequiresAVerifiedASCIIEmail(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = []string{"@example.com"}, nil })
	for name, verified := range map[string]any{"missing": nil, "zero": 0, "string FALSE": "FALSE", "object": map[string]any{}, "false": false} {
		claims := map[string]any{"email": "admin@example.com"}
		if verified != nil {
			claims["email_verified"] = verified
		}
		if w := a.ssoSignIn(t, idp, claims); ssoError(w) != "not_allowed" {
			t.Fatalf("email_verified %s: %d %s", name, w.Code, w.Header().Get("Location"))
		}
		a.clock.advance(loginWindow)
	}
	// U+0130 lowercases to an ASCII i in Go and would match admin@example.com.
	if w := a.ssoSignIn(t, idp, map[string]any{"email": "admİn@example.com", "email_verified": true}); ssoError(w) != "not_allowed" {
		t.Fatalf("non-ASCII email: %d %s", w.Code, w.Header().Get("Location"))
	}
	a.store.background.Wait()
}

// The first single sign-on binds an administrator to that provider account;
// another account that later shows the same email is refused.
func TestSSOBindsAdministratorsToTheirProviderAccount(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t) // password administrator admin@example.com
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, func(s *control.SSO) { s.AllowedUsers, s.AllowedGroups = []string{"admin@example.com"}, nil })
	existing, err := a.db.VerifyCredentials(context.Background(), "admin@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	w := a.ssoSignIn(t, idp, map[string]any{"email": "admin@example.com", "email_verified": true, "sub": "owner"})
	if w.Code != 200 {
		t.Fatalf("first SSO sign-in: %d %s", w.Code, w.Header().Get("Location"))
	}
	if event := a.lastAudit(t); event.Action != "session.login" || event.ResourceID != existing.ID {
		t.Fatalf("SSO sign-in not linked to the existing administrator: %+v", event)
	}
	if w := a.ssoSignIn(t, idp, map[string]any{"email": "admin@example.com", "email_verified": true, "sub": "someone-else"}); ssoError(w) != "not_allowed" {
		t.Fatalf("another account with the same email: %d %s", w.Code, w.Header().Get("Location"))
	}
	if event := a.lastAudit(t); event.Action != "session.login_failed" || !strings.Contains(string(event.Detail), "identity_mismatch") {
		t.Fatalf("mismatch audit: %s %s", event.Action, event.Detail)
	}
	if w := a.ssoSignIn(t, idp, map[string]any{"email": "admin@example.com", "email_verified": true, "sub": "owner"}); w.Code != 200 {
		t.Fatalf("the bound account again: %d %s", w.Code, w.Header().Get("Location"))
	}
	a.store.background.Wait()
}

func TestBootstrapTokenCannotAddAdministratorsBesideEnvironmentSSO(t *testing.T) {
	a := newAlertFixture(t)
	env, _ := control.SSO{Enabled: true, Issuer: "https://idp.example.com", ClientID: "meerkat", Scopes: []string{"openid"}, AllowedUsers: []string{"a@example.com"}, PasswordLogin: true}.Normalize()
	a.db.SetDefaults(control.Defaults{SSO: &env})
	if w := request(a.store.Handler(), "POST", "/api/v1/setup/admin", a.admin, adminLogin); w.Code != 409 || errorCode(t, w) != "setup_complete" {
		t.Fatalf("setup with SSO from the environment: %d %s", w.Code, w.Body.String())
	}
}

// A flood of starts cannot cancel sign-ins already in flight.
func TestSSOPendingSignInsAreRationed(t *testing.T) {
	var st ssoState
	now := time.Now()
	victim := ssoPending{client: "victim", expires: now.Add(ssoStateTTL)}
	if !st.put("victim-state", victim, now) {
		t.Fatal("first sign-in refused")
	}
	for i := range maxSSOPerClient + 5 {
		if !st.put(fmt.Sprint("attacker-", i), ssoPending{client: "attacker", expires: now.Add(ssoStateTTL)}, now) {
			t.Fatalf("one client's own sign-ins refused at %d", i)
		}
	}
	mine := 0
	for _, entry := range st.pending {
		if entry.client == "attacker" {
			mine++
		}
	}
	if mine != maxSSOPerClient {
		t.Fatalf("one client holds %d sign-ins", mine)
	}
	for i := 0; len(st.pending) < maxSSOPending; i++ {
		st.put(fmt.Sprint("crowd-", i), ssoPending{client: fmt.Sprint("client-", i), expires: now.Add(ssoStateTTL)}, now)
	}
	if st.put("late", ssoPending{client: "late", expires: now.Add(ssoStateTTL)}, now) {
		t.Fatal("a full table admitted another sign-in")
	}
	if _, ok := st.take("victim-state", now); !ok {
		t.Fatal("the victim's sign-in was evicted")
	}
}

func TestProviderRedirectsStayOnHTTPS(t *testing.T) {
	plain, _ := http.NewRequest("GET", "http://127.0.0.1:8123/private", nil)
	secure, _ := http.NewRequest("GET", "https://idp.example.com/realms/x", nil)
	if oidcClient.CheckRedirect(plain, []*http.Request{secure}) == nil {
		t.Fatal("followed a redirect to plain HTTP")
	}
	if oidcClient.CheckRedirect(secure, []*http.Request{secure}) != nil {
		t.Fatal("refused an https redirect")
	}
}
