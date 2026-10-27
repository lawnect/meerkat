package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// latencyRetention is how long the hours of request latencies are kept,
// as access decisions are.
const latencyRetention = accessMetricsRetention

// The two latencies the Outlet times for a request it forwards: its own,
// until the request is on a connection to the service (LatencyOutlet), and
// the services', from the request fully sent to the final answer's headers,
// tunnel round trip and failed attempts before a retry included
// (LatencyUpstream).
const (
	LatencyOutlet   = "outlet"
	LatencyUpstream = "upstream"
)

// LatencyBounds are the upper bounds, in microseconds, of the buckets a
// latency is counted in; the last bucket, past 30 seconds, has none.
var LatencyBounds = [...]int64{100, 250, 500, 1_000, 2_500, 5_000, 10_000, 25_000, 50_000, 100_000, 250_000, 500_000, 1_000_000, 2_500_000, 5_000_000, 10_000_000, 30_000_000}

// LatencyBuckets is how many buckets a histogram has.
const LatencyBuckets = len(LatencyBounds) + 1

// LatencyHistogram counts latencies: how many, their sum in microseconds,
// and how many fell in each bucket of LatencyBounds.
type LatencyHistogram struct {
	Count   int64
	SumUS   int64
	Buckets [LatencyBuckets]int64
}

// Add counts a latency of us microseconds.
func (h *LatencyHistogram) Add(us int64) {
	h.Count++
	h.SumUS += us
	h.Buckets[LatencyBucket(us)]++
}

// LatencyBucket returns the bucket a latency of us microseconds falls in.
func LatencyBucket(us int64) int {
	return sort.Search(len(LatencyBounds), func(i int) bool { return us <= LatencyBounds[i] })
}

func (h *LatencyHistogram) merge(o LatencyHistogram) {
	h.Count, h.SumUS = h.Count+o.Count, h.SumUS+o.SumUS
	for i := range h.Buckets {
		h.Buckets[i] += o.Buckets[i]
	}
}

// LatencyKey names one kind of latency in one hour.
type LatencyKey struct {
	Kind string
	Hour time.Time
}

