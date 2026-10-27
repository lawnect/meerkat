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

// With the log on, every refusal and sign-in asked for is logged, and
// requests let in that name a person or a token; anonymous ones aren't.
// The console reads the log filtered and in pages, and turns it on and off.
func TestAccessLog(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	var created struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	policies := func(users ...string) {
		f.setPolicies(t,
			control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/ci/"}}, RequireToken: true, Tokens: []string{created.Token.ID}},
			control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/v1/"}}},
			control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/internal/"}}},
			control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: users},
		)
	}
	// Both sign in; then only alice may pass.
	policies("@corp.example.com")
	alice, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	bob, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("bob@corp.example.com")))
	policies("alice@corp.example.com")
	read := func(query string) logView[accessEntry] {
		t.Helper()
		var view logView[accessEntry]
		w := request(h, "GET", "/api/v1/observe/access-log"+query, f.admin, nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
			t.Fatalf("log%s: %d %s", query, w.Code, w.Body.String())
		}
		return view
	}
	// Off until turned on: nothing is logged.
	f.visit("app.example.com", "/workspace")
	if view := read(""); view.Settings != control.DefaultLogSettings() || !view.Available || len(view.Entries) != 0 || view.SizeBytes == 0 {
		t.Fatalf("off: %+v", view)
	}
	on := map[string]any{"enabled": true, "max_mib": 64, "retention_days": 7}
	if w := request(h, "PUT", "/api/v1/observe/access-log", f.admin, on); w.Code != 200 || !strings.Contains(w.Body.String(), `"settings":{"enabled":true,"max_mib":64,"retention_days":7}`) {
		t.Fatalf("turn on: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAudit(t); event.Action != "observe.log_settings" || string(event.Detail) != `{"enabled":true,"log":"access","max_mib":64,"retention_days":7}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	if limits := f.store.logs.access.log.Limits(); limits.MaxBytes != 64<<20 || limits.Retention != 7*24*time.Hour {
		t.Fatalf("limits: %+v", limits)
	}
	for _, body := range []map[string]any{{"enabled": true, "max_mib": 8, "retention_days": 7}, {"enabled": true, "max_mib": 64, "retention_days": 0}, {"enabled": true}} {
		if w := request(h, "PUT", "/api/v1/observe/access-log", f.admin, body); w.Code != 400 {
			t.Fatalf("settings %v: %d", body, w.Code)
		}
	}
	ci := func(path, value string) {
		r := httptest.NewRequest("POST", "https://app.example.com"+path, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.9")
		r.Header.Set(accessTokenHeader, value)
		f.handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	f.visit("app.example.com", "/workspace")
	f.visit("app.example.com", "/v1/models")
	ci("/ci/run", created.Value)
	ci("/workspace", "junk.secret")
	f.visit("app.example.com", "/internal/x", alice)
	f.visit("app.example.com", "/workspace", alice)
	f.visit("app.example.com", "/workspace", bob)
	view := read("")
	var got []string
	for _, e := range view.Entries {
		got = append(got, strings.Join([]string{e.Method, e.Path, e.Decision, e.Reason, e.Email, e.Token}, " "))
	}
	want := []string{
		"GET /workspace denied not_allowed bob@corp.example.com ",
		"GET /workspace passed session alice@corp.example.com ",
		"GET /internal/x denied blocked  ",
		"POST /workspace denied token_invalid  ",
		"POST /ci/run passed bypass  " + created.Token.ID,
		"GET /workspace challenged no_session  ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if e := view.Entries[3]; e.Client != "198.51.100.9" || e.Host != "app.example.com" || e.Policy == "" || e.At.IsZero() {
		t.Fatalf("entry: %+v", e)
	}
	// Filtered, and in pages.
	if view := read("?decision=denied"); len(view.Entries) != 3 || view.Total != 3 {
		t.Fatalf("denied: %+v", view.Entries)
	}
	if view := read("?policy=" + view.Entries[0].Policy); len(view.Entries) != 4 {
		t.Fatalf("one policy: %+v", view.Entries)
	}
	first := read("?per_page=4")
	if len(first.Entries) != 4 || first.Total != 6 {
		t.Fatalf("first page: %+v", first)
	}
	if next := read("?per_page=4&page=2"); len(next.Entries) != 2 || next.Total != 6 || next.Entries[0].ID >= first.Entries[3].ID {
		t.Fatalf("second page: %+v", next)
	}
	for _, query := range []string{"?decision=maybe", "?page=0", "?page=x", "?per_page=0", "?per_page=201"} {
		if w := request(h, "GET", "/api/v1/observe/access-log"+query, f.admin, nil); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	// Off again: nothing more is logged; what was stays.
	if w := request(h, "PUT", "/api/v1/observe/access-log", f.admin, map[string]any{"enabled": false, "max_mib": 64, "retention_days": 7}); w.Code != 200 {
		t.Fatalf("turn off: %d", w.Code)
	}
	f.visit("app.example.com", "/internal/y")
	if view := read(""); view.Settings.Enabled || len(view.Entries) != 6 {
		t.Fatalf("off again: %d entries", len(view.Entries))
	}
	if w := request(h, "GET", "/api/v1/observe/access-log", "", nil); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
}
