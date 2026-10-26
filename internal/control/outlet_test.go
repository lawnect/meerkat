package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
)

func TestOutletDomainAndRouteBoundary(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.SetDomain(ctx, "Example.com", ""); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("invalid domain error = %v", err)
	}
	outlet, err := db.SetDomain(ctx, "example.com", "")
	if err != nil || outlet.Domain != "example.com" {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "outside.test", false); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("outside route error = %v", err)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetDomain(ctx, "another.example", ""); !errors.Is(err, ErrDomainHasRoutes) {
		t.Fatalf("domain change error = %v", err)
	}
}

func TestTunnelAddressSetting(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if address, err := db.TunnelAddress(ctx); err != nil || address != "" {
		t.Fatalf("unset address = %q, %v", address, err)
	}
	if err := db.SetTunnelAddress(ctx, "meerkat.example.com"); !errors.Is(err, ErrInvalidTunnelAddress) {
		t.Fatalf("address without port error = %v", err)
	}
	for _, want := range []string{"meerkat.example.com:2333", "tunnel.example.com:2444"} {
		if err := db.SetTunnelAddress(ctx, want); err != nil {
			t.Fatal(err)
		}
		if address, err := db.TunnelAddress(ctx); err != nil || address != want {
			t.Fatalf("address = %q, %v; want %q", address, err, want)
		}
	}
	if err := db.SetTunnelAddress(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if address, err := db.TunnelAddress(ctx); err != nil || address != "" {
		t.Fatalf("reset address = %q, %v", address, err)
	}
}

func TestAdminLabelAndDomainModes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	outlet, err := db.SetDomain(ctx, "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	label := strings.TrimSuffix(outlet.AdminHostname, ".example.com")
	if len(label) != 8 || label == contract.LegacyAdminLabel || label[0] < 'a' || label[0] > 'z' {
		t.Fatalf("admin hostname = %q", outlet.AdminHostname)
	}
	// A first domain uses per-host records until a wildcard is chosen.
	if outlet.Modes["example.com"] != DomainHosts {
		t.Fatalf("default mode = %#v", outlet.Modes)
	}
	// The label stays the same when the primary domain changes.
	outlet, err = db.SetDomain(ctx, "example.org", DomainHosts)
	if err != nil || outlet.AdminHostname != label+".example.org" || outlet.Modes["example.org"] != DomainHosts {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	if _, ok := outlet.Modes["example.com"]; ok {
		t.Fatalf("old primary mode kept: %#v", outlet.Modes)
	}
	if _, err := db.AddDomain(ctx, "example.net", "everything"); !errors.Is(err, ErrInvalidDomainMode) {
		t.Fatalf("invalid mode error = %v", err)
	}
	if outlet, err = db.AddDomain(ctx, "example.net", DomainHosts); err != nil || outlet.Modes["example.net"] != DomainHosts {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	if outlet, err = db.SetDomainMode(ctx, "example.net", DomainWildcard); err != nil || outlet.Modes["example.net"] != DomainWildcard {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	if _, err := db.SetDomainMode(ctx, "unknown.example", DomainHosts); !errors.Is(err, ErrDomainNotFound) {
		t.Fatalf("unknown domain error = %v", err)
	}
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, outlet.AdminHostname, false); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("admin hostname route error = %v", err)
	}
	for _, hostname := range []string{"b.example.net", "a.example.net", "app.example.org"} {
		if _, err := db.CreateHTTPRoute(ctx, service.ID, hostname, false); err != nil {
			t.Fatal(err)
		}
	}
	hostnames, err := db.DomainRouteHostnames(ctx, "example.net")
	if err != nil || strings.Join(hostnames, ",") != "a.example.net,b.example.net" {
		t.Fatalf("hostnames = %v, %v", hostnames, err)
	}
	if outlet, err = db.RemoveDomain(ctx, "example.net"); !errors.Is(err, ErrDomainHasRoutes) {
		t.Fatalf("remove with routes = %v", err)
	}
}

func TestLegacyOutletKeepsMeerkatLabel(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an Outlet set up before generated labels: a domain without a label.
	if _, err := db.db.ExecContext(ctx, "INSERT INTO outlet_settings(key, value, updated_at) VALUES ('domain', 'example.com', 'now')"); err != nil {
		t.Fatal(err)
	}
	outlet, err := db.Outlet(ctx)
	if err != nil || outlet.AdminHostname != "meerkat.example.com" || outlet.Modes["example.com"] != DomainWildcard {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	db.Close()
}
