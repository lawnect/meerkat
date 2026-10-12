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
