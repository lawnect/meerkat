package outlet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// setConnectionLimits saves limits, with the default response timeout and
// request time limit when they are zero; a test of no limit saves it with
// request.
func (f proxyFixture) setConnectionLimits(tb testing.TB, limits control.ConnectionLimits) {
	tb.Helper()
	if limits.Hosts == nil {
		limits.Hosts = []control.HostUpload{}
	}
	if limits.ResponseTimeouts == nil {
		limits.ResponseTimeouts = []control.HostResponseTimeout{}
	}
	if limits.ResponseTimeoutSeconds == 0 {
		limits.ResponseTimeoutSeconds = control.DefaultResponseTimeoutSeconds
	}
	if limits.RequestTimeouts == nil {
		limits.RequestTimeouts = []control.HostRequestTimeout{}
	}
	if limits.RequestTimeoutMinutes == 0 {
		limits.RequestTimeoutMinutes = control.DefaultRequestTimeoutMinutes
	}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, limits); w.Code != 200 {
		tb.Fatalf("connection limits: %d %s", w.Code, w.Body.String())
	}
}

func (f proxyFixture) post(host string, body io.Reader, length int64, ip string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/", body)
	r.Host, r.ContentLength = host, length
	if ip != "" {
		r.Header.Set("X-Forwarded-For", ip)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// onlyReader hides bytes.Reader's length, so the request goes out chunked.
type onlyReader struct{ io.Reader }

func TestConnectionLimitsAPI(t *testing.T) {
	f := newProxyFixture(t)
	w := request(f.store.Handler(), "GET", "/api/v1/security/connection-limits", f.admin, nil)
	var got control.ConnectionLimits
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.MaxUploadMB != 100 || got.IdleTimeoutSeconds != 30 || got.Hosts == nil ||
		got.ResponseTimeoutSeconds != 60 || got.ResponseTimeouts == nil || got.RequestTimeoutMinutes != 60 || got.RequestTimeouts == nil {
		t.Fatalf("defaults: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []control.ConnectionLimits{
		{MaxUploadMB: -1},
		{IdleTimeoutSeconds: 2},
		{IdleTimeoutSeconds: 3601},
		{TotalConcurrent: 1_000_001},
		{Hosts: []control.HostUpload{{Host: "*", MaxUploadMB: 1}}},
		{Hosts: []control.HostUpload{{Host: "a.example.com"}, {Host: "A.example.com"}}},
		{ResponseTimeoutSeconds: 4},
		{ResponseTimeoutSeconds: 3601},
		{ResponseTimeouts: []control.HostResponseTimeout{{Host: "ai.example.com", Seconds: 3601}}},
		{ResponseTimeouts: []control.HostResponseTimeout{{Host: "*.example.com", Seconds: 600}}},
		{RequestTimeoutMinutes: -1},
		{RequestTimeoutMinutes: 1441},
		{RequestTimeouts: []control.HostRequestTimeout{{Host: "files.example.com", Minutes: 1441}}},
		{RequestTimeouts: []control.HostRequestTimeout{{Host: "files.example.com"}, {Host: "Files.example.com"}}},
	} {
		if bad.Hosts == nil {
			bad.Hosts = []control.HostUpload{}
		}
		if bad.ResponseTimeouts == nil {
			bad.ResponseTimeouts = []control.HostResponseTimeout{}
		}
		if bad.RequestTimeouts == nil {
			bad.RequestTimeouts = []control.HostRequestTimeout{}
		}
		if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, bad); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_connection_limits") {
			t.Fatalf("%+v: %d %s", bad, w.Code, w.Body.String())
		}
	}
	w = request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, map[string]any{"max_upload_mb": 1})
	if w.Code != 400 {
		t.Fatalf("partial settings accepted: %d", w.Code)
	}
	// A per-host entry without its value would read as no limit.
	bodyWith := func(hosts, timeouts, durations any) map[string]any {
		return map[string]any{"max_upload_mb": 1, "per_ip_concurrent": 0, "total_concurrent": 0, "idle_timeout_seconds": 0, "min_upload_kb_per_second": 0,
			"hosts": hosts, "response_timeout_seconds": 60, "response_timeouts": timeouts, "request_timeout_minutes": 60, "request_timeouts": durations}
	}
	for name, body := range map[string]map[string]any{
		"null timeout":        bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": nil}}, []any{}),
		"missing timeout":     bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com"}}, []any{}),
		"unknown key":         bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": 600, "seconds": 600}}, []any{}),
		"null upload":         bodyWith([]any{map[string]any{"host": "big.example.com", "max_upload_mb": nil}}, []any{}, []any{}),
		"missing upload":      bodyWith([]any{map[string]any{"host": "big.example.com"}}, []any{}, []any{}),
		"missing host":        bodyWith([]any{}, []any{map[string]any{"response_timeout_seconds": 600}}, []any{}),
		"null duration":       bodyWith([]any{}, []any{}, []any{map[string]any{"host": "files.example.com", "request_timeout_minutes": nil}}),
		"missing duration":    bodyWith([]any{}, []any{}, []any{map[string]any{"host": "files.example.com"}}),
		"duration in seconds": bodyWith([]any{}, []any{}, []any{map[string]any{"host": "files.example.com", "request_timeout_minutes": 60, "request_timeout_seconds": 3600}}),
		"null durations":      bodyWith([]any{}, []any{}, nil),
	} {
		if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, body); w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin,
		bodyWith([]any{map[string]any{"host": "big.example.com", "max_upload_mb": 0}}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": 0}},
			[]any{map[string]any{"host": "files.example.com", "request_timeout_minutes": 0}})); w.Code != 200 {
		t.Fatalf("explicit zeros: %d %s", w.Code, w.Body.String())
	}
	// A console from before the response timeout can't save them away.
	old := map[string]any{"max_upload_mb": 1, "per_ip_concurrent": 0, "total_concurrent": 0, "idle_timeout_seconds": 0, "min_upload_kb_per_second": 0, "hosts": []any{}}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, old); w.Code != 400 {
		t.Fatalf("settings without response timeouts accepted: %d %s", w.Code, w.Body.String())
	}
	// Nor one from before the request time limit.
	old["response_timeout_seconds"], old["response_timeouts"] = 60, []any{}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, old); w.Code != 400 {
		t.Fatalf("settings without request time limits accepted: %d %s", w.Code, w.Body.String())
	}
	f.setConnectionLimits(t, control.ConnectionLimits{MaxUploadMB: 5, Hosts: []control.HostUpload{{Host: " Big.Example.com ", MaxUploadMB: 0}},
		ResponseTimeoutSeconds: 30, ResponseTimeouts: []control.HostResponseTimeout{{Host: " AI.Example.com ", Seconds: 600}, {Host: "batch.example.com", Seconds: 0}},
		RequestTimeoutMinutes: 90, RequestTimeouts: []control.HostRequestTimeout{{Host: " Files.Example.com ", Minutes: 0}, {Host: "ai.example.com", Minutes: 1440}}})
	limits := f.store.connectionLimits()
	if limits.uploadLimit("big.example.com") != 0 || limits.uploadLimit("app.example.com") != 5<<20 {
		t.Fatalf("saved limits not applied: %+v", limits.ConnectionLimits)
	}
	if limits.responseTimeout("ai.example.com") != 600*time.Second || limits.responseTimeout("batch.example.com") != 0 || limits.responseTimeout("app.example.com") != 30*time.Second {
		t.Fatalf("saved response timeouts not applied: %+v", limits.ConnectionLimits)
	}
	if limits.requestTimeout("files.example.com") != 0 || limits.requestTimeout("ai.example.com") != 24*time.Hour || limits.requestTimeout("app.example.com") != 90*time.Minute {
		t.Fatalf("saved request time limits not applied: %+v", limits.ConnectionLimits)
	}
	// Up to 50 hostnames of the longest kind in each list fit in a request.
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".com"
	full := control.ConnectionLimits{ResponseTimeoutSeconds: 60}
	for i := range control.MaxUploadHosts {
		full.Hosts = append(full.Hosts, control.HostUpload{Host: fmt.Sprintf("%02d%s", i, long[2:]), MaxUploadMB: 1})
		full.ResponseTimeouts = append(full.ResponseTimeouts, control.HostResponseTimeout{Host: fmt.Sprintf("%02d%s", i, long[2:]), Seconds: 600})
		full.RequestTimeouts = append(full.RequestTimeouts, control.HostRequestTimeout{Host: fmt.Sprintf("%02d%s", i, long[2:]), Minutes: 1440})
	}
	f.setConnectionLimits(t, full)
}

