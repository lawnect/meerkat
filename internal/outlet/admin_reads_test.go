package outlet

import (
	"net/http/httptest"
	"testing"
)

// The audit log, the alert settings and the single sign-on settings are read
// only by the console's own page, as the logs are (see ownPage): a tunneled
// app is same-site with the console and its pages carry the session cookie.
func TestAdminReadsRefuseCrossSiteRequests(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	get := func(path, site string) *httptest.ResponseRecorder {
		r := jsonRequest("GET", path, nil)
		r.AddCookie(cookie)
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/audit", "/api/v1/audit?limit=5&q=login", "/api/v1/alerts", "/api/v1/security/sso"} {
		for _, site := range []string{"cross-site", "same-site"} {
			if w := get(path, site); w.Code != 403 || errorCode(t, w) != "cross_origin_request" {
				t.Fatalf("%s from %s: %d %s", path, site, w.Code, w.Body.String())
			}
		}
		for _, site := range []string{"", "same-origin", "none"} {
			if w := get(path, site); w.Code != 200 {
				t.Fatalf("%s from %q: %d %s", path, site, w.Code, w.Body.String())
			}
		}
	}
}
