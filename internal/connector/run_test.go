package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/rathole"
	"github.com/lawnect/meerkat/internal/securefs"
	"github.com/lawnect/meerkat/internal/transport"
)

// The test executable acts as rathole, so Run can start and stop a real child
// process without a rathole installation.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("Build Version: " + rathole.Version)
		os.Exit(0)
	}
	if len(os.Args) > 2 && os.Args[1] == "--client" {
		_ = os.WriteFile(os.Args[2]+".pid", []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Hour)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

// firstWaitingHeartbeat runs waitForTargets for a configuration whose targets
// cannot be reached and returns the first heartbeat it sends, then checks that
// the wait ends on cancellation.
func firstWaitingHeartbeat(t *testing.T, configuration contract.Configuration) contract.Heartbeat {
	t.Helper()
	heartbeats := make(chan contract.Heartbeat, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connector/v1/heartbeat" {
			t.Errorf("unexpected request: %s", r.URL.Path)
			return
		}
		var heartbeat contract.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			t.Errorf("decode heartbeat: %v", err)
			return
		}
		select {
		case heartbeats <- heartbeat:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	api, err := transport.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	response := contract.EnrollmentResponse{Configuration: configuration, Credential: "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := waitForTargets(ctx, Config{}, api, &response, slog.New(slog.NewTextHandler(io.Discard, nil)))
		result <- err
	}()
	var heartbeat contract.Heartbeat
	select {
	case heartbeat = <-heartbeats:
		cancel()
	case <-time.After(4 * time.Second):
		t.Fatal("no heartbeat for unreachable target")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("connector did not stop after cancellation")
	}
	return heartbeat
}

func TestUnreachableTargetKeepsConnectorReportingServiceError(t *testing.T) {
	heartbeat := firstWaitingHeartbeat(t, contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: []contract.ServiceConfiguration{{ServiceID: "service-test", Target: contract.Target{Host: "missing-app", Port: 3000}}}})
	if heartbeat.ServiceHealths["service-test"] != "unhealthy" || heartbeat.ProcessState != "not_started" || heartbeat.ServiceID != "" {
		t.Fatalf("unexpected heartbeat: %#v", heartbeat)
	}
}

// The Outlet answers 403 to a heartbeat that does not name the service of a
// single-service configuration, which would stop the Connector at startup.
func TestLegacyConfigurationHeartbeatNamesItsServiceWhileWaiting(t *testing.T) {
	heartbeat := firstWaitingHeartbeat(t, contract.Configuration{ConnectorID: "connector-test", ServiceID: "service-test", Revision: 1, Target: contract.Target{Host: "missing-app", Port: 3000}})
	if heartbeat.ServiceID != "service-test" || heartbeat.ProcessState != "not_started" {
		t.Fatalf("unexpected heartbeat: %#v", heartbeat)
	}
}

func TestManyUnreachableTargetsDoNotDelayTheFirstHeartbeat(t *testing.T) {
	var services []contract.ServiceConfiguration
	for i := range 8 {
		services = append(services, contract.ServiceConfiguration{ServiceID: fmt.Sprintf("service-%d", i), Target: contract.Target{Host: fmt.Sprintf("missing-app-%d", i), Port: 3000}})
	}
	// Lookups that each wait their full 2 s timeout would otherwise add up to
	// more than the 4 s allowed here.
	heartbeat := firstWaitingHeartbeat(t, contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: services})
	if len(heartbeat.ServiceHealths) != len(services) {
		t.Fatalf("unexpected heartbeat: %#v", heartbeat)
	}
	for id, state := range heartbeat.ServiceHealths {
		if state != "unhealthy" {
			t.Fatalf("%s = %q", id, state)
		}
	}
}

func TestCorrectedTargetIsPickedUpWithoutRestart(t *testing.T) {
	corrected := contract.Configuration{ConnectorID: "connector-test", Revision: 2, Services: []contract.ServiceConfiguration{{
		ServiceID: "service-test", Target: contract.Target{Host: "127.0.0.1", Port: 80},
		Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service-test", ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"},
	}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connector/v1/heartbeat":
			w.WriteHeader(http.StatusOK)
		case "/connector/v1/config":
			_ = json.NewEncoder(w).Encode(corrected)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	api, err := transport.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	dir := t.TempDir()
	cfg := Config{OutletURL: server.URL, Name: "test", DataDir: dir}
	response := contract.EnrollmentResponse{
		Configuration: contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: []contract.ServiceConfiguration{{ServiceID: "service-test", Target: contract.Target{Host: "missing-app", Port: 3000}}}},
		Credential:    "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	addresses, err := waitForTargets(ctx, cfg, api, &response, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || addresses["service-test"] != "127.0.0.1:80" || response.Revision != 2 {
		t.Fatalf("corrected target: addresses=%#v revision=%d error=%v", addresses, response.Revision, err)
	}
	var saved session
	if err := securefs.ReadJSON(filepath.Join(dir, "session.json"), &saved); err != nil || saved.Enrollment.Revision != 2 {
		t.Fatalf("saved corrected config: revision=%d error=%v", saved.Enrollment.Revision, err)
	}
}