// A hostname given a longer response timeout keeps waiting for its service
// after others would have given up, and one with none waits as long as the
// service takes; the rest answer 504 at the default.
func TestResponseTimeoutPerHost(t *testing.T) {
	shortenResponseTimeouts(t, 200*time.Millisecond)
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		_, _ = io.WriteString(w, "late")
	}))
	f.route(t, 0, "app.example.com")
	f.route(t, 0, "ai.example.com")
	f.route(t, 0, "batch.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{ResponseTimeouts: []control.HostResponseTimeout{
		{Host: "ai.example.com", Seconds: 600}, // 2s here
		{Host: "batch.example.com", Seconds: 0},
	}})
	start := time.Now()
	if code, _ := f.get("app.example.com"); code != http.StatusGatewayTimeout || time.Since(start) > 900*time.Millisecond {
		t.Fatalf("default host: %d after %s", code, time.Since(start))
	}
	for _, host := range []string{"ai.example.com", "batch.example.com"} {
		if code, body := f.get(host); code != 200 || body != "late" {
			t.Fatalf("%s: %d %q", host, code, body)
		}
	}
}

func TestUploadSizeIsLimitedPerHost(t *testing.T) {
	var mu sync.Mutex
	var received []int64
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		mu.Lock()
		received = append(received, n)
		mu.Unlock()
	}))
	f.route(t, 0, "app.example.com")
	f.route(t, 0, "big.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{MaxUploadMB: 1, Hosts: []control.HostUpload{{Host: "big.example.com", MaxUploadMB: 0}}})
	payload := bytes.Repeat([]byte("x"), 2<<20)

	if w := f.post("app.example.com", bytes.NewReader(payload), int64(len(payload)), ""); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("declared oversized upload: %d", w.Code)
	}
	mu.Lock()
	if len(received) != 0 {
		t.Fatalf("an upload declared too large reached the service: %v", received)
	}
	mu.Unlock()
	if w := f.post("app.example.com", onlyReader{bytes.NewReader(payload)}, -1, ""); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked oversized upload: %d %s", w.Code, w.Body.String())
	}
	if w := f.post("app.example.com", bytes.NewReader(payload[:1<<20]), 1<<20, ""); w.Code != 200 {
		t.Fatalf("upload at the limit: %d", w.Code)
	}
	if w := f.post("big.example.com", bytes.NewReader(payload), int64(len(payload)), ""); w.Code != 200 {
		t.Fatalf("host without a limit: %d", w.Code)
	}
	mu.Lock()
	defer mu.Unlock()
	if last := received[len(received)-1]; last != int64(len(payload)) {
		t.Fatalf("unlimited host received %d bytes", last)
	}
}

