package outlet

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

// requestLogging turns the request log on or off, as the console does.
func (f proxyFixture) requestLogging(tb testing.TB, on bool) {
	tb.Helper()
	if w := request(f.store.Handler(), "PUT", "/api/v1/observe/request-log", f.admin, map[string]any{"enabled": on, "max_mib": 64, "retention_days": 7}); w.Code != 200 {
		tb.Fatalf("request log %v: %d %s", on, w.Code, w.Body.String())
	}
}

func (f proxyFixture) requestLog(tb testing.TB, query string) logView[requestEntry] {
	tb.Helper()
	return readLog[requestEntry](tb, f, "request-log", query)
}

// visit sends a public request from client with agent, as Caddy would
// forward it.
func (f proxyFixture) visit(method, host, target, client, agent string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-For", client)
	r.Header.Set("User-Agent", agent)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// requestLines names entries oldest first: status, class, cause, service,
// host, method, path, bytes.
func requestLines(entries []requestEntry) []string {
	lines := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		lines = append(lines, fmt.Sprintf("%d %s [%s] [%s] %s %s %s %d", e.Status, e.Class, e.Cause, e.Service, e.Host, e.Method, e.Path, e.Bytes))
	}
	return lines
}

// Every request to a routed address is kept, whatever became of it: what
// the client got, why the Outlet refused it or the service failed, the
// service tried, and who asked. Requests for other hosts are not.
func TestRequestLogKeepsEveryRequest(t *testing.T) {
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			http.NotFound(w, r)
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/empty":
		default:
			_, _ = io.WriteString(w, "hello")
		}
	})
	f := newProxyFixture(t, app, echo("idle"))
	f.route(t, 0, "app.example.com")
	f.route(t, 1, "idle.example.com")
	f.route(t, 0, "limited.example.com")
	f.setStatus(1, "down")
	f.limitHost(t, "limited.example.com")

	if view := f.requestLog(t, ""); view.Settings != control.DefaultLogSettings(control.LogRequests) || view.Settings.MaxMiB != 512 || !view.Available {
		t.Fatalf("defaults: %+v", view.Settings)
	}
	f.visit("GET", "app.example.com", "/before", "203.0.113.5", "curl/8")
	if view := f.requestLog(t, ""); view.Total != 0 {
		t.Fatalf("kept while off: %+v", view.Entries)
	}
	f.requestLogging(t, true)

	var want []string
	expect := func(what string, w *httptest.ResponseRecorder, code int, logged string) {
		t.Helper()
		if w.Code != code {
			t.Fatalf("%s: %d, want %d", what, w.Code, code)
		}
		if logged != "" {
			want = append(want, logged)
		}
	}
	expect("success", f.visit("GET", "app.example.com", "/page?token=secret", "203.0.113.5", "Mozilla/5.0"), 200, "200 2xx [] [svc-0] app.example.com GET /page 5")
	expect("nothing written", f.visit("POST", "app.example.com", "/empty", "203.0.113.5", "Mozilla/5.0"), 200, "200 2xx [] [svc-0] app.example.com POST /empty 0")
	expect("the app's 404", f.visit("GET", "app.example.com", "/missing", "198.51.100.7", "scanner"), 404, "404 4xx [] [svc-0] app.example.com GET /missing 19")
	expect("the app's 500", f.visit("GET", "app.example.com", "/500", "203.0.113.5", "Mozilla/5.0"), 500, "500 5xx [app_error] [svc-0] app.example.com GET /500 0")
	expect("a block rule", f.visit("GET", "app.example.com", "/.env", "198.51.100.7", "scanner"), 403, "403 4xx [trapped] [] app.example.com GET /.env 10")
	expect("a banned client", f.visit("GET", "app.example.com", "/", "198.51.100.7", "scanner"), 403, "403 4xx [banned] [] app.example.com GET / 10")
	expect("no live service", f.visit("GET", "idle.example.com", "/", "203.0.113.5", "Mozilla/5.0"), 404, "404 4xx [no_service] [] idle.example.com GET / 19")
	expect("within the rate limit", f.visit("GET", "limited.example.com", "/", "203.0.113.5", "Mozilla/5.0"), 200, "200 2xx [] [svc-0] limited.example.com GET / 5")
	expect("over the rate limit", f.visit("GET", "limited.example.com", "/", "203.0.113.5", "Mozilla/5.0"), 429, "429 4xx [rate_limited] [] limited.example.com GET / 0")
	expect("an unknown host", f.visit("GET", "nowhere.example.com", "/", "203.0.113.5", "Mozilla/5.0"), 404, "")
	expect("the management host", f.visit("GET", f.adminHost, "/no-such-page", "203.0.113.5", "Mozilla/5.0"), 404, "")

	view := f.requestLog(t, "")
	if got := requestLines(view.Entries); !slices.Equal(got, want) || view.Total != len(want) {
		t.Fatalf("log (%d):\n%s\nwant:\n%s", view.Total, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, e := range view.Entries {
		if e.Routine != (e.Status >= 400 && e.Status < 500) || e.At.IsZero() || e.Agent == "" || e.Client == "" {
			t.Fatalf("entry: %+v", e)
		}
	}
	for _, c := range []struct {
		query string
		total int
	}{
		{"?class=4xx", 5},
		{"?class=5xx", 1},
		{"?class=2xx", 3},
		{"?host=idle.example.com", 1},
		{"?path=/missing", 1},
		{"?source=198.51.100.7", 3},
		{"?source=198.51.100.7&class=4xx&host=app.example.com", 3},
		{"?q=scanner", 3},
		{"?q=svc-0", 5},
	} {
		if got := f.requestLog(t, c.query); got.Total != c.total || len(got.Entries) != c.total {
			t.Fatalf("%s: %d entries of %d, want %d", c.query, len(got.Entries), got.Total, c.total)
		}
	}
	for _, query := range []string{"?class=6xx", "?class=4XX", "?path=/" + strings.Repeat("a", 512), "?source=" + strings.Repeat("1", 256), "?per_page=201"} {
		if w := request(f.store.Handler(), "GET", "/api/v1/observe/request-log"+query, f.admin, nil); w.Code != 400 {
			t.Fatalf("%.40s: %d", query, w.Code)
		}
	}
	// A path is kept to 512 bytes, so a drill-down to one takes as many.
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/request-log?path=/"+strings.Repeat("a", 511), f.admin, nil); w.Code != 200 {
		t.Fatalf("a long path filter: %d", w.Code)
	}
}

