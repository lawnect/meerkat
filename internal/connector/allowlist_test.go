package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/securefs"
)

func TestParseAllowedTargets(t *testing.T) {
	for _, entry := range []string{"app", "app:3000", "Postgres:5432", " app ", "127.0.0.1", "127.0.0.1:80", "[::1]:80", "::1", "[::1]", "localhost:3000"} {
		if _, err := parseAllowedTargets([]string{entry}); err != nil {
			t.Errorf("entry %q refused: %v", entry, err)
		}
	}
	for _, entry := range []string{"example.com", "app.internal:80", "10.0.0.5", "10.0.0.5:22", "app:0", "app:65536", "app:http", "app:", "a b", "-app", "app:80:90", "169.254.169.254"} {
		if _, err := parseAllowedTargets([]string{entry}); err == nil {
			t.Errorf("entry %q accepted", entry)
		}
	}
}

func TestAllowedTargetsMatchHostAndPort(t *testing.T) {
	cfg := Config{AllowedTargets: []string{"app", "db:5432", "127.0.0.1:8080", "::1"}}
	for target, want := range map[contract.Target]bool{
		{Host: "app", Port: 1}:           true,
		{Host: "APP", Port: 9000}:        true,
		{Host: "db", Port: 5432}:         true,
		{Host: "db", Port: 22}:           false,
		{Host: "127.0.0.1", Port: 8080}:  true,
		{Host: "127.0.0.1", Port: 22}:    false,
		{Host: "::1", Port: 3000}:        true,
		{Host: "localhost", Port: 3000}:  false,
		{Host: "ip-10-0-1-23", Port: 22}: false,
	} {
		if got := cfg.permits(target); got != want {
			t.Errorf("%v permitted = %v, want %v", target, got, want)
		}
	}
	if !(Config{}).permits(contract.Target{Host: "anything", Port: 22}) {
		t.Error("an empty allowlist restricts targets")
	}
}

func TestLoadReadsAllowedTargets(t *testing.T) {
	for _, key := range []string{"MEERKAT_OUTLET", "MEERKAT_NAME", "MEERKAT_ROOT_CA", "MEERKAT_DEVELOPMENT_HTTP"} {
		t.Setenv(key, "")
	}
	t.Setenv("MEERKAT_DATA_DIR", t.TempDir())
	t.Setenv("MEERKAT_OUTLET", "https://outlet.example.com")
	t.Setenv("MEERKAT_NAME", "office")
	t.Setenv("MEERKAT_ALLOWED_TARGETS", "app, db:5432,")
	t.Setenv("MEERKAT_TARGET", "app:3000")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !c.permits(contract.Target{Host: "db", Port: 5432}) || c.permits(contract.Target{Host: "db", Port: 22}) {
		t.Fatalf("allowlist not applied: %v", c.AllowedTargets)
	}
	t.Setenv("MEERKAT_ALLOWED_TARGETS", "example.com")
	if _, err := Load(""); err == nil {
		t.Fatal("an allowlist entry that can never match was accepted")
	}
	t.Setenv("MEERKAT_ALLOWED_TARGETS", " , ")
	if _, err := Load(""); err == nil {
		t.Fatal("an allowlist without entries was read as no allowlist")
	}
}

// listener counts the connections a target receives, which a health check or
// the tunnel would make.
func listener(t *testing.T) (net.Listener, *atomic.Int32) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	connections := new(atomic.Int32)
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			conn.Close()
		}
	}()
	return l, connections
}

func serviceOn(id string, l net.Listener) contract.ServiceConfiguration {
	return contract.ServiceConfiguration{
		ServiceID: id, Target: contract.Target{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port},
		Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: id, ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"},
	}
}

// In connect mode there is no MEERKAT_TARGET, so the allowlist is the only
// local check on what the Outlet asks the Connector to reach. A service outside
// it is neither tunnelled nor probed, and it reports unhealthy.
func TestRunRefusesTargetsOutsideTheAllowlist(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	allowedApp, allowedHits := listener(t)
	foreign, foreignHits := listener(t)
	current := contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: []contract.ServiceConfiguration{serviceOn("service-ok", allowedApp), serviceOn("service-foreign", foreign)}}
	var mu sync.Mutex
	var heartbeats []contract.Heartbeat
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/connector/v1/heartbeat":
			var heartbeat contract.Heartbeat
			if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
				t.Errorf("decode heartbeat: %v", err)
				return
			}
			heartbeats = append(heartbeats, heartbeat)
			w.WriteHeader(http.StatusOK)
		case "/connector/v1/config":
			_ = json.NewEncoder(w).Encode(current)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := Config{OutletURL: server.URL, Name: "test", DataDir: dir, RatholeBinary: binary, HeartbeatInterval: "1s", DevelopmentHTTP: true,
		AllowedTargets: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(allowedApp.Addr().(*net.TCPAddr).Port))}}
	saved := session{cfg.OutletURL, cfg.Name, contract.EnrollmentResponse{Configuration: current, Credential: "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour)}}
	if err := securefs.WriteJSON(filepath.Join(dir, "session.json"), saved); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	deadline := time.Now().Add(12 * time.Second)
	var heartbeat contract.Heartbeat
	for ; time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		mu.Lock()
		for _, h := range heartbeats {
			if h.ServiceHealths["service-ok"] == "healthy" {
				heartbeat = h
			}
		}
		mu.Unlock()
		if heartbeat.ConnectorID != "" {
			break
		}
	}
	if heartbeat.ConnectorID == "" {
		t.Fatal("the allowed service never reported healthy")
	}
	if heartbeat.ServiceHealths["service-foreign"] != "unhealthy" {
		t.Fatalf("refused service reported %q", heartbeat.ServiceHealths["service-foreign"])
	}
	config, err := os.ReadFile(filepath.Join(dir, "rathole-client.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "service-ok") || strings.Contains(string(config), "service-foreign") {
		t.Fatalf("rathole configuration:\n%s", config)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("connector did not stop after cancellation")
	}
	if foreignHits.Load() != 0 {
		t.Fatalf("a refused target received %d connections", foreignHits.Load())
	}
	if allowedHits.Load() == 0 {
		t.Fatal("the allowed target was never checked")
	}
}
