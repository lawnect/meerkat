package control

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrRouteExists     = errors.New("route already exists")
	ErrRouteNotFound   = errors.New("route not found")
	ErrInvalidRoute    = errors.New("invalid route")
	ErrServiceNotFound = errors.New("service not found")
	ErrAddressNotFound = errors.New("public address not found")
	ErrInvalidAddress  = errors.New("invalid public address policy")
	// ErrAddressChanged means the policy or order changed since the caller
	// read it.
	ErrAddressChanged = errors.New("public address policy changed")
)

// Address modes decide which of a public address's services a request goes
// to when several share it.
const (
	// AddressFailover sends every request to the first connected service
	// in priority order.
	AddressFailover = "failover"
	// AddressBalance spreads requests over the connected services.
	AddressBalance = "balance"
)

// AddressPolicy is how requests for one public address are spread over its
// services. Sticky keeps a client address on one service while it stays
// connected, for apps that hold sessions in memory.
type AddressPolicy struct {
	Mode   string `json:"mode"`
	Sticky bool   `json:"sticky"`
}

// Route is desired public state. The reconciler publishes it only after its
// Connector has a verified tunnel probe.
type Route struct {
	ID              string    `json:"id"`
	ServiceID       string    `json:"service_id"`
	Kind            string    `json:"kind"`
	Hostname        string    `json:"hostname,omitempty"`
	Enabled         bool      `json:"enabled"`
	DesiredRevision int       `json:"desired_revision"`
	AppliedRevision int       `json:"applied_revision"`
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// Priority orders the services of one public address for failover;
	// lower comes first.
	Priority int `json:"priority"`
	// The policy of the route's public address and how many services share
	// it, so a list of one service's routes can show both.
	AddressMode     string `json:"address_mode"`
	AddressSticky   bool   `json:"address_sticky"`
	AddressBackends int    `json:"address_backends"`
}

// routeColumns reads a route with its address policy; the alias is r.
const routeColumns = `r.id, r.service_id, r.kind, r.hostname, r.enabled, r.desired_revision, r.applied_revision, COALESCE(r.last_error, ''), r.created_at, r.updated_at,
	r.priority, COALESCE(p.mode, 'failover'), COALESCE(p.sticky, 0), (SELECT COUNT(*) FROM routes o WHERE o.kind = 'http' AND o.hostname = r.hostname)`

type rowScanner interface{ Scan(...any) error }

func scanRoute(row rowScanner, extra ...any) (Route, error) {
	var route Route
	var enabled, sticky int
	var created, updated string
	dest := append([]any{&route.ID, &route.ServiceID, &route.Kind, &route.Hostname, &enabled, &route.DesiredRevision, &route.AppliedRevision, &route.LastError, &created, &updated,
		&route.Priority, &route.AddressMode, &sticky, &route.AddressBackends}, extra...)
	if err := row.Scan(dest...); err != nil {
		return Route{}, err
	}
	var err error
	if route.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return Route{}, err
	}
	if route.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return Route{}, err
	}
	route.Enabled, route.AddressSticky = enabled != 0, sticky != 0
	return route, nil
}

type HTTPRoutePlan struct {
	Route
	ConnectorID string
	// ServiceName is the service's name; only Routing's plans carry it.
	ServiceName string
}