// LatencyStats sum up a histogram in milliseconds: the mean, and
// percentiles estimated within their bucket, linearly.
type LatencyStats struct {
	Count int64   `json:"count"`
	AvgMS float64 `json:"avg_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
}

// Stats sums up the histogram.
func (h LatencyHistogram) Stats() LatencyStats {
	if h.Count == 0 {
		return LatencyStats{}
	}
	return LatencyStats{Count: h.Count, AvgMS: float64(h.SumUS) / float64(h.Count) / 1000, P50MS: h.quantile(0.5), P95MS: h.quantile(0.95), P99MS: h.quantile(0.99)}
}

// quantile estimates the latency, in milliseconds, q of the counted ones
// are at or under: within its bucket, as if they were spread evenly. The
// last bucket has no upper bound, so its latencies count as 30 seconds. It
// ranks among the buckets' own total, which is what it walks.
func (h LatencyHistogram) quantile(q float64) float64 {
	var total int64
	for _, n := range h.Buckets {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := q * float64(total)
	var seen int64
	for i, n := range h.Buckets {
		if n == 0 || float64(seen+n) < rank {
			seen += n
			continue
		}
		low, high := int64(0), LatencyBounds[len(LatencyBounds)-1]
		if i > 0 {
			low = LatencyBounds[i-1]
		}
		if i < len(LatencyBounds) {
			high = LatencyBounds[i]
		} else {
			return float64(low) / 1000
		}
		return (float64(low) + float64(high-low)*(rank-float64(seen))/float64(n)) / 1000
	}
	return float64(LatencyBounds[len(LatencyBounds)-1]) / 1000
}

// LatencyBucketStats are both latencies in one calendar bucket.
type LatencyBucketStats struct {
	Start    string       `json:"start"`
	Outlet   LatencyStats `json:"outlet"`
	Upstream LatencyStats `json:"upstream"`
}

// LatencySeries are both latencies over a range, in buckets of a unit (see
// UsageBoundaries), and over the whole range.
type LatencySeries struct {
	Unit     string               `json:"unit"`
	Buckets  []LatencyBucketStats `json:"buckets"`
	Outlet   LatencyStats         `json:"outlet"`
	Upstream LatencyStats         `json:"upstream"`
}

var latencyColumns = func() string {
	columns := make([]string, LatencyBuckets)
	for i := range columns {
		columns[i] = fmt.Sprintf("b%d", i)
	}
	return strings.Join(columns, ", ")
}()

// RecordLatencies adds histograms to the hours they were counted in and
// forgets hours older than latencyRetention.
func (d *Database) RecordLatencies(ctx context.Context, histograms map[LatencyKey]LatencyHistogram, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	sums := make([]string, LatencyBuckets)
	for i := range sums {
		sums[i] = fmt.Sprintf("b%[1]d = b%[1]d + excluded.b%[1]d", i)
	}
	insert := fmt.Sprintf(`INSERT INTO latency_hourly(kind, hour, count, sum_us, %s) VALUES (?, ?, ?, ?%s)
		ON CONFLICT(kind, hour) DO UPDATE SET count = count + excluded.count, sum_us = sum_us + excluded.sum_us, %s`,
		latencyColumns, strings.Repeat(", ?", LatencyBuckets), strings.Join(sums, ", "))
	for key, h := range histograms {
		args := []any{key.Kind, key.Hour.UTC().Truncate(time.Hour).Format(time.RFC3339), h.Count, h.SumUS}
		for _, n := range h.Buckets {
			args = append(args, n)
		}
		if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM latency_hourly WHERE hour < ?`, now.UTC().Add(-latencyRetention).Truncate(time.Hour).Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// LatencySeries sums latencies into calendar buckets of unit over
// [from, to) in loc; every bucket is returned, zero when nothing was
// counted.
func (d *Database) LatencySeries(ctx context.Context, unit string, from, to time.Time, loc *time.Location) (LatencySeries, error) {
	result := LatencySeries{Unit: unit, Buckets: []LatencyBucketStats{}}
	// Latencies are kept by hour.
	if unit == "minute" {
		return result, ErrInvalidUsageRange
	}
	boundaries, err := UsageBoundaries(unit, from, to, loc)
	if err != nil {
		return result, err
	}
	buckets := len(boundaries) - 1
	type pair struct{ outlet, upstream LatencyHistogram }
	sums := make([]pair, buckets)
	var total pair
	rows, err := d.db.QueryContext(ctx, `SELECT kind, hour, count, sum_us, `+latencyColumns+` FROM latency_hourly WHERE hour >= ? AND hour < ?`,
		boundaries[0].UTC().Format(time.RFC3339), boundaries[buckets].UTC().Format(time.RFC3339))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, hour string
		var h LatencyHistogram
		into := []any{&kind, &hour, &h.Count, &h.SumUS}
		for i := range h.Buckets {
			into = append(into, &h.Buckets[i])
		}
		if err := rows.Scan(into...); err != nil {
			return result, err
		}
		at, err := time.Parse(time.RFC3339, hour)
		if err != nil {
			return result, fmt.Errorf("latency_hourly hour %q: %w", hour, err)
		}
		index := sort.Search(buckets, func(i int) bool { return boundaries[i].After(at) })
		if index == 0 {
			continue
		}
		// The first boundary after the hour ends its bucket.
		target, all := &sums[index-1].outlet, &total.outlet
		if kind == LatencyUpstream {
			target, all = &sums[index-1].upstream, &total.upstream
		}
		target.merge(h)
		all.merge(h)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	for i, start := range boundaries[:buckets] {
		result.Buckets = append(result.Buckets, LatencyBucketStats{Start: start.Format(time.RFC3339), Outlet: sums[i].outlet.Stats(), Upstream: sums[i].upstream.Stats()})
	}
	result.Outlet, result.Upstream = total.outlet.Stats(), total.upstream.Stats()
	return result, nil
}
