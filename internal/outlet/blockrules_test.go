package outlet

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

func TestBlockRulesMatchNormalizedPaths(t *testing.T) {
	s := &Store{}
	rules := control.DefaultBlockRules()
	rules.Rules[2].Enabled = true
	rules.Rules[2].Hosts = []string{"inc.example.com"}
	rules.Rules = append(rules.Rules, control.BlockRule{ID: "ends", Name: "ends", Enabled: true, Hosts: []string{"inc.example.com"},
		Conditions: []control.BlockCondition{{Operator: control.BlockEndsWith, Value: ".old"}, {Operator: control.BlockEndsWith, Value: "/private/"}}})
	rules.Rules = append(rules.Rules, control.BlockRule{ID: "wild", Name: "wild", Enabled: true, Hosts: []string{"*.Corp.Example.com"},
		Conditions: []control.BlockCondition{{Operator: control.BlockEquals, Value: "/internal"}}})
	rules, ok := rules.Normalize()
	if !ok {
		t.Fatal("rules do not normalize")
	}
	s.applyBlockRules(rules)
	for _, c := range []struct {
		host, path string
		want       string
	}{
		{"app.example.com", "/.env", "honeypot"},
		{"app.example.com", "/.git/config", "honeypot"},
		{"app.example.com", "//.env", "honeypot"},
		{"app.example.com", "/a/../.ENV", "honeypot"},
		{"app.example.com", "/.htaccess", "hidden-files"},
		{"app.example.com", "/.hidden/x", "hidden-files"},
		{"app.example.com", "/.well-known/acme-challenge/x", ""},
		{"app.example.com", "/.well-known", ""},
		{"app.example.com", "/index.php", ""},
		{"app.example.com", "/app.js", ""},
		{"app.example.com", "/", ""},
		{"inc.example.com", "/index.PHP", "scanners"},
		{"inc.example.com", "/x.php;jsessionid=1", "scanners"},
		{"inc.example.com", "/x.php.", "scanners"},
		{"inc.example.com", "/x.php\x00.jpg", "scanners"},
		{"inc.example.com", "/config.yml", "scanners"},
		{"inc.example.com", "/cgi-bin", "scanners"},
		{"inc.example.com", "/cgi-bin/test.sh", "scanners"},
		{"inc.example.com", "/blog/wp-login", "scanners"},
		{"inc.example.com", "/setup/", "scanners"},
		{"inc.example.com", "/setup", "scanners"},
		{"inc.example.com", "/setup.html", ""},
		{"inc.example.com", "/PhpMyAdmin/", "scanners"},
		{"inc.example.com", "/assets/app.js", ""},
		{"inc.example.com", "/db.OLD", "ends"},
		{"inc.example.com", "/private", "ends"},
		{"app.example.com", "/db.old", ""},
		{"git.corp.example.com", "/internal", "wild"},
		{"a.b.corp.example.com", "/internal/", "wild"},
		{"corp.example.com", "/internal", ""},
		{"xcorp.example.com", "/internal", ""},
	} {
		got := ""
		if rule := s.blockedBy(c.host, c.path); rule != nil {
			got = rule.id
		}
		if got != c.want {
			t.Errorf("%s%s: blocked by %q, want %q", c.host, c.path, got, c.want)
		}
	}
}

