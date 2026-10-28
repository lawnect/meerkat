package outlet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// addLatencies counts n requests of us microseconds each in an hour; counting
// again adds to it.
func (a alertFixture) addLatencies(t *testing.T, kind string, hour time.Time, n int, us int64) {
	t.Helper()
	var h control.LatencyHistogram
	for range n {
		h.Add(us)
	}
	if err := a.db.RecordLatencies(context.Background(), map[control.LatencyKey]control.LatencyHistogram{{Kind: kind, Hour: hour}: h}, a.clock.now()); err != nil {
		t.Fatal(err)
	}
}

// latencyFixture is at 14:30 with Telegram on, and returns the current hour.
func latencyFixture(t *testing.T) (alertFixture, time.Time) {
	t.Helper()
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.clock.advance(30 * time.Minute)
	return a, a.clock.now().Truncate(time.Hour)
}

// Where the requests of these times fall in the buckets, a p95 is spread
// evenly over the bucket: 3 ms gives 4.9 ms, 60 ms 98 ms, 150 ms 243 ms.
const (
	usualUS     = 3_000
	slowedUS    = 60_000
	appUsualUS  = 150_000
	appSlowUS   = 400_000
	usualCount  = 1000
	slowedCount = 200
)

func TestLatencyAlertOutlet(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, slowedCount, slowedUS)
	// The hour's own slow requests aren't part of the usual it is compared
	// with: they would lift it to 85 ms. The services were as usual.
	a.addLatencies(t, control.LatencyUpstream, hour.Add(-24*time.Hour), usualCount, appUsualUS)
	a.addLatencies(t, control.LatencyUpstream, hour, slowedCount, appUsualUS)
	a.evaluate()
	want := "🟠 <b>Slow responses</b>\nWhere: Meerkat (Outlet)\np95: 98 ms (usual 4.9 ms)\nRequests: 200\nHour: 2026-10-26 14:00 UTC\n<a href=\"https://" + a.host + "/observe\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "latency")
	if len(events) != 1 || events[0].Key != "latency:outlet:2026-10-26T14:00:00Z" || events[0].Subject != "Meerkat (Outlet)" || events[0].Ref != "/observe" || !events[0].Sent || events[0].ResolvedAt == nil ||
		events[0].Message != "Meerkat (Outlet) p95 98 ms (usual 4.9 ms) in the hour of 2026-10-26 14:00 UTC" {
		t.Fatalf("events = %+v", events)
	}
}

// The services' p95 is the one the 10% of slow requests among 200 pull up,
// which their mean would hide.
func TestLatencyAlertUpstream(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyUpstream, hour.Add(-24*time.Hour), usualCount, appUsualUS)
	a.addLatencies(t, control.LatencyUpstream, hour, 180, appUsualUS)
	a.addLatencies(t, control.LatencyUpstream, hour, 20, appSlowUS)
	a.evaluate()
	want := "🟠 <b>Slow responses</b>\nWhere: Services\np95: 375 ms (usual 243 ms)\nRequests: 200\nHour: 2026-10-26 14:00 UTC\n<a href=\"https://" + a.host + "/observe\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "latency")
	if len(events) != 1 || events[0].Key != "latency:upstream:2026-10-26T14:00:00Z" || events[0].Subject != "Services" ||
		events[0].Message != "Services p95 375 ms (usual 243 ms) in the hour of 2026-10-26 14:00 UTC" {
		t.Fatalf("events = %+v", events)
	}
}

func TestLatencyAlertNeedsMinRequests(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, 99, slowedUS)
	a.evaluate()
	if len(a.telegram.messages()) != 0 || len(a.events(t, "latency")) != 0 {
		t.Fatalf("alerted on 99 requests: %q", a.texts())
	}
	// The hundredth request is enough.
	a.addLatencies(t, control.LatencyOutlet, hour, 1, slowedUS)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}

	// The rule's own number counts, not a fixed one.
	b, hour := latencyFixture(t)
	b.setRules(t, func(r *control.AlertRules) { r.Latency.MinRequests = 300 })
	b.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), 3000, usualUS)
	b.addLatencies(t, control.LatencyOutlet, hour, 299, slowedUS)
	b.evaluate()
	if len(b.telegram.messages()) != 0 {
		t.Fatalf("alerted on 299 requests of 300: %q", b.texts())
	}
	b.addLatencies(t, control.LatencyOutlet, hour, 1, slowedUS)
	b.evaluate()
	if len(b.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", b.texts())
	}
}

// Ten times the minimum is the history that makes a usual.
func TestLatencyAlertNeedsHistory(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), 950, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, 100, slowedUS)
	a.evaluate()
	if len(a.telegram.messages()) != 0 || len(a.events(t, "latency")) != 0 {
		t.Fatalf("alerted on 950 requests of history: %q", a.texts())
	}
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-48*time.Hour), 50, usualUS)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
}

// The usual is the seven days before the hour: the hour that ended a week
// ago is in it, and the one before that is not.
func TestLatencyUsualIsTheWeekBefore(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour, 200, slowedUS)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-7*24*time.Hour-time.Hour), usualCount, usualUS)
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("history older than a week counted: %q", a.texts())
	}
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-7*24*time.Hour), usualCount, usualUS)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
}

