package control

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	// Per-route hours stay available for tunnel details; Outlet-wide totals
	// are small and are kept long enough for the yearly overview graph.
	routeUsageRetention = 30 * 24 * time.Hour
	totalUsageRetention = 10 * 365 * 24 * time.Hour
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
			err := tx.QueryRowContext(ctx, `SELECT value FROM usage_counters WHERE route_id = ? AND metric = ?`, routeID, item.key).Scan(&previous)
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
			if _, err := tx.ExecContext(ctx, `INSERT INTO usage_counters(route_id, metric, value) VALUES (?, ?, ?) ON CONFLICT(route_id, metric) DO UPDATE SET value = excluded.value`, routeID, item.key, item.value); err != nil {
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
	// rewrites history.
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_total_hourly(hour, requests, request_bytes, response_bytes, error_responses)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(hour) DO UPDATE SET
		requests = requests + excluded.requests, request_bytes = request_bytes + excluded.request_bytes,
		response_bytes = response_bytes + excluded.response_bytes, error_responses = error_responses + excluded.error_responses`,
		hour, total.Requests, total.RequestBytes, total.ResponseBytes, total.ErrorResponses); err != nil {
		return err
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_hourly WHERE route_id NOT IN (SELECT id FROM routes)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_counters WHERE route_id NOT IN (SELECT id FROM routes)`); err != nil {
		return err
	}
	return tx.Commit()
}

// AddRateLimited adds rejected requests to the hour they happened in.
func (d *Database) AddRateLimited(ctx context.Context, hour time.Time, rejected int64) error {
	_, err := d.db.ExecContext(ctx, `INSERT INTO usage_total_hourly(hour, rate_limited) VALUES (?, ?)
		ON CONFLICT(hour) DO UPDATE SET rate_limited = rate_limited + excluded.rate_limited`, hour.UTC().Truncate(time.Hour).Format(time.RFC3339), rejected)
	return err
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
	rows, err := d.db.QueryContext(ctx, `SELECT hour, requests, request_bytes, response_bytes, error_responses, rate_limited
		FROM usage_total_hourly WHERE hour >= ? AND hour < ? ORDER BY hour`,
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
			return result, fmt.Errorf("usage_total_hourly hour %q: %w", hour, err)
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
	result.Connectors, err = d.connectorTotals(ctx, boundaries[0], boundaries[buckets])
	return result, err
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
