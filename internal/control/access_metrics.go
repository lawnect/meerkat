package control

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// accessMetricsRetention is how long the hours of what access policies
// decided are kept: a year and a little, for monthly views.
const accessMetricsRetention = 400 * 24 * time.Hour

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

// AccessBucket is what every policy decided in one calendar bucket.
type AccessBucket struct {
	Start string `json:"start"`
	AccessCounts
}

// AccessSeries is what access policies decided over a range, in buckets of
// a unit (see UsageBoundaries), and each policy's total over it, by ID.
type AccessSeries struct {
	Unit     string                  `json:"unit"`
	Buckets  []AccessBucket          `json:"buckets"`
	Policies map[string]AccessCounts `json:"policies"`
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

// AccessSeries sums what access policies decided into calendar buckets of
// unit over [from, to) in loc; every bucket is returned, zero when nothing
// was counted.
func (d *Database) AccessSeries(ctx context.Context, unit string, from, to time.Time, loc *time.Location) (AccessSeries, error) {
	result := AccessSeries{Unit: unit, Buckets: []AccessBucket{}, Policies: map[string]AccessCounts{}}
	// Decisions are kept by hour.
	if unit == "minute" {
		return result, ErrInvalidUsageRange
	}
	boundaries, err := UsageBoundaries(unit, from, to, loc)
	if err != nil {
		return result, err
	}
	buckets := len(boundaries) - 1
	for _, start := range boundaries[:buckets] {
		result.Buckets = append(result.Buckets, AccessBucket{Start: start.Format(time.RFC3339)})
	}
	rows, err := d.db.QueryContext(ctx, `SELECT policy_id, hour, passed, challenged, denied FROM access_hourly WHERE hour >= ? AND hour < ?`,
		boundaries[0].UTC().Format(time.RFC3339), boundaries[buckets].UTC().Format(time.RFC3339))
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
		at, err := time.Parse(time.RFC3339, hour)
		if err != nil {
			return result, fmt.Errorf("access_hourly hour %q: %w", hour, err)
		}
		// The first boundary after the hour ends its bucket.
		if index := sort.Search(buckets, func(i int) bool { return boundaries[i].After(at) }); index > 0 {
			result.Buckets[index-1].add(n)
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
