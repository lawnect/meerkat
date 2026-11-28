package outlet

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// upgradeBackend is a service that agrees to any protocol switch it is asked
// for, as Hypercorn does for h2c, and then echoes what the client sends on
// the connection it took over. It records the headers of every request it
// got.
type upgradeBackend struct {
	mu   sync.Mutex
	seen []http.Header
}

func (b *upgradeBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.seen = append(b.seen, r.Header.Clone())
	b.mu.Unlock()
	protocol := r.Header.Get("Upgrade")
	if protocol == "" {
		_, _ = io.WriteString(w, "plain "+r.URL.Path)
		return
	}
	conn, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = io.WriteString(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+protocol+"\r\n\r\n")
	_ = rw.Flush()
	_, _ = io.Copy(conn, rw)
}

func (b *upgradeBackend) requests() []http.Header {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]http.Header(nil), b.seen...)
}

// A request to switch to anything but WebSocket goes through as plain HTTP:
// the service never sees the upgrade, so it can't answer with a 101 that
// would hand the client the connection, with every later request past access
// policies, block rules, limits and the log, and free to forge the headers
// the Outlet sets.
func TestOnlyWebSocketUpgradesAreTunnelled(t *testing.T) {
	for name, headers := range map[string]string{
		"h2c":                  "Connection: Upgrade, HTTP2-Settings\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABkAARAAAAAAAIAAAAA\r\n",
		"h2c settings unnamed": "Connection: Upgrade\r\nUpgrade: h2c\r\nHTTP2-Settings: AAMAAABkAARAAAAAAAIAAAAA\r\n",
		"h2c without settings": "Connection: upgrade\r\nUpgrade: h2c\r\n",
		"other protocol":       "Connection: Upgrade\r\nUpgrade: echo\r\n",
		"websocket and more":   "Connection: Upgrade\r\nUpgrade: websocket, h2c\r\n",
		"two upgrade lines":    "Connection: Upgrade\r\nUpgrade: websocket\r\nUpgrade: h2c\r\n",
		"websocket lookalike":  "Connection: Upgrade\r\nUpgrade: websockets\r\n",
		"settings alone":       "HTTP2-Settings: AAMAAABkAARAAAAAAAIAAAAA\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			backend := &upgradeBackend{}
			f := newProxyFixture(t, backend)
			f.route(t, 0, "app.example.com")
			address := f.guardedServer(t, &connLimits{idle: 5 * time.Second})
			conn, reader := dial(t, address)
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

			_, _ = io.WriteString(conn, "GET /public HTTP/1.1\r\nHost: app.example.com\r\nX-Forwarded-For: 198.51.100.7\r\n"+headers+"\r\n")
			res, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatalf("no response: %v", err)
			}
			if res.StatusCode == http.StatusSwitchingProtocols {
				t.Fatalf("the connection was handed to the service: %d %v", res.StatusCode, res.Header)
			}
			if body, _ := io.ReadAll(res.Body); res.StatusCode != 200 || string(body) != "plain /public" {
				t.Fatalf("answer: %d %q", res.StatusCode, body)
			}

			// What follows on the connection is still a request to the Outlet:
			// its client address wins over what the client claims.
			_, _ = io.WriteString(conn, "GET /second HTTP/1.1\r\nHost: app.example.com\r\nX-Forwarded-For: 198.51.100.7\r\nX-Real-IP: 6.6.6.6\r\nX-Meerkat-Access-Email: admin@corp.example.com\r\n\r\n")
			res, err = http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatalf("second request: %v", err)
			}
			if body, _ := io.ReadAll(res.Body); res.StatusCode != 200 || string(body) != "plain /second" {
				t.Fatalf("second request: %d %q", res.StatusCode, body)
			}
			seen := backend.requests()
			if len(seen) != 2 {
				t.Fatalf("the service got %d requests", len(seen))
			}
			for i, header := range seen {
				if header.Get("Upgrade") != "" || header.Get("Http2-Settings") != "" || strings.Contains(strings.ToLower(strings.Join(header.Values("Connection"), ",")), "upgrade") {
					t.Fatalf("request %d reached the service as an upgrade: %v", i, header)
				}
			}
			if seen[1].Get("X-Real-Ip") != "198.51.100.7" || seen[1].Get(accessEmailHeader) != "" {
				t.Fatalf("the second request carried forged headers: %v", seen[1])
			}
		})
	}
}

