package outlet

import (
	"context"
	"math"
	"strconv"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	// latencyBaseline is how far back "usual" reaches, so a week's rhythm
	// of busy and quiet hours is in it.
	latencyBaseline = 7 * 24 * time.Hour
	// latencyHistoryFactor times the rule's MinRequests is the history the
	// baseline needs before an hour is compared with it.
	latencyHistoryFactor = 10
	// A latency that rose by the percentage but by only this much in
	// milliseconds isn't worth a page: the Outlet's own time is a few
	// milliseconds, so a small step is a large percentage, and the services'
	// time is the app's, which varies more.
	latencyOutletFloorMS   = 20
	latencyUpstreamFloorMS = 100
)

// latencyAlertState is the latency rule's, kept between rounds. It keeps
// nothing: the keys of the events it recorded tell which hours it alerted.
type latencyAlertState struct{}

// latencyKinds are the latencies watched, with where each is spent and the
// least rise worth an alert.
var latencyKinds = []struct {
	kind, where string
	floorMS     float64
}{
	{control.LatencyOutlet, "Meerkat (Outlet)", latencyOutletFloorMS},
	{control.LatencyUpstream, "Services", latencyUpstreamFloorMS},
}

// latencyAlerts compares the p95 of the current and previous UTC hour with
// that of the week before it, for each latency. Each hour of each alerts at
// most once.
func (s *Store) latencyAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	if !env.rules.Latency.Enabled {
		return nil
	}
	current := now.UTC().Truncate(time.Hour)
	for _, watched := range latencyKinds {
		for _, hour := range []time.Time{current.Add(-time.Hour), current} {
			if err := s.latencyHourAlert(ctx, env, watched.kind, watched.where, watched.floorMS, hour, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) latencyHourAlert(ctx context.Context, env alertEnv, kind, where string, floorMS float64, hour, now time.Time) error {
	rule := env.rules.Latency
	hourly, err := s.control.LatencySum(ctx, kind, hour, hour.Add(time.Hour))
	if err != nil || hourly.Count < int64(rule.MinRequests) {
		return err
	}
	usual, err := s.control.LatencySum(ctx, kind, hour.Add(-latencyBaseline), hour)
	if err != nil || usual.Count < latencyHistoryFactor*int64(rule.MinRequests) {
		return err
	}
	p95, usualP95 := hourly.Stats().P95MS, usual.Stats().P95MS
	if p95 < usualP95*(1+rule.IncreasePercent/100) || p95-usualP95 < floorMS {
		return nil
	}
	n := slowResponsesNotice(where, hour, p95, usualP95, hourly.Count)
	e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "latency:" + kind + ":" + hour.Format(time.RFC3339), Kind: "latency", Subject: where, Message: n.summary, Ref: n.ref}, spikeCooldown, now)
	if err != nil {
		return err
	}
	if opened {
		s.deliver(ctx, env, e.ID, n)
	}
	return nil
}

// latencyText reads a latency in milliseconds: "0.4 ms", "420 ms", "12.5 s".
func latencyText(ms float64) string {
	switch {
	case ms < 10:
		return strconv.FormatFloat(math.Round(ms*10)/10, 'f', -1, 64) + " ms"
	case math.Round(ms) < 10_000:
		return strconv.FormatFloat(math.Round(ms), 'f', -1, 64) + " ms"
	}
	return strconv.FormatFloat(math.Round(ms/100)/10, 'f', -1, 64) + " s"
}

// slowResponsesNotice links to the Observe page, whose Metrics tab charts
// the latencies.
func slowResponsesNotice(where string, hour time.Time, p95, usual float64, requests int64) notice {
	detail := latencyText(p95) + " (usual " + latencyText(usual) + ")"
	return notice{icon: "🟠", title: "Slow responses", ref: "/observe",
		summary: where + " p95 " + detail + " in the hour of " + string(utc(hour))}.
		line("Where", esc(where)).line("p95", esc(detail)).line("Requests", esc(strconv.FormatInt(requests, 10))).line("Hour", utc(hour))
}