// Through a real connection: an upgraded connection is kept as 101, and a
// stream still reaches the client a piece at a time.
func TestRequestLogUpgradesAndStreams(t *testing.T) {
	next := make(chan struct{})
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		// The rest waits for the client to have the first piece.
		select {
		case <-next:
		case <-time.After(5 * time.Second):
		}
		_, _ = io.WriteString(w, "second\n")
	})
	upgrade := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
		_ = rw.Flush()
		_, _ = io.Copy(conn, rw)
	})
	f := newProxyFixture(t, stream, upgrade)
	f.route(t, 0, "stream.example.com")
	f.route(t, 1, "ws.example.com")
	f.requestLogging(t, true)
	address := f.guardedServer(t, &connLimits{idle: 5 * time.Second})

	conn, reader := dial(t, address)
	_, _ = io.WriteString(conn, "GET /events HTTP/1.1\r\nHost: stream.example.com\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, len("first\n"))
	if _, err := io.ReadFull(res.Body, first); err != nil || string(first) != "first\n" {
		t.Fatalf("the first piece didn't arrive on its own: %q %v", first, err)
	}
	close(next)
	if rest, _ := io.ReadAll(res.Body); string(rest) != "second\n" {
		t.Fatalf("rest: %q", rest)
	}

	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if res, err := http.ReadResponse(reader, nil); err != nil || res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %v %v", res, err)
	}
	_, _ = io.WriteString(conn, "ping")
	got := make([]byte, 4)
	if _, err := io.ReadFull(reader, got); err != nil || string(got) != "ping" {
		t.Fatalf("upgraded connection: %q %v", got, err)
	}
	conn.Close()

	// The upgraded connection is logged once it ends.
	var view logView[requestEntry]
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if view = f.requestLog(t, ""); view.Total == 2 {
			break
		}
	}
	if got := requestLines(view.Entries); !slices.Equal(got, []string{"200 2xx [] [svc-0] stream.example.com GET /events 13", "101 1xx [] [svc-1] ws.example.com GET /socket 0"}) {
		t.Fatalf("log: %q", got)
	}
}

