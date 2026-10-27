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
// guard keeps out a client it doesn't know until its range is allowed, and
// a flood of junk from another address doesn't take the tunnel down.
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

	// Back to limited, with the Connector's address known; junk from another
	// address, which only Linux has on loopback, is held to the limits.
	g.config.Store(compileTunnelGuard(control.DefaultTunnelGuard()))
	g.learn("127.0.0.1")
	if runtime.GOOS == "linux" {
		var wg sync.WaitGroup
		for range 300 {
			wg.Go(func() {
				dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}, Timeout: 2 * time.Second}
				conn, err := dialer.Dial("tcp", g.address().String())
				if err != nil {
					return
				}
				defer conn.Close()
				_, _ = conn.Write([]byte("not a noise handshake, just junk"))
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
