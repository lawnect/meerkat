package outlet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// failureLogging turns the failure log on or off, as the console does.
func (f proxyFixture) failureLogging(tb testing.TB, on bool) {
	tb.Helper()
	if w := request(f.store.Handler(), "PUT", "/api/v1/observe/failure-log", f.admin, map[string]any{"enabled": on, "max_mib": 64, "retention_days": 7}); w.Code != 200 {
		tb.Fatalf("failure log %v: %d %s", on, w.Code, w.Body.String())
	}
}

// failureLog reads the failure log, with a query such as "?cause=blocked".
func (f proxyFixture) failureLog(tb testing.TB, query string) logView[failureEntry] {
	tb.Helper()
	var view logView[failureEntry]
	w := request(f.store.Handler(), "GET", "/api/v1/observe/failure-log"+query, f.admin, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		tb.Fatalf("failure log%s: %d %s", query, w.Code, w.Body.String())
	}
	return view
}

// failureLines names entries oldest first: cause, status, [service], host and
// path.
func failureLines(entries []failureEntry) []string {
	lines := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		lines = append(lines, fmt.Sprintf("%s %d [%s] %s %s", e.Cause, e.Status, e.Service, e.Host, e.Path))
	}
	return lines
}

// hit sends a public request as Caddy would forward it.
func (f proxyFixture) hit(method, host, target string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	r.Host = host
	r.Header.Set("X-Forwarded-For", "203.0.113.5")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// limitHost lets one request per second through to host, and no more.
func (f proxyFixture) limitHost(tb testing.TB, host string) {
	tb.Helper()
	rule := map[string]any{"host": host, "path_prefix": "/", "rps": 1, "burst": 1, "per": control.RateEndpointTotal}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/rate-limit", f.admin, rateLimitBody(map[string]any{"endpoints": []any{rule}})); w.Code != 200 {
		tb.Fatalf("rate limit: %d %s", w.Code, w.Body.String())
	}
}