// One client can't fill the request log: 4xx past 600 a minute, and the
// rest past 6,000, are only counted.
func TestRequestLogBudget(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.route(t, 0, "app.example.com")
	f.requestLogging(t, true)
	for range requestBudget.routine + 5 {
		f.visit("GET", "app.example.com", "/.git/config", "198.51.100.9", "scanner")
	}
	view := f.requestLog(t, "?per_page=1")
	if view.Total != int(requestBudget.routine) || view.Suppressed != 5 {
		t.Fatalf("%d kept and %d suppressed", view.Total, view.Suppressed)
	}
	if requestBudget.routine <= refusalBudget.routine || requestBudget.notable <= refusalBudget.notable {
		t.Fatal("the request log keeps fewer than the failure log")
	}
}

// requestTop reads the request log's tally.
func (f proxyFixture) requestTop(tb testing.TB, query string) requestTopView {
	tb.Helper()
	var view requestTopView
	w := request(f.store.Handler(), "GET", "/api/v1/observe/request-log/top"+query, f.admin, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		tb.Fatalf("top%s: %d %s", query, w.Code, w.Body.String())
	}
	return view
}

// fillRequestLog stores entries in the request log as they were kept.
func (f proxyFixture) fillRequestLog(tb testing.TB, entries ...requestEntry) {
	tb.Helper()
	for i := range entries {
		entries[i].Class, entries[i].Routine = statusClass(entries[i].Status), entries[i].Status >= 400 && entries[i].Status < 500
		if entries[i].Source == "" {
			entries[i].Source = clientKey(entries[i].Client)
		}
	}
	if err := f.store.logs.requests.log.Append(context.Background(), entries); err != nil {
		tb.Fatal(err)
	}
}

