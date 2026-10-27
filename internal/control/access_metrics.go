package control

import (
	"context"
	"time"
)

// accessMetricsRetention is how long the hours of what access policies
// decided are kept, as long as per-route usage.
const accessMetricsRetention = 30 * 24 * time.Hour

// AccessCounts are requests an access policy decided: let through
// (bypass, or a session or token it takes), sent to sign in (or asked to,
// when not a browser), and refused (block, a session or token it doesn't
// let in, or the provider off).
type AccessCounts struct {
	Passed     int64 `json:"passed"`
	Challenged int64 `json:"challenged"`
	Denied     int64 `json:"denied"`
}

// AccessCountKey names the counts of one policy in one hour.
type AccessCountKey struct {
	Policy string
	Hour   time.Time
}

// AccessHour is what every policy decided in one hour.
type AccessHour struct {
	Hour string `json:"hour"`
	AccessCounts
}

// AccessMetrics is what access policies decided over a period: every hour
// of it, oldest first, and each policy's total, by ID.
type AccessMetrics struct {
	PeriodHours int                     `json:"period_hours"`
	Hours       []AccessHour            `json:"hours"`
	Policies    map[string]AccessCounts `json:"policies"`
}

// RecordAccessCounts adds counts to the hours they happened in and forgets
// hours older than accessMetricsRetention.
func (d *Database) RecordAccessCounts(ctx context.Context, counts map[AccessCountKey]AccessCounts, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, n := range counts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO access_hourly(policy_id, hour, passed, challenged, denied) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(policy_id, hour) DO UPDATE SET passed = passed + excluded.passed, challenged = challenged + excluded.challenged, denied = denied + excluded.denied`,
			key.Policy, key.Hour.UTC().Truncate(time.Hour).Format(time.RFC3339), n.Passed, n.Challenged, n.Denied); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_hourly WHERE hour < ?`, now.UTC().Add(-accessMetricsRetention).Truncate(time.Hour).Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// AccessMetrics returns what access policies decided in the hours hours up
// to and including the one now is in.
func (d *Database) AccessMetrics(ctx context.Context, now time.Time, hours int) (AccessMetrics, error) {
	result := AccessMetrics{PeriodHours: hours, Hours: make([]AccessHour, hours), Policies: map[string]AccessCounts{}}
	start := now.UTC().Truncate(time.Hour).Add(-time.Duration(hours-1) * time.Hour)
	index := map[string]int{}
	for i := range result.Hours {
		result.Hours[i].Hour = start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		index[result.Hours[i].Hour] = i
	}
	rows, err := d.db.QueryContext(ctx, `SELECT policy_id, hour, passed, challenged, denied FROM access_hourly WHERE hour >= ?`, start.Format(time.RFC3339))
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var policy, hour string
		var n AccessCounts
		if err := rows.Scan(&policy, &hour, &n.Passed, &n.Challenged, &n.Denied); err != nil {
			return result, err
		}
		if i, ok := index[hour]; ok {
			result.Hours[i].add(n)
		}
		total := result.Policies[policy]
		total.add(n)
		result.Policies[policy] = total
	}
	return result, rows.Err()
}

func (c *AccessCounts) add(n AccessCounts) {
	c.Passed, c.Challenged, c.Denied = c.Passed+n.Passed, c.Challenged+n.Challenged, c.Denied+n.Denied
}

// settingAccessLog says whether access protection keeps a log of the
// requests it decides.
const settingAccessLog = "access_log"

// AccessLogEnabled reports whether the access log is on; it is off until
// turned on.
func (d *Database) AccessLogEnabled(ctx context.Context) (bool, error) {
	value, err := getSetting(ctx, d.db, settingAccessLog)
	return value == "on", err
}

// SetAccessLog turns the access log on or off.
func (d *Database) SetAccessLog(ctx context.Context, enabled bool, now time.Time) error {
	value := "off"
	if enabled {
		value = "on"
	}
	return putSetting(ctx, d.db, settingAccessLog, value, now)
}
