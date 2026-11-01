package outlet

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// setConnectionLimits saves limits, with the default response timeout when
// it is zero; a test of no response timeout saves it with request.
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
		got.ResponseTimeoutSeconds != 60 || got.ResponseTimeouts == nil {
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
	} {
		if bad.Hosts == nil {
			bad.Hosts = []control.HostUpload{}
		}
		if bad.ResponseTimeouts == nil {
			bad.ResponseTimeouts = []control.HostResponseTimeout{}
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
	bodyWith := func(hosts, timeouts any) map[string]any {
		return map[string]any{"max_upload_mb": 1, "per_ip_concurrent": 0, "total_concurrent": 0, "idle_timeout_seconds": 0, "min_upload_kb_per_second": 0,
			"hosts": hosts, "response_timeout_seconds": 60, "response_timeouts": timeouts}
	}
	for name, body := range map[string]map[string]any{
		"null timeout":    bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": nil}}),
		"missing timeout": bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com"}}),
		"unknown key":     bodyWith([]any{}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": 600, "seconds": 600}}),
		"null upload":     bodyWith([]any{map[string]any{"host": "big.example.com", "max_upload_mb": nil}}, []any{}),
		"missing upload":  bodyWith([]any{map[string]any{"host": "big.example.com"}}, []any{}),
		"missing host":    bodyWith([]any{}, []any{map[string]any{"response_timeout_seconds": 600}}),
	} {
		if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, body); w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin,
		bodyWith([]any{map[string]any{"host": "big.example.com", "max_upload_mb": 0}}, []any{map[string]any{"host": "ai.example.com", "response_timeout_seconds": 0}})); w.Code != 200 {
		t.Fatalf("explicit zeros: %d %s", w.Code, w.Body.String())
	}
	// A console from before the response timeout can't save them away.
	old := map[string]any{"max_upload_mb": 1, "per_ip_concurrent": 0, "total_concurrent": 0, "idle_timeout_seconds": 0, "min_upload_kb_per_second": 0, "hosts": []any{}}
	if w := request(f.store.Handler(), "PUT", "/api/v1/security/connection-limits", f.admin, old); w.Code != 400 {
		t.Fatalf("settings without response timeouts accepted: %d %s", w.Code, w.Body.String())
	}
	f.setConnectionLimits(t, control.ConnectionLimits{MaxUploadMB: 5, Hosts: []control.HostUpload{{Host: " Big.Example.com ", MaxUploadMB: 0}},
		ResponseTimeoutSeconds: 30, ResponseTimeouts: []control.HostResponseTimeout{{Host: " AI.Example.com ", Seconds: 600}, {Host: "batch.example.com", Seconds: 0}}})
	limits := f.store.connectionLimits()
	if limits.uploadLimit("big.example.com") != 0 || limits.uploadLimit("app.example.com") != 5<<20 {
		t.Fatalf("saved limits not applied: %+v", limits.ConnectionLimits)
	}
	if limits.responseTimeout("ai.example.com") != 600*time.Second || limits.responseTimeout("batch.example.com") != 0 || limits.responseTimeout("app.example.com") != 30*time.Second {
		t.Fatalf("saved response timeouts not applied: %+v", limits.ConnectionLimits)
	}
	// Up to 50 hostnames of the longest kind in each list fit in a request.
	long := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 57) + ".com"
	full := control.ConnectionLimits{ResponseTimeoutSeconds: 60}
	for i := range control.MaxUploadHosts {
		full.Hosts = append(full.Hosts, control.HostUpload{Host: fmt.Sprintf("%02d%s", i, long[2:]), MaxUploadMB: 1})
		full.ResponseTimeouts = append(full.ResponseTimeouts, control.HostResponseTimeout{Host: fmt.Sprintf("%02d%s", i, long[2:]), Seconds: 600})
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
	server := httptest.NewServer(f.handler)
	tb.Cleanup(server.Close)
	return strings.TrimPrefix(server.URL, "http://")
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
