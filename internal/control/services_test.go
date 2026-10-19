package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
)

func TestCreateAndListServices(t *testing.T) {
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
	target := contract.Target{Host: "app", Port: 3000}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", target)
	if err != nil {
		t.Fatal(err)
	}
	if service.Target != target || !service.Enabled || service.ObservedStatus != "unknown" {
		t.Fatalf("service = %#v", service)
	}
	if _, err := db.CreateService(ctx, connector.ID, "web", "http", target); !errors.Is(err, ErrServiceExists) {
		t.Fatalf("duplicate error = %v", err)
	}
	second, err := db.CreateService(ctx, connector.ID, "api", "http", contract.Target{Host: "api", Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	services, err := db.ListServices(ctx, connector.ID)
	if err != nil || len(services) != 2 || services[0].ID != second.ID || services[1].ID != service.ID {
		t.Fatalf("services = %#v, %v", services, err)
	}
}

func TestCreateServiceRejectsUnsafeTargetAndUnknownConnector(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateService(ctx, "missing", "web", "http", contract.Target{Host: "app", Port: 3000}); !errors.Is(err, ErrConnectorNotFound) {
		t.Fatalf("missing connector error = %v", err)
	}
	connector, err := db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "8.8.8.8", Port: 53}); !errors.Is(err, ErrInvalidService) {
		t.Fatalf("unsafe target error = %v", err)
	}
}
