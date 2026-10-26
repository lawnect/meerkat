package control

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/validation"
)

var (
	ErrInvalidDomain     = errors.New("invalid outlet domain")
	ErrDomainHasRoutes   = errors.New("cannot change domain while routes exist")
	ErrDomainExists      = errors.New("domain already registered")
	ErrDomainNotFound    = errors.New("domain not found")
	ErrPrimaryDomain     = errors.New("cannot remove primary domain")
	ErrInvalidDomainMode = errors.New("invalid domain mode")

	ErrInvalidTunnelAddress = errors.New("invalid tunnel address")
)

// Domain modes decide which DNS records an administrator creates.
// DomainWildcard points *.domain at the Outlet; DomainHosts points the
// management host and each public address at it one by one, so the root and
// other names under the domain can stay on another server.
const (
	DomainWildcard = "wildcard"
	DomainHosts    = "hosts"
)

type Outlet struct {
	Domain  string            `json:"domain"`
	Domains []string          `json:"domains"`
	Modes   map[string]string `json:"modes"`
	// AdminHostname serves the dashboard and the Connector API. Its label is
	// generated once, so the management endpoint is not guessable from the domain.
	AdminHostname string `json:"admin_hostname,omitempty"`
}

func validMode(mode string) bool { return mode == DomainWildcard || mode == DomainHosts }

// randomLabel returns 8 lowercase letters and digits that start with a letter.
func randomLabel() (string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const alphabet = letters + "0123456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	label := []byte{letters[int(b[0])%len(letters)]}
	for _, v := range b[1:] {
		label = append(label, alphabet[int(v)%len(alphabet)])
	}
	return string(label), nil
}

func (d *Database) Outlet(ctx context.Context) (Outlet, error) {
	outlet := Outlet{Domains: []string{}, Modes: map[string]string{}}
	err := d.db.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'domain'").Scan(&outlet.Domain)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return outlet, nil
		}
		return Outlet{}, err
	}
	label := contract.LegacyAdminLabel
	if err := d.db.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'admin_label'").Scan(&label); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Outlet{}, err
	}
	outlet.AdminHostname = contract.AdminHostname(label, outlet.Domain)
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
	for _, domain := range outlet.Domains {
		outlet.Modes[domain] = DomainWildcard
	}
	modes, err := d.db.QueryContext(ctx, "SELECT domain, mode FROM outlet_domain_modes")
	if err != nil {
		return Outlet{}, err
	}
	defer modes.Close()
	for modes.Next() {
		var domain, mode string
		if err := modes.Scan(&domain, &mode); err != nil {
			return Outlet{}, err
		}
		if _, ok := outlet.Modes[domain]; ok {
			outlet.Modes[domain] = mode
		}
	}
	return outlet, modes.Err()
}

func setMode(ctx context.Context, tx *sql.Tx, domain, mode string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO outlet_domain_modes(domain, mode, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET mode = excluded.mode, updated_at = excluded.updated_at`, domain, mode, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// SetDomainMode changes how a registered domain is pointed at the Outlet.
func (d *Database) SetDomainMode(ctx context.Context, domain, mode string) (Outlet, error) {
	if !validMode(mode) {
		return Outlet{}, ErrInvalidDomainMode
	}
	outlet, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	if _, ok := outlet.Modes[domain]; !ok {
		return Outlet{}, ErrDomainNotFound
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Outlet{}, err
	}
	defer tx.Rollback()
	if err := setMode(ctx, tx, domain, mode); err != nil {
		return Outlet{}, err
	}
	if err := tx.Commit(); err != nil {
		return Outlet{}, err
	}
	return d.Outlet(ctx)
}

// DomainRouteHostnames lists the public addresses registered under a domain.
func (d *Database) DomainRouteHostnames(ctx context.Context, domain string) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT hostname FROM routes WHERE hostname LIKE ? ORDER BY hostname", "%."+domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hostnames := []string{}
	for rows.Next() {
		var hostname string
		if err := rows.Scan(&hostname); err != nil {
			return nil, err
		}
		if strings.HasSuffix(hostname, "."+domain) {
			hostnames = append(hostnames, hostname)
		}
	}
	return hostnames, rows.Err()
}

func overlapsDomain(a, b string) bool {
	return a == b || strings.HasSuffix(a, "."+b) || strings.HasSuffix(b, "."+a)
}

func (d *Database) AddDomain(ctx context.Context, domain, mode string) (Outlet, error) {
	if !validation.Hostname(domain) {
		return Outlet{}, ErrInvalidDomain
	}
	if mode == "" {
		mode = DomainWildcard
	}
	if !validMode(mode) {
		return Outlet{}, ErrInvalidDomainMode
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
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Outlet{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO outlet_domains(domain, created_at) VALUES (?, ?)", domain, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return Outlet{}, err
	}
	if err := setMode(ctx, tx, domain, mode); err != nil {
		return Outlet{}, err
	}
	if err := tx.Commit(); err != nil {
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
	if _, err := d.db.ExecContext(ctx, "DELETE FROM outlet_domain_modes WHERE domain = ?", domain); err != nil {
		return Outlet{}, err
	}
	return d.Outlet(ctx)
}

// TunnelAddress returns the administrator's tunnel endpoint, or "" when the
// Outlet derives it from outlet.json or the primary domain.
func (d *Database) TunnelAddress(ctx context.Context) (string, error) {
	var address string
	err := d.db.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'tunnel_address'").Scan(&address)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return address, err
}

// SetTunnelAddress stores a host:port endpoint. An empty address restores the default.
func (d *Database) SetTunnelAddress(ctx context.Context, address string) error {
	if address == "" {
		_, err := d.db.ExecContext(ctx, "DELETE FROM outlet_settings WHERE key = 'tunnel_address'")
		return err
	}
	if !validation.TunnelAddress(address) {
		return ErrInvalidTunnelAddress
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES ('tunnel_address', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, address, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// SetDomain sets the primary domain. An empty mode keeps the current one.
// The management label is generated the first time a domain is set.
func (d *Database) SetDomain(ctx context.Context, domain, mode string) (Outlet, error) {
	if mode != "" && !validMode(mode) {
		return Outlet{}, ErrInvalidDomainMode
	}
	current, err := d.Outlet(ctx)
	if err != nil {
		return Outlet{}, err
	}
	label := ""
	if current.AdminHostname != "" {
		label = strings.TrimSuffix(current.AdminHostname, "."+current.Domain)
	} else if label, err = randomLabel(); err != nil {
		return Outlet{}, err
	}
	if !validation.Hostname(domain) || !validation.Hostname(contract.AdminHostname(label, domain)) {
		return Outlet{}, ErrInvalidDomain
	}
	if mode == "" {
		mode = current.Modes[current.Domain]
		if mode == "" {
			mode = DomainWildcard
		}
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
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Outlet{}, err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{"domain": domain, "admin_label": label} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now); err != nil {
			return Outlet{}, err
		}
	}
	if current.Domain != "" && current.Domain != domain {
		if _, err := tx.ExecContext(ctx, "DELETE FROM outlet_domain_modes WHERE domain = ?", current.Domain); err != nil {
			return Outlet{}, err
		}
	}
	if err := setMode(ctx, tx, domain, mode); err != nil {
		return Outlet{}, err
	}
	if err := tx.Commit(); err != nil {
		return Outlet{}, err
	}
	return d.Outlet(ctx)
}
