package outlet

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/rathole"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

// startRathole runs the real binary until the test ends.
func startRathole(t *testing.T, binary, mode, config string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), mode+".toml")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binary, "--"+mode, path)
	cmd.Env = append(os.Environ(), "RUST_LOG=off")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
}

// The guard in front of the real rathole v0.5.0 (MEERKAT_RATHOLE names the
// binary): a Noise tunnel is set up through it and carries requests, a strict
// guard keeps out a client it doesn't know until its range is allowed, clients
// that send one byte and go quiet can't hold rathole's accept loop (which does
// the handshake of one connection at a time, up to 5 s each), a handshake
// that rathole answers by hanging up doesn't hold a place for long, and a
// flood of junk from another address doesn't take the tunnel down. The
// attackers need addresses other than 127.0.0.1, which only Linux has.
func TestTunnelGuardWithRealRathole(t *testing.T) {
	binary := os.Getenv("MEERKAT_RATHOLE")
	if binary == "" {
		t.Skip("set MEERKAT_RATHOLE to a rathole v0.5.0 binary to run this")
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private, public := base64.StdEncoding.EncodeToString(key.Bytes()), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes())
	token, internal, service := Token(), freeAddress(t), freeAddress(t)
	server, err := rathole.Server(internal, private, []rathole.ServerService{{Name: "svc", Token: token, BindAddress: service}})
	if err != nil {
		t.Fatal(err)
	}
	startRathole(t, binary, "server", string(server))
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "app-ok") }))
	defer app.Close()

	g := startGuard(t, internal)
	g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{}}))
	client := fmt.Sprintf("[client]\nremote_addr = %q\nretry_interval = 1\n\n[client.transport]\ntype = \"noise\"\n\n[client.transport.noise]\npattern = \"Noise_NK_25519_ChaChaPoly_BLAKE2s\"\nremote_public_key = %q\n\n[client.services.svc]\ntype = \"tcp\"\ntoken = %q\nlocal_addr = %q\n",
		g.address().String(), public, token, strings.TrimPrefix(app.URL, "http://"))
	startRathole(t, binary, "client", client)

	visit := func() string {
		c := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		res, err := c.Get("http://" + service)
		if err != nil {
			return err.Error()
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return string(body)
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); !ok(); time.Sleep(200 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}

	// Strict, and 127.0.0.1 is nobody's: the client is turned away.
	waitFor("the strict guard to refuse the client", func() bool { return g.stats().Refused.Strict >= 2 })
	if got := visit(); got == "app-ok" {
		t.Fatal("the tunnel came up through a strict guard that knows no one")
	}
	// An allowed range lets it in; rathole retries every second.
	g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{"127.0.0.0/8"}}))
	waitFor("the tunnel to carry a request", func() bool { return visit() == "app-ok" })
	if stats := g.stats(); stats.Accepted < 2 {
		t.Fatalf("stats = %+v: a control and a data channel should have been admitted", stats)
	}

	// Back to limited, with the Connector's address known; the rest comes from
	// other addresses, which only Linux has on loopback.
	g.config.Store(compileTunnelGuard(control.DefaultTunnelGuard()))
	g.learn("127.0.0.1")
	attacker := func(last byte) *net.Dialer {
		return &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, last)}, Timeout: 2 * time.Second}
	}
	if runtime.GOOS == "linux" {
		// Every request needs a data channel, and the ones made ahead are used
		// up; from here on each visit goes through rathole's accept loop.
		for range 20 {
			visit()
		}
		// Eight clients send one byte and say nothing more. Passed on at that
		// byte, each held rathole's accept loop for 5 s, one after the other,
		// and no tunnel request got through. Now they wait at the guard, which
		// never dials rathole for them, and a request is answered at once.
		var stalled sync.WaitGroup
		for range 8 {
			stalled.Go(func() {
				conn, err := attacker(3).Dial("tcp", g.address().String())
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = conn.Write([]byte{0})
				_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
				_, _ = io.Copy(io.Discard, conn)
			})
		}
		waitFor("the stallers to be admitted", func() bool { return g.unknownConns.total.Load() == 8 })
		var slowest time.Duration
		for end := time.Now().Add(7 * time.Second); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
			began := time.Now()
			if got := visit(); got != "app-ok" {
				t.Fatalf("a request during the stall attack: %q after %v", got, time.Since(began))
			}
			slowest = max(slowest, time.Since(began))
		}
		if slowest > time.Second {
			t.Fatalf("the slowest request during the stall attack took %v", slowest)
		}
		t.Logf("slowest of the requests made while 8 clients stalled: %v", slowest.Round(time.Millisecond))
		stalled.Wait()
		if stats := g.stats(); stats.Refused.Idle < 8 {
			t.Fatalf("the stallers weren't refused as idle: %+v", stats)
		}

		// A whole first message that is junk: rathole drops it at once, and the
		// guard, which would otherwise wait for a client that has nothing left
		// to say, closes it a few seconds later.
		junk := make([]byte, 50)
		_, _ = rand.Read(junk)
		junk[0], junk[1] = 48, 0
		conn, err := attacker(4).Dial("tcp", g.address().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Write(junk); err != nil {
			t.Fatal(err)
		}
		waitFor("the junk handshake to be admitted", func() bool { return g.unknownConns.total.Load() == 1 })
		began := time.Now()
		waitFor("the place of a client rathole hung up on to be freed", func() bool { return g.unknownConns.total.Load() == 0 })
		if took := time.Since(began); took > guardAfterHangup+2*time.Second {
			t.Fatalf("the junk handshake held its place for %v", took)
		}
		for range 3 {
			if got := visit(); got != "app-ok" {
				t.Fatalf("the tunnel failed after the junk handshake: %q", got)
			}
		}

		// Junk that is the right size for a handshake, more of it than the
		// limits allow.
		var wg sync.WaitGroup
		for range 300 {
			wg.Go(func() {
				conn, err := attacker(2).Dial("tcp", g.address().String())
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = conn.Write(junk)
				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				_, _ = io.Copy(io.Discard, conn)
			})
		}
		wg.Wait()
		if stats := g.stats(); stats.Refused.Rate+stats.Refused.Busy == 0 {
			t.Fatalf("a flood of junk wasn't held to the limits: %+v", stats)
		}
	}
	for range 5 {
		if got := visit(); got != "app-ok" {
			t.Fatalf("the tunnel failed after the flood: %q", got)
		}
	}
}
