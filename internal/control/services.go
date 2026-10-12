package control

import (
	"context"
	"errors"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrServiceExists   = errors.New("service already exists")
	ErrInvalidService  = errors.New("invalid service")
	ErrServiceRequired = errors.New("connector needs exactly one service")
)

// Service is desired state only. It has no public endpoint until a Route is
// created and applied.
type Service struct {
	ID             string          `json:"id"`
	ConnectorID    string          `json:"connector_id"`
	Name           string          `json:"name"`
	Protocol       string          `json:"protocol"`
	Target         contract.Target `json:"target"`
	Enabled        bool            `json:"enabled"`
	ObservedStatus string          `json:"observed_status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Enrollment currently starts one rathole client per Connector. Multiple
// services require a future multiplexed connector protocol and are rejected
// until then rather than silently exposing the wrong target.
func (d *Database) EnrollmentService(ctx context.Context, connectorID string) (Service, error) {
	services, err := d.ListServices(ctx, connectorID)
	if err != nil {
		return Service{}, err
	}
	if len(services) != 1 {
		return Service{}, ErrServiceRequired
	}
	return services[0], nil
}

func (d *Database) CreateService(ctx context.Context, connectorID, name, protocol string, target contract.Target) (Service, error) {
	if !validation.Label(connectorID) || !validation.Label(name) || protocol != "http" || validation.Target(target) != nil {
		return Service{}, ErrInvalidService
	}
	exists, err := d.ConnectorExists(ctx, connectorID)
	if err != nil {
		return Service{}, err
	}
	if !exists {
		return Service{}, ErrConnectorNotFound
	}
	id, err := randomID()
	if err != nil {
		return Service{}, err
	}
	now := time.Now().UTC()
	s := Service{ID: id, ConnectorID: connectorID, Name: name, Protocol: protocol, Target: target, Enabled: true, ObservedStatus: "unknown", CreatedAt: now, UpdatedAt: now}
	_, err = d.db.ExecContext(ctx, `INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, enabled, observed_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.ID, s.ConnectorID, s.Name, s.Protocol, s.Target.Host, s.Target.Port, 1, s.ObservedStatus, s.CreatedAt.Format(time.RFC3339Nano), s.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Service{}, ErrServiceExists
	}
	return s, nil
}

func (d *Database) ListServices(ctx context.Context, connectorID string) ([]Service, error) {
	if !validation.Label(connectorID) {
		return nil, ErrConnectorNotFound
	}
	exists, err := d.ConnectorExists(ctx, connectorID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrConnectorNotFound
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id, connector_id, name, protocol, target_host, target_port, enabled, observed_status, created_at, updated_at
		FROM services WHERE connector_id = ? ORDER BY created_at DESC`, connectorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	services := []Service{}
	for rows.Next() {
		var s Service
		var enabled int
		var created, updated string
		if err := rows.Scan(&s.ID, &s.ConnectorID, &s.Name, &s.Protocol, &s.Target.Host, &s.Target.Port, &enabled, &s.ObservedStatus, &created, &updated); err != nil {
			return nil, err
		}
		if s.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		if s.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return nil, err
		}
		s.Enabled = enabled != 0
		services = append(services, s)
	}
	return services, rows.Err()
}
