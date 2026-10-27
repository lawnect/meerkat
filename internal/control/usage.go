package control

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/bits"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// Per-route hours stay available for tunnel details; Outlet-wide totals
	// are small and are kept long enough for the yearly overview graph.
	routeUsageRetention = 30 * 24 * time.Hour
	totalUsageRetention = 10 * 365 * 24 * time.Hour
	// minuteUsageRetention keeps a week of Outlet-wide minutes.
	minuteUsageRetention = 7 * 24 * time.Hour
	// MaxUsageBuckets bounds one Outlet usage series response.
	MaxUsageBuckets = 1000
)

// ErrInvalidUsageRange reports an unsupported unit or an empty, reversed or
// oversized bucket range.
var ErrInvalidUsageRange = errors.New("invalid usage range")

// UsageSample contains cumulative counters reported by Caddy for one route.
type UsageSample struct {
	Requests       int64
	RequestBytes   int64
	ResponseBytes  int64
	ErrorResponses int64
}

type UsageHour struct {
	Hour           string `json:"hour"`
	Requests       int64  `json:"requests"`
	RequestBytes   int64  `json:"request_bytes"`
	ResponseBytes  int64  `json:"response_bytes"`
	ErrorResponses int64  `json:"error_responses"`
}

type ServiceUsage struct {
	ServiceID      string `json:"service_id"`
	Name           string `json:"name"`
	Requests       int64  `json:"requests"`
	RequestBytes   int64  `json:"request_bytes"`
	ResponseBytes  int64  `json:"response_bytes"`
	ErrorResponses int64  `json:"error_responses"`
}

// UsageBucket sums Outlet-wide traffic for one calendar bucket.
type UsageBucket struct {
	Start          string `json:"start"`
	Requests       int64  `json:"requests"`
	RequestBytes   int64  `json:"request_bytes"`
	ResponseBytes  int64  `json:"response_bytes"`
	ErrorResponses int64  `json:"error_responses"`
	// RateLimited counts requests the Outlet rejected with 429.
	RateLimited int64 `json:"rate_limited"`
}

// ConnectorTotal is one tunnel's traffic over the whole range of a series.
type ConnectorTotal struct {
	ConnectorID    string `json:"connector_id"`
	Name           string `json:"name"`
	Requests       int64  `json:"requests"`
	RequestBytes   int64  `json:"request_bytes"`
	ResponseBytes  int64  `json:"response_bytes"`
	ErrorResponses int64  `json:"error_responses"`
}

type UsageSeries struct {
	Unit      string        `json:"unit"`
	UpdatedAt string        `json:"updated_at,omitempty"`
	Buckets   []UsageBucket `json:"buckets"`
	// Connectors splits the range by tunnel, busiest first. Per-route hours
	// are kept for routeUsageRetention only, so older ranges are partial.
	Connectors []ConnectorTotal `json:"connectors"`
}

type ConnectorUsage struct {
	PeriodHours int            `json:"period_hours"`
	UpdatedAt   string         `json:"updated_at,omitempty"`
	Hours       []UsageHour    `json:"hours"`
	Services    []ServiceUsage `json:"services"`
}

