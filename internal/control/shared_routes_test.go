package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

func sharedServices(t *testing.T, db *Database, n int) []Service {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SetDomain(ctx, "example.com", ""); err != nil {
		t.Fatal(err)
	}
	var services []Service
	for i := range n {
		connector, err := db.CreateConnector(ctx, "edge-"+string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, service)
	}
	return services
}

// An address other services use is joined only on purpose, once per
// service, and the newcomer goes last.
func TestSharedAddressesAreJoinedExplicitly(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	services := sharedServices(t, db, 3)
	first, err := db.CreateHTTPRoute(ctx, services[0].ID, "app.example.com", false)
	if err != nil || first.Priority != 0 || first.AddressBackends != 1 || first.AddressMode != AddressFailover {
		t.Fatalf("first = %+v %v", first, err)
	}
	if _, err := db.CreateHTTPRoute(ctx, services[1].ID, "app.example.com", false); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("joining without asking: %v", err)
	}
	second, err := db.CreateHTTPRoute(ctx, services[1].ID, "app.example.com", true)
	if err != nil || second.Priority != 1 || second.AddressBackends != 2 {
		t.Fatalf("second = %+v %v", second, err)
	}
	if _, err := db.CreateHTTPRoute(ctx, services[1].ID, "app.example.com", true); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("a service twice: %v", err)
	}
	routing := fresh(t, db, "shared")
	plans, policy := routing.Routes("app.example.com")
	if len(plans) != 2 || plans[0].ServiceID != services[0].ID || plans[1].ServiceID != services[1].ID || policy.Mode != AddressFailover || policy.Sticky {
		t.Fatalf("routes = %+v %+v", plans, policy)
	}
	listed, err := db.ListRoutes(ctx, services[0].ID)
	if err != nil || len(listed) != 1 || listed[0].AddressBackends != 2 {
		t.Fatalf("listed = %+v %v", listed, err)
	}
}

// The policy sets the mode and the failover order, and leaves with the
// address's last service.
func TestAddressPolicy(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	services := sharedServices(t, db, 2)
	a, _ := db.CreateHTTPRoute(ctx, services[0].ID, "app.example.com", false)
	b, err := db.CreateHTTPRoute(ctx, services[1].ID, "app.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		policy AddressPolicy
		order  []string
		want   error
	}{
		{AddressPolicy{Mode: "random"}, []string{a.ID, b.ID}, ErrInvalidAddress},
		{AddressPolicy{Mode: AddressBalance}, []string{a.ID}, ErrInvalidAddress},
		{AddressPolicy{Mode: AddressBalance}, []string{a.ID, a.ID}, ErrInvalidAddress},
		{AddressPolicy{Mode: AddressBalance}, []string{a.ID, "m_other"}, ErrInvalidAddress},
	} {
		if err := db.SetAddressPolicy(ctx, "app.example.com", c.policy, c.order); !errors.Is(err, c.want) {
			t.Fatalf("%+v %v: %v", c.policy, c.order, err)
		}
	}
	if err := db.SetAddressPolicy(ctx, "none.example.com", AddressPolicy{Mode: AddressFailover}, nil); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("unknown address: %v", err)
	}
	if err := db.SetAddressPolicy(ctx, "app.example.com", AddressPolicy{Mode: AddressBalance, Sticky: true}, []string{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	plans, policy := fresh(t, db, "balanced").Routes("app.example.com")
	if len(plans) != 2 || plans[0].ID != b.ID || policy != (AddressPolicy{Mode: AddressBalance, Sticky: true}) {
		t.Fatalf("routes = %+v %+v", plans, policy)
	}
	// Failover ignores stickiness.
	if err := db.SetAddressPolicy(ctx, "app.example.com", AddressPolicy{Mode: AddressFailover, Sticky: true}, []string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if _, policy := fresh(t, db, "failover").Routes("app.example.com"); policy != (AddressPolicy{Mode: AddressFailover}) {
		t.Fatalf("policy = %+v", policy)
	}
	if err := db.DeleteHTTPRoute(ctx, services[0].ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, policy := fresh(t, db, "one left").Routes("app.example.com"); policy.Mode != AddressFailover {
		t.Fatalf("policy with one left = %+v", policy)
	}
	var count int
	if err := db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM address_policies").Scan(&count); err != nil || count != 1 {
		t.Fatalf("policies with one left: %d %v", count, err)
	}
	if err := db.DeleteHTTPRoute(ctx, services[1].ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM address_policies").Scan(&count); err != nil || count != 0 {
		t.Fatalf("policies after the last route: %d %v", count, err)
	}
}

// Routes saved before addresses could be shared keep their data.
func TestSharedAddressMigrationKeepsRoutes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	all := migrations
	migrations = all[:len(all)-1]
	db, err := Open(ctx, path)
	if err != nil {
		migrations = all
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.db.ExecContext(ctx, `INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'edge', 'active', ?);
		INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, enabled, created_at, updated_at) VALUES ('s1', 'c1', 'web', 'http', 'app', 3000, 1, ?, ?);
		INSERT INTO routes(id, service_id, kind, hostname, enabled, created_at, updated_at) VALUES ('r1', 's1', 'http', 'app.example.com', 1, ?, ?)`, now, now, now, now, now)
	db.Close()
	migrations = all
	if err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	routes, err := db.ListRoutes(ctx, "s1")
	if err != nil || len(routes) != 1 || routes[0].ID != "r1" || routes[0].Hostname != "app.example.com" || routes[0].Priority != 0 || routes[0].AddressBackends != 1 {
		t.Fatalf("routes = %+v %v", routes, err)
	}
}
