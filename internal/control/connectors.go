package control

import (
	"context"
	"errors"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrConnectorExists   = errors.New("connector already exists")
	ErrInvalidConnector  = errors.New("invalid connector")
	ErrConnectorNotFound = errors.New("connector not found")
)

type Connector struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *Database) CreateConnector(ctx context.Context, name string) (Connector, error) {
	if !validation.Label(name) {
		return Connector{}, ErrInvalidConnector
	}
	id, err := randomID()
	if err != nil {
		return Connector{}, err
	}
	now := time.Now().UTC()
	c := Connector{ID: id, Name: name, Status: "draft", CreatedAt: now}
	_, err = d.db.ExecContext(ctx, "INSERT INTO connectors(id, name, status, created_at) VALUES (?, ?, ?, ?)", c.ID, c.Name, c.Status, c.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Connector{}, ErrConnectorExists
	}
	return c, nil
}

func (d *Database) ConnectorExists(ctx context.Context, id string) (bool, error) {
	var exists int
	err := d.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM connectors WHERE id = ? AND revoked_at IS NULL)", id).Scan(&exists)
	return exists == 1, err
}

// DeleteConnector removes a Connector and all of its services and routes in
// one transaction so its name and public hostnames can be reused.
func (d *Database) DeleteConnector(ctx context.Context, id string) error {
	if !validation.Label(id) {
		return ErrConnectorNotFound
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM routes WHERE service_id IN (SELECT id FROM services WHERE connector_id = ?)`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE connector_id = ?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM connectors WHERE id = ? AND revoked_at IS NULL`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConnectorNotFound
	}
	return tx.Commit()
}

func (d *Database) ListConnectors(ctx context.Context) ([]Connector, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id, name, status, created_at FROM connectors WHERE revoked_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connectors := []Connector{}
	for rows.Next() {
		var c Connector
		var created string
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &created); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		connectors = append(connectors, c)
	}
	return connectors, rows.Err()
}
