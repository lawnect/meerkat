package outlet

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

func TestServiceAPIStoresDesiredService(t *testing.T) {
	f := setup(t)
	db, err := control.Open(context.Background(), filepath.Join(f.dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	h := store.Handler()
	w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"})
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "example.com") {
		t.Fatalf("outlet status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors", f.admin, map[string]any{"name": "edge-a"})
	if w.Code != 201 {
		t.Fatalf("connector status = %d: %s", w.Code, w.Body.String())
	}
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/api/v1/connectors", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "edge-a") {
		t.Fatalf("managed connector list status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}})
	if w.Code != 201 {
		t.Fatalf("service status = %d: %s", w.Code, w.Body.String())
	}
	var service control.Service
	if err := json.Unmarshal(w.Body.Bytes(), &service); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/api/v1/connectors/"+connector.ID+"/services", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "web") {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", f.admin, nil)
	if w.Code != 201 {
		t.Fatalf("token status = %d: %s", w.Code, w.Body.String())
	}
	var enrollmentToken struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &enrollmentToken); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", "/connector/v1/enrollment", enrollmentToken.Token, enrollment("edge-a"))
	if w.Code != 201 || !containsJSON(w.Body.Bytes(), "app") {
		t.Fatalf("managed enrollment status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/services/"+service.ID+"/routes", f.admin, map[string]any{"hostname": "app.example.com"})
	if w.Code != 201 {
		t.Fatalf("route status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/services/"+service.ID+"/routes", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "app.example.com") {
		t.Fatalf("route list status = %d: %s", w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/connectors/missing/services", f.admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}}); w.Code != 404 {
		t.Fatalf("missing connector status = %d", w.Code)
	}
	if w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "bad", "protocol": "http", "target": map[string]any{"host": "8.8.8.8", "port": 53}}); w.Code != 400 {
		t.Fatalf("unsafe target status = %d", w.Code)
	}
}

func TestTunnelHostsMultipleServices(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelListenAddress = "0.0.0.0:2333"
	f.cfg.TunnelServerPrivateKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	db, err := control.Open(context.Background(), filepath.Join(f.dir, "multi-control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	h := store.Handler()
	if w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"}); w.Code != 200 {
		t.Fatalf("outlet: %d %s", w.Code, w.Body.String())
	}
	w := request(h, "POST", "/api/v1/connectors", f.admin, map[string]any{"name": "edge-multi"})
	if w.Code != 201 {
		t.Fatalf("connector: %d %s", w.Code, w.Body.String())
	}
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/connectors/" + connector.ID + "/services"
	ids := make([]string, 0, 2)
	for _, item := range []struct {
		name, host string
		port       int
	}{{"web", "app", 3000}, {"api", "api", 8080}} {
		w = request(h, "POST", path, f.admin, map[string]any{"name": item.name, "protocol": "http", "target": map[string]any{"host": item.host, "port": item.port}})
		if w.Code != 201 {
			t.Fatalf("service %s: %d %s", item.name, w.Code, w.Body.String())
		}
		var service control.Service
		if err := json.Unmarshal(w.Body.Bytes(), &service); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, service.ID)
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", f.admin, nil)
	if w.Code != 201 {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	var token struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	w = request(h, "POST", "/connector/v1/enrollment", token.Token, enrollment("edge-multi"))
	if w.Code != 201 {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	var enrolled contract.EnrollmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	if len(enrolled.Services) != 2 {
		t.Fatalf("services = %#v", enrolled.Services)
	}
	record := store.managed[connector.ID]
	if record.probeFor(ids[0]) == record.probeFor(ids[1]) {
		t.Fatal("services share a probe port")
	}
	probes := []*httptest.Server{
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })),
		httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })),
	}
	defer probes[0].Close()
	defer probes[1].Close()
	for i, id := range ids {
		record.ProbeAddresses[id] = strings.TrimPrefix(probes[i].URL, "http://")
	}
	store.managed[connector.ID] = record
	for i, id := range ids {
		w = request(h, "POST", "/api/v1/services/"+id+"/routes", f.admin, map[string]any{"hostname": []string{"web.example.com", "api.example.com"}[i]})
		if w.Code != 201 {
			t.Fatalf("route %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	hb := contract.Heartbeat{ConnectorID: connector.ID, Revision: enrolled.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "unknown", ServiceHealths: map[string]string{ids[0]: "healthy", ids[1]: "healthy"}, ObservedAt: time.Now().UTC()}
	w = request(h, "POST", "/connector/v1/heartbeat", enrolled.Credential, hb)
	if w.Code != 200 {
		t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
	}
	proxyConfig, err := os.ReadFile(store.proxyPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, hostname := range []string{"web.example.com", "api.example.com"} {
		if !strings.Contains(string(proxyConfig), hostname) {
			t.Fatalf("missing route %s", hostname)
		}
	}
	probes[1].Close()
	hb.ObservedAt = time.Now().UTC().Add(time.Millisecond)
	w = request(h, "POST", "/connector/v1/heartbeat", enrolled.Credential, hb)
	if w.Code != 200 {
		t.Fatalf("partial heartbeat: %d %s", w.Code, w.Body.String())
	}
	proxyConfig, err = os.ReadFile(store.proxyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(proxyConfig), "web.example.com") || strings.Contains(string(proxyConfig), "api.example.com") {
		t.Fatal("healthy service route was lost or unhealthy route remained")
	}
	config, err := os.ReadFile(store.tunnelPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if !strings.Contains(string(config), "[server.services."+id+"]") {
			t.Fatalf("missing server service %s", id)
		}
	}
	w = request(h, "PATCH", path+"/"+ids[0], f.admin, map[string]any{"target": map[string]any{"host": "new-app", "port": 3001}})
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	updated := store.managed[connector.ID].Configuration.ActiveServices()
	targets := map[string]int{}
	for _, item := range updated {
		targets[item.ServiceID] = item.Target.Port
	}
	if targets[ids[0]] != 3001 || targets[ids[1]] != 8080 {
		t.Fatalf("targets = %#v", updated)
	}
	for i, id := range ids {
		routes := request(h, "GET", "/api/v1/services/"+id+"/routes", f.admin, nil)
		var listed struct {
			Routes []control.Route `json:"routes"`
		}
		if err := json.Unmarshal(routes.Body.Bytes(), &listed); err != nil {
			t.Fatal(err)
		}
		if len(listed.Routes) != 1 {
			t.Fatalf("route count for %d: %d", i, len(listed.Routes))
		}
		if removed := request(h, "DELETE", "/api/v1/services/"+id+"/routes/"+listed.Routes[0].ID, f.admin, nil); removed.Code != 204 {
			t.Fatalf("delete route: %d", removed.Code)
		}
	}
	w = request(h, "DELETE", path+"/"+ids[0], f.admin, nil)
	if w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if services := store.managed[connector.ID].Configuration.ActiveServices(); len(services) != 1 || services[0].ServiceID != ids[1] {
		t.Fatalf("remaining = %#v", services)
	}
}

func containsJSON(body []byte, value string) bool {
	var decoded any
	return json.Unmarshal(body, &decoded) == nil && string(body) != "" && string(body) != value && jsonContains(decoded, value)
}

func jsonContains(value any, want string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if jsonContains(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if jsonContains(child, want) {
				return true
			}
		}
	case string:
		return v == want
	}
	return false
}
