package control

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrConnectorExists    = errors.New("connector already exists")
	ErrInvalidConnector   = errors.New("invalid connector")
	ErrConnectorNotFound  = errors.New("connector not found")
	ErrInvalidDescription = errors.New("invalid connector description")
	ErrInvalidDisplayName = errors.New("invalid connector display name")
)

// MaxDescriptionLength limits the administrator's note on a Connector.
const MaxDescriptionLength = 500

// NormalizeDescription trims a note and rejects control characters other
// than line breaks so it renders as plain text everywhere.
func NormalizeDescription(raw string) (string, bool) {
	description := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if !utf8.ValidString(description) || utf8.RuneCountInString(description) > MaxDescriptionLength {
		return "", false
	}
	for _, r := range description {
		if r != '\n' && unicode.IsControl(r) {
			return "", false
		}
	}
	return description, true
}

type Connector struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// DisplayName is what the dashboard shows. Name stays fixed because the
	// Connector enrolls with it and the install command uses it.
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    string    `json:"last_seen,omitempty"`
	// Version is the release the Connector reported in its last heartbeat;
	// the Outlet fills it, and it isn't stored here.
	Version string `json:"version,omitempty"`
}

func (d *Database) CreateConnector(ctx context.Context, name string) (Connector, error) {
	return d.CreateDescribedConnector(ctx, name, "")
}

// CreateDescribedConnector creates a draft Connector with an optional note.
func (d *Database) CreateDescribedConnector(ctx context.Context, name, description string) (Connector, error) {
	if !validation.Label(name) {
		return Connector{}, ErrInvalidConnector
	}
	description, ok := NormalizeDescription(description)
	if !ok {
		return Connector{}, ErrInvalidDescription
	}
	id, err := randomID()
	if err != nil {
		return Connector{}, err
	}
	now := time.Now().UTC()
	c := Connector{ID: id, Name: name, Description: description, Status: "draft", CreatedAt: now}
	_, err = d.db.ExecContext(ctx, "INSERT INTO connectors(id, name, description, status, created_at) VALUES (?, ?, ?, ?, ?)", c.ID, c.Name, c.Description, c.Status, c.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return Connector{}, ErrConnectorExists
	}
	d.routingChanged()
	return c, nil
}

// MaxDisplayNameLength limits the dashboard name of a Connector.
const MaxDisplayNameLength = 64

// NormalizeDisplayName trims a dashboard name and rejects control characters.
// An empty name means "show the Connector name".
func NormalizeDisplayName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

// SetConnectorDisplayName changes only the name shown in the dashboard.
func (d *Database) SetConnectorDisplayName(ctx context.Context, id, raw string) (string, error) {
	name, ok := NormalizeDisplayName(raw)
	if !ok {
		return "", ErrInvalidDisplayName
	}
	result, err := d.db.ExecContext(ctx, "UPDATE connectors SET display_name = ? WHERE id = ? AND revoked_at IS NULL", name, id)
	if err != nil {
		return "", err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return "", err
	} else if affected == 0 {
		return "", ErrConnectorNotFound
	}
	d.routingChanged()
	return name, nil
}

// SetConnectorDescription replaces the administrator's note on a Connector.
func (d *Database) SetConnectorDescription(ctx context.Context, id, raw string) (string, error) {
	description, ok := NormalizeDescription(raw)
	if !ok {
		return "", ErrInvalidDescription
	}
	result, err := d.db.ExecContext(ctx, "UPDATE connectors SET description = ? WHERE id = ? AND revoked_at IS NULL", description, id)
	if err != nil {
		return "", err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return "", err
	} else if affected == 0 {
		return "", ErrConnectorNotFound
	}
	d.routingChanged()
	return description, nil
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
	return d.changeRouting(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM routes WHERE service_id IN (SELECT id FROM services WHERE connector_id = ?)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM services WHERE connector_id = ?`, id); err != nil {
			return err
		}
		// An address no service uses any more takes its policy along.
		if _, err := tx.ExecContext(ctx, `DELETE FROM address_policies WHERE hostname NOT IN (SELECT hostname FROM routes WHERE kind = 'http')`); err != nil {
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
		return nil
	})
}

// ListConnectors answers from memory until a Connector changes.
func (d *Database) ListConnectors(ctx context.Context) ([]Connector, error) {
	connectors, err := d.connectors.get(&d.generation, func() ([]Connector, error) { return d.loadConnectors(ctx) })
	return slices.Clone(connectors), err
}

func (d *Database) loadConnectors(ctx context.Context) ([]Connector, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id, name, display_name, description, status, created_at FROM connectors WHERE revoked_at IS NULL ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	connectors := []Connector{}
	for rows.Next() {
		var c Connector
		var created string
		if err := rows.Scan(&c.ID, &c.Name, &c.DisplayName, &c.Description, &c.Status, &created); err != nil {
			return nil, err
		}
		if c.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		connectors = append(connectors, c)
	}
	return connectors, rows.Err()
}
