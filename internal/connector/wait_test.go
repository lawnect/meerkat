package connector

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/securefs"
)

// A Connector waiting with no services takes a newer revision that still has
// none: heartbeats name the revision, and the Outlet refuses an old one.
func TestWaitingConnectorTakesANewerRevisionWithoutServices(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	current := contract.Configuration{ConnectorID: "connector-test", Revision: 1}
	var accepted []contract.Heartbeat
	var conflicts int
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
				conflicts++
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
	saved := session{cfg.OutletURL, cfg.Name, contract.EnrollmentResponse{Configuration: current, Credential: "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour)}}
	if err := securefs.WriteJSON(filepath.Join(dir, "session.json"), saved); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, cfg, "", slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	await := func(what string, match func(contract.Heartbeat) bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
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
	await("revision 1", func(h contract.Heartbeat) bool { return h.Revision == 1 && h.ProcessState == "running" })
	mu.Lock()
	current = contract.Configuration{ConnectorID: "connector-test", Revision: 2}
	mu.Unlock()
	await("revision 2", func(h contract.Heartbeat) bool { return h.Revision == 2 && h.ProcessState == "running" })
	var kept session
	if err := securefs.ReadJSON(filepath.Join(dir, "session.json"), &kept); err != nil || kept.Enrollment.Revision != 2 {
		t.Fatalf("saved session: revision=%d error=%v", kept.Enrollment.Revision, err)
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
