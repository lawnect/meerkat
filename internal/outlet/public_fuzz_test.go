package outlet

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FuzzPublicHandler feeds parseable but hostile requests to the public
// listener, routed and management hosts included. Nothing may panic, and the
// only server errors allowed are the proxy's own 502 and 504.
//
//	go test ./internal/outlet -run '^$' -fuzz FuzzPublicHandler -fuzztime 3m
func FuzzPublicHandler(f *testing.F) {
	for _, seed := range []string{
		"GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: APP.example.com:443\r\nX-Forwarded-For: 1.2.3.4, ::1, nonsense\r\n\r\n",
		"GET http://{admin}/api/v1/connectors HTTP/1.1\r\nHost: app.example.com\r\n\r\n",
		"GET /../../connector/v1/config HTTP/1.1\r\nHost: {admin}\r\nAuthorization: Bearer x\r\n\r\n",
		"GET /.well-known/meerkat/cert-ask?domain=app.example.com HTTP/1.1\r\nHost: outlet:8080\r\n\r\n",
		"GET /.well-known/meerkat/cert-ask?domain=%00..%2f HTTP/1.1\r\nHost: outlet:8080\r\n\r\n",
		"POST / HTTP/1.1\r\nHost: app.example.com\r\nTransfer-Encoding: chunked\r\n\r\n4\r\nabcd\r\n0\r\n\r\n",
		"POST /api/v1/session HTTP/1.1\r\nHost: {admin}\r\nContent-Type: application/json\r\nContent-Length: 9\r\n\r\n{\"email\":",
		"PUT /api/v1/outlet HTTP/1.1\r\nHost: {admin}\r\nContent-Type: application/json\r\nCookie: __Host-meerkat_session=%%%\r\nContent-Length: 2\r\n\r\n{}",
		"GET /api/v1/usage?from=x&to=y&tz=../../etc/passwd HTTP/1.1\r\nHost: {admin}\r\n\r\n",
		"GET /connector/v1/config HTTP/1.1\r\nHost: example.com\r\n\r\n",
		"OPTIONS * HTTP/1.1\r\nHost: app.example.com\r\n\r\n",
		"GET / HTTP/1.1\r\nHost: app.example.com\r\nConnection: Upgrade, X-Forwarded-For\r\nUpgrade: websocket\r\n\r\n",
	} {
		f.Add(seed)
	}
	p := newProxyFixture(f, echo("A"))
	if _, err := p.db.CreateHTTPRoute(context.Background(), p.services[0].ID, "app.example.com"); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		raw = strings.ReplaceAll(raw, "{admin}", p.adminHost)
		r, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
		if err != nil {
			return
		}
		r.RemoteAddr = "172.18.0.5:41234"
		w := httptest.NewRecorder()
		p.handler.ServeHTTP(w, r)
		if w.Code >= 500 && w.Code != http.StatusBadGateway && w.Code != http.StatusGatewayTimeout {
			t.Fatalf("%d for %q: %s", w.Code, raw, w.Body.String())
		}
	})
}
