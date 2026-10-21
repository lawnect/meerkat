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
	ErrInvalidDomain   = errors.New("invalid outlet domain")
	ErrDomainHasRoutes = errors.New("cannot change domain while routes exist")
	ErrDomainExists    = errors.New("domain already registered")
	ErrDomainNotFound  = errors.New("domain not found")
	ErrPrimaryDomain   = errors.New("cannot remove primary domain")
)

type Outlet struct {
	Domain  string   `json:"domain"`
	Domains []string `json:"domains"`
}

func (d *Database) Outlet(ctx context.Context) (Outlet, error) {
	var outlet Outlet
	err := d.db.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'domain'").Scan(&outlet.Domain)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Outlet{Domains: []string{}}, nil
		}
		return Outlet{}, err
	}
	outlet.Domains = []string{outlet.Domain}
	rows, err := d.db.QueryContext(ctx, "SELECT domain FROM outlet_domains ORDER BY domain")
	if err != nil {
		return Outlet{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return Outlet{}, err
		}
		outlet.Domains = append(outlet.Domains, domain)
	}
	if err := rows.Err(); err != nil {
		return Outlet{}, err
	}
	return outlet, nil
}

func overlapsDomain(a, b string) bool {
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

func (d *Database) AddDomain(ctx context.Context, domain string) (Outlet, error) {
	if !validation.Hostname(domain) {
		return Outlet{}, ErrInvalidDomain
	}
	outlet, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	if outlet.Domain == "" {
		return Outlet{}, ErrInvalidDomain
	}
	for _, existing := range outlet.Domains {
		if overlapsDomain(existing, domain) {
			return Outlet{}, ErrDomainExists
		}
	}
	_, err = d.db.ExecContext(ctx, "INSERT INTO outlet_domains(domain, created_at) VALUES (?, ?)", domain, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return Outlet{}, err
	}
	return d.Outlet(ctx)
}

func (d *Database) RemoveDomain(ctx context.Context, domain string) (Outlet, error) {
	outlet, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	if domain == outlet.Domain {
		return Outlet{}, ErrPrimaryDomain
	}
	found := false
	for _, existing := range outlet.Domains {
		if domain == existing {
			found = true
			break
		}
	}
	if !found {
		return Outlet{}, ErrDomainNotFound
	}
	var routes int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM routes WHERE hostname = ? OR hostname LIKE ?", domain, "%."+domain).Scan(&routes); err != nil {
		return Outlet{}, err
	}
	if routes != 0 {
		return Outlet{}, ErrDomainHasRoutes
	}
	if _, err := d.db.ExecContext(ctx, "DELETE FROM outlet_domains WHERE domain = ?", domain); err != nil {
		return Outlet{}, err
	}
	return d.Outlet(ctx)
}

func (d *Database) SetDomain(ctx context.Context, domain string) (Outlet, error) {
	if !validation.Hostname(domain) || !validation.Hostname(contract.AdminHostname(domain)) {
		return Outlet{}, ErrInvalidDomain
	}
	current, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	if current.Domain != "" && current.Domain != domain {
		for _, existing := range current.Domains[1:] {
			if overlapsDomain(existing, domain) {
				return Outlet{}, ErrDomainExists
			}
		}
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
	return d.Outlet(ctx)
}
