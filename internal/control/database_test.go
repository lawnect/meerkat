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
	if version, err := db.Version(ctx); err != nil || version != 1 {
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
