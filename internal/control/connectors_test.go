package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateAndListConnectors(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateConnector(context.Background(), "edge-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateConnector(context.Background(), "edge-a"); !errors.Is(err, ErrConnectorExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	connectors, err := db.ListConnectors(context.Background())
	if err != nil || len(connectors) != 1 || connectors[0].Name != "edge-a" {
		t.Fatalf("connectors = %#v, %v", connectors, err)
	}
}

func TestConnectorDescription(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connector, err := db.CreateDescribedConnector(ctx, "edge-a", "  NAS in the living room\r\nUPS backed  ")
	if err != nil || connector.Description != "NAS in the living room\nUPS backed" {
		t.Fatalf("connector = %#v, %v", connector, err)
	}
	for _, bad := range []string{"tab\there", string(make([]rune, MaxDescriptionLength+1))} {
		if _, err := db.SetConnectorDescription(ctx, connector.ID, bad); !errors.Is(err, ErrInvalidDescription) {
			t.Fatalf("description %q error = %v", bad, err)
		}
	}
	if _, err := db.SetConnectorDescription(ctx, "m_missing", "note"); !errors.Is(err, ErrConnectorNotFound) {
		t.Fatalf("missing connector error = %v", err)
	}
	if description, err := db.SetConnectorDescription(ctx, connector.ID, "Backups only"); err != nil || description != "Backups only" {
		t.Fatalf("description = %q, %v", description, err)
	}
	connectors, err := db.ListConnectors(ctx)
	if err != nil || connectors[0].Description != "Backups only" {
		t.Fatalf("connectors = %#v, %v", connectors, err)
	}
}

func TestConnectorDisplayName(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	if name, err := db.SetConnectorDisplayName(ctx, connector.ID, "  거실 NAS  "); err != nil || name != "거실 NAS" {
		t.Fatalf("display name = %q, %v", name, err)
	}
	if _, err := db.SetConnectorDisplayName(ctx, connector.ID, "bad\nname"); !errors.Is(err, ErrInvalidDisplayName) {
		t.Fatalf("control character error = %v", err)
	}
	connectors, err := db.ListConnectors(ctx)
	if err != nil || connectors[0].Name != "edge-a" || connectors[0].DisplayName != "거실 NAS" {
		t.Fatalf("connectors = %#v, %v", connectors, err)
	}
}
