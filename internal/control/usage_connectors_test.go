package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestOutletUsageSplitsTheRangeByTunnel(t *testing.T) {
	ctx, db := context.Background(), openUsageDB(t)
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c2', 'busy-tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c3', 'quiet-tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s2', 'c2', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s3', 'c3', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r3', 's2', 'http', 'three.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r4', 's3', 'http', 'four.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		route, hour                      string
		requests, sent, received, failed int
	}{
		{"r1", "2026-10-21T14:00:00Z", 10, 100, 1000, 1},
		{"r2", "2026-10-21T14:00:00Z", 5, 50, 500, 0},
		{"r2", "2026-10-21T15:00:00Z", 7, 70, 700, 2},
		{"r3", "2026-10-21T14:00:00Z", 30, 300, 3000, 4},
		{"r3", "2026-10-21T16:00:00Z", 99, 990, 9900, 9}, // after the range
		{"r3", "2026-10-21T13:00:00Z", 99, 990, 9900, 9}, // before the range
		{"r4", "2026-10-21T14:00:00Z", 0, 0, 0, 0},       // no requests: left out
		{"gone", "2026-10-21T14:00:00Z", 500, 1, 1, 1},   // the route was deleted
	} {
		if _, err := db.SQL().ExecContext(ctx, `INSERT INTO usage_hourly(route_id, hour, requests, request_bytes, response_bytes, error_responses) VALUES (?, ?, ?, ?, ?, ?)`,
			row.route, row.hour, row.requests, row.sent, row.received, row.failed); err != nil {
			t.Fatal(err)
		}
	}
	series, err := db.OutletUsageSeries(ctx, "hour", mustTime(t, "2026-10-21T14:00:00Z"), mustTime(t, "2026-10-21T16:00:00Z"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := []ConnectorTotal{
		{ConnectorID: "c2", Name: "busy-tunnel", Requests: 30, RequestBytes: 300, ResponseBytes: 3000, ErrorResponses: 4},
		{ConnectorID: "c1", Name: "tunnel", Requests: 22, RequestBytes: 220, ResponseBytes: 2200, ErrorResponses: 3},
	}
	if len(series.Connectors) != len(want) || series.Connectors[0] != want[0] || series.Connectors[1] != want[1] {
		t.Fatalf("connectors = %+v, want %+v", series.Connectors, want)
	}
	raw, _ := json.Marshal(series.Connectors[0])
	if string(raw) != `{"connector_id":"c2","name":"busy-tunnel","requests":30,"request_bytes":300,"response_bytes":3000,"error_responses":4}` {
		t.Fatalf("json = %s", raw)
	}

	// A range with no traffic is an empty list, not null.
	empty, err := db.OutletUsageSeries(ctx, "day", mustTime(t, "2026-01-01T00:00:00Z"), mustTime(t, "2026-01-03T00:00:00Z"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(empty)
	if !strings.Contains(string(raw), `"connectors":[]`) {
		t.Fatalf("empty range = %s", raw)
	}

	// A deleted route's hours leave the totals, like the series' route-level data.
	if _, err := db.SQL().ExecContext(ctx, "DELETE FROM routes WHERE id = 'r3'"); err != nil {
		t.Fatal(err)
	}
	series, _ = db.OutletUsageSeries(ctx, "hour", mustTime(t, "2026-10-21T14:00:00Z"), mustTime(t, "2026-10-21T16:00:00Z"), time.UTC)
	if len(series.Connectors) != 1 || series.Connectors[0].ConnectorID != "c1" {
		t.Fatalf("after deleting the route: %+v", series.Connectors)
	}
}

// The Outlet's totals are kept by minute too, for a week: a scrape and the
// rate limit's rejections land in their minute, and a range of minutes is
// read from them, its tunnels from the hours it touches.
func TestOutletUsageByMinute(t *testing.T) {
	ctx, db := context.Background(), openUsageDB(t)
	at := mustTime(t, "2026-10-21T14:05:20Z")
	if err := db.RecordUsage(ctx, map[string]UsageSample{"r1": {Requests: 10, RequestBytes: 100, ResponseBytes: 1000, ErrorResponses: 1}}, at, "g"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUsage(ctx, map[string]UsageSample{"r1": {Requests: 13, RequestBytes: 130, ResponseBytes: 1300, ErrorResponses: 1}}, at.Add(50*time.Second), "g"); err != nil {
		t.Fatal(err)
	}
	if err := db.AddRateLimited(ctx, at.Add(time.Second), 4); err != nil {
		t.Fatal(err)
	}
	series, err := db.OutletUsageSeries(ctx, "minute", mustTime(t, "2026-10-21T14:00:00Z"), mustTime(t, "2026-10-21T15:00:00Z"), time.UTC)
	if err != nil || len(series.Buckets) != 60 {
		t.Fatalf("%d buckets, %v", len(series.Buckets), err)
	}
	five, six := series.Buckets[5], series.Buckets[6]
	if five.Start != "2026-10-21T14:05:00Z" || five.Requests != 10 || five.RateLimited != 4 || six.Requests != 3 || six.ResponseBytes != 300 {
		t.Fatalf("14:05 %+v, 14:06 %+v", five, six)
	}
	hourly, _ := db.OutletUsageSeries(ctx, "hour", mustTime(t, "2026-10-21T14:00:00Z"), mustTime(t, "2026-10-21T15:00:00Z"), time.UTC)
	if hourly.Buckets[0].Requests != 13 || hourly.Buckets[0].RateLimited != 4 {
		t.Fatalf("hour: %+v", hourly.Buckets[0])
	}
	// The tunnels split the range's own totals, as the hours it touches do.
	if len(series.Connectors) != 1 || series.Connectors[0].Requests != 13 {
		t.Fatalf("tunnels: %+v", series.Connectors)
	}
	part, _ := db.OutletUsageSeries(ctx, "minute", mustTime(t, "2026-10-21T14:06:00Z"), mustTime(t, "2026-10-21T14:10:00Z"), time.UTC)
	if len(part.Connectors) != 1 || part.Connectors[0].Requests != 3 || part.Connectors[0].ResponseBytes != 300 {
		t.Fatalf("tunnels of part of the hour: %+v", part.Connectors)
	}
	// A week later the minutes are gone; the hour stays.
	if err := db.RecordUsage(ctx, nil, at.Add(8*24*time.Hour), "g"); err != nil {
		t.Fatal(err)
	}
	if series, _ = db.OutletUsageSeries(ctx, "minute", mustTime(t, "2026-10-21T14:00:00Z"), mustTime(t, "2026-10-21T15:00:00Z"), time.UTC); series.Buckets[5].Requests != 0 {
		t.Fatalf("kept past a week: %+v", series.Buckets[5])
	}
	if _, err := db.AccessSeries(ctx, "minute", at, at.Add(time.Hour), time.UTC); err == nil {
		t.Fatal("access decisions by minute")
	}
	if _, err := db.LatencySeries(ctx, "minute", at, at.Add(time.Hour), time.UTC); err == nil {
		t.Fatal("latency by minute")
	}
}
