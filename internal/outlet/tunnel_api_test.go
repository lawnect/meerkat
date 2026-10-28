package outlet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/validation"
)

func controlStore(t *testing.T, f fixture) (*Store, *control.Database) {
	t.Helper()
	db, err := control.Open(context.Background(), filepath.Join(f.dir, "tunnel-control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, db
}

func decodeOutlet(t *testing.T, body []byte) outletResponse {
	t.Helper()
	var outlet outletResponse
	if err := json.Unmarshal(body, &outlet); err != nil {
		t.Fatal(err)
	}
	return outlet
}

// enrolledConnector creates a Connector with one service and enrolls it.
func enrolledConnector(t *testing.T, h http.Handler, admin string) (control.Connector, contract.EnrollmentResponse) {
	t.Helper()
	return enrolledConnectorNamed(t, h, admin, "edge-a")
}

func enrolledConnectorNamed(t *testing.T, h http.Handler, admin, name string) (control.Connector, contract.EnrollmentResponse) {
	t.Helper()
	w := request(h, "POST", "/api/v1/connectors", admin, map[string]any{"name": name})
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil || w.Code != 201 {
		t.Fatalf("connector: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}}); w.Code != 201 {
		t.Fatalf("service: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", admin, nil)
	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil || w.Code != 201 {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/connector/v1/enrollment", token.Token, enrollment(name))
	var enrolled contract.EnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enrolled); err != nil || w.Code != 201 {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	return connector, enrolled
}

func assertTunnelAddress(t *testing.T, store *Store, connectorID, want string) {
	t.Helper()
	config := store.managed[connectorID].Configuration
	if err := validation.Configuration(config); err != nil {
		t.Fatalf("invalid configuration: %v", err)
	}
	for _, service := range config.ActiveServices() {
		if service.Tunnel.RemoteAddress != want {
			t.Fatalf("service %s uses %q, want %q", service.ServiceID, service.Tunnel.RemoteAddress, want)
		}
	}
}

func TestTunnelAddressSettingRetargetsConnectors(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelRemoteAddress = ""
	f.cfg.TunnelListenAddress = "0.0.0.0:2333"
	f.cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, _ := controlStore(t, f)
	h := store.Handler()
	// The default address uses the generated management host, not the domain
	// root, which may serve another site.
	w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"})
	outlet := decodeOutlet(t, w.Body.Bytes())
	label := strings.TrimSuffix(outlet.AdminHostname, ".example.com")
	defaultAddress := label + ".example.com:2333"
	if w.Code != 200 || len(label) != 8 || outlet.Tunnel != (outletTunnel{Address: defaultAddress, DefaultAddress: defaultAddress}) {
		t.Fatalf("outlet: %d %s", w.Code, w.Body.String())
	}
	connector, enrolled := enrolledConnector(t, h, f.admin)
	if got := enrolled.Services[0].Tunnel.RemoteAddress; got != defaultAddress {
		t.Fatalf("default tunnel address = %q", got)
	}

	w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": " Tunnel.Example.com "})
	if outlet := decodeOutlet(t, w.Body.Bytes()); w.Code != 200 || outlet.Tunnel != (outletTunnel{Address: "tunnel.example.com:2333", DefaultAddress: defaultAddress, Custom: true}) {
		t.Fatalf("custom tunnel: %d %s", w.Code, w.Body.String())
	}
	hb := contract.Heartbeat{ConnectorID: connector.ID, Revision: enrolled.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "unknown", ServiceHealths: map[string]string{enrolled.Services[0].ServiceID: "healthy"}, ObservedAt: time.Now().UTC()}
	if w = request(h, "POST", "/connector/v1/heartbeat", enrolled.Credential, hb); w.Code != 409 {
		t.Fatalf("heartbeat after retarget: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/connector/v1/config", enrolled.Credential, nil)
	var updated contract.Configuration
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || w.Code != 200 {
		t.Fatalf("config: %d %s", w.Code, w.Body.String())
	}
	if updated.Revision <= enrolled.Revision || updated.Services[0].Tunnel.RemoteAddress != "tunnel.example.com:2333" {
		t.Fatalf("retargeted config = %#v", updated)
	}
	if w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "api", "protocol": "http", "target": map[string]any{"host": "api", "port": 8080}}); w.Code != 201 {
		t.Fatalf("second service: %d %s", w.Code, w.Body.String())
	}
	assertTunnelAddress(t, store, connector.ID, "tunnel.example.com:2333")

	w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": ""})
	if outlet := decodeOutlet(t, w.Body.Bytes()); w.Code != 200 || outlet.Tunnel != (outletTunnel{Address: defaultAddress, DefaultAddress: defaultAddress}) {
		t.Fatalf("reset tunnel: %d %s", w.Code, w.Body.String())
	}
	assertTunnelAddress(t, store, connector.ID, defaultAddress)

	// A default address follows the primary domain and keeps the label.
	revision := store.managed[connector.ID].Configuration.Revision
	if w = request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.org"}); w.Code != 200 {
		t.Fatalf("domain change: %d %s", w.Code, w.Body.String())
	}
	assertTunnelAddress(t, store, connector.ID, label+".example.org:2333")
	if store.managed[connector.ID].Configuration.Revision <= revision {
		t.Fatal("domain change did not publish a new revision")
	}

	for _, body := range []map[string]any{{"address": "bad host"}, {"address": "example.com:0"}, {"address": "127.0.0.1:2333"}, {"address": "localhost"}, {}} {
		if w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, body); w.Code != 400 {
			t.Fatalf("invalid tunnel %v: %d %s", body, w.Code, w.Body.String())
		}
	}
	if w = request(h, "PUT", "/api/v1/outlet/tunnel", "", map[string]any{"address": "meerkat.example.com"}); w.Code != 401 {
		t.Fatalf("unauthenticated tunnel change: %d", w.Code)
	}
}

func TestTunnelAddressPrefersSettingOverOutletConfig(t *testing.T) {
	f := setup(t)
	store, _ := controlStore(t, f)
	h := store.Handler()
	w := request(h, "GET", "/api/v1/outlet", f.admin, nil)
	if outlet := decodeOutlet(t, w.Body.Bytes()); w.Code != 200 || outlet.Tunnel != (outletTunnel{Address: "outlet:2333", DefaultAddress: "outlet:2333"}) {
		t.Fatalf("configured tunnel: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": "tunnel.example.com:2444"})
	if outlet := decodeOutlet(t, w.Body.Bytes()); w.Code != 200 || outlet.Tunnel != (outletTunnel{Address: "tunnel.example.com:2444", DefaultAddress: "outlet:2333", Custom: true}) {
		t.Fatalf("custom tunnel: %d %s", w.Code, w.Body.String())
	}
	// Without a listen address there is no port to add to a bare host.
	if w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": "tunnel.example.com"}); w.Code != 400 {
		t.Fatalf("bare host without listen port: %d %s", w.Code, w.Body.String())
	}
}

func TestTunnelAddressCannotBeClearedWithoutDefault(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelRemoteAddress = ""
	store, _ := controlStore(t, f)
	h := store.Handler()
	if w := request(h, "POST", "/api/v1/outlet/tunnel/check", f.admin, map[string]any{}); w.Code != 409 || !containsJSON(w.Body.Bytes(), "tunnel_address_unset") {
		t.Fatalf("check without address: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": "tunnel.example.com:2333"}); w.Code != 200 {
		t.Fatalf("custom tunnel: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": ""}); w.Code != 409 || !containsJSON(w.Body.Bytes(), "tunnel_default_unavailable") {
		t.Fatalf("clear without default: %d %s", w.Code, w.Body.String())
	}
	w := request(h, "GET", "/api/v1/outlet", f.admin, nil)
	if outlet := decodeOutlet(t, w.Body.Bytes()); outlet.Tunnel != (outletTunnel{Address: "tunnel.example.com:2333", Custom: true}) {
		t.Fatalf("tunnel after refused clear: %s", w.Body.String())
	}
}

func TestServicesShareTunnelAddressAfterOutletConfigChange(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelListenAddress = "0.0.0.0:2333"
	f.cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, db := controlStore(t, f)
	h := store.Handler()
	if w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"}); w.Code != 200 {
		t.Fatalf("outlet: %d %s", w.Code, w.Body.String())
	}
	connector, _ := enrolledConnector(t, h, f.admin)
	assertTunnelAddress(t, store, connector.ID, "outlet:2333")

	// tunnel_remote_address changed while the Outlet runs: a new service must
	// not leave the existing ones on the previous endpoint.
	store.config.TunnelRemoteAddress = "tunnel.example.com:2333"
	if w := request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "api", "protocol": "http", "target": map[string]any{"host": "api", "port": 8080}}); w.Code != 201 {
		t.Fatalf("second service: %d %s", w.Code, w.Body.String())
	}
	assertTunnelAddress(t, store, connector.ID, "tunnel.example.com:2333")

	// After a restart with a new outlet.json the Connectors follow it.
	revision := store.managed[connector.ID].Configuration.Revision
	f.cfg.TunnelRemoteAddress = "edge.example.com:2333"
	restarted, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	assertTunnelAddress(t, restarted, connector.ID, "edge.example.com:2333")
	if restarted.managed[connector.ID].Configuration.Revision <= revision {
		t.Fatal("startup retarget did not publish a new revision")
	}
	reopened, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	assertTunnelAddress(t, reopened, connector.ID, "edge.example.com:2333")
}