// The tally counts what the log holds by endpoint or client, in a period,
// and sorts it by any of its measures.
func TestRequestTop(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.requestLogging(t, true)
	now := time.Now()
	hit := func(ago time.Duration, host, path, client string, status int) requestEntry {
		return requestEntry{At: now.Add(-ago), Host: host, Method: "GET", Path: path, Client: client, Status: status}
	}
	var entries []requestEntry
	for range 5 {
		entries = append(entries, hit(time.Minute, "app.example.com", "/", "203.0.113.1", 200))
	}
	entries = append(entries,
		hit(2*time.Minute, "app.example.com", "/", "203.0.113.2", 200),
		hit(3*time.Minute, "app.example.com", "/.env", "198.51.100.9", 403),
		hit(3*time.Minute, "app.example.com", "/wp-login.php", "198.51.100.9", 404),
		hit(3*time.Minute, "app.example.com", "/admin", "198.51.100.9", 404),
		hit(4*time.Minute, "api.example.com", "/v1/chat", "203.0.113.1", 502),
		hit(4*time.Minute, "api.example.com", "/v1/chat", "203.0.113.1", 200),
		hit(48*time.Hour, "api.example.com", "/v1/old", "203.0.113.3", 200),
		// One client rotating through its IPv6 addresses is one client.
		hit(5*time.Minute, "app.example.com", "/", "2001:db8:0:1a1::1", 200),
		hit(5*time.Minute, "app.example.com", "/", "2001:db8:0:1b2::2", 200),
		hit(5*time.Minute, "app.example.com", "/", "2001:db8:0:1c3::3", 200),
	)
	f.fillRequestLog(t, entries...)
	lines := func(view requestTopView) []string {
		out := make([]string, len(view.Rows))
		for i, r := range view.Rows {
			key := r.Client
			if key == "" {
				key = r.Host + r.Path
			}
			out[i] = fmt.Sprintf("%s %d %d %d %d", key, r.Requests, r.ClientErrors, r.ServerErrors, r.Distinct)
		}
		return out
	}
	for _, c := range []struct {
		query string
		want  []string
		total int
	}{
		{"", []string{"app.example.com/ 9 0 0 3", "api.example.com/v1/chat 2 0 1 1", "app.example.com/.env 1 1 0 1", "app.example.com/admin 1 1 0 1", "app.example.com/wp-login.php 1 1 0 1"}, 5},
		{"?period=all", []string{"app.example.com/ 9 0 0 3", "api.example.com/v1/chat 2 0 1 1", "api.example.com/v1/old 1 0 0 1", "app.example.com/.env 1 1 0 1", "app.example.com/admin 1 1 0 1", "app.example.com/wp-login.php 1 1 0 1"}, 6},
		{"?sort=client_errors.desc&per_page=2", []string{"app.example.com/.env 1 1 0 1", "app.example.com/admin 1 1 0 1"}, 5},
		{"?sort=client_errors.desc&per_page=2&page=3", []string{"app.example.com/ 9 0 0 3"}, 5},
		{"?sort=server_errors.desc&per_page=1", []string{"api.example.com/v1/chat 2 0 1 1"}, 5},
		{"?sort=distinct.desc&per_page=1", []string{"app.example.com/ 9 0 0 3"}, 5},
		{"?sort=last_at.desc&per_page=1", []string{"app.example.com/ 9 0 0 3"}, 5},
		{"?sort=requests.asc&per_page=1", []string{"app.example.com/.env 1 1 0 1"}, 5},
		{"?host=api.example.com&period=all", []string{"api.example.com/v1/chat 2 0 1 1", "api.example.com/v1/old 1 0 0 1"}, 2},
		{"?q=wp-", []string{"app.example.com/wp-login.php 1 1 0 1"}, 1},
		{"?by=client", []string{"203.0.113.1 7 0 1 2", "198.51.100.9 3 3 0 3", "2001:db8:0:100::/56 3 0 0 1", "203.0.113.2 1 0 0 1"}, 4},
		{"?by=client&sort=client_errors.desc&per_page=1", []string{"198.51.100.9 3 3 0 3"}, 4},
		{"?by=client&period=1h&page=9", []string{}, 4},
	} {
		view := f.requestTop(t, c.query)
		if got := lines(view); !slices.Equal(got, c.want) || view.Total != c.total || !view.Available || !view.Settings.Enabled {
			t.Fatalf("%s: %q (%d), want %q (%d)", c.query, got, view.Total, c.want, c.total)
		}
	}
	view := f.requestTop(t, "?period=1h")
	if view.From == nil || time.Since(*view.From) < 59*time.Minute || view.Rows[0].LastAt.Before(now.Add(-time.Minute-time.Second)) {
		t.Fatalf("period: %+v", view)
	}
	if view := f.requestTop(t, "?period=all"); view.From != nil {
		t.Fatalf("all has a start: %v", view.From)
	}
	for _, query := range []string{"?by=path", "?period=2h", "?sort=requests", "?sort=bytes.desc", "?sort=requests.up", "?per_page=201", "?format=json", "?utc_offset=900"} {
		if w := request(f.store.Handler(), "GET", "/api/v1/observe/request-log/top"+query, f.admin, nil); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
}

// download reads a CSV download, its rows after the header, and its header.
func download(tb testing.TB, f proxyFixture, path string) ([][]string, []string) {
	tb.Helper()
	w := request(f.store.Handler(), "GET", path, f.admin, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), `attachment; filename="meerkat-`) {
		tb.Fatalf("%s: %d %v %s", path, w.Code, w.Header(), w.Body.String())
	}
	body := w.Body.Bytes()
	if !bytes.HasPrefix(body, []byte("\xEF\xBB\xBF")) {
		tb.Fatalf("%s: no byte order mark", path)
	}
	rows, err := csv.NewReader(bytes.NewReader(body[3:])).ReadAll()
	if err != nil || len(rows) == 0 {
		tb.Fatalf("%s: %v", path, err)
	}
	return rows[1:], rows[0]
}

