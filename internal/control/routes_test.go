package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
)

func TestCreateAndListHTTPRoutes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.SetDomain(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	route, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if route.Enabled || route.Kind != "http" || route.Hostname != "app.example.com" {
		t.Fatalf("route = %#v", route)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com"); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	routes, err := db.ListRoutes(ctx, service.ID)
	if err != nil || len(routes) != 1 || routes[0].ID != route.ID {
		t.Fatalf("routes = %#v, %v", routes, err)
	}
}

func TestCreateHTTPRouteRejectsUnsafeHostname(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.SetDomain(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	for _, hostname := range []string{"*.example.com", "example.com.", "Example.com", "127.0.0.1", "example"} {
		if _, err := db.CreateHTTPRoute(ctx, service.ID, hostname); !errors.Is(err, ErrInvalidRoute) {
			t.Fatalf("hostname %q error = %v", hostname, err)
		}
	}
}