// An update that leaves no reachable service must not end the Connector: it
// stops the tunnel, keeps the new configuration, and starts again by itself
// when a service comes back.
func TestRunWaitsAgainWhenAnUpdateLeavesNoService(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	withService := func(revision int) contract.Configuration {
		return contract.Configuration{ConnectorID: "connector-test", Revision: revision, Services: []contract.ServiceConfiguration{{
			ServiceID: "service-test", Target: contract.Target{Host: "127.0.0.1", Port: app.Addr().(*net.TCPAddr).Port},
			Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service-test", ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"},
		}}}
	}
	var mu sync.Mutex
	current := withService(1)
	var accepted []contract.Heartbeat
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
			if heartbeat.Revision != current.Revision {
				w.WriteHeader(http.StatusConflict)
				return
			}
			accepted = append(accepted, heartbeat)
			w.WriteHeader(http.StatusOK)
		case "/connector/v1/config":
			_ = json.NewEncoder(w).Encode(current)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := Config{OutletURL: server.URL, Name: "test", DataDir: dir, RatholeBinary: binary, HeartbeatInterval: "1s", DevelopmentHTTP: true}
	sessionPath := filepath.Join(dir, "session.json")
	saved := session{cfg.OutletURL, cfg.Name, contract.EnrollmentResponse{Configuration: current, Credential: "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour)}}
	if err := securefs.WriteJSON(sessionPath, saved); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	await := func(what string, match func(contract.Heartbeat) bool) {
		t.Helper()
		for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			select {
			case err := <-result:
				t.Fatalf("connector stopped while waiting for %s: %v", what, err)
			default:
			}
			mu.Lock()
			found := slices.ContainsFunc(accepted, match)
			mu.Unlock()
			if found {
				return
			}
		}
		t.Fatalf("no heartbeat for %s", what)
	}
	// The child writes its PID shortly after it starts.
	pidPath := filepath.Join(dir, "rathole-client.toml.pid")
	childPID := func() int {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if b, err := os.ReadFile(pidPath); err == nil {
				if pid, err := strconv.Atoi(string(b)); err == nil {
					return pid
				}
			}
		}
		t.Fatal("rathole child did not start")
		return 0
	}

	// A running tunnel reports the health of its service.
	await("the running tunnel", func(h contract.Heartbeat) bool { return h.Revision == 1 && h.ServiceHealth == "healthy" })
	first := childPID()

	// The last service is removed.
	mu.Lock()
	current = contract.Configuration{ConnectorID: "connector-test", Revision: 2}
	mu.Unlock()
	await("the waiting phase", func(h contract.Heartbeat) bool { return h.Revision == 2 && h.ServiceHealth == "unknown" })
	select {
	case err := <-result:
		t.Fatalf("connector stopped when the last service was removed: %v", err)
	default:
	}
	if err := syscall.Kill(first, 0); err != syscall.ESRCH {
		t.Fatalf("rathole child %d still running: %v", first, err)
	}
	if err := os.Remove(pidPath); err != nil {
		t.Fatal(err)
	}
	var kept session
	if err := securefs.ReadJSON(sessionPath, &kept); err != nil || kept.Enrollment.Revision != 2 || len(kept.Enrollment.ActiveServices()) != 0 {
		t.Fatalf("saved session: revision=%d services=%d error=%v", kept.Enrollment.Revision, len(kept.Enrollment.ActiveServices()), err)
	}

	// A service returns: the tunnel starts again without a restart.
	mu.Lock()
	current = withService(3)
	mu.Unlock()
	await("the restarted tunnel", func(h contract.Heartbeat) bool { return h.Revision == 3 && h.ServiceHealth == "healthy" })
	if second := childPID(); second == first {
		t.Fatal("rathole was not started again")
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
}