func TestConcurrentRequestsArePerClientAndCapped(t *testing.T) {
	arrived := make(chan struct{}, 10)
	release := make(chan struct{})
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- struct{}{}
		<-release
	}))
	f.route(t, 0, "app.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{PerIPConcurrent: 2, TotalConcurrent: 3})
	get := func(host, ip string) int {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		r.Header.Set("X-Forwarded-For", ip)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	var wg sync.WaitGroup
	for _, ip := range []string{"203.0.113.1", "203.0.113.1", "203.0.113.2"} {
		wg.Go(func() {
			if code := get("app.example.com", ip); code != 200 {
				t.Errorf("admitted request from %s: %d", ip, code)
			}
		})
		<-arrived
	}
	if code := get("app.example.com", "203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("third request from one address: %d", code)
	}
	if code := get("app.example.com", "203.0.113.3"); code != http.StatusServiceUnavailable {
		t.Fatalf("request over the total: %d", code)
	}
	// The management host is not a proxied request, so a full proxy does not
	// lock administrators out.
	if code := get(f.adminHost, "203.0.113.3"); code == http.StatusServiceUnavailable || code == http.StatusTooManyRequests {
		t.Fatalf("management host while the proxy is full: %d", code)
	}
	close(release)
	wg.Wait()
	if code := get("app.example.com", "203.0.113.1"); code != 200 {
		t.Fatalf("after the requests ended: %d", code)
	}
	if total := f.store.connections.inflight.total.Load(); total != 0 {
		t.Fatalf("%d proxy slots still held", total)
	}
	for i := range f.store.connections.inflight.shards {
		if n := len(f.store.connections.inflight.shards[i].counts); n != 0 {
			t.Fatalf("shard %d still counts %d clients", i, n)
		}
	}
}

// guardedServer serves the fixture over real TCP, where deadlines apply.
func (f proxyFixture) guardedServer(tb testing.TB, limits *connLimits) string {
	tb.Helper()
	f.store.connections.config.Store(limits)
	return f.publicServer(tb)
}

// publicServer serves the public listener on a real connection, with the
// limits in force.
func (f proxyFixture) publicServer(tb testing.TB) string {
	tb.Helper()
	server := httptest.NewServer(f.handler)
	tb.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
}

func shortenRequestTimeouts(t *testing.T, minute time.Duration) {
	previous := requestTimeoutUnit
	requestTimeoutUnit = minute
	t.Cleanup(func() { requestTimeoutUnit = previous })
}

func dial(tb testing.TB, address string) (net.Conn, *bufio.Reader) {
	tb.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { conn.Close() })
	return conn, bufio.NewReader(conn)
}

