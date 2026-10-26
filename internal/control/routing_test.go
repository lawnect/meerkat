package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

func openRouting(t *testing.T) *Database {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// fresh asserts that every cached view matches the database right after a
// step, and returns the routing snapshot.
func fresh(t *testing.T, db *Database, step string) *Routing {
	t.Helper()
	ctx := context.Background()
	cached, err := db.Routing(ctx)
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
	loaded, err := db.loadRouting(ctx)
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
	if !reflect.DeepEqual(cached, loaded) {
		t.Fatalf("%s: stale routing\ncached %+v\nloaded %+v", step, cached, loaded)
	}
	outlet, err := db.Outlet(ctx)
	if err != nil || !reflect.DeepEqual(outlet, loaded.Outlet) {
		t.Fatalf("%s: Outlet() = %+v, %v; database has %+v", step, outlet, err, loaded.Outlet)
	}
	if tunnel, err := db.TunnelAddress(ctx); err != nil || tunnel != loaded.tunnelAddress {
		t.Fatalf("%s: TunnelAddress() = %q, %v; database has %q", step, tunnel, err, loaded.tunnelAddress)
	}
	connectors, err := db.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("%s: %v", step, err)
	}
	stored, err := db.loadConnectors(ctx)
	if err != nil || !reflect.DeepEqual(connectors, stored) {
		t.Fatalf("%s: ListConnectors() = %+v; database has %+v", step, connectors, stored)
	}
	return cached
}

func TestRoutingFollowsEveryChange(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if r := fresh(t, db, "empty"); r.Outlet.Domain != "" || len(r.routes) != 0 {
		t.Fatalf("empty = %+v", r)
	}
	must := func(step string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
	}
	_, err := db.SetDomain(ctx, "example.com", "")
	must("set domain", err)
	r := fresh(t, db, "set domain")
	if r.Outlet.Domain != "example.com" || r.Outlet.AdminHostname == "" {
		t.Fatalf("outlet = %+v", r.Outlet)
	}
	_, err = db.AddDomain(ctx, "example.org", DomainHosts)
	must("add domain", err)
	if r := fresh(t, db, "add domain"); r.Outlet.Modes["example.org"] != DomainHosts {
		t.Fatalf("modes = %v", r.Outlet.Modes)
	}
	_, err = db.SetDomainMode(ctx, "example.org", DomainWildcard)
	must("domain mode", err)
	if r := fresh(t, db, "domain mode"); r.Outlet.Modes["example.org"] != DomainWildcard {
		t.Fatalf("modes = %v", r.Outlet.Modes)
	}
	connector, err := db.CreateConnector(ctx, "edge")
	must("connector", err)
	fresh(t, db, "connector")
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	must("service", err)
	fresh(t, db, "service")
	route, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false)
	must("route", err)
	if plan, ok := fresh(t, db, "route").Route("app.example.com"); !ok || plan.ServiceID != service.ID || plan.ConnectorID != connector.ID {
		t.Fatalf("plan = %+v, %v", plan, ok)
	}
	other, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.org", false)
	must("second route", err)
	if _, ok := fresh(t, db, "second route").Route("app.example.org"); !ok {
		t.Fatal("second route missing")
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.org", false); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("duplicate = %v", err)
	}
	fresh(t, db, "failed route")
	_, err = db.UpdateServiceTarget(ctx, connector.ID, service.ID, contract.Target{Host: "app", Port: 3001})
	must("target", err)
	fresh(t, db, "target")
	_, err = db.SetConnectorDisplayName(ctx, connector.ID, "Edge")
	must("display name", err)
	fresh(t, db, "display name")
	_, err = db.SetConnectorDescription(ctx, connector.ID, "note")
	must("description", err)
	fresh(t, db, "description")
	must("tunnel address", db.SetTunnelAddress(ctx, "tunnel.example.com:2333"))
	if r := fresh(t, db, "tunnel address"); r.tunnelAddress != "tunnel.example.com:2333" {
		t.Fatalf("tunnel = %q", r.tunnelAddress)
	}
	must("default tunnel address", db.SetTunnelAddress(ctx, ""))
	fresh(t, db, "default tunnel address")
	if err := db.SetTunnelAddress(ctx, "not a host"); !errors.Is(err, ErrInvalidTunnelAddress) {
		t.Fatalf("invalid tunnel = %v", err)
	}
	fresh(t, db, "refused tunnel address")
	if err := db.DeleteService(ctx, connector.ID, service.ID); !errors.Is(err, ErrServiceHasRoutes) {
		t.Fatalf("delete service with routes = %v", err)
	}
	fresh(t, db, "refused service delete")
	must("delete route", db.DeleteHTTPRoute(ctx, service.ID, other.ID))
	if _, ok := fresh(t, db, "delete route").Route("app.example.org"); ok {
		t.Fatal("deleted route still served")
	}
	_, err = db.RemoveDomain(ctx, "example.org")
	must("remove domain", err)
	if r := fresh(t, db, "remove domain"); len(r.Outlet.Domains) != 1 {
		t.Fatalf("domains = %v", r.Outlet.Domains)
	}
	must("delete last route", db.DeleteHTTPRoute(ctx, service.ID, route.ID))
	must("delete service", db.DeleteService(ctx, connector.ID, service.ID))
	if r := fresh(t, db, "delete service"); len(r.routes) != 0 {
		t.Fatalf("routes = %v", r.routes)
	}
	service, err = db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	must("service again", err)
	_, err = db.CreateHTTPRoute(ctx, service.ID, "app.example.com", false)
	must("route again", err)
	fresh(t, db, "route again")
	must("delete connector", db.DeleteConnector(ctx, connector.ID))
	if _, ok := fresh(t, db, "delete connector").Route("app.example.com"); ok {
		t.Fatal("route of a deleted connector still served")
	}
	before := fresh(t, db, "before rename").Outlet.AdminHostname
	_, err = db.SetDomain(ctx, "example.net", "")
	must("new primary domain", err)
	if r := fresh(t, db, "new primary domain"); r.Outlet.Domain != "example.net" || r.Outlet.AdminHostname == before {
		t.Fatalf("outlet = %+v", r.Outlet)
	}
}

