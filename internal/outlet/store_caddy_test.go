package outlet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

func TestOutletStartsWithEmptyCaddyfile(t *testing.T) {
	dir := t.TempDir()
	db, err := control.Open(context.Background(), filepath.Join(dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(Token()), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 600}
	if _, err := OpenWithControl(config, dir, db); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "caddy", "Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), ":9090 {") || strings.Contains(string(contents), "issuer acme") {
		t.Fatalf("invalid pre-domain Caddyfile: %q", contents)
	}
}

// A deployment that renames the outlet service points Caddy at it; Caddy's
// certificate check then arrives with that Host, and the old name is just a
// public request.
func TestCaddyUpstreamFollowsTheDeployment(t *testing.T) {
	dir := t.TempDir()
	db, err := control.Open(context.Background(), filepath.Join(dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(Token()), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 600, Domain: "example.com", CaddyUpstream: "outlet-k3x9:8080"}
	store, err := OpenWithControl(config, dir, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.reconcileProxy(context.Background()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "caddy", "Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "reverse_proxy http://outlet-k3x9:8080 {") || strings.Contains(string(contents), "outlet:8080") {
		t.Fatalf("Caddyfile ignores the upstream: %q", contents)
	}
	ask := httptest.NewRequest("GET", certificateAskPath, nil)
	ask.Host = "outlet-k3x9:8080"
	old := httptest.NewRequest("GET", certificateAskPath, nil)
	old.Host = "outlet:8080"
	if !store.isCertificateAsk(ask) || store.isCertificateAsk(old) {
		t.Fatal("certificate check doesn't follow the upstream")
	}
	config.CaddyUpstream = "outlet:8080 {"
	if _, err := OpenWithControl(config, t.TempDir(), nil); err == nil {
		t.Fatal("invalid upstream accepted")
	}
}

// Traffic usage is read from wherever the deployment says Caddy's metrics
// are, not from a fixed caddy:9090.
func TestUsageIsReadFromTheConfiguredMetrics(t *testing.T) {
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	f.route(t, 0, "app.example.com")
	var asked atomic.Int32
	metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			asked.Add(1)
		}
	}))
	defer metrics.Close()
	f.store.config.CaddyMetrics = strings.TrimPrefix(metrics.URL, "http://")
	if err := f.store.CollectUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked.Load() != 1 {
		t.Fatalf("metrics asked %d times", asked.Load())
	}
}
