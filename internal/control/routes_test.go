package control

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
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
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
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
	route, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if !route.Enabled || route.Kind != "http" || route.Hostname != "app.example.com" {
		t.Fatalf("route = %#v", route)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false); !errors.Is(err, ErrRouteExists) {
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
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
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
		if _, err := db.CreateHTTPRoute(ctx, service.ID, hostname, false); !errors.Is(err, ErrInvalidRoute) {
			t.Fatalf("hostname %q error = %v", hostname, err)
		}
	}
}

// CreateHTTPRoute first checks the domains against the cache, which a change
// can outrun. Its transaction checks again against the database, so a route
// is never left under a domain removed in between, or on a management host
// that was renamed.
func TestCreateHTTPRouteChecksTheDomainsInItsTransaction(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AddDomain(ctx, "extra.test", ""); err != nil {
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
	outlet, err := db.Outlet(ctx)
	if err != nil || len(outlet.Domains) != 2 {
		t.Fatalf("outlet = %#v, %v", outlet, err)
	}
	// Written behind the cache, as a removal that lands between the check and
	// the insert would be.
	for _, statement := range []string{
		`DELETE FROM outlet_domains WHERE domain = 'extra.test'`,
		`UPDATE outlet_settings SET value = 'renamed' WHERE key = 'admin_label'`,
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, hostname := range []string{"app.extra.test", "renamed.example.com"} {
		if _, err := db.CreateHTTPRoute(ctx, service.ID, hostname, false); !errors.Is(err, ErrInvalidRoute) {
			t.Fatalf("%s: error = %v", hostname, err)
		}
	}
	var routes int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM routes").Scan(&routes); err != nil || routes != 0 {
		t.Fatalf("%d routes, %v", routes, err)
	}
	// What is still allowed is.
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false); err != nil {
		t.Fatal(err)
	}
}

// raceRoute makes a route at hostname while change runs, both released at
// once, and reports whether the route was made.
func raceRoute(db *Database, serviceID, hostname string, change func()) bool {
	start := make(chan struct{})
	made := make(chan bool)
	go func() {
		<-start
		_, err := db.CreateHTTPRoute(context.Background(), serviceID, hostname, false)
		made <- err == nil
	}()
	go func() {
		<-start
		change()
	}()
	close(start)
	return <-made
}

// raceFixture is a database with the primary domain one.test and a service.
func raceFixture(t *testing.T) (*Database, Service) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.SetDomain(ctx, "one.test", ""); err != nil {
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
	return db, service
}

// Removing a domain races with creating a route under it. Whichever way they
// interleave, no route may be left under a domain that is gone.
func TestRemoveDomainAndRouteCreationNeverLeaveAnOrphan(t *testing.T) {
	ctx := context.Background()
	db, service := raceFixture(t)
	for round := 0; round < 25; round++ {
		if _, err := db.AddDomain(ctx, "extra.test", ""); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		raceRoute(db, service.ID, "app.extra.test", func() { db.RemoveDomain(ctx, "extra.test") })
		outlet, err := db.Outlet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		hostnames, err := db.DomainRouteHostnames(ctx, "extra.test")
		if err != nil {
			t.Fatal(err)
		}
		if len(hostnames) != 0 && !slices.Contains(outlet.Domains, "extra.test") {
			t.Fatalf("round %d: %v left under the removed domain", round, hostnames)
		}
		// Start the next round clean.
		routes, _ := db.ListRoutes(ctx, service.ID)
		for _, route := range routes {
			if err := db.DeleteHTTPRoute(ctx, service.ID, route.ID); err != nil {
				t.Fatal(err)
			}
		}
		if slices.Contains(outlet.Domains, "extra.test") {
			if _, err := db.RemoveDomain(ctx, "extra.test"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Changing the primary domain races with creating a route under the old one
// in the same way.
func TestSetDomainAndRouteCreationNeverLeaveAnOrphan(t *testing.T) {
	ctx := context.Background()
	db, service := raceFixture(t)
	for round := 0; round < 25; round++ {
		// Swap between the two names whichever one the last round left.
		before, err := db.Outlet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		next := "two.test"
		if before.Domain == next {
			next = "one.test"
		}
		raceRoute(db, service.ID, "app."+before.Domain, func() { db.SetDomain(ctx, next, "") })
		outlet, err := db.Outlet(ctx)
		if err != nil {
			t.Fatal(err)
		}
		routes, _ := db.ListRoutes(ctx, service.ID)
		for _, route := range routes {
			if !strings.HasSuffix(route.Hostname, "."+outlet.Domain) {
				t.Fatalf("round %d: %s left under the replaced domain, now %s", round, route.Hostname, outlet.Domain)
			}
			if err := db.DeleteHTTPRoute(ctx, service.ID, route.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
}
