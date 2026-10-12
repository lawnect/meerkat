package control

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrInvalidDomain   = errors.New("invalid outlet domain")
	ErrDomainHasRoutes = errors.New("cannot change domain while routes exist")
)

type Outlet struct {
	Domain string `json:"domain"`
}

func (d *Database) Outlet(ctx context.Context) (Outlet, error) {
	var outlet Outlet
	err := d.db.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'domain'").Scan(&outlet.Domain)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Outlet{}, nil
		}
		return Outlet{}, err
	}
	return outlet, nil
}

func (d *Database) SetDomain(ctx context.Context, domain string) (Outlet, error) {
	if !validation.Hostname(domain) {
		return Outlet{}, ErrInvalidDomain
	}
	current, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	if current.Domain != "" && current.Domain != domain {
		var routes int
		if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM routes").Scan(&routes); err != nil {
			return Outlet{}, err
		}
		if routes != 0 {
			return Outlet{}, ErrDomainHasRoutes
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.db.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES ('domain', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, domain, now); err != nil {
		return Outlet{}, err
	}
	return Outlet{Domain: domain}, nil
}