func TestRetargetedKeepsConfigurationShape(t *testing.T) {
	legacy := contract.Configuration{ConnectorID: "c", ServiceID: "s", Revision: 1, Tunnel: contract.Tunnel{RemoteAddress: "old.example.com:2333", ServiceName: "s"}}
	got, changed := retargeted(legacy, "new.example.com:2333")
	if !changed || got.Tunnel.RemoteAddress != "new.example.com:2333" || len(got.Services) != 0 || got.ServiceID != "s" {
		t.Fatalf("legacy retarget = %#v, %v", got, changed)
	}
	multi := contract.Configuration{Services: []contract.ServiceConfiguration{{ServiceID: "a", Tunnel: contract.Tunnel{RemoteAddress: "old.example.com:2333"}}, {ServiceID: "b", Tunnel: contract.Tunnel{RemoteAddress: "new.example.com:2333"}}}}
	got, changed = retargeted(multi, "new.example.com:2333")
	if !changed || got.Services[0].Tunnel.RemoteAddress != "new.example.com:2333" || multi.Services[0].Tunnel.RemoteAddress != "old.example.com:2333" {
		t.Fatalf("multi retarget = %#v, %v (input changed: %#v)", got, changed, multi)
	}
	if _, changed := retargeted(got, "new.example.com:2333"); changed {
		t.Fatal("unchanged configuration reported a change")
	}
}