// Every way a public request can fail or be turned away is logged with what
// the client got and the service involved; the ones that went well, and
// requests for addresses nobody routes, are not.
func TestFailureLogCauses(t *testing.T) {
	previous := upstreamHeaderTimeout
	upstreamHeaderTimeout = 200 * time.Millisecond
	defer func() { upstreamHeaderTimeout = previous }()
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		switch r.URL.Path {
		case "/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/503":
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	f := newProxyFixture(t, app, hang, echo("gone"), echo("idle"), app)
	for i, name := range []string{"app", "slow", "gone", "idle", "big"} {
		f.route(t, i, name+".example.com")
	}
	f.route(t, 0, "limited.example.com")
	f.upstreams[2].Close()
	f.setStatus(3, "down")
	f.setConnectionLimits(t, control.ConnectionLimits{MaxUploadMB: 1})
	f.limitHost(t, "limited.example.com")
	f.failureLogging(t, true)

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
	expect("success", f.hit("GET", "app.example.com", "/ok", nil), 200, "")
	expect("a 500", f.hit("GET", "app.example.com", "/500?token=secret", nil), 500, "app_error 500 [svc-0] app.example.com /500")
	expect("a 503", f.hit("POST", "app.example.com", "/503", strings.NewReader("{}")), 503, "app_error 503 [svc-0] app.example.com /503")
	expect("a service that never answers", f.hit("GET", "slow.example.com", "/", nil), 504, "timeout 504 [svc-1] slow.example.com /")
	expect("a service that is gone", f.hit("GET", "gone.example.com", "/", nil), 502, "unreachable 502 [svc-2] gone.example.com /")
	expect("an address without a live service", f.hit("GET", "idle.example.com", "/", nil), 404, "no_service 404 [] idle.example.com /")
	expect("an unknown host", f.hit("GET", "nowhere.example.com", "/", nil), 404, "")
	expect("the management host", f.hit("GET", f.adminHost, "/no-such-page", nil), 404, "")
	expect("a block rule", f.hit("GET", "app.example.com", "/.hidden", nil), 403, "blocked 403 [] app.example.com /.hidden")
	long := "/" + strings.Repeat("a", maxPublicPath)
	expect("a long path", f.hit("GET", "app.example.com", long, nil), 414, "too_long 414 [] app.example.com "+long[:512])
	payload := bytes.Repeat([]byte("x"), 2<<20)
	expect("an upload declared too large", f.post("big.example.com", bytes.NewReader(payload), int64(len(payload)), "203.0.113.5"), 413, "too_large 413 [] big.example.com /")
	expect("an upload that grows too large", f.post("big.example.com", onlyReader{bytes.NewReader(payload)}, -1, "203.0.113.5"), 413, "too_large 413 [svc-4] big.example.com /")
	expect("an upload at the limit", f.post("big.example.com", bytes.NewReader(payload[:1<<20]), 1<<20, "203.0.113.5"), 200, "")
	expect("within the rate limit", f.hit("GET", "limited.example.com", "/", nil), 200, "")
	expect("over the rate limit", f.hit("GET", "limited.example.com", "/", nil), 429, "rate_limited 429 [] limited.example.com /")

	view := f.failureLog(t, "")
	if got := failureLines(view.Entries); !slices.Equal(got, want) || view.Total != len(want) {
		t.Fatalf("log (%d):\n%s\nwant:\n%s", view.Total, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !view.Settings.Enabled || !view.Available || view.Dropped != 0 || view.Suppressed != 0 || view.SizeBytes == 0 {
		t.Fatalf("view: %+v", view)
	}
	// Refusals are routine, anyone can have them; the rest mean a service is in trouble. The log
	// can be asked for either group, or searched.
	for _, e := range view.Entries {
		if routine := slices.Contains([]string{"rate_limited", "blocked", "too_long", "too_large", "busy"}, e.Cause); e.Routine != routine {
			t.Fatalf("%s is routine: %v", e.Cause, e.Routine)
		}
	}
	for _, c := range []struct {
		query string
		total int
	}{
		{"?cause=service", 5},
		{"?cause=refused", 5},
		{"?cause=timeout", 1},
		{"?q=svc-", 5},
		{"?q=SVC-0", 2},
		{"?q=limited", 1},
		{"?cause=service&q=slow", 1},
		{"?cause=refused&q=big", 2},
		{"?cause=refused&q=svc-0", 0},
	} {
		if got := f.failureLog(t, c.query); got.Total != c.total || len(got.Entries) != c.total {
			t.Fatalf("%s: %d entries of %d, want %d", c.query, len(got.Entries), got.Total, c.total)
		}
	}
	entries := slices.Clone(view.Entries)
	slices.Reverse(entries)
	for i, e := range entries {
		if e.Client != "203.0.113.5" || e.At.IsZero() || time.Since(e.At) > time.Minute || i > 0 && e.ID <= entries[i-1].ID {
			t.Fatalf("entry %d: %+v", i, e)
		}
		switch e.Cause {
		case failTimeout:
			// It waited out the header timeout.
			if e.DurationMS < 150 || e.DurationMS > 5000 {
				t.Fatalf("timeout took %d ms", e.DurationMS)
			}
		case failBlocked, failTooLong, failNoService, failRateLimited:
			if e.DurationMS > 100 {
				t.Fatalf("%s took %d ms", e.Cause, e.DurationMS)
			}
		}
	}
	if entries[0].Method != "GET" || entries[1].Method != "POST" {
		t.Fatalf("methods: %s %s", entries[0].Method, entries[1].Method)
	}
}

// The connection limits turn a request away before the address is looked
// up; it is logged for an address that is routed, and not for any other.
func TestFailureLogTurnedAway(t *testing.T) {
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
	}))
	f.route(t, 0, "app.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{PerIPConcurrent: 1, TotalConcurrent: 1})
	f.failureLogging(t, true)
	var wg sync.WaitGroup
	wg.Go(func() {
		if code, _ := f.from("GET", "app.example.com", "203.0.113.1", ""); code != 200 {
			t.Errorf("admitted request: %d", code)
		}
	})
	<-arrived
	for _, c := range []struct {
		name, host, ip string
		code           int
	}{
		{"the same address again", "app.example.com", "203.0.113.1", http.StatusTooManyRequests},
		{"the same address, for an unrouted host", "nowhere.example.com", "203.0.113.1", http.StatusTooManyRequests},
		{"another address over the total", "app.example.com", "203.0.113.2", http.StatusServiceUnavailable},
	} {
		if code, _ := f.from("GET", c.host, c.ip, ""); code != c.code {
			t.Fatalf("%s: %d, want %d", c.name, code, c.code)
		}
	}
	close(release)
	wg.Wait()
	want := []string{"busy 429 [] app.example.com /", "busy 503 [] app.example.com /"}
	if got := failureLines(f.failureLog(t, "").Entries); !slices.Equal(got, want) {
		t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The rate limit turns requests away the same way.
	f.setConnectionLimits(t, control.ConnectionLimits{})
	f.limitHost(t, "nowhere.example.com")
	for range 3 {
		f.hit("GET", "nowhere.example.com", "/", nil)
	}
	if view := f.failureLog(t, ""); view.Total != 2 {
		t.Fatalf("a rate limit on an unrouted host was logged: %v", failureLines(view.Entries))
	}
}

// A request that goes on to another service is logged once, for how it
// ended, and not at all when that was well.
func TestFailureLogCountsARetryOnce(t *testing.T) {
	previous := upstreamHeaderTimeout
	upstreamHeaderTimeout = 200 * time.Millisecond
	defer func() { upstreamHeaderTimeout = previous }()
	broken := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
	hang := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	for _, c := range []struct {
		name         string
		first, next  http.Handler
		closeFirst   bool
		closeNext    bool
		code         int
		want         []string
		wantServedBy string
	}{
		{name: "the next service answers", first: echo("a"), next: echo("b"), closeFirst: true, code: 200, wantServedBy: "b "},
		{name: "the next service answers after a timeout", first: hang, next: echo("b"), code: 200, wantServedBy: "b "},
		{name: "no service can be reached", first: echo("a"), next: echo("b"), closeFirst: true, closeNext: true, code: 502, want: []string{"unreachable 502 [svc-1] app.example.com /"}},
		{name: "the next service fails", first: echo("a"), next: broken, closeFirst: true, code: 500, want: []string{"app_error 500 [svc-1] app.example.com /"}},
		{name: "an answer is final", first: broken, next: echo("b"), code: 500, want: []string{"app_error 500 [svc-0] app.example.com /"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newProxyFixture(t, c.first, c.next)
			f.route(t, 0, "app.example.com")
			f.share(t, 1, "app.example.com")
			if c.closeFirst {
				f.upstreams[0].Close()
			}
			if c.closeNext {
				f.upstreams[1].Close()
			}
			f.failureLogging(t, true)
			if code, body := f.get("app.example.com"); code != c.code || !strings.HasPrefix(body, c.wantServedBy) {
				t.Fatalf("%d %q, want %d %q", code, body, c.code, c.wantServedBy)
			}
			if got := failureLines(f.failureLog(t, "").Entries); !slices.Equal(got, c.want) {
				t.Fatalf("log: %q, want %q", got, c.want)
			}
		})
	}
}

// With the log off nothing is kept and a request reads no clock for it;
// what was kept stays when it is turned off again.
func TestFailureLogKeepsNothingWhileOff(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	f.route(t, 0, "app.example.com")
	failing := func() {
		t.Helper()
		if w := f.hit("GET", "app.example.com", "/", nil); w.Code != 500 {
			t.Fatalf("app: %d", w.Code)
		}
		if w := f.hit("GET", "app.example.com", "/.hidden", nil); w.Code != 403 {
			t.Fatalf("block rule: %d", w.Code)
		}
	}
	failing()
	if view := f.failureLog(t, ""); view.Settings != control.DefaultLogSettings(control.LogFailures) || !view.Available || view.Total != 0 || len(view.Entries) != 0 {
		t.Fatalf("off: %+v", view)
	}
	f.failureLogging(t, true)
	failing()
	if view := f.failureLog(t, ""); view.Total != 2 {
		t.Fatalf("on: %+v", view)
	}
	f.failureLogging(t, false)
	failing()
	if view := f.failureLog(t, ""); view.Settings.Enabled || view.Total != 2 {
		t.Fatalf("off again: %+v", view)
	}
	// A log whose file couldn't be opened keeps nothing and answers all the same.
	logs := f.store.logs.failures
	f.store.logs.failures = nil
	defer func() { f.store.logs.failures = logs }()
	failing()
	if view := f.failureLog(t, ""); view.Available || len(view.Entries) != 0 {
		t.Fatalf("unavailable: %+v", view)
	}
	if w := request(f.store.Handler(), "PUT", "/api/v1/observe/failure-log", f.admin, map[string]any{"enabled": true, "max_mib": 64, "retention_days": 7}); w.Code != 409 || !strings.Contains(w.Body.String(), "log_unavailable") {
		t.Fatalf("turn on without a file: %d %s", w.Code, w.Body.String())
	}
}

// Whatever goes wrong while logging, the response is not touched.
func TestFailureLogCannotHarmAResponse(t *testing.T) {
	f := newProxyFixture(t)
	f.failureLogging(t, true)
	f.store.logFailure(nil, "app.example.com", time.Now(), failBlocked, http.StatusForbidden, "")
	f.store.logTurnedAway(nil, "app.example.com", time.Now(), failBusy, http.StatusTooManyRequests)
	if view := f.failureLog(t, ""); view.Total != 0 {
		t.Fatalf("entries from nothing: %+v", view)
	}
}

// The console reads the log in pages, filtered by cause and address, and
// turns it on and off with settings of its own.
func TestFailureLogAPI(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	read := func(query string) logView[failureEntry] {
		t.Helper()
		return f.failureLog(t, query)
	}
	long := "/" + strings.Repeat("a", maxPublicPath)
	f.failureLogging(t, false)
	f.visit("app.example.com", long)
	if view := read(""); view.Total != 0 {
		t.Fatalf("logged while off: %+v", view)
	}
	on := map[string]any{"enabled": true, "max_mib": 64, "retention_days": 7}
	if w := request(h, "PUT", "/api/v1/observe/failure-log", f.admin, on); w.Code != 200 || !strings.Contains(w.Body.String(), `"settings":{"enabled":true,"max_mib":64,"retention_days":7}`) {
		t.Fatalf("turn on: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAudit(t); event.Action != "observe.log_settings" || string(event.Detail) != `{"enabled":true,"log":"failures","max_mib":64,"retention_days":7}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	if limits := f.store.logs.failures.log.Limits(); limits.MaxBytes != 64<<20 || limits.Retention != 7*24*time.Hour {
		t.Fatalf("limits: %+v", limits)
	}
	// The two logs have settings of their own.
	var access logView[accessEntry]
	if w := request(h, "GET", "/api/v1/observe/access-log", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &access) != nil || access.Settings != control.DefaultLogSettings(control.LogAccess) {
		t.Fatalf("access log: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []map[string]any{{"enabled": true, "max_mib": 8, "retention_days": 7}, {"enabled": true, "max_mib": 64, "retention_days": 0}, {"enabled": true}} {
		if w := request(h, "PUT", "/api/v1/observe/failure-log", f.admin, body); w.Code != 400 {
			t.Fatalf("settings %v: %d", body, w.Code)
		}
	}
	for _, c := range []struct{ host, path string }{
		{"app.example.com", long}, {"other.example.com", "/.dotfile"}, {"app.example.com", long}, {"other.example.com", "/.other/config"},
		{"app.example.com", "/.dotfile"}, {"other.example.com", long}, {"app.example.com", long},
	} {
		f.visit(c.host, c.path)
	}
	if view := read(""); view.Total != 7 || len(view.Entries) != 7 || view.Entries[0].Host != "app.example.com" || view.Entries[0].Cause != failTooLong {
		t.Fatalf("all: %+v", view)
	}
	for _, c := range []struct {
		query string
		total int
	}{
		{"?cause=too_long", 4},
		{"?cause=blocked", 3},
		{"?cause=busy", 0},
		{"?host=other.example.com", 3},
		{"?cause=blocked&host=other.example.com", 2},
		{"?cause=too_long&host=other.example.com", 1},
		{"?host=nowhere.example.com", 0},
		{"?cause=refused", 7},
		{"?cause=service", 0},
		{"?cause=refused&host=other.example.com", 3},
		{"?q=other", 3},
		{"?q=.DOTFILE", 2},
		{"?q=198.51.100.30", 7},
		{"?q=%25", 0},
		{"?cause=blocked&q=other", 2},
	} {
		if view := read(c.query); view.Total != c.total || len(view.Entries) != c.total {
			t.Fatalf("%s: %d entries of %d, want %d", c.query, len(view.Entries), view.Total, c.total)
		}
	}
	first := read("?per_page=3")
	second, third := read("?per_page=3&page=2"), read("?per_page=3&page=3")
	if len(first.Entries) != 3 || len(second.Entries) != 3 || len(third.Entries) != 1 || first.Total != 7 || third.Total != 7 ||
		second.Entries[0].ID >= first.Entries[2].ID || third.Entries[0].ID >= second.Entries[2].ID {
		t.Fatalf("pages: %+v %+v %+v", first, second, third)
	}
	for _, e := range read("").Entries {
		if !e.Routine {
			t.Fatalf("a refusal that is not routine: %+v", e)
		}
	}
	if w := request(h, "GET", "/api/v1/observe/failure-log?per_page=1", f.admin, nil); !strings.Contains(w.Body.String(), `"suppressed":0`) || !strings.Contains(w.Body.String(), `"routine":true`) {
		t.Fatalf("view: %s", w.Body.String())
	}
	for _, query := range []string{"?cause=maybe", "?cause=", "?cause=refused,service", "?q=" + strings.Repeat("a", 129), "?page=0", "?page=x", "?per_page=0", "?per_page=201", "?host=" + strings.Repeat("a", 256)} {
		w := request(h, "GET", "/api/v1/observe/failure-log"+query, f.admin, nil)
		if query == "?cause=" {
			// An empty filter is no filter.
			if w.Code != 200 {
				t.Fatalf("%s: %d", query, w.Code)
			}
			continue
		}
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_log_filter") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", "/api/v1/observe/failure-log", "", nil); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}
	if w := request(h, "PUT", "/api/v1/observe/failure-log", "", on); w.Code != 401 {
		t.Fatalf("save without a session: %d", w.Code)
	}
}

// A client that floods the log with refusals gets 60 a minute kept and the
// rest counted, and nothing changes for the client: it is refused all the
// same. Another client's refusals, and a service's failures, are logged
// whatever the flood.
func TestFailureLogSuppressesAFlood(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	f.route(t, 0, "app.example.com")
	f.failureLogging(t, true)
	for range 70 {
		if w := f.hit("GET", "app.example.com", "/.hidden", nil); w.Code != 403 {
			t.Fatalf("a refusal that was not logged: %d", w.Code)
		}
	}
	if view := f.failureLog(t, ""); view.Total != 60 || view.Suppressed != 10 || view.Dropped != 0 {
		t.Fatalf("after the flood: %d entries, %d suppressed, %d dropped", view.Total, view.Suppressed, view.Dropped)
	}
	// Another client, one with a budget of its own.
	other := apart(f.store.logs.failures.slot, "203.0.113.%d", "203.0.113.5")
	if code, _ := f.from("GET", "app.example.com", other, ""); code != 500 {
		t.Fatalf("app: %d", code)
	}
	r := httptest.NewRequest("GET", "/.hidden", nil)
	r.Host = "app.example.com"
	r.Header.Set("X-Forwarded-For", other)
	f.handler.ServeHTTP(httptest.NewRecorder(), r)
	for range 70 {
		f.hit("GET", "app.example.com", "/", nil)
	}
	if view := f.failureLog(t, ""); view.Total != 60+1+1+70 || view.Suppressed != 10 {
		t.Fatalf("after the app failed: %d entries, %d suppressed", view.Total, view.Suppressed)
	}
	if view := f.failureLog(t, "?cause=service"); view.Total != 71 || view.Entries[0].Routine {
		t.Fatalf("service: %+v", view)
	}
	if view := f.failureLog(t, "?cause=refused"); view.Total != 61 || !view.Entries[0].Routine {
		t.Fatalf("refused: %+v", view)
	}
}

// BenchmarkPublicRequestFailureLog compares a successful proxied request with
// the failure log off and on: a request that goes well pays nothing for it.
func BenchmarkPublicRequestFailureLog(b *testing.B) {
	for _, on := range []bool{false, true} {
		b.Run(map[bool]string{false: "off", true: "on"}[on], func(b *testing.B) {
			f := newProxyFixture(b, echo("A"))
			f.route(b, 0, "app.example.com")
			f.failureLogging(b, on)
			b.ReportAllocs()
			for b.Loop() {
				r := httptest.NewRequest("GET", "/", nil)
				r.Host = "app.example.com"
				w := httptest.NewRecorder()
				f.handler.ServeHTTP(w, r)
				if w.Code != 200 {
					b.Fatal(w.Code)
				}
			}
		})
	}
}

// BenchmarkPublicRequestFailureLogParallel is the same with many requests at
// once, where anything shared would show.
func BenchmarkPublicRequestFailureLogParallel(b *testing.B) {
	for _, on := range []bool{false, true} {
		b.Run(map[bool]string{false: "off", true: "on"}[on], func(b *testing.B) {
			f := newProxyFixture(b, echo("A"))
			f.route(b, 0, "app.example.com")
			f.failureLogging(b, on)
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					r := httptest.NewRequest("GET", "/", nil)
					r.Host = "app.example.com"
					w := httptest.NewRecorder()
					f.handler.ServeHTTP(w, r)
					if w.Code != 200 {
						b.Error(w.Code)
						return
					}
				}
			})
		})
	}
}

// BenchmarkPublicRequestFailureLogFailing shows what a failed request pays
// to be logged: it builds an entry and hands it over without waiting.
func BenchmarkPublicRequestFailureLogFailing(b *testing.B) {
	for _, on := range []bool{false, true} {
		b.Run(map[bool]string{false: "off", true: "on"}[on], func(b *testing.B) {
			f := newProxyFixture(b, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
			f.route(b, 0, "app.example.com")
			f.failureLogging(b, on)
			b.ReportAllocs()
			for b.Loop() {
				r := httptest.NewRequest("GET", "/", nil)
				r.Host = "app.example.com"
				w := httptest.NewRecorder()
				f.handler.ServeHTTP(w, r)
				if w.Code != 500 {
					b.Fatal(w.Code)
				}
			}
		})
	}
}
