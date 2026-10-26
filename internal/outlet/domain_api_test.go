package outlet

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

func TestDomainModesAndDNSHostnames(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelRemoteAddress = ""
	f.cfg.TunnelListenAddress = "0.0.0.0:2333"
	store, db := controlStore(t, f)
	h := store.Handler()
	ctx := context.Background()

	w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com", "mode": "hosts"})
	outlet := decodeOutlet(t, w.Body.Bytes())
	if w.Code != 200 || outlet.Modes["example.com"] != control.DomainHosts {
		t.Fatalf("primary: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/outlet/domains", f.admin, map[string]any{"domain": "example.net"}); w.Code != 201 || decodeOutlet(t, w.Body.Bytes()).Modes["example.net"] != control.DomainWildcard {
		t.Fatalf("additional: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PUT", "/api/v1/outlet/domains/example.net/mode", f.admin, map[string]any{"mode": "all"}); w.Code != 400 {
		t.Fatalf("invalid mode: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PUT", "/api/v1/outlet/domains/unknown.example/mode", f.admin, map[string]any{"mode": "hosts"}); w.Code != 404 {
		t.Fatalf("unknown domain: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PUT", "/api/v1/outlet/domains/example.net/mode", "", map[string]any{"mode": "hosts"}); w.Code != 401 {
		t.Fatalf("unauthenticated: %d %s", w.Code, w.Body.String())
	}

	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	for _, hostname := range []string{"app.example.com", "api.example.com", "www.example.net"} {
		if _, err := db.CreateHTTPRoute(ctx, service.ID, hostname); err != nil {
			t.Fatal(err)
		}
	}
	current, err := db.Outlet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Hosts mode: the management host and each public address; never the root.
	hostnames, err := store.dnsHostnames(ctx, current, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := current.AdminHostname + ",api.example.com,app.example.com"; strings.Join(hostnames, ",") != want {
		t.Fatalf("hosts mode hostnames = %v, want %s", hostnames, want)
	}
	// Wildcard mode: one fresh probe name instead of each address.
	hostnames, err = store.dnsHostnames(ctx, current, "example.net")
	if err != nil {
		t.Fatal(err)
	}
	if len(hostnames) != 1 || !strings.HasPrefix(hostnames[0], "check-") || !strings.HasSuffix(hostnames[0], ".example.net") {
		t.Fatalf("wildcard hostnames = %v", hostnames)
	}
	// A tunnel host under the domain is checked too, including the root when it is the tunnel host.
	for address, want := range map[string]string{"tunnel.example.net:2333": "tunnel.example.net", "example.net:2333": "example.net"} {
		if w = request(h, "PUT", "/api/v1/outlet/tunnel", f.admin, map[string]any{"address": address}); w.Code != 200 {
			t.Fatalf("tunnel: %d %s", w.Code, w.Body.String())
		}
		hostnames, err = store.dnsHostnames(ctx, current, "example.net")
		if err != nil || len(hostnames) != 2 || hostnames[1] != want {
			t.Fatalf("tunnel %s hostnames = %v, %v", address, hostnames, err)
		}
	}
}

func TestConnectorDescriptionAPI(t *testing.T) {
	f := setup(t)
	store, _ := controlStore(t, f)
	h := store.Handler()
	w := request(h, "POST", "/api/v1/connectors", f.admin, map[string]any{"name": "edge-a", "description": "Home NAS"})
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil || w.Code != 201 || connector.Description != "Home NAS" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/"+connector.ID, f.admin, map[string]any{"display_name": "Home NAS"}); w.Code != 200 || !strings.Contains(w.Body.String(), `"display_name":"Home NAS"`) || strings.Contains(w.Body.String(), "description") {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/"+connector.ID, f.admin, map[string]any{}); w.Code != 400 {
		t.Fatalf("empty update: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/"+connector.ID, f.admin, map[string]any{"description": "Backups"}); w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/"+connector.ID, f.admin, map[string]any{"description": "bad\u0007"}); w.Code != 400 {
		t.Fatalf("invalid: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/m_missing", f.admin, map[string]any{"description": "x"}); w.Code != 404 {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}
	if w = request(h, "PATCH", "/api/v1/connectors/"+connector.ID, "", map[string]any{"description": "x"}); w.Code != 401 {
		t.Fatalf("unauthenticated: %d %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/connectors", f.admin, nil)
	if !strings.Contains(w.Body.String(), `"description":"Backups"`) {
		t.Fatalf("list: %s", w.Body.String())
	}
}
