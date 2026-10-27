package control

import (
	"context"
	"math"
	"testing"
	"time"
)

// A histogram's mean is exact and its percentiles fall in the right
// bucket, spread evenly within it.
func TestLatencyHistogramStats(t *testing.T) {
	var h LatencyHistogram
	if h.Stats() != (LatencyStats{}) {
		t.Fatal("an empty histogram has stats")
	}
	// 90 requests at 0.2 ms, 9 at 3 ms, one at 40 s.
	for range 90 {
		h.Add(200)
	}
	for range 9 {
		h.Add(3_000)
	}
	h.Add(40_000_000)
	stats := h.Stats()
	if stats.Count != 100 || math.Abs(stats.AvgMS-(90*0.2+9*3+40_000)/100) > 1e-9 {
		t.Fatalf("mean: %+v", stats)
	}
	// The median and 95th are within the buckets of 0.1–0.25 ms and 2.5–5 ms.
	if stats.P50MS <= 0.1 || stats.P50MS > 0.25 || stats.P95MS <= 2.5 || stats.P95MS > 5 || stats.P99MS != 5 {
		t.Fatalf("percentiles: %+v", stats)
	}
	var slow LatencyHistogram
	slow.Add(45_000_000)
	if s := slow.Stats(); s.P99MS != 30_000 {
		t.Fatalf("past the last bound: %+v", s)
	}
	for us, bucket := range map[int64]int{0: 0, 100: 0, 101: 1, 30_000_000: LatencyBuckets - 2, 30_000_001: LatencyBuckets - 1} {
		if got := LatencyBucket(us); got != bucket {
			t.Fatalf("%d µs in bucket %d, want %d", us, got, bucket)
		}
	}
}

// Hours of both latencies add up across saves, fall in calendar buckets
// of the reader's zone, and go after the retention.
func TestLatencySeries(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	seoul, _ := time.LoadLocation("Asia/Seoul")
	day := time.Date(2026, 10, 27, 0, 0, 0, 0, seoul)
	at := func(hour int) time.Time { return day.Add(time.Duration(hour) * time.Hour) }
	histogram := func(us ...int64) LatencyHistogram {
		var h LatencyHistogram
		for _, v := range us {
			h.Add(v)
		}
		return h
	}
	now := at(23)
	if err := db.RecordLatencies(ctx, map[LatencyKey]LatencyHistogram{
		{LatencyOutlet, at(9)}:                   histogram(200, 400),
		{LatencyUpstream, at(9)}:                 histogram(80_000),
		{LatencyOutlet, at(13)}:                  histogram(600),
		{LatencyOutlet, day.AddDate(0, 0, -500)}: histogram(1),
	}, now); err != nil {
		t.Fatal(err)
	}
	// A second save of the same hour adds to it.
	if err := db.RecordLatencies(ctx, map[LatencyKey]LatencyHistogram{{LatencyUpstream, at(9).Add(20 * time.Minute)}: histogram(120_000)}, now); err != nil {
		t.Fatal(err)
	}
	series, err := db.LatencySeries(ctx, "hour", day, day.AddDate(0, 0, 1), seoul)
	if err != nil || len(series.Buckets) != 24 {
		t.Fatalf("%d buckets, %v", len(series.Buckets), err)
	}
	nine := series.Buckets[9]
	if nine.Start != at(9).Format(time.RFC3339) || nine.Outlet.Count != 2 || math.Abs(nine.Outlet.AvgMS-0.3) > 1e-9 || nine.Upstream.Count != 2 || math.Abs(nine.Upstream.AvgMS-100) > 1e-9 {
		t.Fatalf("09:00: %+v", nine)
	}
	if series.Buckets[13].Outlet.Count != 1 || series.Buckets[10].Outlet.Count != 0 || series.Outlet.Count != 3 || series.Upstream.Count != 2 {
		t.Fatalf("totals: %+v %+v", series.Outlet, series.Upstream)
	}
	var kept int
	if err := db.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM latency_hourly").Scan(&kept); err != nil || kept != 3 {
		t.Fatalf("%d hours kept, %v", kept, err)
	}
	if daily, err := db.LatencySeries(ctx, "day", day, day.AddDate(0, 0, 1), seoul); err != nil || len(daily.Buckets) != 1 || daily.Buckets[0].Outlet.Count != 3 {
		t.Fatalf("daily: %+v %v", daily, err)
	}
}

// Percentiles rank among the buckets: a count that ran ahead of them, a
// request flushed between its count and its bucket, doesn't push p99 past
// the last bucket.
func TestLatencyPercentilesRankAmongBuckets(t *testing.T) {
	var h LatencyHistogram
	for range 50 {
		h.Add(200)
	}
	h.Count++
	if s := h.Stats(); s.P99MS > 0.25 {
		t.Fatalf("p99 %v ms", s.P99MS)
	}
	if s := (LatencyHistogram{Count: 1, SumUS: 200}).Stats(); s.P99MS != 0 {
		t.Fatalf("no buckets: %+v", s)
	}
}
