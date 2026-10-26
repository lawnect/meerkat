package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUsageDeltasAndRouteReplacement(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r1', 's1', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	for _, sample := range []struct {
		usage      UsageSample
		generation string
	}{
		{UsageSample{Requests: 10, RequestBytes: 100, ResponseBytes: 200, ErrorResponses: 1}, "start-1"},
		{UsageSample{Requests: 15, RequestBytes: 150, ResponseBytes: 300, ErrorResponses: 2}, "start-1"},
		{UsageSample{Requests: 2, RequestBytes: 20, ResponseBytes: 40}, "start-2"},
	} {
		if err := db.RecordUsage(ctx, map[string]UsageSample{"r1": sample.usage}, at, sample.generation); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := db.ConnectorUsage(ctx, "c1", at)
	if err != nil {
		t.Fatal(err)
	}
	last := usage.Hours[len(usage.Hours)-1]
	if last.Requests != 17 || last.RequestBytes != 170 || last.ResponseBytes != 340 || last.ErrorResponses != 2 {
		t.Fatalf("usage before replacement = %+v", last)
	}
	if _, err := db.SQL().ExecContext(ctx, `DELETE FROM routes WHERE id = 'r1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r2', 's1', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUsage(ctx, map[string]UsageSample{"r2": {Requests: 4}}, at, "start-2"); err != nil {
		t.Fatal(err)
	}
	usage, err = db.ConnectorUsage(ctx, "c1", at)
	if err != nil {
		t.Fatal(err)
	}
	// The baseline belongs to the host, so the new route counts only the
	// requests since the last scrape, not the host's history again.
	if got := usage.Hours[len(usage.Hours)-1].Requests; got != 2 {
		t.Fatalf("usage after replacement = %d", got)
	}
}

// A shared address recorded on another route after a reorder keeps one
// baseline, so its history isn't counted twice.
func TestSharedAddressUsageSurvivesReordering(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'a', 'http', 'a', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s2', 'c1', 'b', 'http', 'b', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at, priority) VALUES ('r1', 's1', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z', 0)`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at, priority) VALUES ('r2', 's2', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z', 1)`,
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Date(2026, 10, 21, 1, 0, 0, 0, time.UTC)
	for _, scrape := range []struct {
		route    string
		requests int64
	}{{"r1", 100}, {"r2", 110}, {"r1", 120}} {
		if err := db.RecordUsage(ctx, map[string]UsageSample{scrape.route: {Requests: scrape.requests}}, at, "g"); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := db.ConnectorUsage(ctx, "c1", at)
	if err != nil {
		t.Fatal(err)
	}
	if got := usage.Hours[len(usage.Hours)-1].Requests; got != 120 {
		t.Fatalf("requests = %d, want 120", got)
	}
}
