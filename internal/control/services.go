package control

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrServiceExists       = errors.New("service already exists")
	ErrServiceTargetExists = errors.New("service target already exists")
	ErrServiceHasRoutes    = errors.New("service has routes")
	ErrInvalidService      = errors.New("invalid service")
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
	result, err := d.db.ExecContext(ctx, `INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, enabled, observed_status, created_at, updated_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM services WHERE connector_id = ? AND target_host = ? COLLATE NOCASE AND target_port = ?)`,
		s.ID, s.ConnectorID, s.Name, s.Protocol, s.Target.Host, s.Target.Port, 1, s.ObservedStatus, s.CreatedAt.Format(time.RFC3339Nano), s.UpdatedAt.Format(time.RFC3339Nano), connectorID, target.Host, target.Port)
	if err != nil {
		return Service{}, ErrServiceExists
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Service{}, err
	}
	if affected == 0 {
		var nameExists bool
		if err := d.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE connector_id = ? AND name = ?)`, connectorID, name).Scan(&nameExists); err != nil {
			return Service{}, err
		}
		if nameExists {
			return Service{}, ErrServiceExists
		}
		return Service{}, ErrServiceTargetExists
	}
	d.routingChanged()
	return s, nil
}

func (d *Database) UpdateServiceTarget(ctx context.Context, connectorID, serviceID string, target contract.Target) (Service, error) {
	if !validation.Label(connectorID) || !validation.Label(serviceID) || validation.Target(target) != nil {
		return Service{}, ErrInvalidService
	}
	now := time.Now().UTC()
	result, err := d.db.ExecContext(ctx, `UPDATE services SET target_host = ?, target_port = ?, observed_status = 'unknown', updated_at = ?
		WHERE id = ? AND connector_id = ?
		AND NOT EXISTS (SELECT 1 FROM services WHERE connector_id = ? AND id != ? AND target_host = ? COLLATE NOCASE AND target_port = ?)`,
		target.Host, target.Port, now.Format(time.RFC3339Nano), serviceID, connectorID, connectorID, serviceID, target.Host, target.Port)
	if err != nil {
		return Service{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Service{}, err
	}
	if affected != 0 {
		d.routingChanged()
	}
	if affected == 0 {
		var exists bool
		if err := d.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id = ? AND connector_id = ?)`, serviceID, connectorID).Scan(&exists); err != nil {
			return Service{}, err
		}
		if exists {
			return Service{}, ErrServiceTargetExists
		}
		return Service{}, ErrServiceNotFound
	}
	services, err := d.ListServices(ctx, connectorID)
	if err != nil {
		return Service{}, err
	}
	for _, service := range services {
		if service.ID == serviceID {
			return service, nil
		}
	}
	return Service{}, ErrServiceNotFound
}

// DeleteService removes a service only when it has no public routes.
func (d *Database) DeleteService(ctx context.Context, connectorID, serviceID string) error {
	if !validation.Label(connectorID) || !validation.Label(serviceID) {
		return ErrServiceNotFound
	}
	return d.changeRouting(ctx, func(tx *sql.Tx) error {
		var routes int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routes r JOIN services s ON s.id = r.service_id WHERE s.id = ? AND s.connector_id = ?`, serviceID, connectorID).Scan(&routes)
		if err != nil {
			return err
		}
		if routes != 0 {
			return ErrServiceHasRoutes
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM services WHERE id = ? AND connector_id = ?`, serviceID, connectorID)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			return ErrServiceNotFound
		}
		return nil
	})
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
		FROM services WHERE connector_id = ? ORDER BY rowid DESC`, connectorID)
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