// A log downloads as CSV: every entry its filters match, newest first, in
// chunks, with times in the reader's zone and nothing a spreadsheet would
// run.
func TestLogExports(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.requestLogging(t, true)
	f.failureLogging(t, true)
	at := time.Date(2026, 10, 27, 11, 30, 0, 0, time.UTC)
	// The tally's default period is the last 24 hours, so the clock stays
	// beside the entries.
	f.store.now = func() time.Time { return at.Add(time.Hour) }
	var entries []requestEntry
	const n = 2*1000 + 3
	for i := range n {
		entries = append(entries, requestEntry{At: at.Add(time.Duration(i) * time.Second), Host: "app.example.com", Method: "GET", Path: fmt.Sprintf("/%d", i), Client: "203.0.113.1", Status: 200, Agent: "curl/8"})
	}
	entries = append(entries,
		requestEntry{At: at, Host: "app.example.com", Method: "GET", Path: "/x", Client: "198.51.100.9", Status: 404, Agent: `=HYPERLINK("http://evil.example","open")`},
		requestEntry{At: at, Host: "app.example.com", Method: "GET", Path: "/y", Client: "198.51.100.9", Status: 404, Agent: "+1", Service: "@svc"},
		requestEntry{At: at, Host: "app.example.com", Method: "GET", Path: "/z", Client: "198.51.100.9", Status: 404, Agent: "-2\tx"},
	)
	f.fillRequestLog(t, entries...)

	rows, header := download(t, f, "/api/v1/observe/request-log/export?source=203.0.113.1&utc_offset=540")
	if want := []string{"id", "at (UTC+09:00)", "host", "method", "path", "client", "source", "status", "class", "cause", "service", "duration_ms", "outlet_us", "upstream_us", "bytes", "agent"}; !slices.Equal(header, want) {
		t.Fatalf("header %q", header)
	}
	if len(rows) != n || rows[0][4] != fmt.Sprintf("/%d", n-1) || rows[n-1][4] != "/0" || rows[n-1][1] != "2026-10-27 20:30:00.000" {
		t.Fatalf("%d rows; first %q last %q", len(rows), rows[0], rows[len(rows)-1])
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row[0]] {
			t.Fatalf("row %s twice", row[0])
		}
		seen[row[0]] = true
	}
	rows, _ = download(t, f, "/api/v1/observe/request-log/export?class=4xx")
	agents := []string{}
	for _, row := range rows {
		agents = append(agents, row[15], row[10])
	}
	if want := []string{"'-2\tx", "", "'+1", "'@svc", `'=HYPERLINK("http://evil.example","open")`, ""}; !slices.Equal(agents, want) {
		t.Fatalf("cells: %q", agents)
	}
	if rows, header := download(t, f, "/api/v1/observe/request-log/export?q=nothing-matches"); len(rows) != 0 || header[1] != "at (UTC)" {
		t.Fatalf("empty: %q %q", rows, header)
	}
	if rows, header := download(t, f, "/api/v1/observe/request-log/top?format=csv&by=client&utc_offset=-330"); len(rows) != 2 || !slices.Equal(header, []string{"client", "requests", "client_errors", "server_errors", "endpoints", "last_at (UTC-05:30)"}) || rows[0][0] != "203.0.113.1" || rows[0][1] != fmt.Sprint(n) {
		t.Fatalf("tally: %q %q", header, rows)
	}
	if rows, header := download(t, f, "/api/v1/observe/request-log/top?format=csv&period=all&q=/x"); len(rows) != 1 || header[0] != "host" || rows[0][1] != "/x" {
		t.Fatalf("tally by endpoint: %q %q", header, rows)
	}

	if err := f.store.logs.failures.log.Append(context.Background(), []failureEntry{{At: at, Host: "app.example.com", Method: "GET", Path: "/.env", Client: "198.51.100.9", Cause: failTrapped, Status: 403, Routine: true}}); err != nil {
		t.Fatal(err)
	}
	if rows, header := download(t, f, "/api/v1/observe/failure-log/export?cause=refused"); len(rows) != 1 || header[6] != "cause" || rows[0][6] != "trapped" {
		t.Fatalf("failure log: %q %q", header, rows)
	}
	if rows, _ := download(t, f, "/api/v1/observe/failure-log/export?cause=service"); len(rows) != 0 {
		t.Fatalf("failure log filtered: %q", rows)
	}
	if rows, header := download(t, f, "/api/v1/observe/access-log/export"); len(rows) != 0 || header[6] != "policy" {
		t.Fatalf("access log: %q %q", header, rows)
	}

	for _, path := range []string{"/api/v1/observe/request-log/export?utc_offset=841", "/api/v1/observe/request-log/export?utc_offset=x", "/api/v1/observe/failure-log/export?cause=nope", "/api/v1/audit/export?utc_offset=-900"} {
		if w := request(f.store.Handler(), "GET", path, f.admin, nil); w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/v1/observe/request-log/export", "/api/v1/observe/request-log/top?format=csv", "/api/v1/audit/export"} {
		if w := request(f.store.Handler(), "GET", path, "wrong", nil); w.Code != 401 {
			t.Fatalf("%s without a session: %d", path, w.Code)
		}
	}
	logs := f.store.logs.requests
	f.store.logs.requests = nil
	defer func() { f.store.logs.requests = logs }()
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/request-log/export", f.admin, nil); w.Code != 409 {
		t.Fatalf("without a file: %d", w.Code)
	}
	if view := f.requestTop(t, ""); view.Available || len(view.Rows) != 0 {
		t.Fatalf("tally without a file: %+v", view)
	}
}

