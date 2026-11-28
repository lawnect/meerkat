package outlet

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// An app behind a proxy that decodes once more reads "/%252eenv" as /.env,
// as access policies already assume: block rules compare a path decoded
// twice too, and the honeypot sees what is behind the second decoding.
func TestBlockRulesSeeDoubleEncodedPaths(t *testing.T) {
	s := &Store{}
	s.applyBlockRules(control.DefaultBlockRules())
	// As the request's decoded path, once: what r.URL.Path holds.
	for path, want := range map[string]string{
		"/%2eenv":                      "honeypot",
		"/%2Eenv":                      "honeypot",
		"/api/%2egit/config":           "honeypot",
		"/backup/%2eaws%2fcredentials": "honeypot",
		"/%2e%2e/%2eenv":               "honeypot",
		"/x/%2e%2e/%2egit/HEAD":        "honeypot",
		"/%2ehtaccess":                 "hidden-files",
		"/%2e%73ecret/x":               "hidden-files",
		"/.well-known/%2eenv":          "honeypot",
		"/wp-config.%70hp":             "honeypot",
		// What is read once is read as before.
		"/.env":                       "honeypot",
		"/files/100%":                 "",
		"/files/100%25":               "",
		"/files/50%2525":              "",
		"/search/%41%42":              "",
		"/.well-known/%73ecurity.txt": "",
		"/.well-known/security.txt":   "",
		"/%2f.env%2f":                 "honeypot",
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

// Through the whole handler: a double-encoded scanner path is refused like
// the plain one, and a visitor is not banned for it by a browser's request.
func TestDoubleEncodedScannerPathsAreRefused(t *testing.T) {
	f := newBanFixture(t)
	for _, target := range []string{"/%252eenv", "/backup/%252egit/config", "/%252e%252e/%252eenv", "/.well-known/%252eenv"} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
	// What only looks like an escape is served.
	for _, target := range []string{"/files/100%25", "/files/50%2525", "/search/%2541"} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "203.0.113.9")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
}
