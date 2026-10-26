package outlet

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

func TestOutletUsageAPIListsTunnelTotals(t *testing.T) {
	f, db, h := usageStore(t)
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'home-server', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r1', 's1', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
	} {
		if _, err := db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.RecordUsage(t.Context(), map[string]control.UsageSample{"r1": {Requests: 12, RequestBytes: 120, ResponseBytes: 1200, ErrorResponses: 2}}, time.Date(2026, 10, 21, 3, 30, 0, 0, time.UTC), ""); err != nil {
		t.Fatal(err)
	}
	w := request(h, "GET", usageQuery("day", "2026-10-21T00:00:00Z", "2026-10-22T00:00:00Z", ""), f.admin, nil)
	var body struct {
		Connectors []map[string]any `json:"connectors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || len(body.Connectors) != 1 {
		t.Fatalf("usage: %d %s", w.Code, w.Body.String())
	}
	want := map[string]any{"connector_id": "c1", "name": "home-server", "requests": 12.0, "request_bytes": 120.0, "response_bytes": 1200.0, "error_responses": 2.0}
	if len(body.Connectors[0]) != len(want) {
		t.Fatalf("connector = %v", body.Connectors[0])
	}
	for key, value := range want {
		if body.Connectors[0][key] != value {
			t.Errorf("%s = %v, want %v", key, body.Connectors[0][key], value)
		}
	}
	// Another day has no traffic: an empty list, never null.
	w = request(h, "GET", usageQuery("day", "2026-10-22T00:00:00Z", "2026-10-23T00:00:00Z", ""), f.admin, nil)
	if !strings.Contains(w.Body.String(), `"connectors":[]`) {
		t.Fatalf("empty day: %s", w.Body.String())
	}
}
