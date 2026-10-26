package control

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndPreservesData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if version, err := db.Version(ctx); err != nil || version != migrations[len(migrations)-1].version {
		t.Fatalf("version = %d, %v", version, err)
	}
	if _, err := db.SQL().ExecContext(ctx, "INSERT INTO outlet_settings(key, value, updated_at) VALUES ('domain', 'example.com', 'now')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var domain string
	if err := db.SQL().QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'domain'").Scan(&domain); err != nil {
		t.Fatal(err)
	}
	if domain != "example.com" {
		t.Fatalf("domain = %q", domain)
	}
}

func TestRouteConstraints(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := db.SQL()
	for _, statement := range []string{
		"INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'connector', 'pending', 'now')",
		"INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'web', 'http', 'app', 3000, 'now', 'now')",
		"INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r1', 's1', 'http', 'app.example.com', 'now', 'now')",
	} {
		if _, err := q.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	_, err = q.ExecContext(ctx, "INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r2', 's1', 'http', 'app.example.com', 'now', 'now')")
	if err == nil {
		t.Fatal("duplicate HTTP hostname was accepted")
	}
	if err == sql.ErrNoRows {
		t.Fatal("unexpected no rows error")
	}
}

func TestUsageMigrationAfterAbandonedVersionThree(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"DROP TABLE usage_counters",
		"DROP TABLE usage_hourly",
		"UPDATE schema_migrations SET version = 3 WHERE version = 4",
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('usage_counters', 'usage_hourly')").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("usage tables = %d", count)
	}
}

func TestDomainModeMigrationKeepsLegacyAdminLabel(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	all := migrations
	defer func() { migrations = all }()
	migrations = nil
	for _, m := range all {
		if m.version < 6 {
			migrations = append(migrations, m)
		}
	}
	if err := migrate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(ctx, "INSERT INTO outlet_settings(key, value, updated_at) VALUES ('domain', 'example.com', 'now')"); err != nil {
		t.Fatal(err)
	}
	migrations = all
	if err := migrate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var label string
	if err := raw.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'admin_label'").Scan(&label); err != nil || label != "meerkat" {
		t.Fatalf("admin label = %q, %v", label, err)
	}
}
