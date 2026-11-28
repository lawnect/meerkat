package outlet

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// headersSeen answers with the request headers the app got, as JSON.
func headersSeen() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(r.Header)
	})
}

// A client can't pass the app a header that names an address, a host, a
// scheme or a path under another name: not as one a CDN or a proxy would set,
// and not with "_" for "-", which WSGI and CGI servers read as the same.
func TestAppsGetNoForgeableClientHeaders(t *testing.T) {
	f := newProxyFixture(t, headersSeen())
	f.route(t, 0, "app.example.com")
	forged := []string{
		// With "_" for "-".
		"X_Real_Ip", "X_Forwarded_For", "X_Forwarded_Host", "X_Forwarded_Proto", "Cf_Connecting_Ip", "True_Client_Ip", "X_Meerkat_Access_Email",
		"x_real_ip", "Forwarded_For", "X-Forwarded_For", "X_Custom",
		// Under names other proxies, CDNs and frameworks read.
		"X-Original-URL", "X-Rewrite-URL", "X-Forwarded-Scheme", "X-Forwarded-Prefix", "X-Forwarded-Server", "X-Original-Host", "X-Host",
		"X-Cluster-Client-IP", "Client-IP", "Fastly-Client-IP", "Fly-Client-IP", "X-Envoy-External-Address", "X-Azure-ClientIP",
		"X-AppEngine-User-IP", "DO-Connecting-IP", "Forwarded-For", "X-Forwarded", "X-Original-Forwarded-For",
		// Already dropped.
		"X-Real-IP", "CF-Connecting-IP", "CF-Connecting-IPv6", "True-Client-IP", "X-Client-IP", "Forwarded", "X-Forwarded-Ssl", "X-Url-Scheme",
	}
	// Read as the server reads a request, so names come spelled as Go does.
	raw := "GET / HTTP/1.1\r\nHost: app.example.com\r\nX-Forwarded-For: 198.51.100.7\r\nX-Custom-Header: kept\r\nX-HTTP-Method-Override: kept\r\n"
	for _, name := range forged {
		raw += name + ": 6.6.6.6\r\n"
	}
	r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw + "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	r.RemoteAddr = "192.0.2.1:4000"
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var got http.Header
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for name, values := range got {
		if strings.Contains(name, "_") || strings.Contains(strings.Join(values, ","), "6.6.6.6") {
			t.Errorf("the app got %s: %v", name, values)
		}
	}
	// The addresses the Outlet vouches for, and what apps use as sent.
	if got.Get("X-Real-Ip") != "198.51.100.7" || got.Get("X-Forwarded-Host") != "app.example.com" || got.Get("X-Forwarded-Proto") != "https" {
		t.Errorf("the app got %v", got)
	}
	if got.Get("X-Custom-Header") != "kept" || got.Get("X-Http-Method-Override") != "kept" {
		t.Errorf("headers apps use were dropped: %v", got)
	}
}
