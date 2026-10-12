package control

import (
	"context"
	"errors"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var ErrConnectorExists = errors.New("connector already exists")

type Connector struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *Database) CreateConnector(ctx context.Context, name string) (Connector, error) {
	if !validation.Label(name) {
		return Connector{}, ErrInvalidCredentials
	}
	id, err := randomToken()
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
