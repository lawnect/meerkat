package outlet

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// The pages the Outlet shows on a protected address (sign-in required,
// denied, sign out) hold a button or a link: no other site may frame them.
func TestAccessPagesCannotBeFramed(t *testing.T) {
	f := newAccessFixture(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"other.example.com"}, Conditions: conditions()},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}},
	)
	api := httptest.NewRequest("POST", "https://app.example.com/api", nil)
	api.Host = "app.example.com"
	api.Header.Set("X-Forwarded-For", "198.51.100.30")
	api.Header.Set("Accept", "application/json")
	required := httptest.NewRecorder()
	f.handler.ServeHTTP(required, api)

	const policy = "default-src 'none'; frame-ancestors 'none'"
	for name, c := range map[string]struct {
		w    *httptest.ResponseRecorder
		code int
	}{
		"sign-in required": {required, http.StatusUnauthorized},
		"blocked":          {f.visit("other.example.com", "/"), http.StatusForbidden},
		"sign out":         {f.visit("app.example.com", accessPathPrefix+"logout"), http.StatusOK},
		"unknown address":  {f.visit(f.adminHost, accessStartPath+"?host=nowhere.example.com&return=/&bind="+Hash("x")), http.StatusNotFound},
	} {
		if c.w.Code != c.code || c.w.Header().Get("Content-Security-Policy") != policy {
			t.Errorf("%s: %d, Content-Security-Policy %q, want %d and %q", name, c.w.Code, c.w.Header().Get("Content-Security-Policy"), c.code, policy)
		}
	}
}