// The audit log downloads as the console lists it, filtered, past a page.
func TestAuditExport(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	for i := range control.MaxAuditLimit + 5 {
		f.requestLogging(t, i%2 == 0)
	}
	f.store.journal.flush()
	rows, header := download(t, f, "/api/v1/audit/export?action=observe.&utc_offset=540")
	if !slices.Equal(header, []string{"id", "at (UTC+09:00)", "action", "actor_email", "resource_type", "resource_id", "source", "ip", "user_agent", "detail"}) || len(rows) != control.MaxAuditLimit+5 {
		t.Fatalf("%d rows, header %q", len(rows), header)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		if seen[row[0]] || row[2] != "observe.log_settings" || row[5] != control.LogRequests || !strings.Contains(row[9], `"max_mib":64`) || !strings.HasPrefix(row[1], "20") {
			t.Fatalf("row %q", row)
		}
		seen[row[0]] = true
	}
	if rows, _ := download(t, f, "/api/v1/audit/export?action=connector."); len(rows) != 0 {
		t.Fatalf("filtered: %d rows", len(rows))
	}
}

// closingWriter closes a log once a response has begun, as a disk error
// would break it in the middle of a download.
type closingWriter struct {
	*httptest.ResponseRecorder
	close func()
}

func (w *closingWriter) Write(p []byte) (int, error) {
	if w.close != nil {
		w.close()
		w.close = nil
	}
	return w.ResponseRecorder.Write(p)
}

// An export that can't be read says so: with an error before the file
// begins, and by cutting the connection after, never as a short file that
// looks whole.
func TestLogExportFailsLoudly(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.requestLogging(t, true)
	var entries []requestEntry
	for i := range 2500 {
		entries = append(entries, requestEntry{At: time.Now(), Host: "app.example.com", Method: "GET", Path: fmt.Sprintf("/%d", i), Client: "203.0.113.1", Status: 200})
	}
	f.fillRequestLog(t, entries...)
	l := f.store.logs.requests
	export := func(w http.ResponseWriter) (aborted any) {
		defer func() { aborted = recover() }()
		r := httptest.NewRequest("GET", "/api/v1/observe/request-log/export", nil)
		r.Header.Set("Authorization", "Bearer "+f.admin)
		f.store.Handler().ServeHTTP(w, r)
		return nil
	}
	broken := &closingWriter{ResponseRecorder: httptest.NewRecorder(), close: func() { l.log.Close() }}
	if aborted := export(broken); aborted != http.ErrAbortHandler || broken.Code != 200 {
		t.Fatalf("broken midway: %v, %d", aborted, broken.Code)
	}
	w := httptest.NewRecorder()
	if aborted := export(w); aborted != nil || w.Code != 503 || !strings.Contains(w.Body.String(), "log_failed") {
		t.Fatalf("broken before: %v, %d %s", aborted, w.Code, w.Body.String())
	}
}

// A path that isn't UTF-8 is kept as JSON shows it, so two such paths are
// one endpoint, and the tally's row leads to their requests.
func TestRequestLogPathsAreUTF8(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.route(t, 0, "app.example.com")
	f.requestLogging(t, true)
	f.visit("GET", "app.example.com", "/x%FF", "203.0.113.5", "a")
	f.visit("GET", "app.example.com", "/x%FE", "203.0.113.5", "b")
	view := f.requestTop(t, "")
	if len(view.Rows) != 1 || view.Rows[0].Path != "/x�" || view.Rows[0].Requests != 2 {
		t.Fatalf("tally: %+v", view.Rows)
	}
	if got := f.requestLog(t, "?path=/x%EF%BF%BD"); got.Total != 2 {
		t.Fatalf("drill-down: %d", got.Total)
	}
}