// A WebSocket upgrade still works, in whatever case it is written, and the
// service gets it as one.
func TestWebSocketUpgradesStillWork(t *testing.T) {
	backend := &upgradeBackend{}
	f := newProxyFixture(t, backend)
	f.route(t, 0, "ws.example.com")
	address := f.guardedServer(t, &connLimits{idle: 5 * time.Second})
	for _, headers := range []string{
		"Connection: Upgrade\r\nUpgrade: websocket\r\n",
		"Connection: keep-alive, Upgrade\r\nUpgrade: WebSocket\r\nHTTP2-Settings: AAMAAABkAARAAAAAAAIAAAAA\r\n",
		"Connection: upgrade\r\nUpgrade:  WEBSOCKET \r\n",
	} {
		conn, reader := dial(t, address)
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.WriteString(conn, "GET /socket HTTP/1.1\r\nHost: ws.example.com\r\n"+headers+"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n")
		res, err := http.ReadResponse(reader, nil)
		if err != nil || res.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(res.Header.Get("Upgrade"), "websocket") {
			t.Fatalf("%q: upgrade: %v %v", headers, res, err)
		}
		_, _ = io.WriteString(conn, "ping")
		got := make([]byte, 4)
		if _, err := io.ReadFull(reader, got); err != nil || string(got) != "ping" {
			t.Fatalf("%q: upgraded connection: %q %v", headers, got, err)
		}
	}
	seen := backend.requests()
	if len(seen) != 3 {
		t.Fatalf("the service got %d requests", len(seen))
	}
	for _, header := range seen {
		if !strings.EqualFold(header.Get("Upgrade"), "websocket") || header.Get("Http2-Settings") != "" || header.Get("Sec-Websocket-Key") == "" {
			t.Fatalf("the service got: %v", header)
		}
	}
}

// A service that answers a plain request, or a WebSocket upgrade, with a
// switch to another protocol doesn't get the connection.
func TestUnrequestedProtocolSwitchesAreNotRelayed(t *testing.T) {
	var mu sync.Mutex
	var answer string
	switching := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		mu.Lock()
		reply := answer
		mu.Unlock()
		_, _ = io.WriteString(rw, reply)
		_ = rw.Flush()
		_, _ = io.Copy(conn, rw)
	})
	f := newProxyFixture(t, switching)
	f.route(t, 0, "app.example.com")
	address := f.guardedServer(t, &connLimits{idle: 5 * time.Second})
	for _, c := range []struct{ request, answer string }{
		{"", "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n"},
		{"", "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n"},
		{"", "HTTP/1.1 101 Switching Protocols\r\n\r\n"},
		{"Connection: Upgrade\r\nUpgrade: h2c\r\n", "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n"},
		{"Connection: Upgrade\r\nUpgrade: websocket\r\n", "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: h2c\r\n\r\n"},
		{"Connection: Upgrade\r\nUpgrade: websocket\r\n", "HTTP/1.1 101 Switching Protocols\r\n\r\n"},
	} {
		mu.Lock()
		answer = c.answer
		mu.Unlock()
		conn, reader := dial(t, address)
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _ = io.WriteString(conn, "GET /x HTTP/1.1\r\nHost: app.example.com\r\n"+c.request+"\r\n")
		res, err := http.ReadResponse(reader, nil)
		if err != nil {
			t.Fatalf("%q answered %q: %v", c.request, c.answer, err)
		}
		if res.StatusCode != http.StatusBadGateway {
			t.Fatalf("%q answered %q: the client got %d %v", c.request, c.answer, res.StatusCode, res.Header)
		}
	}
}