func readResponse(tb testing.TB, conn net.Conn, reader *bufio.Reader, within time.Duration) (int, string) {
	tb.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(within))
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		tb.Fatalf("no response: %v", err)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		tb.Fatalf("response body: %v", err)
	}
	return res.StatusCode, string(body)
}

func TestStalledUploadsAreCut(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	f.route(t, 0, "app.example.com")
	address := f.guardedServer(t, &connLimits{idle: 300 * time.Millisecond, minRate: 1024})

	conn, reader := dial(t, address)
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: app.example.com\r\nContent-Length: 100\r\n\r\nabc")
	if code, _ := readResponse(t, conn, reader, 5*time.Second); code != http.StatusRequestTimeout {
		t.Fatalf("stalled upload: %d", code)
	}

	// Trickling a byte at a time never stalls, but stays far below the
	// minimum speed.
	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: app.example.com\r\nContent-Length: 100\r\n\r\n")
	start := time.Now()
	go func() {
		for range 100 {
			if _, err := conn.Write([]byte("x")); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	if code, _ := readResponse(t, conn, reader, 5*time.Second); code != http.StatusRequestTimeout {
		t.Fatalf("trickled upload: %d", code)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("trickled upload ran for %s", elapsed)
	}
}

func TestStalledDownloadsAreCut(t *testing.T) {
	stopped := make(chan error, 1)
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for {
			if _, err := w.Write(chunk); err != nil {
				stopped <- err
				return
			}
		}
	}))
	f.route(t, 0, "app.example.com")
	address := f.guardedServer(t, &connLimits{ConnectionLimits: control.ConnectionLimits{TotalConcurrent: 10}, idle: 300 * time.Millisecond})
	conn, _ := dial(t, address)
	// The client asks and never reads.
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n")
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the response kept waiting for a client that stopped reading")
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.store.connections.inflight.total.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the stalled request still holds a proxy slot")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Deadlines cover only reads and writes involving the client: waiting for
// the service, pauses in a stream, the next request on a kept-alive
// connection and an upgraded connection are never cut.
func TestWaitingIsNotStalling(t *testing.T) {
	const pause = 600 * time.Millisecond
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(pause)
		_, _ = io.WriteString(w, "late")
	})
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := range 3 {
			_, _ = fmt.Fprintf(w, "event %d\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(pause)
		}
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
	f := newProxyFixture(t, slow, stream, upgrade)
	f.route(t, 0, "slow.example.com")
	f.route(t, 1, "stream.example.com")
	f.route(t, 2, "ws.example.com")
	address := f.guardedServer(t, &connLimits{idle: 300 * time.Millisecond, minRate: 1024})

	conn, reader := dial(t, address)
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: slow.example.com\r\nContent-Length: 5\r\n\r\nhello")
	if code, body := readResponse(t, conn, reader, 5*time.Second); code != 200 || body != "late" {
		t.Fatalf("slow service after an upload: %d %q", code, body)
	}
	time.Sleep(pause)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: stream.example.com\r\n\r\n")
	if code, body := readResponse(t, conn, reader, 5*time.Second); code != 200 || body != "event 0\nevent 1\nevent 2\n" {
		t.Fatalf("paused stream on a kept-alive connection: %d %q", code, body)
	}

	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %v %v", res, err)
	}
	time.Sleep(pause)
	_, _ = io.WriteString(conn, "ping")
	got := make([]byte, 4)
	if _, err := io.ReadFull(reader, got); err != nil || string(got) != "ping" {
		t.Fatalf("idle upgraded connection: %q %v", got, err)
	}
}