// The console lists the release each Connector reported in its last
// heartbeat, and nothing before one.
func TestConnectorListShowsVersion(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	version := func() string {
		t.Helper()
		var list struct {
			Connectors []control.Connector `json:"connectors"`
		}
		w := request(f.store.Handler(), "GET", "/api/v1/connectors", f.admin, nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Connectors) != 1 {
			t.Fatalf("list: %d %s", w.Code, w.Body.String())
		}
		return list.Connectors[0].Version
	}
	f.store.mu.Lock()
	record := f.store.managed[f.connector.ID]
	record.LastHeartbeat = nil
	f.store.managed[f.connector.ID] = record
	f.store.mu.Unlock()
	if got := version(); got != "" {
		t.Fatalf("before a heartbeat: %q", got)
	}
	f.store.mu.Lock()
	record.LastHeartbeat = &contract.Heartbeat{Version: "0.5.2", ObservedAt: time.Now()}
	f.store.managed[f.connector.ID] = record
	f.store.mu.Unlock()
	if got := version(); got != "0.5.2" {
		t.Fatalf("after a heartbeat: %q", got)
	}
}

// A forwarded request's time splits into the Outlet's, until it is on a
// connection to the service, and the service's, to the first byte back;
// both go in the request log and in the hourly latencies. A request the
// Outlet refused isn't timed.
func TestLatencySplitsOutletAndUpstream(t *testing.T) {
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	})
	f := newProxyFixture(t, slow)
	f.route(t, 0, "app.example.com")
	f.requestLogging(t, true)
	if w := f.visit("GET", "app.example.com", "/", "203.0.113.5", "a"); w.Code != 200 {
		t.Fatalf("proxied: %d", w.Code)
	}
	if w := f.visit("GET", "app.example.com", "/.env", "203.0.113.6", "b"); w.Code != 403 {
		t.Fatalf("refused: %d", w.Code)
	}
	view := f.requestLog(t, "")
	if view.Total != 2 {
		t.Fatalf("%d entries", view.Total)
	}
	refused, proxied := view.Entries[0], view.Entries[1]
	if refused.OutletUS != 0 || refused.UpstreamUS != 0 {
		t.Fatalf("refused request timed: %+v", refused)
	}
	if proxied.UpstreamUS < 60_000 || proxied.OutletUS <= 0 || proxied.OutletUS > 50_000 || proxied.OutletUS+proxied.UpstreamUS > proxied.DurationMS*1000+1000 {
		t.Fatalf("proxied: outlet %d µs, upstream %d µs, total %d ms", proxied.OutletUS, proxied.UpstreamUS, proxied.DurationMS)
	}
	var series control.LatencySeries
	now := time.Now().UTC()
	query := fmt.Sprintf("?unit=hour&from=%s&to=%s&tz=UTC", now.Add(-time.Hour).Truncate(time.Hour).Format(time.RFC3339), now.Add(time.Hour).Truncate(time.Hour).Format(time.RFC3339))
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/latency"+query, f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &series) != nil {
		t.Fatalf("latency: %d %s", w.Code, w.Body.String())
	}
	if series.Outlet.Count != 1 || series.Upstream.Count != 1 || series.Upstream.AvgMS < 60 || series.Outlet.AvgMS > 50 || len(series.Buckets) != 2 {
		t.Fatalf("series: %+v", series)
	}
	// The log being off doesn't stop the timing.
	f.requestLogging(t, false)
	f.visit("GET", "app.example.com", "/", "203.0.113.5", "a")
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/latency"+query, f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &series) != nil || series.Upstream.Count != 2 {
		t.Fatalf("with the log off: %s", w.Body.String())
	}
	if w := request(f.store.Handler(), "GET", "/api/v1/observe/latency?unit=week", f.admin, nil); w.Code != 400 {
		t.Fatalf("bad range: %d", w.Code)
	}
}

