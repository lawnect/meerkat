// Package control owns Outlet management state. Data-plane credentials and
// Connector private keys deliberately remain outside this database.
package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"

	_ "modernc.org/sqlite"

	"github.com/lawnect/meerkat/internal/securefs"
)

type Database struct {
	db *sql.DB
	// generation counts committed changes to the tables the cached views
	// below are built from; see routingChanged.
	generation atomic.Uint64
	routing    cached[*Routing]
	connectors cached[[]Connector]
	// defaults are the environment's settings; see SetDefaults.
	defaults atomic.Pointer[Defaults]
}

func Open(ctx context.Context, path string) (*Database, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if err := securefs.Directory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Database{db: db}, nil
}

func (d *Database) Close() error { return d.db.Close() }

// SQL exposes the connection for the control-plane repositories only. Writes
// through it to the tables behind Routing, Outlet, TunnelAddress and
// ListConnectors bypass their caches.
func (d *Database) SQL() *sql.DB { return d.db }

func (d *Database) Version(ctx context.Context) (int, error) {
	var version int
	err := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	return version, err
}

type migration struct {
	version int
	sql     string
}

// Version 3 was used by a rolled-back rate-limit preview. Keep it reserved so
// databases that briefly ran that preview still receive the usage migration.
var migrations = []migration{{version: 1, sql: `
CREATE TABLE outlet_settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE admins (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL COLLATE NOCASE UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_login_at TEXT
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  admin_id TEXT NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);
CREATE INDEX sessions_active_by_admin ON sessions(admin_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE connectors (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  version TEXT,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE TABLE services (
  id TEXT PRIMARY KEY,
  connector_id TEXT NOT NULL REFERENCES connectors(id) ON DELETE RESTRICT,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL,
  target_host TEXT NOT NULL,
  target_port INTEGER NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  observed_status TEXT NOT NULL DEFAULT 'unknown',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(connector_id, name)
);

CREATE TABLE routes (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL,
  hostname TEXT COLLATE NOCASE UNIQUE,
  listen_port INTEGER UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 0,
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK ((kind = 'http' AND hostname IS NOT NULL AND listen_port IS NULL) OR
         (kind = 'tcp' AND hostname IS NULL AND listen_port IS NOT NULL))
);

CREATE TABLE operations (
  id TEXT PRIMARY KEY,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  state TEXT NOT NULL,
  detail TEXT,
  started_at TEXT NOT NULL,
  finished_at TEXT
);

CREATE TABLE audit_events (
  id TEXT PRIMARY KEY,
  actor_id TEXT REFERENCES admins(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  resource_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
`}, {version: 2, sql: `
CREATE TABLE outlet_domains (
  domain TEXT PRIMARY KEY,
  created_at TEXT NOT NULL
);
`}, {version: 4, sql: `
CREATE TABLE usage_counters (
  route_id TEXT NOT NULL,
  metric TEXT NOT NULL,
  value INTEGER NOT NULL,
  PRIMARY KEY(route_id, metric)
);
CREATE TABLE usage_hourly (
  route_id TEXT NOT NULL,
  hour TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  request_bytes INTEGER NOT NULL DEFAULT 0,
  response_bytes INTEGER NOT NULL DEFAULT 0,
  error_responses INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(route_id, hour)
);
CREATE INDEX usage_hourly_by_hour ON usage_hourly(hour);
`}, {version: 5, sql: `
CREATE TABLE usage_total_hourly (
  hour TEXT PRIMARY KEY,
  requests INTEGER NOT NULL DEFAULT 0,
  request_bytes INTEGER NOT NULL DEFAULT 0,
  response_bytes INTEGER NOT NULL DEFAULT 0,
  error_responses INTEGER NOT NULL DEFAULT 0
);
INSERT INTO usage_total_hourly(hour, requests, request_bytes, response_bytes, error_responses)
SELECT hour, SUM(requests), SUM(request_bytes), SUM(response_bytes), SUM(error_responses)
FROM usage_hourly WHERE true GROUP BY hour
ON CONFLICT(hour) DO NOTHING;
`}, {version: 6, sql: `
CREATE TABLE outlet_domain_modes (
  domain TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK (mode IN ('wildcard', 'hosts')),
  updated_at TEXT NOT NULL
);
INSERT INTO outlet_settings(key, value, updated_at)
SELECT 'admin_label', 'meerkat', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM outlet_settings WHERE key = 'domain'
ON CONFLICT(key) DO NOTHING;
`}, {version: 7, sql: `
ALTER TABLE connectors ADD COLUMN description TEXT NOT NULL DEFAULT '';
`}, {version: 8, sql: `
CREATE TABLE alert_events (
  id INTEGER PRIMARY KEY,
  key TEXT NOT NULL,
  kind TEXT NOT NULL,
  subject TEXT NOT NULL,
  message TEXT NOT NULL,
  ref TEXT NOT NULL DEFAULT '',
  opened_at TEXT NOT NULL,
  resolved_at TEXT,
  sent INTEGER NOT NULL DEFAULT 0,
  error TEXT
);
CREATE UNIQUE INDEX alert_events_open_key ON alert_events(key) WHERE resolved_at IS NULL;
CREATE INDEX alert_events_by_key ON alert_events(key, opened_at);
`}, {version: 9, sql: `
ALTER TABLE audit_events ADD COLUMN actor_email TEXT;
ALTER TABLE audit_events ADD COLUMN source TEXT;
ALTER TABLE audit_events ADD COLUMN ip TEXT;
ALTER TABLE audit_events ADD COLUMN user_agent TEXT;
ALTER TABLE audit_events ADD COLUMN detail TEXT;
CREATE INDEX audit_events_by_created ON audit_events(created_at);
CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit log is append-only'); END;
CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit log is append-only'); END;
CREATE TRIGGER audit_events_no_replace BEFORE INSERT ON audit_events
WHEN EXISTS (SELECT 1 FROM audit_events WHERE id = NEW.id)
BEGIN SELECT RAISE(ABORT, 'audit log is append-only'); END;
`}, {version: 10, sql: `
ALTER TABLE usage_total_hourly ADD COLUMN rate_limited INTEGER NOT NULL DEFAULT 0;
`}, {version: 11, sql: `
ALTER TABLE connectors ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
`}, {version: 12, sql: `
ALTER TABLE admins ADD COLUMN sso_issuer TEXT;
ALTER TABLE admins ADD COLUMN sso_subject TEXT;
CREATE UNIQUE INDEX admins_sso_identity ON admins(sso_issuer, sso_subject) WHERE sso_subject IS NOT NULL;
`}, {version: 13, sql: `
-- A public address may now lead to several services, each once, ordered by
-- priority; its policy says how requests are spread over them.
CREATE TABLE routes_shared (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id) ON DELETE RESTRICT,
  kind TEXT NOT NULL,
  hostname TEXT COLLATE NOCASE,
  listen_port INTEGER UNIQUE,
  enabled INTEGER NOT NULL DEFAULT 0,
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  UNIQUE(hostname, service_id),
  CHECK ((kind = 'http' AND hostname IS NOT NULL AND listen_port IS NULL) OR
         (kind = 'tcp' AND hostname IS NULL AND listen_port IS NOT NULL))
);
INSERT INTO routes_shared(id, service_id, kind, hostname, listen_port, enabled, desired_revision, applied_revision, last_error, created_at, updated_at)
  SELECT id, service_id, kind, hostname, listen_port, enabled, desired_revision, applied_revision, last_error, created_at, updated_at FROM routes;
DROP TABLE routes;
ALTER TABLE routes_shared RENAME TO routes;
CREATE INDEX routes_hostname ON routes(hostname);
CREATE TABLE address_policies (
  hostname TEXT PRIMARY KEY COLLATE NOCASE,
  mode TEXT NOT NULL CHECK (mode IN ('failover', 'balance')),
  sticky INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);
`}, {version: 14, sql: `
-- What each access policy decided, by hour.
CREATE TABLE access_hourly (
  policy_id TEXT NOT NULL,
  hour TEXT NOT NULL,
  passed INTEGER NOT NULL DEFAULT 0,
  challenged INTEGER NOT NULL DEFAULT 0,
  denied INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(policy_id, hour)
);
CREATE INDEX access_hourly_by_hour ON access_hourly(hour);
`}}

func migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	for _, migration := range migrations {
		var exists int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", migration.version).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, migration.sql); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))", migration.version)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", migration.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d: %w", migration.version, err)
		}
	}
	return nil
}