// net/http drains a body the handler left unread after the handler has
// returned and released its slots. A client that sends part of a body to a
// request turned away early must be cut then, not waited for.
func TestUnreadBodiesAreCutNotDrained(t *testing.T) {
	// Go's server, like many, drains an unread body before it answers;
	// full duplex makes this one answer at once, as some services do.
	refuse := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = http.NewResponseController(w).EnableFullDuplex()
		w.WriteHeader(http.StatusUnauthorized)
	})
	f := newProxyFixture(t, refuse)
	f.route(t, 0, "app.example.com")
	address := f.guardedServer(t, &connLimits{ConnectionLimits: control.ConnectionLimits{PerIPConcurrent: 1, TotalConcurrent: 1}, idle: time.Hour})
	for _, c := range []struct {
		name, host string
		want       int
	}{
		{"not found", "example.com", http.StatusNotFound},
		{"service answers without reading", "app.example.com", http.StatusUnauthorized},
		{"unknown host", "nowhere.example.net", http.StatusNotFound},
	} {
		conn, reader := dial(t, address)
		_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: "+c.host+"\r\nX-Forwarded-For: 203.0.113.9\r\nContent-Length: 100\r\n\r\nx")
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		res, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("%s: no response while the body is unfinished: %v", c.name, err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		if res.StatusCode != c.want {
			t.Fatalf("%s: %d", c.name, res.StatusCode)
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			t.Fatalf("%s: connection kept after an unfinished body: %v", c.name, err)
		}
	}
}