// The services' time runs from the request fully sent to the final answer:
// a client's slow upload and a 1xx before the answer count for neither
// side, and the time a service that timed out took before a retry counts
// for the services, not the Outlet.
func TestLatencyCountsTheServicesTime(t *testing.T) {
	previous := upstreamHeaderTimeout
	upstreamHeaderTimeout = 200 * time.Millisecond
	defer func() { upstreamHeaderTimeout = previous }()
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/hints" {
			w.Header().Set("Link", "</a.css>; rel=preload")
			w.WriteHeader(http.StatusEarlyHints)
			time.Sleep(150 * time.Millisecond)
		}
		_, _ = io.WriteString(w, "ok")
	})
	f := newProxyFixture(t, hang, app)
	f.route(t, 1, "app.example.com")
	f.route(t, 0, "both.example.com")
	f.share(t, 1, "both.example.com")
	f.requestLogging(t, true)

	body, upload := io.Pipe()
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = upload.Write([]byte("x"))
		upload.Close()
	}()
	r := httptest.NewRequest("POST", "/upload", body)
	r.Host = "app.example.com"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	f.handler.ServeHTTP(httptest.NewRecorder(), r)
	f.visit("GET", "app.example.com", "/hints", "203.0.113.5", "a")
	f.visit("GET", "both.example.com", "/", "203.0.113.5", "a")

	view := f.requestLog(t, "")
	if view.Total != 3 {
		t.Fatalf("%d entries", view.Total)
	}
	got := map[string]requestEntry{}
	for _, e := range view.Entries {
		got[e.Host+e.Path] = e
	}
	upload1, hints, retried := got["app.example.com/upload"], got["app.example.com/hints"], got["both.example.com/"]
	if upload1.DurationMS < 300 || upload1.UpstreamUS > 100_000 || upload1.OutletUS > 100_000 {
		t.Fatalf("slow upload: total %d ms, outlet %d µs, upstream %d µs", upload1.DurationMS, upload1.OutletUS, upload1.UpstreamUS)
	}
	if hints.UpstreamUS < 150_000 {
		t.Fatalf("early hints: upstream %d µs", hints.UpstreamUS)
	}
	if retried.Status != 200 || retried.UpstreamUS < 200_000 || retried.OutletUS > 100_000 {
		t.Fatalf("retried: %d, outlet %d µs, upstream %d µs", retried.Status, retried.OutletUS, retried.UpstreamUS)
	}
}

// Reads that cost the Outlet answer only the console's own page: a request
// a tunneled app's page makes with the session cookie is refused.
func TestHeavyReadsRefuseOtherPages(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.requestLogging(t, true)
	for _, path := range []string{"/api/v1/observe/request-log/export", "/api/v1/observe/request-log/top", "/api/v1/observe/request-log", "/api/v1/audit/export"} {
		for site, want := range map[string]int{"same-site": 403, "cross-site": 403, "same-origin": 200, "none": 200, "": 200} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Authorization", "Bearer "+f.admin)
			if site != "" {
				r.Header.Set("Sec-Fetch-Site", site)
			}
			w := httptest.NewRecorder()
			f.store.Handler().ServeHTTP(w, r)
			if w.Code != want {
				t.Fatalf("%s from %q: %d", path, site, w.Code)
			}
		}
	}
}

// A client's upload that grows past the limit is refused, and the service
// it was on its way to isn't passed over for it.
func TestOversizedUploadLeavesTheServiceTrusted(t *testing.T) {
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body) })
	f := newProxyFixture(t, app)
	f.route(t, 0, "big.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{MaxUploadMB: 1})
	payload := bytes.Repeat([]byte("x"), 2<<20)
	if w := f.post("big.example.com", onlyReader{bytes.NewReader(payload)}, -1, "203.0.113.5"); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: %d", w.Code)
	}
	if _, suspect := f.store.suspect.Load(f.services[0].ID); suspect {
		t.Fatal("a client's oversized upload marked the service failed")
	}
}

// The minimum upload speed counts the time spent waiting on the client: a
// service that pauses before reading a body sent at once doesn't get the
// client cut for its own slowness.
func TestUploadSpeedCountsTheClientNotTheService(t *testing.T) {
	pause := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		n, _ := io.Copy(io.Discard, r.Body)
		_, _ = fmt.Fprint(w, n)
	})
	f := newProxyFixture(t, pause)
	f.route(t, 0, "up.example.com")
	address := f.guardedServer(t, &connLimits{idle: 500 * time.Millisecond, minRate: 10 << 20})
	conn, reader := dial(t, address)
	const size = 8 << 20
	go func() {
		_, _ = fmt.Fprintf(conn, "POST /upload HTTP/1.1\r\nHost: up.example.com\r\nContent-Length: %d\r\n\r\n", size)
		_, _ = conn.Write(bytes.Repeat([]byte("x"), size))
	}()
	if code, body := readResponse(t, conn, reader, 15*time.Second); code != 200 || body != fmt.Sprint(size) {
		t.Fatalf("upload: %d %q", code, body)
	}
}
