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
	if _, err := db.CreateService(ctx, connector.ID, "other", "http", target); !errors.Is(err, ErrServiceTargetExists) {
		t.Fatalf("duplicate target error = %v", err)
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

// A service is "exists" only when its name is taken. A connector deleted
// after it was found is a missing connector, and any other failure of the
// insert is reported as what it is.
func TestCreateServiceTellsConstraintFailuresApart(t *testing.T) {
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
	// The connector goes between the lookup and the insert: here, as the
	// insert starts.
	if _, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER vanishing BEFORE INSERT ON services BEGIN DELETE FROM connectors WHERE id = NEW.connector_id; END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateService(ctx, connector.ID, "web", "http", target); !errors.Is(err, ErrConnectorNotFound) {
		t.Fatalf("vanished connector error = %v", err)
	}
	if _, err := db.SQL().ExecContext(ctx, `DROP TRIGGER vanishing`); err != nil {
		t.Fatal(err)
	}
	connector, err = db.CreateConnector(ctx, "edge-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `CREATE TRIGGER broken BEFORE INSERT ON services BEGIN SELECT RAISE(ABORT, 'disk is full'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateService(ctx, connector.ID, "web", "http", target); err == nil || errors.Is(err, ErrServiceExists) || errors.Is(err, ErrConnectorNotFound) {
		t.Fatalf("failing insert error = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := db.CreateService(canceled, connector.ID, "web", "http", target); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}