// RecordUsage stores deltas so a Caddy restart or counter reset does not
// subtract past traffic. One scrape is committed atomically.
func (d *Database) RecordUsage(ctx context.Context, samples map[string]UsageSample, at time.Time, generation string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if generation != "" {
		var previous string
		err := tx.QueryRowContext(ctx, `SELECT value FROM outlet_settings WHERE key = 'usage_generation'`).Scan(&previous)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil && previous != generation {
			if _, err := tx.ExecContext(ctx, `DELETE FROM usage_counters`); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES ('usage_generation', ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, generation, at.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	hour := at.UTC().Truncate(time.Hour).Format(time.RFC3339)
	var total UsageSample
	for routeID, sample := range samples {
		// Caddy's counters are per host and cumulative, so the baseline a
		// delta is taken from belongs to the host: a shared address can
		// change the route it is recorded on without counting its history
		// again. A baseline stored per route before is still honoured.
		counter := routeID
		var hostname string
		if err := tx.QueryRowContext(ctx, `SELECT hostname FROM routes WHERE id = ? AND kind = 'http'`, routeID).Scan(&hostname); err == nil {
			counter = "host:" + strings.ToLower(hostname)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		values := []struct {
			key   string
			value int64
		}{
			{"requests", sample.Requests}, {"request_bytes", sample.RequestBytes},
			{"response_bytes", sample.ResponseBytes}, {"error_responses", sample.ErrorResponses},
		}
		delta := UsageSample{}
		for _, item := range values {
			var previous int64
			err := tx.QueryRowContext(ctx, `SELECT value FROM usage_counters WHERE route_id = ? AND metric = ?`, counter, item.key).Scan(&previous)
			if err == sql.ErrNoRows && counter != routeID {
				err = tx.QueryRowContext(ctx, `SELECT value FROM usage_counters WHERE route_id = ? AND metric = ?`, routeID, item.key).Scan(&previous)
			}
			if err != nil && err != sql.ErrNoRows {
				return err
			}
			change := item.value - previous
			if err == sql.ErrNoRows || change < 0 {
				change = item.value
			}
			switch item.key {
			case "requests":
				delta.Requests = change
			case "request_bytes":
				delta.RequestBytes = change
			case "response_bytes":
				delta.ResponseBytes = change
			case "error_responses":
				delta.ErrorResponses = change
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO usage_counters(route_id, metric, value) VALUES (?, ?, ?) ON CONFLICT(route_id, metric) DO UPDATE SET value = excluded.value`, counter, item.key, item.value); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_hourly(route_id, hour, requests, request_bytes, response_bytes, error_responses)
			VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(route_id, hour) DO UPDATE SET
			requests = requests + excluded.requests, request_bytes = request_bytes + excluded.request_bytes,
			response_bytes = response_bytes + excluded.response_bytes, error_responses = error_responses + excluded.error_responses`,
			routeID, hour, delta.Requests, delta.RequestBytes, delta.ResponseBytes, delta.ErrorResponses); err != nil {
			return err
		}
		total.Requests += delta.Requests
		total.RequestBytes += delta.RequestBytes
		total.ResponseBytes += delta.ResponseBytes
		total.ErrorResponses += delta.ErrorResponses
	}
	// Outlet-wide totals are independent of routes so deleting a route never
	// rewrites history. They are kept by minute too, for a while.
	for _, totals := range []struct{ table, column, at string }{
		{"usage_total_hourly", "hour", hour},
		{"usage_total_minutely", "minute", at.UTC().Truncate(time.Minute).Format(time.RFC3339)},
	} {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %[1]s(%[2]s, requests, request_bytes, response_bytes, error_responses)
			VALUES (?, ?, ?, ?, ?) ON CONFLICT(%[2]s) DO UPDATE SET
			requests = requests + excluded.requests, request_bytes = request_bytes + excluded.request_bytes,
			response_bytes = response_bytes + excluded.response_bytes, error_responses = error_responses + excluded.error_responses`, totals.table, totals.column),
			totals.at, total.Requests, total.RequestBytes, total.ResponseBytes, total.ErrorResponses); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES ('usage_updated_at', ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, at.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_hourly WHERE hour < ?`, at.UTC().Add(-routeUsageRetention).Truncate(time.Hour).Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_total_hourly WHERE hour < ?`, at.UTC().Add(-totalUsageRetention).Truncate(time.Hour).Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_total_minutely WHERE minute < ?`, at.UTC().Add(-minuteUsageRetention).Truncate(time.Minute).Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_hourly WHERE route_id NOT IN (SELECT id FROM routes)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_counters WHERE route_id NOT IN (SELECT id FROM routes)
		AND route_id NOT IN (SELECT 'host:' || lower(hostname) FROM routes WHERE kind = 'http')`); err != nil {
		return err
	}
	return tx.Commit()
}

// AddRateLimited adds rejected requests to the hour and the minute they
// happened in.
func (d *Database) AddRateLimited(ctx context.Context, minute time.Time, rejected int64) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_total_hourly(hour, rate_limited) VALUES (?, ?)
		ON CONFLICT(hour) DO UPDATE SET rate_limited = rate_limited + excluded.rate_limited`, minute.UTC().Truncate(time.Hour).Format(time.RFC3339), rejected); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_total_minutely(minute, rate_limited) VALUES (?, ?)
		ON CONFLICT(minute) DO UPDATE SET rate_limited = rate_limited + excluded.rate_limited`, minute.UTC().Truncate(time.Minute).Format(time.RFC3339), rejected); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) ConnectorUsage(ctx context.Context, connectorID string, now time.Time) (ConnectorUsage, error) {
	return d.ConnectorUsageFiltered(ctx, connectorID, now, 24, "")
}

// ConnectorUsageFiltered returns hourly buckets for a supported period. Service
// totals remain available for the selector even when the series is filtered.
func (d *Database) ConnectorUsageFiltered(ctx context.Context, connectorID string, now time.Time, hours int, serviceID string) (ConnectorUsage, error) {
	result := ConnectorUsage{PeriodHours: hours, Hours: []UsageHour{}, Services: []ServiceUsage{}}
	start := now.UTC().Truncate(time.Hour).Add(-time.Duration(hours-1) * time.Hour)
	for i := 0; i < hours; i++ {
		result.Hours = append(result.Hours, UsageHour{Hour: start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)})
	}
	if err := d.db.QueryRowContext(ctx, `SELECT value FROM outlet_settings WHERE key = 'usage_updated_at'`).Scan(&result.UpdatedAt); err != nil && err != sql.ErrNoRows {
		return result, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT u.hour, COALESCE(SUM(u.requests), 0), COALESCE(SUM(u.request_bytes), 0), COALESCE(SUM(u.response_bytes), 0), COALESCE(SUM(u.error_responses), 0)
		FROM usage_hourly u JOIN routes r ON r.id = u.route_id JOIN services s ON s.id = r.service_id
		WHERE s.connector_id = ? AND u.hour >= ? AND (? = '' OR s.id = ?) GROUP BY u.hour`, connectorID, start.Format(time.RFC3339), serviceID, serviceID)
	if err != nil {
		return result, err
	}
	byHour := map[string]UsageHour{}
	for rows.Next() {
		var item UsageHour
		if err := rows.Scan(&item.Hour, &item.Requests, &item.RequestBytes, &item.ResponseBytes, &item.ErrorResponses); err != nil {
			rows.Close()
			return result, err
		}
		byHour[item.Hour] = item
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	for i := range result.Hours {
		if item, ok := byHour[result.Hours[i].Hour]; ok {
			result.Hours[i] = item
		}
	}
	rows, err = d.db.QueryContext(ctx, `SELECT s.id, s.name, COALESCE(SUM(u.requests), 0), COALESCE(SUM(u.request_bytes), 0), COALESCE(SUM(u.response_bytes), 0), COALESCE(SUM(u.error_responses), 0)
		FROM services s LEFT JOIN routes r ON r.service_id = s.id AND r.kind = 'http'
		LEFT JOIN usage_hourly u ON u.route_id = r.id AND u.hour >= ?
		WHERE s.connector_id = ? GROUP BY s.id ORDER BY s.name`, start.Format(time.RFC3339), connectorID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ServiceUsage
		if err := rows.Scan(&item.ServiceID, &item.Name, &item.Requests, &item.RequestBytes, &item.ResponseBytes, &item.ErrorResponses); err != nil {
			return result, err
		}
		result.Services = append(result.Services, item)
	}
	return result, rows.Err()
}

// UsageBoundaries returns the start of every bucket plus the end of the last
// one, so bucket i covers [boundaries[i], boundaries[i+1]). The range [from, to)
// is aligned down to unit boundaries in loc. Each boundary is derived from
// calendar fields, so a daylight-saving change never shifts later buckets.
func UsageBoundaries(unit string, from, to time.Time, loc *time.Location) ([]time.Time, error) {
	if loc == nil || !from.Before(to) {
		return nil, ErrInvalidUsageRange
	}
	local := from.In(loc)
	var boundary func(i int) time.Time
	switch unit {
	case "minute":
		first := local.Truncate(time.Minute)
		boundary = func(i int) time.Time { return first.Add(time.Duration(i) * time.Minute) }
	case "hour":
		// Whole elapsed hours keep the overlapping hour of a clock change distinct.
		first := local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
		boundary = func(i int) time.Time { return first.Add(time.Duration(i) * time.Hour) }
	case "day":
		boundary = func(i int) time.Time { return time.Date(local.Year(), local.Month(), local.Day()+i, 0, 0, 0, 0, loc) }
	case "month":
		boundary = func(i int) time.Time { return time.Date(local.Year(), local.Month()+time.Month(i), 1, 0, 0, 0, 0, loc) }
	case "year":
		boundary = func(i int) time.Time { return time.Date(local.Year()+i, time.January, 1, 0, 0, 0, 0, loc) }
	default:
		return nil, ErrInvalidUsageRange
	}
	boundaries := []time.Time{}
	for i := 0; ; i++ {
		start := boundary(i)
		if len(boundaries) > 0 && !start.After(boundaries[len(boundaries)-1]) {
			return nil, ErrInvalidUsageRange
		}
		boundaries = append(boundaries, start)
		if !start.Before(to) {
			break
		}
		if len(boundaries) > MaxUsageBuckets {
			return nil, ErrInvalidUsageRange
		}
	}
	// Stored hours are four-digit-year RFC3339 text, which sorts chronologically.
	if boundaries[len(boundaries)-1].UTC().Year() > 9999 || boundaries[0].UTC().Year() < 0 {
		return nil, ErrInvalidUsageRange
	}
	return boundaries, nil
}

// OutletUsageSeries sums the Outlet-wide hourly totals into calendar buckets.
// Every bucket in the range is returned, with zeros when nothing was recorded.
func (d *Database) OutletUsageSeries(ctx context.Context, unit string, from, to time.Time, loc *time.Location) (UsageSeries, error) {
	result := UsageSeries{Unit: unit, Buckets: []UsageBucket{}, Connectors: []ConnectorTotal{}}
	boundaries, err := UsageBoundaries(unit, from, to, loc)
	if err != nil {
		return result, err
	}
	buckets := len(boundaries) - 1
	for _, start := range boundaries[:buckets] {
		result.Buckets = append(result.Buckets, UsageBucket{Start: start.Format(time.RFC3339)})
	}
	if err := d.db.QueryRowContext(ctx, `SELECT value FROM outlet_settings WHERE key = 'usage_updated_at'`).Scan(&result.UpdatedAt); err != nil && err != sql.ErrNoRows {
		return result, err
	}
	table, column := "usage_total_hourly", "hour"
	if unit == "minute" {
		table, column = "usage_total_minutely", "minute"
	}
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(`SELECT %[2]s, requests, request_bytes, response_bytes, error_responses, rate_limited
		FROM %[1]s WHERE %[2]s >= ? AND %[2]s < ? ORDER BY %[2]s`, table, column),
		boundaries[0].UTC().Format(time.RFC3339), boundaries[buckets].UTC().Format(time.RFC3339))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var hour string
		var row UsageBucket
		if err := rows.Scan(&hour, &row.Requests, &row.RequestBytes, &row.ResponseBytes, &row.ErrorResponses, &row.RateLimited); err != nil {
			return result, err
		}
		at, err := time.Parse(time.RFC3339, hour)
		if err != nil {
			return result, fmt.Errorf("%s %s %q: %w", table, column, hour, err)
		}
		// The first boundary after the hour ends its bucket.
		index := sort.Search(buckets, func(i int) bool { return boundaries[i].After(at) })
		if index == 0 {
			continue
		}
		bucket := &result.Buckets[index-1]
		bucket.Requests += row.Requests
		bucket.RequestBytes += row.RequestBytes
		bucket.ResponseBytes += row.ResponseBytes
		bucket.ErrorResponses += row.ErrorResponses
		bucket.RateLimited += row.RateLimited
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	if unit != "minute" {
		result.Connectors, err = d.connectorTotals(ctx, boundaries[0], boundaries[buckets])
		return result, err
	}
	// Tunnels are counted by hour. A range of minutes splits its own totals
	// among them as the hours it touches do, so the shares add up to the
	// chart.
	result.Connectors, err = d.connectorTotals(ctx, boundaries[0].UTC().Truncate(time.Hour), boundaries[buckets].UTC().Add(time.Hour-time.Nanosecond).Truncate(time.Hour))
	if err != nil {
		return result, err
	}
	var window UsageBucket
	for _, bucket := range result.Buckets {
		window.Requests, window.RequestBytes, window.ResponseBytes, window.ErrorResponses = window.Requests+bucket.Requests, window.RequestBytes+bucket.RequestBytes, window.ResponseBytes+bucket.ResponseBytes, window.ErrorResponses+bucket.ErrorResponses
	}
	// Each metric is split on its own, so the tunnels add up to the range's
	// total whatever the rounding: the shares of a single request over three
	// equal tunnels would each round to nothing.
	for _, metric := range []struct {
		total int64
		field func(*ConnectorTotal) *int64
	}{
		{window.Requests, func(c *ConnectorTotal) *int64 { return &c.Requests }},
		{window.RequestBytes, func(c *ConnectorTotal) *int64 { return &c.RequestBytes }},
		{window.ResponseBytes, func(c *ConnectorTotal) *int64 { return &c.ResponseBytes }},
		{window.ErrorResponses, func(c *ConnectorTotal) *int64 { return &c.ErrorResponses }},
	} {
		weights := make([]int64, len(result.Connectors))
		for i := range result.Connectors {
			weights[i] = *metric.field(&result.Connectors[i])
		}
		for i, part := range apportion(metric.total, weights) {
			*metric.field(&result.Connectors[i]) = part
		}
	}
	kept := result.Connectors[:0]
	for _, c := range result.Connectors {
		if c.Requests > 0 {
			kept = append(kept, c)
		}
	}
	result.Connectors = kept
	return result, nil
}

// apportion splits total over the weights in proportion, by the
// largest-remainder method: each part is its exact share rounded down, and
// the units left over go one each to the largest remainders (the earlier
// weight on a tie). The parts add up to total whenever any weight is above
// zero; with none, nothing can be split and every part is 0.
func apportion(total int64, weights []int64) []int64 {
	parts := make([]int64, len(weights))
	// Counters only grow, but a negative weight would wrap below: it
	// counts as none.
	weight := func(i int) int64 { return max(weights[i], 0) }
	var whole int64
	for i := range weights {
		whole += weight(i)
	}
	if whole <= 0 || total <= 0 {
		return parts
	}
	type remainder struct {
		index int
		rest  uint64
	}
	remainders := make([]remainder, len(weights))
	left := total
	for i := range weights {
		// The product can pass 64 bits for byte counts; its quotient by whole
		// cannot, as a weight is at most whole.
		high, low := bits.Mul64(uint64(weight(i)), uint64(total))
		quotient, rest := bits.Div64(high, low, uint64(whole))
		parts[i], remainders[i] = int64(quotient), remainder{i, rest}
		left -= int64(quotient)
	}
	slices.SortStableFunc(remainders, func(a, b remainder) int { return cmp.Compare(b.rest, a.rest) })
	for _, r := range remainders[:min(left, int64(len(remainders)))] {
		parts[r.index]++
	}
	return parts
}

// connectorTotals sums the per-route hours of [from, to) by tunnel. Hours of
// deleted routes are gone, so they do not appear.
func (d *Database) connectorTotals(ctx context.Context, from, to time.Time) ([]ConnectorTotal, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT c.id, c.name, SUM(u.requests), SUM(u.request_bytes), SUM(u.response_bytes), SUM(u.error_responses)
		FROM usage_hourly u JOIN routes r ON r.id = u.route_id JOIN services s ON s.id = r.service_id JOIN connectors c ON c.id = s.connector_id
		WHERE u.hour >= ? AND u.hour < ? GROUP BY c.id HAVING SUM(u.requests) > 0 ORDER BY SUM(u.requests) DESC, c.name`,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totals := []ConnectorTotal{}
	for rows.Next() {
		var total ConnectorTotal
		if err := rows.Scan(&total.ConnectorID, &total.Name, &total.Requests, &total.RequestBytes, &total.ResponseBytes, &total.ErrorResponses); err != nil {
			return nil, err
		}
		totals = append(totals, total)
	}
	return totals, rows.Err()
}