// A path disguised with trailing dots or spaces in a segment matches as
// the server that drops them reads it; a pattern holds with or without the
// trailing slash; conditions that only make sense on the raw path, such as
// "../", match nothing.
func TestBlockRulesCompareTrimmedPaths(t *testing.T) {
	s := &Store{}
	rules := control.DefaultBlockRules()
	for i, c := range []control.BlockCondition{
		{Operator: control.BlockContains, Value: "../"},
		{Operator: control.BlockEndsWith, Value: "/.."},
		{Operator: control.BlockMatches, Value: "^/wp-admin/"},
		{Operator: control.BlockStartsWith, Value: "/v2./"},
		{Operator: control.BlockEquals, Value: "/private"},
	} {
		rules.Rules = append(rules.Rules, control.BlockRule{ID: fmt.Sprint("r", i), Name: "r", Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{c}})
	}
	rules, ok := rules.Normalize()
	if !ok {
		t.Fatal("rules do not normalize")
	}
	s.applyBlockRules(rules)
	for path, want := range map[string]string{
		"/index.html":  "",
		"/":            "",
		"/wp-admin":    "r2",
		"/wp-admin./x": "r2",
		"/v2/secret":   "r3",
		"/v2./secret":  "r3",
		"/private":     "r4",
		"/private.":    "r4",
		"/private. /":  "r4",
		"/.../secret":  "hidden-files",
		"/. /secret":   "hidden-files",
		"/.env.":       "honeypot",
		"/.envrc":      "honeypot",
		"/.htaccess":   "hidden-files",
	} {
		got := ""
		if rule := s.blockedBy("app.example.com", path); rule != nil {
			got = rule.id
		}
		if got != want {
			t.Errorf("%s: blocked by %q, want %q", path, got, want)
		}
	}
}

func TestBlockRulesGuardPublicAddressesOnly(t *testing.T) {
	var served atomic.Int64
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
	}))
	f.route(t, 0, "app.example.com")
	get := func(host, path string) int {
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.Path, r.Host = path, host
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	if code := get("app.example.com", "/.env"); code != http.StatusForbidden {
		t.Fatalf("hidden file: %d", code)
	}
	if code := get("app.example.com", "/.htaccess"); code != http.StatusForbidden {
		t.Fatalf("hidden file: %d", code)
	}
	if code := get("app.example.com", "/.well-known/security.txt"); code != 200 {
		t.Fatalf("well-known path: %d", code)
	}
	if code := get(f.adminHost, "/.env"); code == http.StatusForbidden {
		t.Fatal("rules applied to the management host")
	}
	if n := served.Load(); n != 1 {
		t.Fatalf("service saw %d requests, want only the allowed one", n)
	}

	w := request(f.store.Handler(), "GET", "/api/v1/security/block-rules", f.admin, nil)
	var view blockRulesView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Hits["honeypot"] != 1 || view.Hits["hidden-files"] != 1 || len(view.Rules) != 4 || !view.Rules[0].Ban || len(view.Addresses) != 1 || view.Addresses[0] != "app.example.com" {
		t.Fatalf("rules view: %d %s", w.Code, w.Body.String())
	}
	// Turning a rule off lets the path through and keeps its count.
	view.Rules[0].Enabled = false
	view.Rules[1].Enabled = false
	w = request(f.store.Handler(), "PUT", "/api/v1/security/block-rules", f.admin, map[string]any{"rules": view.Rules})
	if w.Code != 200 || get("app.example.com", "/.env") != 200 {
		t.Fatalf("disabled rule: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Hits["honeypot"] != 1 || view.Hits["hidden-files"] != 1 {
		t.Fatalf("hits after an edit: %s", w.Body.String())
	}
	bad := request(f.store.Handler(), "PUT", "/api/v1/security/block-rules", f.admin, map[string]any{"rules": []map[string]any{{"id": "", "name": "x", "enabled": true, "hosts": []string{}, "conditions": []map[string]string{{"operator": "matches", "value": "(?!x)"}}}}})
	if bad.Code != 400 || errorCode(t, bad) != "invalid_block_rules" {
		t.Fatalf("invalid rules: %d %s", bad.Code, bad.Body.String())
	}
}

func BenchmarkBlockedBy(b *testing.B) {
	s := &Store{}
	rules := control.DefaultBlockRules()
	for i := range rules.Rules {
		rules.Rules[i].Enabled = true
	}
	rules, _ = rules.Normalize()
	s.applyBlockRules(rules)
	for b.Loop() {
		if s.blockedBy("app.example.com", "/api/v1/photos/2026/10/IMG_1234.jpg") != nil {
			b.Fatal("blocked")
		}
	}
}
