// Package control owns Outlet management state. Data-plane credentials and
// Connector private keys deliberately remain outside this database.
package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/lawnect/meerkat/internal/securefs"
)

type Database struct{ db *sql.DB }

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

// SQL exposes the connection for the control-plane repositories only.
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
