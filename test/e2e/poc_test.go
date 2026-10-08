package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/outlet"
)

type poc struct {
	t                *testing.T
	root, dir, admin string
	client           *http.Client
}

func (p poc) compose(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	argv := append([]string{"compose", "--env-file", filepath.Join(p.dir, "compose.env"), "-f", filepath.Join(p.root, "deploy/compose.poc.yaml")}, args...)
	return exec.CommandContext(ctx, "docker", argv...).CombinedOutput()
}
func (p poc) command(args ...string) {
	p.t.Helper()
	if b, err := p.compose(args...); err != nil {
		p.t.Fatalf("Compose operation failed: %v\n%s", err, b)
	}
}

func (p poc) request(method, path, token string, body any) (int, []byte) {
	p.t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			p.t.Fatal(err)
		}
	}
	r, err := http.NewRequest(method, "https://localhost:18443"+path, &b)
	if err != nil {
		p.t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	res, err := p.client.Do(r)
	if err != nil {
		return 0, nil
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		p.t.Fatal(err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		p.t.Fatal("missing no-store response header")
	}
	return res.StatusCode, data
}

func (p poc) statuses() map[string]outlet.Status {
	code, b := p.request("GET", "/api/v1/connectors", p.admin, nil)
	if code != 200 {
		return nil
	}
	var response struct {
		Connectors []outlet.Status `json:"connectors"`
	}
	if err := json.Unmarshal(b, &response); err != nil {
		p.t.Fatal("invalid status response")
	}
	m := map[string]outlet.Status{}
	for _, s := range response.Connectors {
		m[s.ID] = s
	}
	return m
}

func (p poc) wait(check func(map[string]outlet.Status) bool) {
	p.t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		if check(p.statuses()) {
			return
		}
		time.Sleep(time.Second)
	}
	p.t.Fatal("timed out waiting for expected connector state")
}

func (p poc) probe(letter string) {
	p.t.Helper()
	port := "31001"
	if letter == "b" {
		port = "31002"
	}
	b, err := p.compose("exec", "-T", "outlet", "/usr/local/bin/poc-probe", "-url", "http://127.0.0.1:"+port+"/", "-expect", "meerkat:app-"+letter)
	if err != nil || strings.TrimSpace(string(b)) != "meerkat:app-"+letter {
		p.t.Fatalf("HTTP tunnel %s failed: %v", letter, err)
	}
}

func healthy(s outlet.Status) bool {
	return s.Status == "connected" && s.TunnelStatus == "connected" && s.Heartbeat != nil && s.Heartbeat.ProcessState == "running" && s.Heartbeat.ServiceHealth == "healthy"
}

func TestPOC(t *testing.T) {
	if os.Getenv("MEERKAT_E2E") != "1" {
		t.Skip("set MEERKAT_E2E=1 after starting the local PoC")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("MEERKAT_POC_DIR")
	if dir == "" {
		dir = filepath.Join(root, ".local/poc")
	}
	pem, err := os.ReadFile(filepath.Join(dir, "configs/root-ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		t.Fatal("invalid local CA")
	}
	admin, err := os.ReadFile(filepath.Join(dir, "configs/admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	p := poc{t: t, root: root, dir: dir, admin: strings.TrimSpace(string(admin)), client: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil}}}
	defer p.client.CloseIdleConnections()
	t.Run("two encrypted HTTP tunnels", func(t *testing.T) {
		p.t = t
		p.wait(func(s map[string]outlet.Status) bool { return healthy(s["connector_a"]) && healthy(s["connector_b"]) })
		p.probe("a")
		p.probe("b")
	})
	if t.Failed() {
		return
	}
	t.Run("registration and authorization", func(t *testing.T) {
		p.t = t
		req := contract.EnrollmentRequest{Name: "connector-a", Version: contract.Version, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		for _, token := range []string{outlet.Token(), readToken(t, filepath.Join(dir, "configs/connector-a.token"))} {
			if code, _ := p.request("POST", "/connector/v1/enrollment", token, req); code != 401 {
				t.Fatalf("invalid/reused token status %d", code)
			}
		}
		var session struct {
			Enrollment contract.EnrollmentResponse `json:"enrollment"`
		}
		b, err := os.ReadFile(filepath.Join(dir, "connector-a-data/session.json"))
		if err != nil {
			t.Fatal(err)
		}
		if json.Unmarshal(b, &session) != nil {
			t.Fatal("invalid saved session")
		}
		hb := contract.Heartbeat{ConnectorID: "connector_a", ServiceID: "service_b", Revision: 1, Version: contract.Version, ProcessState: "running", ServiceHealth: "healthy", ObservedAt: time.Now().UTC()}
		if code, _ := p.request("POST", "/connector/v1/heartbeat", session.Enrollment.Credential, hb); code != 403 {
			t.Fatalf("cross-service heartbeat status %d", code)
		}
		if code, _ := p.request("GET", "/api/v1/connectors", session.Enrollment.Credential, nil); code != 401 {
			t.Fatal("connector accessed administrator API")
		}
	})
	t.Run("rathole crash is restarted", func(t *testing.T) {
		p.t = t
		p.command("exec", "-T", "connector-a", "/bin/sh", "-c", `for p in /proc/[0-9]*/comm; do read name < "$p"; if [ "$name" = rathole ]; then pid=${p#/proc/}; kill -KILL "${pid%/comm}"; fi; done`)
		p.wait(func(s map[string]outlet.Status) bool {
			return healthy(s["connector_a"]) && s["connector_a"].Heartbeat.ProcessRestarts > 0
		})
		p.probe("a")
		p.probe("b")
	})
	t.Run("service failure preserves other connector", func(t *testing.T) {
		p.t = t
		p.command("stop", "app-a")
		defer p.command("start", "app-a")
		p.wait(func(s map[string]outlet.Status) bool {
			a := s["connector_a"]
			return a.Status == "connected" && a.Heartbeat != nil && a.Heartbeat.ProcessState == "running" && a.Heartbeat.ServiceHealth == "unhealthy" && a.TunnelStatus == "disconnected" && healthy(s["connector_b"])
		})
		p.probe("b")
	})
	t.Run("connector restart reuses saved identity", func(t *testing.T) {
		p.t = t
		p.command("stop", "connector-a")
		defer p.command("start", "connector-a")
		p.wait(func(s map[string]outlet.Status) bool {
			return s["connector_a"].Status == "stale" && healthy(s["connector_b"])
		})
		p.probe("b")
		p.command("start", "connector-a")
		p.wait(func(s map[string]outlet.Status) bool { return healthy(s["connector_a"]) && healthy(s["connector_b"]) })
		p.probe("a")
	})
	t.Run("outlet restart retains consumed tokens and credentials", func(t *testing.T) {
		p.t = t
		p.command("restart", "outlet")
		p.command("restart", "rathole")
		p.wait(func(s map[string]outlet.Status) bool { return healthy(s["connector_a"]) && healthy(s["connector_b"]) })
		p.probe("a")
		p.probe("b")
		req := contract.EnrollmentRequest{Name: "connector-a", Version: contract.Version, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
		if code, _ := p.request("POST", "/connector/v1/enrollment", readToken(t, filepath.Join(dir, "configs/connector-a.token")), req); code != 401 {
			t.Fatal("consumed token revived after restart")
		}
	})
}

func readToken(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}
