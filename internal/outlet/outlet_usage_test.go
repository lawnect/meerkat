package outlet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

func usageQuery(unit, from, to, zone string) string {
	values := url.Values{}
	for key, value := range map[string]string{"unit": unit, "from": from, "to": to, "tz": zone} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return "/api/v1/usage?" + values.Encode()
}

func usageStore(t *testing.T) (fixture, *control.Database, http.Handler) {
	t.Helper()
	f := setup(t)
	db, err := control.Open(context.Background(), filepath.Join(f.dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	return f, db, store.Handler()
}

func TestOutletUsageReturnsZoneAlignedBuckets(t *testing.T) {
	f, db, h := usageStore(t)
	for _, statement := range []string{
		`INSERT INTO connectors(id, name, status, created_at) VALUES ('c1', 'tunnel', 'connected', '2026-10-21T00:00:00Z')`,
		`INSERT INTO services(id, connector_id, name, protocol, target_host, target_port, created_at, updated_at) VALUES ('s1', 'c1', 'app', 'http', 'app', 80, '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
		`INSERT INTO routes(id, service_id, kind, hostname, created_at, updated_at) VALUES ('r1', 's1', 'http', 'app.example.test', '2026-10-21T00:00:00Z', '2026-10-21T00:00:00Z')`,
	} {
		if _, err := db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	// 01:30 and 02:10 on 2026-10-21 in Seoul, then the next Seoul day.
	for _, scrape := range []struct {
		at      time.Time
		samples map[string]control.UsageSample
	}{
		{time.Date(2026, 10, 20, 16, 30, 0, 0, time.UTC), map[string]control.UsageSample{"r1": {Requests: 10, RequestBytes: 100, ResponseBytes: 1000, ErrorResponses: 1}}},
		{time.Date(2026, 10, 20, 17, 10, 0, 0, time.UTC), map[string]control.UsageSample{"r1": {Requests: 14, RequestBytes: 140, ResponseBytes: 1400, ErrorResponses: 3}}},
		{time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), map[string]control.UsageSample{"r1": {Requests: 20, RequestBytes: 200, ResponseBytes: 2000, ErrorResponses: 3}}},
	} {
		if err := db.RecordUsage(context.Background(), scrape.samples, scrape.at, "start-1"); err != nil {
			t.Fatal(err)
		}
	}
	w := request(h, "GET", usageQuery("day", "2026-10-21T00:00:00+09:00", "2026-10-23T00:00:00+09:00", "Asia/Seoul"), f.admin, nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status = %d, type = %q: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var series control.UsageSeries
	if err := json.Unmarshal(w.Body.Bytes(), &series); err != nil {
		t.Fatal(err)
	}
	want := []control.UsageBucket{
		{Start: "2026-10-21T00:00:00+09:00", Requests: 14, RequestBytes: 140, ResponseBytes: 1400, ErrorResponses: 3},
		{Start: "2026-10-22T00:00:00+09:00", Requests: 6, RequestBytes: 60, ResponseBytes: 600},
	}
	if series.Unit != "day" || series.UpdatedAt != "2026-10-21T15:00:00Z" || len(series.Buckets) != len(want) {
		t.Fatalf("series = %+v", series)
	}
	for i := range want {
		if series.Buckets[i] != want[i] {
			t.Fatalf("bucket %d = %+v, want %+v", i, series.Buckets[i], want[i])
		}
	}
	// Without tz the buckets are UTC.
	w = request(h, "GET", usageQuery("hour", "2026-10-20T16:00:00Z", "2026-10-20T18:00:00Z", ""), f.admin, nil)
	if w.Code != 200 {
		t.Fatalf("utc status = %d: %s", w.Code, w.Body.String())
	}
	series = control.UsageSeries{}
	if err := json.Unmarshal(w.Body.Bytes(), &series); err != nil {
		t.Fatal(err)
	}
	if len(series.Buckets) != 2 || series.Buckets[0].Start != "2026-10-20T16:00:00Z" || series.Buckets[0].Requests != 10 || series.Buckets[1].Requests != 4 {
		t.Fatalf("utc series = %+v", series)
	}
	// An empty range still lists every bucket, and an unset updated_at is omitted.
	other, _, empty := usageStore(t)
	w = request(empty, "GET", usageQuery("year", "2024-01-01T00:00:00Z", "2026-01-01T00:00:00Z", ""), other.admin, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "updated_at") || !strings.Contains(w.Body.String(), `"requests":0`) {
		t.Fatalf("empty status = %d: %s", w.Code, w.Body.String())
	}
}

func TestOutletUsageRequiresAdminAndRejectsInvalidRanges(t *testing.T) {
	f, _, h := usageStore(t)
	valid := usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", "UTC")
	for name, token := range map[string]string{"missing": "", "wrong": Token(), "connector enrollment": f.tokenA} {
		w := request(h, "GET", valid, token, nil)
		var body contract.Error
		if w.Code != 401 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "invalid_admin_credential" {
			t.Fatalf("%s token: status = %d: %s", name, w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", valid, f.admin, nil); w.Code != 200 {
		t.Fatalf("admin status = %d: %s", w.Code, w.Body.String())
	}
	// Authentication runs before range validation.
	if w := request(h, "GET", usageQuery("week", "", "", ""), "", nil); w.Code != 401 {
		t.Fatalf("invalid range without token status = %d", w.Code)
	}
	for name, path := range map[string]string{
		"missing unit":   usageQuery("", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", ""),
		"unknown unit":   usageQuery("week", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", ""),
		"bad from":       usageQuery("day", "yesterday", "2026-10-08T00:00:00Z", ""),
		"missing from":   usageQuery("day", "", "2026-10-08T00:00:00Z", ""),
		"bad to":         usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-08", ""),
		"missing to":     usageQuery("day", "2026-10-01T00:00:00Z", "", ""),
		"empty range":    usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-01T00:00:00Z", ""),
		"reversed range": usageQuery("day", "2026-10-08T00:00:00Z", "2026-10-01T00:00:00Z", ""),
		"unknown zone":   usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", "Mars/Olympus"),
		"server zone":    usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", "Local"),
		"too many hours": usageQuery("hour", "2026-10-01T00:00:00Z", "2026-11-12T10:00:00Z", ""),
		"too many days":  usageQuery("day", "2020-01-01T00:00:00Z", "2026-10-01T00:00:00Z", ""),
	} {
		w := request(h, "GET", path, f.admin, nil)
		var body contract.Error
		if w.Code != 400 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "invalid_usage_range" || body.RequestID == "" {
			t.Fatalf("%s: status = %d: %s", name, w.Code, w.Body.String())
		}
	}
	// 1000 buckets is the limit.
	if w := request(h, "GET", usageQuery("hour", "2026-10-01T00:00:00Z", "2026-11-11T16:00:00Z", ""), f.admin, nil); w.Code != 200 {
		t.Fatalf("1000 buckets status = %d: %s", w.Code, w.Body.String())
	}
	// Without a control database there is nothing to query.
	bare := setup(t)
	if w := request(bare.store.Handler(), "GET", valid, bare.admin, nil); w.Code != 503 {
		t.Fatalf("no database status = %d: %s", w.Code, w.Body.String())
	}
}

func TestOutletUsageAcceptsAdminSessionOnly(t *testing.T) {
	f, _, h := usageStore(t)
	path := usageQuery("day", "2026-10-01T00:00:00Z", "2026-10-08T00:00:00Z", "")
	credentials := map[string]any{"email": "admin@example.com", "password": "correct horse battery staple"}
	if w := request(h, "POST", "/api/v1/setup/admin", f.admin, credentials); w.Code != 201 {
		t.Fatalf("setup status = %d: %s", w.Code, w.Body.String())
	}
	w := request(h, "POST", "/api/v1/session", "", credentials)
	if w.Code != 201 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login status = %d: %s", w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	// Once an administrator exists the bootstrap token no longer authorizes the API.
	if w := request(h, "GET", path, f.admin, nil); w.Code != 401 {
		t.Fatalf("bootstrap token status = %d: %s", w.Code, w.Body.String())
	}
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("session status = %d: %s", w.Code, w.Body.String())
	}
}
