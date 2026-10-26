package control

import (
	"context"
	"database/sql"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
)

// Routing is what the public listener needs for every request: the Outlet
// domains and the enabled HTTP routes. It is read-only once built.
type Routing struct {
	Outlet        Outlet
	tunnelAddress string
	routes        map[string]HTTPRoutePlan
}

// Route returns the enabled HTTP route for an exact hostname.
func (r *Routing) Route(hostname string) (HTTPRoutePlan, bool) {
	plan, ok := r.routes[hostname]
	return plan, ok
}

// Hostnames lists the hostnames of the enabled HTTP routes, sorted.
func (r *Routing) Hostnames() []string { return slices.Sorted(maps.Keys(r.routes)) }

// snapshot is a value read from the database under one change generation.
type snapshot[T any] struct {
	generation uint64
	value      T
}

// cached keeps the last snapshot of one view. A snapshot built under an older
// generation is never served, so a read that starts after a change returns
// always sees it.
type cached[T any] struct {
	current atomic.Pointer[snapshot[T]]
	rebuild sync.Mutex
}

func (c *cached[T]) get(generation *atomic.Uint64, load func() (T, error)) (T, error) {
	g := generation.Load()
	if current := c.current.Load(); current != nil && current.generation == g {
		return current.value, nil
	}
	// One rebuild at a time: a change under load would otherwise send every
	// waiting request to the database at once.
	c.rebuild.Lock()
	defer c.rebuild.Unlock()
	g = generation.Load()
	if current := c.current.Load(); current != nil && current.generation == g {
		return current.value, nil
	}
	value, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	c.current.Store(&snapshot[T]{generation: g, value: value})
	return value, nil
}

// Routing returns the cached snapshot, rebuilding it after a change.
// Callers must not modify it.
func (d *Database) Routing(ctx context.Context) (*Routing, error) {
	return d.routing.get(&d.generation, func() (*Routing, error) { return d.loadRouting(ctx) })
}

// routingChanged must follow every committed write to outlet_settings
// (domain, admin_label, tunnel_address), outlet_domains, outlet_domain_modes,
// routes, services or connectors: the cached views are built from them.
// changeRouting does it for transactions.
func (d *Database) routingChanged() { d.generation.Add(1) }

// changeRouting runs fn in a transaction and invalidates the cached views
// once it commits. Failed or rolled-back changes leave them alone.
func (d *Database) changeRouting(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.routingChanged()
	return nil
}

// clone copies the slice and map so a caller cannot change the cache.
func (o Outlet) clone() Outlet {
	o.Domains, o.Modes = slices.Clone(o.Domains), maps.Clone(o.Modes)
	return o
}

func (d *Database) loadRouting(ctx context.Context) (*Routing, error) {
	outlet, err := d.loadOutlet(ctx)
	if err != nil {
		return nil, err
	}
	tunnel, err := d.loadTunnelAddress(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT r.hostname, r.service_id, s.connector_id FROM routes r
		JOIN services s ON s.id = r.service_id
		WHERE r.kind = 'http' AND r.enabled = 1 AND s.enabled = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := map[string]HTTPRoutePlan{}
	for rows.Next() {
		var plan HTTPRoutePlan
		if err := rows.Scan(&plan.Hostname, &plan.ServiceID, &plan.ConnectorID); err != nil {
			return nil, err
		}
		plan.Kind, plan.Enabled = "http", true
		routes[plan.Hostname] = plan
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &Routing{Outlet: outlet, tunnelAddress: tunnel, routes: routes}, nil
}
