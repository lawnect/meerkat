package outlet

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

type fixture struct {
	store                      *Store
	cfg                        Config
	dir, admin, tokenA, tokenB string
}

func setup(t *testing.T) fixture {
	t.Helper()
	f := fixture{dir: t.TempDir(), admin: Token(), tokenA: Token(), tokenB: Token()}
	f.cfg = Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(f.admin), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 3600, TunnelRemoteAddress: "outlet:2333", TunnelServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	for i, name := range []string{"a", "b"} {
		token := f.tokenA
		if i == 1 {
			token = f.tokenB
		}
		c := contract.Configuration{ConnectorID: "connector_" + name, ServiceID: "service_" + name, Revision: 1, Target: contract.Target{Host: "app-" + name, Port: 3000}, Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service_" + name, ServiceToken: Token()}}
		f.cfg.Connectors = append(f.cfg.Connectors, Seed{Name: "connector-" + name, Configuration: c, ProbeAddress: "127.0.0.1:" + []string{"31001", "31002"}[i], EnrollmentHash: Hash(token), EnrollmentExpiresAt: time.Now().Add(time.Hour)})
	}
	var err error
	f.store, err = Open(f.cfg, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func request(handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func enrollment(name string) contract.EnrollmentRequest {
	return contract.EnrollmentRequest{Name: name, Version: contract.Version, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
}
func registered(t *testing.T, f fixture) contract.EnrollmentResponse {
	t.Helper()
	w := request(f.store.Handler(), "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a"))
	if w.Code != 201 {
		t.Fatalf("enrollment status %d", w.Code)
	}
	var response contract.EnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestEnrollmentAuthenticationAndPersistence(t *testing.T) {
	f := setup(t)
	h := f.store.Handler()
	for _, tc := range []struct {
		token, name string
		status      int
	}{{Token(), "connector-a", 401}, {f.tokenA, "connector-b", 403}} {
		if got := request(h, "POST", "/connector/v1/enrollment", tc.token, enrollment(tc.name)).Code; got != tc.status {
			t.Fatalf("status %d, want %d", got, tc.status)
		}
	}
	response := registered(t, f)
	if w := request(h, "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a")); w.Code != 401 {
		t.Fatal("token reuse accepted")
	}
	data, err := os.ReadFile(f.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(f.tokenA)) || bytes.Contains(data, []byte(response.Credential)) {
		t.Fatal("raw enrollment/API token persisted")
	}
	reopened, err := Open(f.cfg, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reopened.records["connector_a"].LastSeen.IsZero() || reopened.records["connector_a"].TunnelStatus != "unknown" {
		t.Fatal("restart reused observed liveness")
	}
	w := request(reopened.Handler(), "GET", "/connector/v1/config", response.Credential, nil)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("saved credential/config unavailable")
	}
	if strings.Contains(w.Body.String(), "service_b") {
		t.Fatal("another connector's configuration leaked")
	}
	w = request(h, "GET", "/api/v1/connectors", response.Credential, nil)
	if w.Code != 401 {
		t.Fatal("connector authorized as administrator")
	}
	w = request(h, "GET", "/api/v1/connectors", f.admin, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), response.Credential) || strings.Contains(w.Body.String(), response.Tunnel.ServiceToken) {
		t.Fatal("admin status leaked credentials")
	}
}

func TestConcurrentEnrollmentConsumesOnce(t *testing.T) {
	f := setup(t)
	h := f.store.Handler()
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a"))
			if w.Code == 201 {
				wins.Add(1)
			} else if w.Code != 401 {
				t.Errorf("unexpected enrollment status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("enrollment successes %d", wins.Load())
	}
}

func TestExpiryAndFreshToken(t *testing.T) {
	f := setup(t)
	h := f.store.Handler()
	now := time.Now()
	f.store.now = func() time.Time { return now.Add(2 * time.Hour) }
	if w := request(h, "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a")); w.Code != 401 {
		t.Fatal("expired enrollment accepted")
	}
	w := request(h, "POST", "/api/v1/connectors/connector_a/enrollment-tokens", f.admin, nil)
	if w.Code != 201 {
		t.Fatalf("token issuance %d", w.Code)
	}
	var fresh struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &fresh)
	w = request(h, "POST", "/connector/v1/enrollment", fresh.Token, enrollment("connector-a"))
	if w.Code != 201 {
		t.Fatal("fresh token rejected")
	}
	var response contract.EnrollmentResponse
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w := request(h, "POST", "/api/v1/connectors/connector_a/enrollment-tokens", f.admin, nil); w.Code != 409 {
		t.Fatal("active identity replaced")
	}
	f.store.now = func() time.Time { return now.Add(4 * time.Hour) }
	if w := request(h, "GET", "/connector/v1/config", response.Credential, nil); w.Code != 401 {
		t.Fatal("expired credential accepted")
	}
}

func TestFailedPersistenceDoesNotConsumeToken(t *testing.T) {
	f := setup(t)
	path := f.store.path
	f.store.path = f.dir // rename onto a directory fails
	if w := request(f.store.Handler(), "POST", "/connector/v1/enrollment", f.tokenA, enrollment("connector-a")); w.Code != 503 {
		t.Fatalf("status %d", w.Code)
	}
	f.store.path = path
	_ = registered(t, f)
}

func TestHeartbeatOwnershipAndStatusSeparation(t *testing.T) {
	f := setup(t)
	probe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer probe.Close()
	r := f.store.records["connector_a"]
	r.ProbeAddress = strings.TrimPrefix(probe.URL, "http://")
	f.store.records["connector_a"] = r
	response := registered(t, f)
	h := f.store.Handler()
	hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "unhealthy", ObservedAt: time.Now().UTC()}
	hb.ServiceID = "service_b"
	if w := request(h, "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 403 {
		t.Fatal("cross-service heartbeat accepted")
	}
	hb.ServiceID = response.ServiceID
	if w := request(h, "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 200 {
		t.Fatalf("heartbeat %d", w.Code)
	}
	if f.store.records["connector_a"].LastHeartbeat.ServiceHealth != "unhealthy" || f.store.records["connector_a"].TunnelStatus != "connected" {
		t.Fatal("process, health and tunnel states conflated")
	}
	probe.Close()
	hb.ObservedAt = time.Now().UTC()
	if w := request(h, "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 200 {
		t.Fatal("offline tunnel heartbeat failed")
	}
	if f.store.records["connector_a"].TunnelStatus != "disconnected" {
		t.Fatal("offline tunnel marked connected")
	}
	f.store.now = func() time.Time { return time.Now().Add(20 * time.Second) }
	w := request(h, "GET", "/api/v1/connectors", f.admin, nil)
	if !strings.Contains(w.Body.String(), `"status":"stale"`) {
		t.Fatal("stale heartbeat not detected")
	}
}

func TestStrictRequestValidation(t *testing.T) {
	f := setup(t)
	for _, body := range []string{`{"connector_name":"connector-a","connector_version":"0.1.0","public_key":"bad"}`, `{"unknown":true}`, `{} {}`, strings.Repeat(" ", 17000) + `{}`} {
		r := httptest.NewRequest("POST", "/connector/v1/enrollment", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.tokenA)
		w := httptest.NewRecorder()
		f.store.Handler().ServeHTTP(w, r)
		if w.Code != 400 || strings.Contains(w.Body.String(), f.tokenA) {
			t.Fatal("invalid request accepted or secret echoed")
		}
	}
}

func TestRestartRequiresNewLivenessObservation(t *testing.T) {
	f := setup(t)
	record := f.store.records["connector_a"]
	record.Enrolled = true
	record.LastSeen = time.Now().UTC()
	record.TunnelStatus = "connected"
	if err := f.store.save("connector_a", record); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(f.cfg, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if r := reopened.records["connector_a"]; !r.LastSeen.IsZero() || r.TunnelStatus != "unknown" {
		t.Fatal("persisted heartbeat reused as current liveness")
	}
	w := request(reopened.Handler(), "GET", "/api/v1/connectors", f.admin, nil)
	if strings.Contains(w.Body.String(), `"status":"connected"`) {
		t.Fatal("connector appears connected before a new heartbeat")
	}
}
