package control

import (
	"context"
	"database/sql"
	"time"
)

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
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES ('usage_updated_at', ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, at.UTC().Format(time.RFC3339), at.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage_hourly WHERE hour < ?`, at.UTC().Add(-30*24*time.Hour).Truncate(time.Hour).Format(time.RFC3339)); err != nil {
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