// Hostnames with different response timeouts share the connections to their
// service, and a changed timeout keeps them: each new one is a tunnel channel.
func TestResponseTimeoutsShareConnections(t *testing.T) {
	var mu sync.Mutex
	peers := map[string]bool{}
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		peers[r.RemoteAddr] = true
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	f.route(t, 0, "app.example.com")
	f.route(t, 0, "ai.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{ResponseTimeouts: []control.HostResponseTimeout{{Host: "ai.example.com", Seconds: 600}}})
	for range 3 {
		for _, host := range []string{"app.example.com", "ai.example.com"} {
			if code, _ := f.get(host); code != 200 {
				t.Fatalf("%s: %d", host, code)
			}
		}
	}
	f.setConnectionLimits(t, control.ConnectionLimits{ResponseTimeoutSeconds: 90})
	for _, host := range []string{"app.example.com", "ai.example.com"} {
		if code, _ := f.get(host); code != 200 {
			t.Fatalf("%s after the change: %d", host, code)
		}
	}
	if len(peers) != 1 {
		t.Fatalf("%d connections to the service, want 1", len(peers))
	}
}

// A service that answers before it has the whole request body has kept to
// the response timeout: its response streams on past it as the rest of the
// body goes out.
func TestResponseTimeoutEndsWithEarlyAnswer(t *testing.T) {
	shortenResponseTimeouts(t, 200*time.Millisecond)
	answered := make(chan struct{})
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = http.NewResponseController(w).EnableFullDuplex()
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		close(answered)
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(600 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	}))
	f.route(t, 0, "app.example.com")
	// The body ends only once the answer is under way.
	body := onlyReader{readerFunc(func(p []byte) (int, error) {
		<-answered
		time.Sleep(100 * time.Millisecond)
		return copy(p, "rest"), io.EOF
	})}
	w := f.post("app.example.com", body, -1, "")
	if w.Code != 200 || w.Body.String() != "done" {
		t.Fatalf("early answer: %d %q", w.Code, w.Body.String())
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// A request still going at its time limit is ended: a stream is cut off,
// with the service's request cancelled; a service yet to answer gets 504 and
// an upload unfinished 408. A hostname without a limit streams on, and so
// does an upgraded connection.
func TestRequestTimeLimit(t *testing.T) {
	const limit = 400 * time.Millisecond
	shortenRequestTimeouts(t, limit)
	stopped := make(chan struct{}, 4)
	stream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { stopped <- struct{}{} }()
		for {
			if _, err := io.WriteString(w, "tick\n"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			_, _ = io.WriteString(w, "late")
		}
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
	f := newProxyFixture(t, stream, slow, upgrade)
	f.route(t, 0, "stream.example.com")
	f.route(t, 0, "files.example.com")
	f.route(t, 1, "slow.example.com")
	f.route(t, 2, "ws.example.com")
	f.failureLogging(t, true)
	f.requestLogging(t, true)
	// The response timeout is off for slow.example.com, so the time limit
	// is what ends its wait.
	f.setConnectionLimits(t, control.ConnectionLimits{RequestTimeoutMinutes: 1,
		RequestTimeouts:  []control.HostRequestTimeout{{Host: "files.example.com", Minutes: 0}},
		ResponseTimeouts: []control.HostResponseTimeout{{Host: "slow.example.com", Seconds: 0}}})
	address := f.publicServer(t)

	conn, reader := dial(t, address)
	start := time.Now()
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: stream.example.com\r\nX-Forwarded-For: 203.0.113.5\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("stream: %v %v", res, err)
	}
	got, err := io.ReadAll(res.Body)
	if err == nil || !strings.HasPrefix(string(got), "tick\n") {
		t.Fatalf("stream past its time limit ended cleanly: %q %v", got, err)
	}
	if elapsed := time.Since(start); elapsed < limit || elapsed > limit+time.Second {
		t.Fatalf("stream cut after %s, limit %s", elapsed, limit)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("the service kept streaming to a request ended")
	}

	// No limit for files.example.com: well past the limit, it streams on.
	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: files.example.com\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err = http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("unlimited stream: %v %v", res, err)
	}
	until := time.Now().Add(3 * limit)
	for time.Now().Before(until) {
		line, err := bufio.NewReader(io.LimitReader(res.Body, 5)).ReadString('\n')
		if err != nil || line != "tick\n" {
			t.Fatalf("unlimited stream cut: %q %v", line, err)
		}
	}
	conn.Close()
	<-stopped

	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: slow.example.com\r\n\r\n")
	if code, body := readResponse(t, conn, reader, 5*time.Second); code != http.StatusGatewayTimeout || body != "request time limit reached\n" {
		t.Fatalf("service past the time limit: %d %q", code, body)
	}

	// The client still uploading at the limit took the time.
	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: slow.example.com\r\nContent-Length: 100\r\n\r\nabc")
	if code, _ := readResponse(t, conn, reader, 5*time.Second); code != http.StatusRequestTimeout {
		t.Fatalf("upload past the time limit: %d", code)
	}

	conn, reader = dial(t, address)
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: ws.example.com\r\nConnection: Upgrade\r\nUpgrade: echo\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if res, err := http.ReadResponse(reader, nil); err != nil || res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %v %v", res, err)
	}
	time.Sleep(2 * limit)
	_, _ = io.WriteString(conn, "ping")
	echo := make([]byte, 4)
	if _, err := io.ReadFull(reader, echo); err != nil || string(echo) != "ping" {
		t.Fatalf("upgraded connection past the time limit: %q %v", echo, err)
	}

	lines := failureLines(f.failureLog(t, "?cause=time_limit").Entries)
	if want := []string{"time_limit 200 [svc-0] stream.example.com /", "time_limit 504 [svc-1] slow.example.com /", "time_limit 408 [svc-1] slow.example.com /"}; !slices.Equal(lines, want) {
		t.Fatalf("failure log = %q, want %q", lines, want)
	}
	if refused := failureLines(f.failureLog(t, "?cause=refused").Entries); len(refused) != 3 {
		t.Fatalf("time limits are the Outlet's own doing: %q", refused)
	}
	var cut *requestEntry
	for _, entry := range f.requestLog(t, "").Entries {
		if entry.Host == "stream.example.com" {
			cut = &entry
		}
	}
	if cut == nil || cut.Status != 200 || cut.Cause != failTimeLimit || cut.Bytes == 0 {
		t.Fatalf("request log of the cut stream: %+v", cut)
	}
}

