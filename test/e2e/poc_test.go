package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/outlet"
)

// The admin port and the Outlet's public listener, where Caddy would send
// requests, as compose.poc.yaml publishes them.
const (
	adminURL  = "https://localhost:18443"
	publicURL = "http://127.0.0.1:18080"
)

type poc struct {
	t                *testing.T
	root, dir, admin string
	client, public   *http.Client
}

// tunnel is one of the PoC's two tunnels: Connector connector-<letter>
// serving app-<letter> at app-<letter>.poc.test.
type tunnel struct {
	letter, id, serviceID, token string
}

func (n tunnel) name() string     { return "connector-" + n.letter }
func (n tunnel) app() string      { return "app-" + n.letter }
func (n tunnel) hostname() string { return n.app() + ".poc.test" }

// state is what the admin API reports for a tunnel: its connection status
// and each service's.
type state struct {
	status   string
	services map[string]string
}

func (s state) healthy() bool {
	if s.status != "connected" || len(s.services) == 0 {
		return false
	}
	for _, status := range s.services {
		if status != "connected" {
			return false
		}
	}
	return true
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
	r, err := http.NewRequest(method, adminURL+path, &b)
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

// call is an admin API request that must answer want; out, when given,
// receives the response.
func (p poc) call(method, path string, body any, want int, out any) {
	p.t.Helper()
	code, b := p.request(method, path, p.admin, body)
	if code != want {
		p.t.Fatalf("%s %s: status %d, want %d\n%s", method, path, code, want, b)
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		p.t.Fatalf("%s %s: invalid response", method, path)
	}
}

// get reads an admin API list; false while the API doesn't answer, as
// during an Outlet restart.
func (p poc) get(path string, out any) bool {
	p.t.Helper()
	code, b := p.request("GET", path, p.admin, nil)
	if code == 0 || code == 502 || code == 503 {
		return false
	}
	if code != 200 || json.Unmarshal(b, out) != nil {
		p.t.Fatalf("GET %s: status %d\n%s", path, code, b)
	}
	return true
}

func (p poc) connectors() ([]control.Connector, bool) {
	p.t.Helper()
	var response struct {
		Connectors []control.Connector `json:"connectors"`
	}
	ok := p.get("/api/v1/connectors", &response)
	return response.Connectors, ok
}

func (p poc) services(id string) ([]control.Service, bool) {
	p.t.Helper()
	var response struct {
		Services []control.Service `json:"services"`
	}
	ok := p.get("/api/v1/connectors/"+id+"/services", &response)
	return response.Services, ok
}

// states reports every tunnel by name; nil while the admin API doesn't
// answer.
func (p poc) states() map[string]state {
	p.t.Helper()
	connectors, ok := p.connectors()
	if !ok {
		return nil
	}
	m := map[string]state{}
	for _, c := range connectors {
		services, ok := p.services(c.ID)
		if !ok {
			return nil
		}
		s := state{status: c.Status, services: map[string]string{}}
		for _, service := range services {
			s.services[service.ID] = service.ObservedStatus
		}
		m[c.Name] = s
	}
	return m
}

func (p poc) wait(check func(map[string]state) bool) {
	p.t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if s := p.states(); s != nil && check(s) {
			return
		}
		time.Sleep(time.Second)
	}
	p.t.Fatalf("timed out waiting for expected tunnel state: %v", p.states())
}

// serves says whether the tunnel's public address answers with its app,
// through the Outlet's public listener, the tunnel and the Connector.
func (p poc) serves(n tunnel) bool {
	r, err := http.NewRequest("GET", publicURL+"/", nil)
	if err != nil {
		p.t.Fatal(err)
	}
	r.Host = n.hostname()
	res, err := p.public.Do(r)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1024))
	return err == nil && res.StatusCode == 200 && strings.TrimSpace(string(b)) == "meerkat:"+n.app()
}

// ratholePID is the process ID of rathole in the Connector's container,
// or "" when none runs.
func (p poc) ratholePID(n tunnel) string {
	p.t.Helper()
	b, err := p.compose("exec", "-T", n.name(), "/bin/sh", "-c", `for p in /proc/[0-9]*/comm; do read name < "$p"; if [ "$name" = rathole ]; then pid=${p#/proc/}; echo "${pid%/comm}"; fi; done`)
	if err != nil {
		p.t.Fatalf("cannot list %s processes: %v\n%s", n.name(), err, b)
	}
	return strings.TrimSpace(string(b))
}

func (p poc) enroll(token, name string) int {
	p.t.Helper()
	req := contract.EnrollmentRequest{Name: name, Version: contract.Version, PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32))}
	code, _ := p.request("POST", "/connector/v1/enrollment", token, req)
	return code
}

func (p poc) issueToken(id, query string) string {
	p.t.Helper()
	var issued struct {
		Token string `json:"token"`
	}
	p.call("POST", "/api/v1/connectors/"+id+"/enrollment-tokens"+query, nil, 201, &issued)
	return issued.Token
}

