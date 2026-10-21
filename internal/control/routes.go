package control

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrRouteExists     = errors.New("route already exists")
	ErrRouteNotFound   = errors.New("route not found")
	ErrInvalidRoute    = errors.New("invalid route")
	ErrServiceNotFound = errors.New("service not found")
)

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
}

type HTTPRoutePlan struct {
	Route
	ConnectorID string
}

// PublicRoute returns a registered, enabled HTTP route for an exact hostname.
// A missing route is distinct from a database failure.
func (d *Database) PublicRoute(ctx context.Context, hostname string) (HTTPRoutePlan, bool, error) {
	var plan HTTPRoutePlan
	if !validation.Hostname(hostname) {
		return plan, false, nil
	}
	err := d.db.QueryRowContext(ctx, `SELECT r.service_id, s.connector_id FROM routes r
		JOIN services s ON s.id = r.service_id
		WHERE r.kind = 'http' AND r.enabled = 1 AND s.enabled = 1 AND r.hostname = ?`, hostname).Scan(&plan.ServiceID, &plan.ConnectorID)
	if errors.Is(err, sql.ErrNoRows) {
		return HTTPRoutePlan{}, false, nil
	}
	if err != nil {
		return HTTPRoutePlan{}, false, err
	}
	plan.Hostname, plan.Enabled = hostname, true
	return plan, true, nil
}

func (d *Database) HTTPRoutePlans(ctx context.Context) ([]HTTPRoutePlan, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT r.id, r.service_id, r.kind, r.hostname, r.enabled, r.desired_revision, r.applied_revision, COALESCE(r.last_error, ''), r.created_at, r.updated_at, s.connector_id
		FROM routes r JOIN services s ON s.id = r.service_id WHERE r.kind = 'http' ORDER BY r.hostname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []HTTPRoutePlan{}
	for rows.Next() {
		var plan HTTPRoutePlan
		var enabled int
		var created, updated string
		if err := rows.Scan(&plan.ID, &plan.ServiceID, &plan.Kind, &plan.Hostname, &enabled, &plan.DesiredRevision, &plan.AppliedRevision, &plan.LastError, &created, &updated, &plan.ConnectorID); err != nil {
			return nil, err
		}
		if plan.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if plan.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		plan.Enabled = enabled != 0
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (d *Database) CreateHTTPRoute(ctx context.Context, serviceID, hostname string) (Route, error) {
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
	if !allowed || hostname == contract.AdminHostname(outlet.Domain) {
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
	now := time.Now().UTC()
	route := Route{ID: id, ServiceID: serviceID, Kind: "http", Hostname: hostname, Enabled: true, CreatedAt: now, UpdatedAt: now}
	_, err = d.db.ExecContext(ctx, `INSERT INTO routes(id, service_id, kind, hostname, enabled, desired_revision, applied_revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, route.ID, route.ServiceID, route.Kind, route.Hostname, 1, 0, 0, route.CreatedAt.Format(time.RFC3339Nano), route.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Route{}, ErrRouteExists
	}
	return route, nil
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
	rows, err := d.db.QueryContext(ctx, `SELECT id, service_id, kind, hostname, enabled, desired_revision, applied_revision, COALESCE(last_error, ''), created_at, updated_at
		FROM routes WHERE service_id = ? ORDER BY created_at DESC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	routes := []Route{}
	for rows.Next() {
		var route Route
		var enabled int
		var created, updated string
		if err := rows.Scan(&route.ID, &route.ServiceID, &route.Kind, &route.Hostname, &enabled, &route.DesiredRevision, &route.AppliedRevision, &route.LastError, &created, &updated); err != nil {
			return nil, err
		}
		if route.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if route.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		route.Enabled = enabled != 0
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (d *Database) DeleteHTTPRoute(ctx context.Context, serviceID, routeID string) error {
	if !validation.Label(serviceID) || !validation.Label(routeID) {
		return ErrRouteNotFound
	}
	result, err := d.db.ExecContext(ctx, "DELETE FROM routes WHERE id = ? AND service_id = ? AND kind = 'http'", routeID, serviceID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrRouteNotFound
	}
	return nil
}