func TestRoutingIsReusedUntilAChange(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}
	a := fresh(t, db, "first")
	if b := fresh(t, db, "second"); a != b {
		t.Fatal("unchanged routing was rebuilt")
	}
	// Writes to unrelated settings keep the snapshot.
	if _, err := db.SetRateLimit(ctx, DefaultRateLimit(""), time.Now()); err != nil {
		t.Fatal(err)
	}
	if b := fresh(t, db, "rate limit"); a != b {
		t.Fatal("rate limit settings rebuilt the routing")
	}
	if _, err := db.SetDomainMode(ctx, "example.com", DomainHosts); err != nil {
		t.Fatal(err)
	}
	if b := fresh(t, db, "mode"); a == b {
		t.Fatal("change kept the old snapshot")
	}
}

// A request that starts after a change returns must see it, even while other
// requests rebuild the snapshot concurrently.
func TestRoutingIsNeverStaleAfterAChangeReturns(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}
	connector, err := db.CreateConnector(ctx, "edge")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for !stop.Load() {
				if _, err := db.Routing(ctx); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	defer func() {
		stop.Store(true)
		readers.Wait()
	}()
	for i := range 50 {
		host := fmt.Sprintf("h%d.example.com", i)
		route, err := db.CreateHTTPRoute(ctx, service.ID, host, false)
		if err != nil {
			t.Fatal(err)
		}
		r, err := db.Routing(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := r.Route(host); !ok {
			t.Fatalf("%s missing right after it was created", host)
		}
		if err := db.DeleteHTTPRoute(ctx, service.ID, route.ID); err != nil {
			t.Fatal(err)
		}
		if r, err = db.Routing(ctx); err != nil {
			t.Fatal(err)
		}
		if _, ok := r.Route(host); ok {
			t.Fatalf("%s still served right after it was deleted", host)
		}
	}
}

// Callers of Outlet and ListConnectors get copies: changing one must not
// change what the next caller sees.
func TestCachedViewsAreCopies(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateConnector(ctx, "edge"); err != nil {
		t.Fatal(err)
	}
	outlet, err := db.Outlet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outlet.Domains[0], outlet.Modes["example.com"] = "evil.example", DomainWildcard
	outlet.Domains = append(outlet.Domains, "more.example")
	connectors, err := db.ListConnectors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	connectors[0].Name = "renamed"
	again := fresh(t, db, "after caller edits")
	if again.Outlet.Domains[0] != "example.com" || len(again.Outlet.Domains) != 1 || again.Outlet.Modes["example.com"] != DomainHosts {
		t.Fatalf("cache changed through a copy: %+v", again.Outlet)
	}
	if listed, _ := db.ListConnectors(ctx); listed[0].Name != "edge" {
		t.Fatalf("connector cache changed through a copy: %+v", listed)
	}
}