// setUp creates the tunnel the way the console does: the tunnel, its run
// token for the Connector, and once the Connector connects, the service and
// its public address. A fixture set up by an earlier run keeps its tunnel.
func (p poc) setUp(n *tunnel) {
	p.t.Helper()
	tokenPath := filepath.Join(p.dir, n.name()+"-token", "enrollment.token")
	if b, err := os.ReadFile(tokenPath); err == nil {
		n.token = strings.TrimSpace(string(b))
		connectors, _ := p.connectors()
		for _, c := range connectors {
			if c.Name == n.name() {
				n.id = c.ID
			}
		}
		if n.id == "" {
			p.t.Fatalf("%s has a run token but no tunnel; create a new fixture with poc-init", n.name())
		}
	} else if !os.IsNotExist(err) {
		p.t.Fatal(err)
	} else {
		var created control.Connector
		p.call("POST", "/api/v1/connectors", map[string]string{"name": n.name()}, 201, &created)
		n.id = created.ID
		n.token = p.issueToken(n.id, "")
		// Renamed into place, so the waiting Connector never reads half a token.
		partial := tokenPath + ".partial"
		if err := os.WriteFile(partial, []byte(n.token+"\n"), 0600); err != nil {
			p.t.Fatal(err)
		}
		if err := os.Rename(partial, tokenPath); err != nil {
			p.t.Fatal(err)
		}
	}
	p.wait(func(s map[string]state) bool { return s[n.name()].status == "connected" })
	services, _ := p.services(n.id)
	if len(services) == 0 {
		var service control.Service
		p.call("POST", "/api/v1/connectors/"+n.id+"/services", map[string]any{"name": n.app(), "protocol": "http", "target": contract.Target{Host: n.app(), Port: 3000}}, 201, &service)
		services = append(services, service)
	}
	n.serviceID = services[0].ID
	var routes struct {
		Routes []control.Route `json:"routes"`
	}
	p.call("GET", "/api/v1/services/"+n.serviceID+"/routes", nil, 200, &routes)
	if len(routes.Routes) == 0 {
		p.call("POST", "/api/v1/services/"+n.serviceID+"/routes", map[string]string{"hostname": n.hostname()}, 201, nil)
	}
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
	p := poc{t: t, root: root, dir: dir, admin: strings.TrimSpace(string(admin)),
		client: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, Proxy: nil}},
		public: &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	defer p.client.CloseIdleConnections()
	a, b := &tunnel{letter: "a"}, &tunnel{letter: "b"}
	both := func(s map[string]state) bool {
		return s[a.name()].healthy() && s[b.name()].healthy() && p.serves(*a) && p.serves(*b)
	}
	t.Run("two tunnels set up through the admin API", func(t *testing.T) {
		p.t = t
		// The Outlet may still be starting.
		p.wait(func(map[string]state) bool { return true })
		p.setUp(a)
		p.setUp(b)
		p.wait(both)
	})
	if t.Failed() {
		return
	}
	// revoked is the run token of spare, a tunnel no Connector runs, replaced
	// by a reset; it must stay refused, also after the Outlet restarts.
	var spare control.Connector
	var revoked string
	top := t
	t.Run("registration and authorization", func(t *testing.T) {
		p.t = t
		if code := p.enroll(outlet.Token(), a.name()); code != 401 {
			t.Fatalf("unknown token status %d", code)
		}
		if code := p.enroll(a.token, b.name()); code != 403 {
			t.Fatalf("another tunnel's name status %d", code)
		}
		p.call("POST", "/api/v1/connectors", map[string]string{"name": fmt.Sprintf("spare-%d", time.Now().Unix())}, 201, &spare)
		top.Cleanup(func() {
			p.t = top
			p.call("DELETE", "/api/v1/connectors/"+spare.ID, nil, 204, nil)
		})
		revoked = p.issueToken(spare.ID, "")
		if code := p.enroll(revoked, spare.Name); code != 201 {
			t.Fatalf("run token status %d", code)
		}
		replacement := p.issueToken(spare.ID, "?reset=true")
		if code := p.enroll(revoked, spare.Name); code != 401 {
			t.Fatalf("reset token status %d", code)
		}
		if code := p.enroll(replacement, spare.Name); code != 201 {
			t.Fatalf("replacement token status %d", code)
		}
		hb := contract.Heartbeat{ConnectorID: b.id, Revision: 1, Version: contract.Version, ProcessState: "running", ServiceHealth: "healthy", ObservedAt: time.Now().UTC()}
		if code, _ := p.request("POST", "/connector/v1/heartbeat", a.token, hb); code != 403 {
			t.Fatalf("cross-tunnel heartbeat status %d", code)
		}
		if code, _ := p.request("GET", "/api/v1/connectors", a.token, nil); code != 401 {
			t.Fatal("connector accessed administrator API")
		}
	})
	t.Run("rathole crash is restarted", func(t *testing.T) {
		p.t = t
		before := p.ratholePID(*a)
		if before == "" {
			t.Fatal("no rathole runs for connector-a")
		}
		p.command("exec", "-T", a.name(), "/bin/sh", "-c", "kill -KILL "+before)
		p.wait(func(s map[string]state) bool {
			after := p.ratholePID(*a)
			return after != "" && after != before && both(s)
		})
	})
	t.Run("service failure preserves other tunnel", func(t *testing.T) {
		p.t = t
		p.command("stop", a.app())
		defer p.command("start", a.app())
		p.wait(func(s map[string]state) bool {
			return s[a.name()].status == "connected" && s[a.name()].services[a.serviceID] == "error" && !p.serves(*a) && s[b.name()].healthy() && p.serves(*b)
		})
		p.command("start", a.app())
		p.wait(both)
	})
	t.Run("connector restart reconnects", func(t *testing.T) {
		p.t = t
		p.command("stop", a.name())
		defer p.command("start", a.name())
		p.wait(func(s map[string]state) bool {
			return s[a.name()].status == "stale" && s[b.name()].healthy() && p.serves(*b)
		})
		p.command("start", a.name())
		p.wait(both)
	})
	t.Run("outlet restart keeps tunnels and revoked tokens", func(t *testing.T) {
		p.t = t
		p.command("restart", "outlet")
		// rathole shares the Outlet's network namespace, which the restart replaced.
		p.command("restart", "rathole")
		p.wait(both)
		if revoked == "" {
			t.Skip("no reset token from the authorization test")
		}
		if code := p.enroll(revoked, spare.Name); code != 401 {
			t.Fatalf("reset token status %d after restart", code)
		}
	})
}
