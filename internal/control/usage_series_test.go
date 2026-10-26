package control

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openUsageDB(t *testing.T) *Database {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r1', 's1', 'http', 'one.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r2', 's1', 'http', 'two.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
	} {
		if _, err := db.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

// insertTotals stores one hour of Outlet-wide totals; request_bytes, response
// bytes and errors are derived from the request count to keep sums checkable.
func insertTotals(t *testing.T, db *Database, hour string, requests int64) {
	t.Helper()
	if _, err := db.SQL().Exec(`INSERT INTO usage_total_hourly(hour, requests, request_bytes, response_bytes, error_responses) VALUES (?, ?, ?, ?, ?)`,
		hour, requests, requests*10, requests*100, requests%2); err != nil {
		t.Fatal(err)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func requestsOf(series UsageSeries) []int64 {
	values := make([]int64, len(series.Buckets))
	for i, bucket := range series.Buckets {
		values[i] = bucket.Requests
	}
	return values
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOutletTotalsAccumulateAndSurviveRouteDeletion(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	scrapes := []struct {
		at      time.Time
		samples map[string]UsageSample
	}{
		{time.Date(2026, 10, 21, 1, 10, 0, 0, time.UTC), map[string]UsageSample{
			"r1": {Requests: 10, RequestBytes: 100, ResponseBytes: 200, ErrorResponses: 1},
			"r2": {Requests: 5, RequestBytes: 50, ResponseBytes: 90},
		}},
		{time.Date(2026, 10, 21, 1, 40, 0, 0, time.UTC), map[string]UsageSample{
			"r1": {Requests: 15, RequestBytes: 150, ResponseBytes: 300, ErrorResponses: 2},
			"r2": {Requests: 8, RequestBytes: 80, ResponseBytes: 140, ErrorResponses: 1},
		}},
		{time.Date(2026, 10, 21, 2, 5, 0, 0, time.UTC), map[string]UsageSample{"r1": {Requests: 20, RequestBytes: 200, ResponseBytes: 400, ErrorResponses: 2}}},
	}
	for _, scrape := range scrapes {
		if err := db.RecordUsage(ctx, scrape.samples, scrape.at, "start-1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.SQL().ExecContext(ctx, `DELETE FROM routes`); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUsage(ctx, nil, time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC), "start-1"); err != nil {
		t.Fatal(err)
	}
	var routeRows int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_hourly`).Scan(&routeRows); err != nil || routeRows != 0 {
		t.Fatalf("per-route rows after route deletion = %d, %v", routeRows, err)
	}
	series, err := db.OutletUsageSeries(ctx, "hour", mustTime(t, "2026-10-21T00:00:00Z"), mustTime(t, "2026-10-21T04:00:00Z"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if series.Unit != "hour" || series.UpdatedAt != "2026-10-21T03:00:00Z" || len(series.Buckets) != 4 {
		t.Fatalf("series = %+v", series)
	}
	want := []UsageBucket{
		{Start: "2026-10-21T00:00:00Z"},
		{Start: "2026-10-21T01:00:00Z", Requests: 23, RequestBytes: 230, ResponseBytes: 440, ErrorResponses: 3},
		{Start: "2026-10-21T02:00:00Z", Requests: 5, RequestBytes: 50, ResponseBytes: 100},
		{Start: "2026-10-21T03:00:00Z"},
	}
	for i := range want {
		if series.Buckets[i] != want[i] {
			t.Fatalf("bucket %d = %+v, want %+v", i, series.Buckets[i], want[i])
		}
	}
}

func TestOutletTotalsPruneAfterTenYears(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	at := time.Date(2026, 10, 21, 1, 10, 0, 0, time.UTC)
	cutoff := at.Add(-10 * 365 * 24 * time.Hour).Truncate(time.Hour)
	insertTotals(t, db, cutoff.Add(-time.Hour).Format(time.RFC3339), 7)
	insertTotals(t, db, cutoff.Format(time.RFC3339), 3)
	if err := db.RecordUsage(ctx, map[string]UsageSample{"r1": {Requests: 1}}, at, "g"); err != nil {
		t.Fatal(err)
	}
	var hours []string
	rows, err := db.SQL().QueryContext(ctx, `SELECT hour FROM usage_total_hourly ORDER BY hour`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hour string
		if err := rows.Scan(&hour); err != nil {
			t.Fatal(err)
		}
		hours = append(hours, hour)
	}
	if len(hours) != 2 || hours[0] != cutoff.Format(time.RFC3339) || hours[1] != "2026-10-21T01:00:00Z" {
		t.Fatalf("hours after pruning = %v", hours)
	}
}

func TestUsageMigrationBackfillsTotalsFromRouteHours(t *testing.T) {
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
		if m.version < 5 {
			migrations = append(migrations, m)
		}
	}
	if err := migrate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO usage_hourly VALUES ('r1', '2026-10-21T01:00:00Z', 10, 100, 1000, 1)`,
		`INSERT INTO usage_hourly VALUES ('r2', '2026-10-21T01:00:00Z', 5, 50, 500, 0)`,
		`INSERT INTO usage_hourly VALUES ('r1', '2026-10-21T02:00:00Z', 2, 20, 200, 2)`,
	} {
		if _, err := raw.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	migrations = all
	if err := migrate(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := raw.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != migrations[len(migrations)-1].version {
		t.Fatalf("version = %d, %v", version, err)
	}
	rows, err := raw.QueryContext(ctx, `SELECT hour, requests, request_bytes, response_bytes, error_responses FROM usage_total_hourly ORDER BY hour`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []UsageBucket
	for rows.Next() {
		var item UsageBucket
		if err := rows.Scan(&item.Start, &item.Requests, &item.RequestBytes, &item.ResponseBytes, &item.ErrorResponses); err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}
	want := []UsageBucket{
		{Start: "2026-10-21T01:00:00Z", Requests: 15, RequestBytes: 150, ResponseBytes: 1500, ErrorResponses: 1},
		{Start: "2026-10-21T02:00:00Z", Requests: 2, RequestBytes: 20, ResponseBytes: 200, ErrorResponses: 2},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("backfilled totals = %+v", got)
	}
}

func TestOutletUsageDayBucketsAcrossDaylightSavingChange(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	newYork := mustLocation(t, "America/New_York")
	for hour, requests := range map[string]int64{
		"2026-10-31T03:00:00Z": 1,  // Oct 30 23:00 EDT
		"2026-10-31T04:00:00Z": 2,  // Oct 31 00:00 EDT
		"2026-11-01T03:00:00Z": 4,  // Oct 31 23:00 EDT
		"2026-11-01T04:00:00Z": 8,  // Nov 1 00:00 EDT
		"2026-11-01T06:00:00Z": 16, // Nov 1 01:00 EST, after the clock change
		"2026-11-02T04:00:00Z": 32, // Nov 1 23:00 EST
		"2026-11-02T05:00:00Z": 64, // Nov 2 00:00 EST
		"2026-11-03T04:00:00Z": 128,
		"2026-11-03T05:00:00Z": 256, // Nov 3 00:00 EST, outside the range
	} {
		insertTotals(t, db, hour, requests)
	}
	from := time.Date(2026, 10, 31, 0, 0, 0, 0, newYork)
	to := time.Date(2026, 11, 3, 0, 0, 0, 0, newYork)
	series, err := db.OutletUsageSeries(ctx, "day", from, to, newYork)
	if err != nil {
		t.Fatal(err)
	}
	starts := []string{"2026-10-31T00:00:00-04:00", "2026-11-01T00:00:00-04:00", "2026-11-02T00:00:00-05:00"}
	if len(series.Buckets) != len(starts) {
		t.Fatalf("buckets = %+v", series.Buckets)
	}
	for i, start := range starts {
		if series.Buckets[i].Start != start {
			t.Fatalf("bucket %d start = %q, want %q", i, series.Buckets[i].Start, start)
		}
	}
	if got, want := requestsOf(series), []int64{2 + 4, 8 + 16 + 32, 64 + 128}; !equalInts(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	last := series.Buckets[2]
	if last.RequestBytes != 1920 || last.ResponseBytes != 19200 || last.ErrorResponses != 0 {
		t.Fatalf("last bucket = %+v", last)
	}
	// The same range in spring has a 23-hour day.
	spring, err := UsageBoundaries("day", time.Date(2026, 3, 7, 0, 0, 0, 0, newYork), time.Date(2026, 3, 10, 0, 0, 0, 0, newYork), newYork)
	if err != nil {
		t.Fatal(err)
	}
	if spring[2].Sub(spring[1]) != 23*time.Hour || spring[1].Sub(spring[0]) != 24*time.Hour || spring[1].Format(time.RFC3339) != "2026-03-08T00:00:00-05:00" || spring[2].Format(time.RFC3339) != "2026-03-09T00:00:00-04:00" {
		t.Fatalf("spring boundaries = %v", spring)
	}
}

func TestOutletUsageHourBucketsKeepRepeatedHour(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	newYork := mustLocation(t, "America/New_York")
	insertTotals(t, db, "2026-11-01T05:00:00Z", 1) // 01:00 EDT
	insertTotals(t, db, "2026-11-01T06:00:00Z", 2) // 01:00 EST
	series, err := db.OutletUsageSeries(ctx, "hour", mustTime(t, "2026-11-01T04:00:00Z"), mustTime(t, "2026-11-01T08:00:00Z"), newYork)
	if err != nil {
		t.Fatal(err)
	}
	starts := []string{"2026-11-01T00:00:00-04:00", "2026-11-01T01:00:00-04:00", "2026-11-01T01:00:00-05:00", "2026-11-01T02:00:00-05:00"}
	for i, start := range starts {
		if series.Buckets[i].Start != start {
			t.Fatalf("bucket %d start = %q, want %q", i, series.Buckets[i].Start, start)
		}
	}
	if got, want := requestsOf(series), []int64{0, 1, 2, 0}; !equalInts(got, want) {
		t.Fatalf("requests = %v, want %v", got, want)
	}
}

func TestOutletUsageMonthAndYearBuckets(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	seoul := mustLocation(t, "Asia/Seoul")
	for hour, requests := range map[string]int64{
		"2025-12-31T14:00:00Z": 1,   // 2025-12-31 23:00 KST
		"2025-12-31T15:00:00Z": 2,   // 2026-01-01 00:00 KST
		"2026-01-31T14:00:00Z": 4,   // 2026-01-31 23:00 KST
		"2026-01-31T15:00:00Z": 8,   // 2026-02-01 00:00 KST
		"2026-12-31T14:00:00Z": 16,  // 2026-12-31 23:00 KST
		"2026-12-31T15:00:00Z": 32,  // 2027-01-01 00:00 KST
		"2028-12-31T14:00:00Z": 64,  // 2028-12-31 23:00 KST
		"2028-12-31T15:00:00Z": 128, // 2029-01-01 00:00 KST, outside the range
	} {
		insertTotals(t, db, hour, requests)
	}
	months, err := db.OutletUsageSeries(ctx, "month", time.Date(2026, 1, 1, 0, 0, 0, 0, seoul), time.Date(2026, 4, 1, 0, 0, 0, 0, seoul), seoul)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := requestsOf(months), []int64{2 + 4, 8, 0}; !equalInts(got, want) {
		t.Fatalf("month requests = %v, want %v", got, want)
	}
	if months.Buckets[1].Start != "2026-02-01T00:00:00+09:00" || months.Buckets[2].Start != "2026-03-01T00:00:00+09:00" {
		t.Fatalf("month starts = %+v", months.Buckets)
	}
	years, err := db.OutletUsageSeries(ctx, "year", time.Date(2026, 1, 1, 0, 0, 0, 0, seoul), time.Date(2029, 1, 1, 0, 0, 0, 0, seoul), seoul)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := requestsOf(years), []int64{2 + 4 + 8 + 16, 32, 64}; !equalInts(got, want) {
		t.Fatalf("year requests = %v, want %v", got, want)
	}
	if years.Buckets[2].Start != "2028-01-01T00:00:00+09:00" {
		t.Fatalf("year starts = %+v", years.Buckets)
	}
	// A month range crossing a year boundary in UTC.
	utc, err := db.OutletUsageSeries(ctx, "month", mustTime(t, "2025-11-01T00:00:00Z"), mustTime(t, "2026-02-01T00:00:00Z"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if utc.Buckets[0].Start != "2025-11-01T00:00:00Z" || utc.Buckets[2].Start != "2026-01-01T00:00:00Z" || len(utc.Buckets) != 3 {
		t.Fatalf("utc months = %+v", utc.Buckets)
	}
	if got, want := requestsOf(utc), []int64{0, 1 + 2, 4 + 8}; !equalInts(got, want) {
		t.Fatalf("utc month requests = %v, want %v", got, want)
	}
}

func TestOutletUsageSeriesIsZeroFilledWithoutUpdatedAt(t *testing.T) {
	db := openUsageDB(t)
	series, err := db.OutletUsageSeries(context.Background(), "day", mustTime(t, "2026-10-01T00:00:00Z"), mustTime(t, "2026-10-04T00:00:00Z"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if series.UpdatedAt != "" || len(series.Buckets) != 3 {
		t.Fatalf("series = %+v", series)
	}
	for _, bucket := range series.Buckets {
		if bucket.Requests != 0 || bucket.RequestBytes != 0 || bucket.ResponseBytes != 0 || bucket.ErrorResponses != 0 {
			t.Fatalf("bucket = %+v", bucket)
		}
	}
}

func TestOutletUsageRangeValidation(t *testing.T) {
	ctx := context.Background()
	db := openUsageDB(t)
	from := mustTime(t, "2026-01-01T00:00:00Z")
	for name, tc := range map[string]struct {
		unit     string
		from, to time.Time
		valid    bool
		buckets  int
	}{
		"unknown unit":        {"week", from, from.AddDate(0, 0, 14), false, 0},
		"empty unit":          {"", from, from.AddDate(0, 0, 1), false, 0},
		"equal bounds":        {"day", from, from, false, 0},
		"reversed bounds":     {"day", from.AddDate(0, 0, 1), from, false, 0},
		"exactly 1000 hours":  {"hour", from, from.Add(1000 * time.Hour), true, 1000},
		"1001 hours":          {"hour", from, from.Add(1001 * time.Hour), false, 0},
		"1000 days":           {"day", from, from.AddDate(0, 0, 1000), true, 1000},
		"1001 days":           {"day", from, from.AddDate(0, 0, 1001), false, 0},
		"unaligned end":       {"day", from, from.Add(36 * time.Hour), true, 2},
		"unaligned start":     {"day", from.Add(5 * time.Hour), from.AddDate(0, 0, 2), true, 2},
		"year past year 9999": {"year", mustTime(t, "9999-01-01T00:00:00Z"), mustTime(t, "9999-06-01T00:00:00Z"), false, 0},
	} {
		series, err := db.OutletUsageSeries(ctx, tc.unit, tc.from, tc.to, time.UTC)
		if tc.valid {
			if err != nil || len(series.Buckets) != tc.buckets {
				t.Errorf("%s: buckets = %d, err = %v, want %d", name, len(series.Buckets), err, tc.buckets)
			}
			continue
		}
		if !errors.Is(err, ErrInvalidUsageRange) {
			t.Errorf("%s: err = %v, want ErrInvalidUsageRange", name, err)
		}
	}
}
