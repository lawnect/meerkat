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
