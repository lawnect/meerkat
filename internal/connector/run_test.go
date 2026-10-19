package connector

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/securefs"
	"github.com/lawnect/meerkat/internal/transport"
)

func TestUnreachableTargetKeepsConnectorReportingServiceError(t *testing.T) {
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
		heartbeats <- heartbeat
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	api, err := transport.New(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	response := contract.EnrollmentResponse{
		Configuration: contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: []contract.ServiceConfiguration{{ServiceID: "service-test", Target: contract.Target{Host: "missing-app", Port: 3000}}}},
		Credential:    "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := waitForTargets(ctx, Config{}, api, &response, slog.New(slog.NewTextHandler(io.Discard, nil)))
		result <- err
	}()
	select {
	case heartbeat := <-heartbeats:
		if heartbeat.ServiceHealths["service-test"] != "unhealthy" || heartbeat.ProcessState != "not_started" {
			t.Fatalf("unexpected heartbeat: %#v", heartbeat)
		}
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