func TestTunnelCheckResolvesAndDialsEachAddress(t *testing.T) {
	f := setup(t)
	store, _ := controlStore(t, f)
	store.lookupIP = func(_ context.Context, host string) ([]net.IPAddr, error) {
		switch host {
		case "tunnel.example.test":
			return []net.IPAddr{{IP: net.ParseIP("203.0.113.10")}, {IP: net.ParseIP("2001:db8::10")}}, nil
		case "outlet":
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.5")}}, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	store.dialTCP = func(_ context.Context, address string) (net.Conn, error) {
		switch address {
		case "203.0.113.10:2333", "10.0.0.5:2333":
			client, server := net.Pipe()
			server.Close()
			return client, nil
		case "[2001:db8::10]:2333":
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ENETUNREACH)}
		case "192.0.2.1:2333":
			return nil, context.DeadlineExceeded
		}
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	}
	h := store.Handler()
	check := func(body map[string]any) tunnelCheck {
		t.Helper()
		w := request(h, "POST", "/api/v1/outlet/tunnel/check", f.admin, body)
		var result tunnelCheck
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
			t.Fatalf("check %v: %d %s", body, w.Code, w.Body.String())
		}
		return result
	}
	if result := check(map[string]any{}); !result.Reachable || result.Address != "outlet:2333" || len(result.Results) != 1 || !result.Results[0].Reachable {
		t.Fatalf("current tunnel check = %#v", result)
	}
	result := check(map[string]any{"address": "tunnel.example.test:2333"})
	if !result.Reachable || len(result.Results) != 2 || !result.Results[0].Reachable || result.Results[1].Error != "unreachable" {
		t.Fatalf("partial check = %#v", result)
	}
	for address, want := range map[string]string{"198.51.100.7:2333": "connection_refused", "192.0.2.1:2333": "timeout"} {
		if result := check(map[string]any{"address": address}); result.Reachable || len(result.Results) != 1 || result.Results[0].Error != want {
			t.Fatalf("check %s = %#v", address, result)
		}
	}
	if result := check(map[string]any{"address": "missing.example.test:2333"}); result.Reachable || result.Error != "dns_lookup_failed" || len(result.Results) != 0 {
		t.Fatalf("missing host check = %#v", result)
	}
	for _, address := range []string{"bad host:1", "127.0.0.1:2333", "localhost:2333"} {
		if w := request(h, "POST", "/api/v1/outlet/tunnel/check", f.admin, map[string]any{"address": address}); w.Code != 400 {
			t.Fatalf("invalid check %s: %d %s", address, w.Code, w.Body.String())
		}
	}
	if w := request(h, "POST", "/api/v1/outlet/tunnel/check", "", map[string]any{}); w.Code != 401 {
		t.Fatalf("unauthenticated check: %d", w.Code)
	}
}

// A service and its public address can be added after the install code is
// issued and before the Connector first connects: the enrollment answer
// already carries the service.
func TestServicesCanBeAddedBeforeTheConnectorConnects(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelListenAddress = "0.0.0.0:2333"
	f.cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	store, _ := controlStore(t, f)
	h := store.Handler()
	if w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"}); w.Code != 200 {
		t.Fatalf("domain: %d %s", w.Code, w.Body.String())
	}
	w := request(h, "POST", "/api/v1/connectors", f.admin, map[string]any{"name": "edge-a"})
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil || w.Code != 201 {
		t.Fatalf("connector: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", f.admin, nil)
	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil || w.Code != 201 {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}})
	var service control.Service
	if err := json.Unmarshal(w.Body.Bytes(), &service); err != nil || w.Code != 201 {
		t.Fatalf("service: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/services/"+service.ID+"/routes", f.admin, map[string]any{"hostname": "app.example.com"}); w.Code != 201 {
		t.Fatalf("route: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/connector/v1/enrollment", token.Token, enrollment("edge-a"))
	var enrolled contract.EnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enrolled); err != nil || w.Code != 201 {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	services := enrolled.Configuration.ActiveServices()
	if len(services) != 1 || services[0].ServiceID != service.ID || services[0].Target.Host != "app" || validation.Configuration(enrolled.Configuration) != nil {
		t.Fatalf("configuration: %+v", enrolled.Configuration)
	}
}
