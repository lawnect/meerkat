package outlet

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// Sessions end with the provider they were signed in by: a changed issuer or
// client ID, or fewer hours, signs everyone out as "Sign everyone out" does,
// while a changed secret, scopes, claim or more hours leave them be.
func TestAccessSessionsEndWithTheirProvider(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	elsewhere := newFakeIdP(t)
	h := f.store.Handler()
	save := func(change map[string]any) {
		t.Helper()
		body := map[string]any{"enabled": true, "issuer": f.idp.server.URL, "client_id": "access", "scopes": []string{"openid", "email", "groups"}, "groups_claim": "groups", "session_hours": 8}
		for key, value := range change {
			body[key] = value
		}
		if w := request(h, "PUT", "/api/v1/security/access-provider", f.admin, body); w.Code != 200 {
			t.Fatalf("save %v: %d %s", change, w.Code, w.Body.String())
		}
	}
	signIn := func() *http.Cookie {
		t.Helper()
		session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
		return session
	}
	signedIn := func(session *http.Cookie) bool {
		t.Helper()
		w := f.visit("app.example.com", "/", session)
		if w.Code != 200 && w.Code != http.StatusFound {
			t.Fatalf("visit: %d", w.Code)
		}
		return w.Code == 200
	}

	session := signIn()
	for _, c := range []struct {
		name   string
		change map[string]any
	}{
		{"the same", nil},
		{"a new secret", map[string]any{"client_secret": "another-secret-9999"}},
		{"other scopes", map[string]any{"scopes": []string{"openid", "email"}}},
		{"another claim", map[string]any{"groups_claim": "roles"}},
		{"more hours", map[string]any{"groups_claim": "groups", "session_hours": 12}},
		{"the same hours", map[string]any{"session_hours": 12}},
	} {
		save(c.change)
		if !signedIn(session) {
			t.Fatalf("%s: the session ended", c.name)
		}
	}

	for _, c := range []struct {
		name   string
		change map[string]any
	}{
		{"fewer hours", map[string]any{"session_hours": 4}},
		{"another client", map[string]any{"client_id": "access-2"}},
		{"another provider", map[string]any{"issuer": elsewhere.server.URL}},
	} {
		save(map[string]any{"session_hours": 12})
		session := signIn()
		if !signedIn(session) {
			t.Fatalf("%s: no session before the change", c.name)
		}
		save(c.change)
		if signedIn(session) {
			t.Fatalf("%s: the session outlived the change", c.name)
		}
		if event := f.lastAudit(t); event.Action != "security.access_provider" || !strings.Contains(string(event.Detail), `"sessions_revoked":true`) {
			t.Fatalf("%s: audit: %s %s", c.name, event.Action, event.Detail)
		}
	}
	// A change that keeps them says so.
	save(map[string]any{"session_hours": 12})
	session = signIn()
	save(map[string]any{"session_hours": 12, "client_secret": "another-secret-9999"})
	if !signedIn(session) {
		t.Fatal("the session ended without a change")
	}
	if event := f.lastAudit(t); !strings.Contains(string(event.Detail), `"sessions_revoked":false`) {
		t.Fatalf("audit: %s", event.Detail)
	}
}