// 24 ms is five times the usual 4.9 ms but not 20 ms more; the services
// need 100 ms more than their usual.
func TestLatencyAlertNeedsAFloor(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, slowedCount, 20_000)
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("alerted on a rise of 19 ms: %q", a.texts())
	}
	a.addLatencies(t, control.LatencyOutlet, hour, 2000, 40_000)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}

	// The same times are a rise of 93 ms for the services: not enough there.
	b, hour := latencyFixture(t)
	b.addLatencies(t, control.LatencyUpstream, hour.Add(-24*time.Hour), usualCount, usualUS)
	b.addLatencies(t, control.LatencyUpstream, hour, slowedCount, slowedUS)
	b.evaluate()
	if len(b.telegram.messages()) != 0 {
		t.Fatalf("alerted on a rise of 93 ms: %q", b.texts())
	}
	b.addLatencies(t, control.LatencyUpstream, hour, 2000, 200_000)
	b.evaluate()
	if texts := b.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
}

// 375 ms is 55% over 243 ms.
func TestLatencyAlertNeedsThePercentage(t *testing.T) {
	a, hour := latencyFixture(t)
	a.setRules(t, func(r *control.AlertRules) { r.Latency.IncreasePercent = 100 })
	a.addLatencies(t, control.LatencyUpstream, hour.Add(-24*time.Hour), usualCount, appUsualUS)
	a.addLatencies(t, control.LatencyUpstream, hour, 180, appUsualUS)
	a.addLatencies(t, control.LatencyUpstream, hour, 20, appSlowUS)
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("alerted on a rise of 55%% with 100%% asked: %q", a.texts())
	}
	a.setRules(t, func(r *control.AlertRules) { r.Latency.IncreasePercent = 50 })
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
}

// Each latency alerts on its own, once an hour, and the hour that follows is
// another.
func TestLatencyAlertsOncePerHourPerKind(t *testing.T) {
	a, hour := latencyFixture(t)
	// A usual long enough that the slow hours don't lift it.
	for _, kind := range []string{control.LatencyOutlet, control.LatencyUpstream} {
		a.addLatencies(t, kind, hour.Add(-24*time.Hour), 10*usualCount, usualUS)
		a.addLatencies(t, kind, hour, slowedCount, 250_000)
	}
	a.evaluate()
	a.evaluate()
	a.clock.advance(time.Minute)
	a.evaluate()
	texts := a.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[0], "🟠 <b>Slow responses</b>\nWhere: Meerkat (Outlet)\n") || !strings.HasPrefix(texts[1], "🟠 <b>Slow responses</b>\nWhere: Services\n") {
		t.Fatalf("messages = %q", texts)
	}
	if events := a.events(t, "latency"); len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}

	// At 15:00 the slow hour is the previous one, still looked at but not
	// repeated; the new hour is slow too, and alerts once more for each.
	a.clock.advance(30 * time.Minute)
	for _, kind := range []string{control.LatencyOutlet, control.LatencyUpstream} {
		a.addLatencies(t, kind, hour.Add(time.Hour), slowedCount, 250_000)
	}
	a.evaluate()
	a.evaluate()
	if texts := a.texts(); len(texts) != 4 || len(a.events(t, "latency")) != 4 {
		t.Fatalf("messages = %q", texts)
	}
}

// An hour is looked at once it has ended, with the requests of its last
// minutes, and not once it is two hours gone.
func TestLatencyAlertLooksAtThePreviousHour(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, slowedCount, slowedUS)
	// 15:10: the slow hour has just ended, and the new one has no requests.
	a.clock.advance(40 * time.Minute)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
	if events := a.events(t, "latency"); len(events) != 1 || events[0].Key != "latency:outlet:2026-10-26T14:00:00Z" {
		t.Fatalf("events = %+v", events)
	}

	b, hour := latencyFixture(t)
	b.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	b.addLatencies(t, control.LatencyOutlet, hour, slowedCount, slowedUS)
	// 16:10: two hours ago is past looking at.
	b.clock.advance(100 * time.Minute)
	b.evaluate()
	if len(b.telegram.messages()) != 0 || len(b.events(t, "latency")) != 0 {
		t.Fatalf("alerted on an hour long gone: %q", b.texts())
	}
}

func TestLatencyAlertRespectsItsRule(t *testing.T) {
	a, hour := latencyFixture(t)
	a.addLatencies(t, control.LatencyOutlet, hour.Add(-24*time.Hour), usualCount, usualUS)
	a.addLatencies(t, control.LatencyOutlet, hour, slowedCount, slowedUS)
	a.setRules(t, func(r *control.AlertRules) { r.Latency.Enabled = false })
	a.evaluate()
	if len(a.telegram.messages()) != 0 || len(a.events(t, "latency")) != 0 {
		t.Fatalf("alerted with the rule off: %q", a.texts())
	}
	a.setRules(t, func(r *control.AlertRules) {})
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
}

func TestLatencyText(t *testing.T) {
	for ms, want := range map[float64]string{
		0.095:   "0.1 ms",
		4.875:   "4.9 ms",
		9.96:    "10 ms",
		10:      "10 ms",
		97.5:    "98 ms",
		420.4:   "420 ms",
		2425:    "2425 ms",
		9999.4:  "9999 ms",
		9999.6:  "10 s",
		12_500:  "12.5 s",
		29_000:  "29 s",
		30_000:  "30 s",
		120_000: "120 s",
	} {
		if got := latencyText(ms); got != want {
			t.Errorf("latencyText(%v) = %q, want %q", ms, got, want)
		}
	}
}
