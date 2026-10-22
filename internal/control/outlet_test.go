package control

import (
	"context"
	"errors"
	"path/filepath"
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
	if _, err := db.SetDomain(ctx, "Example.com"); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("invalid domain error = %v", err)
	}
	outlet, err := db.SetDomain(ctx, "example.com")
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
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "outside.test"); !errors.Is(err, ErrInvalidRoute) {
		t.Fatalf("outside route error = %v", err)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetDomain(ctx, "another.example"); !errors.Is(err, ErrDomainHasRoutes) {
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
