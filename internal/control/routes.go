package control

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrRouteExists     = errors.New("route already exists")
	ErrInvalidRoute    = errors.New("invalid route")
	ErrServiceNotFound = errors.New("service not found")
)

// Route is a desired public endpoint. It remains disabled until a later
// reconciler validates and applies the corresponding proxy configuration.
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

func (d *Database) CreateHTTPRoute(ctx context.Context, serviceID, hostname string) (Route, error) {
	if !validation.Label(serviceID) || !validation.Hostname(hostname) {
		return Route{}, ErrInvalidRoute
	}
	outlet, err := d.Outlet(ctx)
	if err != nil {
		return Route{}, err
	}
	if outlet.Domain == "" {
		return Route{}, ErrInvalidRoute
	}
	if hostname != outlet.Domain && !strings.HasSuffix(hostname, "."+outlet.Domain) {
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
	route := Route{ID: id, ServiceID: serviceID, Kind: "http", Hostname: hostname, CreatedAt: now, UpdatedAt: now}
	_, err = d.db.ExecContext(ctx, `INSERT INTO routes(id, service_id, kind, hostname, enabled, desired_revision, applied_revision, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, route.ID, route.ServiceID, route.Kind, route.Hostname, 0, 0, 0, route.CreatedAt.Format(time.RFC3339Nano), route.UpdatedAt.Format(time.RFC3339Nano))
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
