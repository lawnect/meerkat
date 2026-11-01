package outlet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

// Public addresses send HSTS by default, on an app's answers and the
// Outlet's own. An app's own header is the only one browsers see. Turned
// off, only the management host sends it; plain HTTP never does.
func TestHSTSOnPublicAddresses(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if own := r.URL.Query().Get("hsts"); own != "" {
			w.Header().Set("Strict-Transport-Security", own)
		}
		_, _ = io.WriteString(w, "app")
	}))
	f.route(t, 0, "app.example.com")
	// Each request from its own address: the block rule bans the client.
	client := 0
	send := func(proto, host, target string) *httptest.ResponseRecorder {
		t.Helper()
		client++
		r := httptest.NewRequest("GET", target, nil)
		r.Host = host
		r.Header.Set("X-Forwarded-Proto", proto)
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", client))
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w
	}
	expect := func(w *httptest.ResponseRecorder, code int, want ...string) {
		t.Helper()
		if got := w.Header().Values(hstsHeader); w.Code != code || !slices.Equal(got, want) {
			t.Fatalf("%d %q, want %d %q", w.Code, got, code, want)
		}
	}
	expect(send("https", "app.example.com", "/"), 200, hstsValue)
	expect(send("https", "app.example.com", "/?hsts=max-age%3D60"), 200, "max-age=60")
	expect(send("https", "app.example.com", "/?hsts=max-age%3D0"), 200, "max-age=0")
	// The Outlet's own answer: the default hidden-files block rule.
	expect(send("https", "app.example.com", "/.env"), 403, hstsValue)
	expect(send("https", f.adminHost, "/api/v1/setup/status"), 200, hstsValue)
	expect(send("http", "app.example.com", "/"), 308)

	h := f.store.Handler()
	if w := request(h, "PUT", "/api/v1/security/hsts", f.admin, control.HSTS{Enabled: false}); w.Code != 200 {
		t.Fatalf("turn off: %d %s", w.Code, w.Body.String())
	}
	expect(send("https", "app.example.com", "/"), 200)
	expect(send("https", "app.example.com", "/.env"), 403)
	expect(send("https", "app.example.com", "/?hsts=max-age%3D60"), 200, "max-age=60")
	// The management console keeps it whatever the setting.
	expect(send("https", f.adminHost, "/api/v1/setup/status"), 200, hstsValue)
}

func TestHSTSSettingsAPI(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	read := func(w *httptest.ResponseRecorder) control.HSTS {
		t.Helper()
		var got control.HSTS
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		return got
	}
	if got := read(request(h, "GET", "/api/v1/security/hsts", a.admin, nil)); !got.Enabled {
		t.Fatalf("default: %+v", got)
	}
	before := len(a.auditLog(t))
	for _, bad := range []any{map[string]any{}, map[string]any{"enabled": "yes"}} {
		if w := request(h, "PUT", "/api/v1/security/hsts", a.admin, bad); w.Code != 400 {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body.String())
		}
	}
	if len(a.auditLog(t)) != before || !a.store.hsts.Load() {
		t.Fatal("a bad request changed something")
	}
	if got := read(request(h, "PUT", "/api/v1/security/hsts", a.admin, control.HSTS{Enabled: false})); got.Enabled {
		t.Fatalf("saved: %+v", got)
	}
	if got := read(request(h, "GET", "/api/v1/security/hsts", a.admin, nil)); got.Enabled || a.store.hsts.Load() {
		t.Fatalf("read back: %+v, running %v", got, a.store.hsts.Load())
	}
	if e := a.lastAudit(t); e.Action != "security.hsts" || e.ResourceType != "security" || e.ResourceID != "hsts" || string(e.Detail) != `{"enabled":false}` {
		t.Fatalf("audit = %s %s/%s %s", e.Action, e.ResourceType, e.ResourceID, e.Detail)
	}
	if w := request(h, "GET", "/api/v1/security/hsts", "", nil); w.Code != 401 {
		t.Fatalf("without credentials: %d", w.Code)
	}
}