func (d *Database) HTTPRoutePlans(ctx context.Context) ([]HTTPRoutePlan, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT `+routeColumns+`, s.connector_id
		FROM routes r JOIN services s ON s.id = r.service_id LEFT JOIN address_policies p ON p.hostname = r.hostname
		WHERE r.kind = 'http' ORDER BY r.hostname, r.priority, r.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []HTTPRoutePlan{}
	for rows.Next() {
		var plan HTTPRoutePlan
		if plan.Route, err = scanRoute(rows, &plan.ConnectorID); err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

// CreateHTTPRoute publishes hostname for a service. A hostname another
// service already uses is refused unless shared is set; then this service
// joins that address after its current services.
func (d *Database) CreateHTTPRoute(ctx context.Context, serviceID, hostname string, shared bool) (Route, error) {
	if !validation.Label(serviceID) || !validation.Hostname(hostname) {
		return Route{}, ErrInvalidRoute
	}
	outlet, err := d.Outlet(ctx)
	if err != nil {
		return Route{}, err
	}
	allowed := false
	for _, domain := range outlet.Domains {
		if strings.HasSuffix(hostname, "."+domain) {
			allowed = true
			break
		}
	}
	if !allowed || hostname == outlet.AdminHostname {
		return Route{}, ErrInvalidRoute
	}
	var protocol string
	err = d.db.QueryRowContext(ctx, "SELECT protocol FROM services WHERE id = ?", serviceID).Scan(&protocol)
	if err != nil {
		return Route{}, ErrServiceNotFound
	}
	if protocol != "http" {
		return Route{}, ErrInvalidRoute
	}
	id, err := randomID()
	if err != nil {
		return Route{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Route{}, err
	}
	defer tx.Rollback()
	var others, mine, next int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(service_id = ?), 0), COALESCE(MAX(priority) + 1, 0) FROM routes WHERE kind = 'http' AND hostname = ?`, serviceID, hostname).Scan(&others, &mine, &next); err != nil {
		return Route{}, err
	}
	if mine != 0 || others != 0 && !shared {
		return Route{}, ErrRouteExists
	}
	now := time.Now().UTC()
	route := Route{ID: id, ServiceID: serviceID, Kind: "http", Hostname: hostname, Enabled: true, CreatedAt: now, UpdatedAt: now, Priority: next, AddressMode: AddressFailover, AddressBackends: others + 1}
	if _, err := tx.ExecContext(ctx, `INSERT INTO routes(id, service_id, kind, hostname, enabled, desired_revision, applied_revision, created_at, updated_at, priority)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, route.ID, route.ServiceID, route.Kind, route.Hostname, 1, 0, 0, route.CreatedAt.Format(time.RFC3339Nano), route.UpdatedAt.Format(time.RFC3339Nano), route.Priority); err != nil {
		return Route{}, err
	}
	var mode string
	var sticky int
	switch err := tx.QueryRowContext(ctx, "SELECT mode, sticky FROM address_policies WHERE hostname = ?", hostname).Scan(&mode, &sticky); {
	case err == nil:
		route.AddressMode, route.AddressSticky = mode, sticky != 0
	case !errors.Is(err, sql.ErrNoRows):
		return Route{}, err
	}
	if err := tx.Commit(); err != nil {
		return Route{}, err
	}
	d.routingChanged()
	return route, nil
}

// AddressState is an address's policy and failover order as a caller read
// it, to save over only what it saw.
type AddressState struct {
	Mode   string   `json:"mode"`
	Sticky bool     `json:"sticky"`
	Order  []string `json:"order"`
}

// SetAddressPolicy sets how requests for hostname are spread over its
// services, and their failover order: order lists every route of the
// address, first to last. With current, it refuses when the address no
// longer looks like that: another administrator changed it.
func (d *Database) SetAddressPolicy(ctx context.Context, hostname string, policy AddressPolicy, order []string, current *AddressState) error {
	if !validation.Hostname(hostname) || policy.Mode != AddressFailover && policy.Mode != AddressBalance {
		return ErrInvalidAddress
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM routes WHERE kind = 'http' AND hostname = ? ORDER BY priority, created_at", hostname)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return ErrAddressNotFound
	}
	if current != nil {
		saved := AddressState{Mode: AddressFailover}
		var sticky int
		switch err := tx.QueryRowContext(ctx, "SELECT mode, sticky FROM address_policies WHERE hostname = ?", hostname).Scan(&saved.Mode, &sticky); {
		case err == nil:
			saved.Sticky = sticky != 0
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if saved.Mode != current.Mode || saved.Sticky != current.Sticky || !slices.Equal(ids, current.Order) {
			return ErrAddressChanged
		}
	}
	sorted := slices.Clone(order)
	slices.Sort(sorted)
	slices.Sort(ids)
	if !slices.Equal(sorted, ids) {
		return ErrInvalidAddress
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for priority, id := range order {
		if _, err := tx.ExecContext(ctx, "UPDATE routes SET priority = ?, updated_at = ? WHERE id = ?", priority, now, id); err != nil {
			return err
		}
	}
	// Stickiness only means something while requests are spread.
	sticky := policy.Mode == AddressBalance && policy.Sticky
	if _, err := tx.ExecContext(ctx, `INSERT INTO address_policies(hostname, mode, sticky, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(hostname) DO UPDATE SET mode = excluded.mode, sticky = excluded.sticky, updated_at = excluded.updated_at`, hostname, policy.Mode, sticky, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.routingChanged()
	return nil
}

func (d *Database) ListRoutes(ctx context.Context, serviceID string) ([]Route, error) {
	if !validation.Label(serviceID) {
		return nil, ErrServiceNotFound
	}
	var exists int
	if err := d.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM services WHERE id = ?)", serviceID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrServiceNotFound
	}
	rows, err := d.db.QueryContext(ctx, `SELECT `+routeColumns+`
		FROM routes r LEFT JOIN address_policies p ON p.hostname = r.hostname WHERE r.service_id = ? ORDER BY r.created_at DESC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := []Route{}
	for rows.Next() {
		route, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (d *Database) DeleteHTTPRoute(ctx context.Context, serviceID, routeID string) error {
	if !validation.Label(serviceID) || !validation.Label(routeID) {
		return ErrRouteNotFound
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hostname string
	err = tx.QueryRowContext(ctx, "SELECT hostname FROM routes WHERE id = ? AND service_id = ? AND kind = 'http'", routeID, serviceID).Scan(&hostname)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRouteNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM routes WHERE id = ?", routeID); err != nil {
		return err
	}
	// The last service of an address takes its policy along.
	if _, err := tx.ExecContext(ctx, "DELETE FROM address_policies WHERE hostname = ? AND NOT EXISTS (SELECT 1 FROM routes WHERE kind = 'http' AND hostname = ?)", hostname, hostname); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.routingChanged()
	return nil
}