// The time limit holds without the idle timeout: a client that stopped
// reading is cut off at the limit, its write given up and its slot freed.
func TestRequestTimeLimitCutsAStalledClient(t *testing.T) {
	shortenRequestTimeouts(t, 400*time.Millisecond)
	stopped := make(chan error, 1)
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for {
			if _, err := w.Write(chunk); err != nil {
				stopped <- err
				return
			}
		}
	}))
	f.route(t, 0, "app.example.com")
	f.setConnectionLimits(t, control.ConnectionLimits{TotalConcurrent: 10, RequestTimeoutMinutes: 1})
	if f.store.connectionLimits().idle != 0 {
		t.Fatal("the idle timeout is on")
	}
	conn, _ := dial(t, f.publicServer(t))
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n")
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the response kept waiting for a client that stopped reading, past the time limit")
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.store.connections.inflight.total.Load() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the request still holds a proxy slot past its time limit")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// streamTicks answers with status and streams a line every 50 ms until the
// request ends.
func streamTicks(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		for {
			if _, err := io.WriteString(w, "tick\n"); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
}

// A 5xx stream cut at the limit is one failed request, the app's error.
func TestRequestTimeLimitLogsA5xxStreamOnce(t *testing.T) {
	shortenRequestTimeouts(t, 300*time.Millisecond)
	f := newProxyFixture(t, streamTicks(http.StatusServiceUnavailable))
	f.route(t, 0, "app.example.com")
	f.failureLogging(t, true)
	f.setConnectionLimits(t, control.ConnectionLimits{RequestTimeoutMinutes: 1})
	conn, reader := dial(t, f.publicServer(t))
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 503 {
		t.Fatalf("%v %v", res, err)
	}
	if _, err := io.ReadAll(res.Body); err == nil {
		t.Fatal("stream ended cleanly")
	}
	if lines := failureLines(f.failureLog(t, "").Entries); !slices.Equal(lines, []string{"app_error 503 [svc-0] app.example.com /"}) {
		t.Fatalf("failure log = %q", lines)
	}
}

// A service that never answers a whole upload gets the request ended with
// 504, and the client's connection serves the next request.
func TestRequestTimeLimitKeepsTheConnectionAfter504(t *testing.T) {
	shortenRequestTimeouts(t, 300*time.Millisecond)
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method == http.MethodPost {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	f.route(t, 0, "app.example.com")
	f.failureLogging(t, true)
	f.setConnectionLimits(t, control.ConnectionLimits{RequestTimeoutMinutes: 1, IdleTimeoutSeconds: 30,
		ResponseTimeouts: []control.HostResponseTimeout{{Host: "app.example.com", Seconds: 0}}})
	conn, reader := dial(t, f.publicServer(t))
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: app.example.com\r\nContent-Length: 3\r\n\r\nabc")
	if code, body := readResponse(t, conn, reader, 5*time.Second); code != 504 || body != "request time limit reached\n" {
		t.Fatalf("%d %q", code, body)
	}
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n")
	if code, body := readResponse(t, conn, reader, 5*time.Second); code != 200 || body != "ok" {
		t.Fatalf("next request on the connection: %d %q", code, body)
	}
	if lines := failureLines(f.failureLog(t, "").Entries); !slices.Equal(lines, []string{"time_limit 504 [svc-0] app.example.com /"}) {
		t.Fatalf("%q", lines)
	}
}

// A stream cut after a retry is logged once, for the service that streamed.
func TestRequestTimeLimitAfterARetryLogsOnce(t *testing.T) {
	shortenRequestTimeouts(t, 300*time.Millisecond)
	f := newProxyFixture(t, echo("a"), streamTicks(http.StatusOK))
	f.route(t, 0, "app.example.com")
	f.share(t, 1, "app.example.com")
	f.upstreams[0].Close()
	f.failureLogging(t, true)
	f.setConnectionLimits(t, control.ConnectionLimits{RequestTimeoutMinutes: 1})
	conn, reader := dial(t, f.publicServer(t))
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: app.example.com\r\n\r\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("%v %v", res, err)
	}
	if _, err := io.ReadAll(res.Body); err == nil {
		t.Fatal("stream ended cleanly")
	}
	if lines := failureLines(f.failureLog(t, "").Entries); !slices.Equal(lines, []string{"time_limit 200 [svc-1] app.example.com /"}) {
		t.Fatalf("%q", lines)
	}
}

// A cut body stays cut: the next read sets its deadline again, and must not
// wait for the client then (here the minimum speed alone would give it
// about 94 seconds).
func TestCutBodyStaysCut(t *testing.T) {
	limits := compileConnectionLimits(control.ConnectionLimits{MinUploadKBPerSecond: 1})
	readAgain := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, body := guardTransfer(w, r, limits)
		if _, err := io.ReadFull(body, make([]byte, 64<<10)); err != nil {
			readAgain <- err
			return
		}
		body.cut() // between two of the transport's reads
		_, err := body.Read(make([]byte, 1))
		readAgain <- err
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 1000000\r\n\r\n")
	_, _ = conn.Write(bytes.Repeat([]byte("x"), 64<<10))
	select {
	case err := <-readAgain:
		if err == nil {
			t.Fatal("read data that was never sent")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a read after the cut waited for the client again")
	}
}

// The limit expiring as a response finishes leaves the rest of it to go out,
// and once the request is done with the limit it never touches the
// connection, which goes on to the next request.
func TestTimeLimitLetsAFinishedResponseBe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, l := limitTime(w, r, nil, time.Hour, time.Now())
		l.answered(http.StatusOK)
		_, _ = io.WriteString(w, "a")
		w.(http.Flusher).Flush()
		if r.URL.Path == "/expired" {
			l.expire() // as the last bytes leave
			l.finish()
		} else {
			l.stop()
			l.expire() // a timer that fired as the request ended
		}
		_, _ = io.WriteString(w, "b")
	}))
	defer server.Close()
	client := server.Client()
	for _, path := range []string{"/expired", "/stopped", "/stopped"} {
		res, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || string(body) != "ab" {
			t.Fatalf("%s: %q %v", path, body, err)
		}
	}
}

// Streams cut at their time limit stay out of the standard log; the
// proxies' other errors don't.
func TestProxyLogLeavesOutTimeLimits(t *testing.T) {
	var out bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(&out)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(previous); log.SetFlags(flags) })
	proxyLog.Printf("httputil: ReverseProxy read error during body copy: %v", errTimeLimit)
	if out.Len() != 0 {
		t.Fatalf("logged %q", out.String())
	}
	proxyLog.Printf("httputil: ReverseProxy read error during body copy: %v", io.ErrUnexpectedEOF)
	if !strings.Contains(out.String(), "unexpected EOF") {
		t.Fatalf("logged %q", out.String())
	}
}
