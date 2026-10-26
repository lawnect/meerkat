package outlet

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

func (a alertFixture) evaluate() { a.store.evaluateAlerts(context.Background()) }

// connector registers an enrolled managed Connector last heard from lastSeen.
func (a alertFixture) connector(t *testing.T, name string, lastSeen time.Time) control.Connector {
	t.Helper()
	c, err := a.db.CreateConnector(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	a.setLastSeen(c.ID, name, lastSeen)
	return c
}

func (a alertFixture) setLastSeen(id, name string, lastSeen time.Time) {
	a.store.mu.Lock()
	a.store.managed[id] = Record{Seed: Seed{Name: name}, Enrolled: true, LastSeen: lastSeen}
	a.store.mu.Unlock()
}

func (a alertFixture) events(t *testing.T, kind string) []control.AlertEvent {
	t.Helper()
	all, err := a.db.RecentAlerts(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var events []control.AlertEvent
	for _, e := range all {
		if e.Kind == kind {
			events = append(events, e)
		}
	}
	return events
}

func (a alertFixture) setRules(t *testing.T, change func(*control.AlertRules)) {
	t.Helper()
	rules := control.DefaultAlertRules()
	change(&rules)
	if err := a.db.SetAlertRules(context.Background(), rules, a.clock.now()); err != nil {
		t.Fatal(err)
	}
}

func (a alertFixture) texts() []string {
	var texts []string
	for _, m := range a.telegram.messages() {
		texts = append(texts, m.text)
	}
	return texts
}

func TestTunnelDownAlertLifecycle(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	start := a.clock.now()
	edge := a.connector(t, "edge-a", start.Add(-3*time.Minute))
	// An unenrolled Connector is a draft, not an outage.
	if _, err := a.db.CreateConnector(context.Background(), "draft-b"); err != nil {
		t.Fatal(err)
	}

	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("alerted before %d minutes: %v", 5, a.texts())
	}

	a.clock.advance(3 * time.Minute)
	a.evaluate()
	want := "🔴 <b>Tunnel disconnected</b>\nTunnel: <code>edge-a</code>\nDown for: 6 min\nLast seen: 2026-10-26 13:57 UTC\n<a href=\"https://" + a.host + "/connectors/" + edge.ID + "\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "tunnel_down")
	if len(events) != 1 || events[0].Subject != "edge-a" || events[0].ResolvedAt != nil || !events[0].Sent || events[0].Message != "Tunnel edge-a disconnected (6 min)" {
		t.Fatalf("events = %+v", events)
	}

	a.clock.advance(time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 1 {
		t.Fatalf("an open alert was sent again: %q", a.texts())
	}

	a.setLastSeen(edge.ID, "edge-a", a.clock.now())
	a.clock.advance(time.Minute)
	a.setLastSeen(edge.ID, "edge-a", a.clock.now())
	a.evaluate()
	texts := a.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Tunnel reconnected</b>\nTunnel: <code>edge-a</code>\nAlerted: 2026-10-26 14:03 UTC\nOpen for: 2 min") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := a.events(t, "tunnel_down"); events[0].ResolvedAt == nil {
		t.Fatalf("event still open: %+v", events)
	}

	// Going down again inside the hour does not page again.
	a.clock.advance(10 * time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 2 || len(a.events(t, "tunnel_down")) != 1 {
		t.Fatalf("flapping alert repeated: %q", a.texts())
	}
	a.clock.advance(50 * time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 3 || len(a.events(t, "tunnel_down")) != 2 {
		t.Fatalf("alert after the cooldown: %q", a.texts())
	}
}

func TestTunnelDownWaitsForConfiguredMinutes(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.setRules(t, func(r *control.AlertRules) { r.TunnelDown.AfterMinutes = 30 })
	a.connector(t, "edge-a", a.clock.now().Add(-29*time.Minute))
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatal("alerted before the configured time")
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", a.texts())
	}
}

func TestNeverSeenConnectorCountsFromFirstObservation(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.connector(t, "edge-a", time.Time{})
	a.evaluate()
	a.clock.advance(4 * time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatal("alerted before the outage lasted five minutes")
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	if texts := a.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Down for: 5 min") || strings.Contains(texts[0], "Last seen") {
		t.Fatalf("messages = %q", texts)
	}
}

func TestUndeliveredAlertIsRetriedThenRecovers(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.connector(t, "edge-a", a.clock.now().Add(-10*time.Minute))
	a.telegram.fail(errTelegramUnreachable)
	a.evaluate()
	events := a.events(t, "tunnel_down")
	if len(events) != 1 || events[0].Sent || events[0].Error == nil || *events[0].Error != "telegram_unreachable" {
		t.Fatalf("events = %+v", events)
	}
	a.telegram.fail(nil)
	a.clock.advance(time.Minute)
	a.evaluate()
	events = a.events(t, "tunnel_down")
	if len(a.telegram.messages()) != 1 || len(events) != 1 || !events[0].Sent || events[0].Error != nil {
		t.Fatalf("retry: %q %+v", a.texts(), events)
	}

}

// An outage that was never announced is not announced as recovered either.
func TestUnannouncedOutageClearsQuietly(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	edge := a.connector(t, "edge-a", a.clock.now().Add(-10*time.Minute))
	a.telegram.fail(errTelegramFailed)
	a.evaluate()
	a.telegram.fail(nil)
	a.setLastSeen(edge.ID, "edge-a", a.clock.now())
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("messages = %q", a.texts())
	}
	if events := a.events(t, "tunnel_down"); len(events) != 1 || events[0].ResolvedAt == nil || events[0].Sent {
		t.Fatalf("events = %+v", events)
	}
}

func TestAlertsStayQuietUntilTelegramIsEnabled(t *testing.T) {
	a := newAlertFixture(t)
	a.connector(t, "edge-a", a.clock.now().Add(-10*time.Minute))
	a.evaluate()
	a.configureTelegram(t, false)
	if err := a.db.SetTelegram(context.Background(), "", "123456", false, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	a.evaluate()
	if len(a.telegram.messages()) != 0 || len(a.events(t, "tunnel_down")) != 0 {
		t.Fatal("alerts ran without Telegram enabled")
	}
	if err := a.db.SetTelegram(context.Background(), "", "123456", true, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	a.evaluate()
	if len(a.telegram.messages()) != 1 {
		t.Fatalf("messages = %q", a.texts())
	}
}

func TestDisablingARuleClosesItsAlertsQuietly(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.connector(t, "edge-a", a.clock.now().Add(-10*time.Minute))
	a.evaluate()
	a.setRules(t, func(r *control.AlertRules) { r.TunnelDown.Enabled = false })
	a.clock.advance(time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 1 {
		t.Fatalf("disabling announced a recovery: %q", a.texts())
	}
	if events := a.events(t, "tunnel_down"); len(events) != 1 || events[0].ResolvedAt == nil {
		t.Fatalf("events = %+v", events)
	}
}

func TestDeletedConnectorClosesItsAlertQuietly(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	edge := a.connector(t, "edge-a", a.clock.now().Add(-10*time.Minute))
	a.evaluate()
	if err := a.db.DeleteConnector(context.Background(), edge.ID); err != nil {
		t.Fatal(err)
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	if len(a.telegram.messages()) != 1 || a.events(t, "tunnel_down")[0].ResolvedAt == nil {
		t.Fatalf("messages = %q", a.texts())
	}
}

func TestRouteErrorAlert(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	ctx := context.Background()
	connector, err := a.db.CreateConnector(ctx, "edge-a")
	if err != nil {
		t.Fatal(err)
	}
	service, err := a.db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	route, err := a.db.CreateHTTPRoute(ctx, service.ID, "app.example.com")
	if err != nil {
		t.Fatal(err)
	}
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("healthy route alerted: %q", a.texts())
	}
	if _, err := a.db.SQL().ExecContext(ctx, "UPDATE routes SET last_error = ? WHERE id = ?", "tls: <certificate> failed & retrying", route.ID); err != nil {
		t.Fatal(err)
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	a.evaluate()
	want := "🔴 <b>Route error</b>\nRoute: <code>app.example.com</code>\nError: tls: &lt;certificate&gt; failed &amp; retrying\n<a href=\"https://" + a.host + "/connectors/" + connector.ID + "\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	if events := a.events(t, "route_error"); len(events) != 1 || events[0].Subject != "app.example.com" || events[0].ResolvedAt != nil {
		t.Fatalf("events = %+v", events)
	}
	if _, err := a.db.SQL().ExecContext(ctx, "UPDATE routes SET last_error = NULL WHERE id = ?", route.ID); err != nil {
		t.Fatal(err)
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	if texts := a.texts(); len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>Route error cleared</b>\nRoute: <code>app.example.com</code>") {
		t.Fatalf("recovery = %q", texts)
	}
	if events := a.events(t, "route_error"); events[0].ResolvedAt == nil {
		t.Fatal("event still open")
	}
}

func (a alertFixture) setUsage(t *testing.T, hour time.Time, requests, failed int) {
	t.Helper()
	if _, err := a.db.SQL().ExecContext(context.Background(), `INSERT INTO usage_total_hourly(hour, requests, error_responses) VALUES (?, ?, ?)
		ON CONFLICT(hour) DO UPDATE SET requests = excluded.requests, error_responses = excluded.error_responses`, hour.UTC().Format(time.RFC3339), requests, failed); err != nil {
		t.Fatal(err)
	}
}

func TestErrorSpikeAlertsOncePerHour(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.clock.advance(30 * time.Minute) // 14:30
	thisHour := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	a.setUsage(t, thisHour, 200, 9)                 // 4.5%: below the threshold
	a.setUsage(t, thisHour.Add(-time.Hour), 49, 49) // too few requests
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("alerted without a spike: %q", a.texts())
	}

	a.setUsage(t, thisHour, 200, 10) // exactly 5%
	a.evaluate()
	a.evaluate()
	want := "🟠 <b>5xx spike</b>\nError rate: 5.0% (10 of 200 requests)\nHour: 2026-10-26 14:00 UTC\n<a href=\"https://" + a.host + "/\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "error_spike")
	if len(events) != 1 || events[0].Subject != "2026-10-26T14:00:00Z" || events[0].ResolvedAt == nil || !events[0].Sent {
		t.Fatalf("events = %+v", events)
	}

	// The next hour still looks back at the spiked one, without repeating it.
	a.clock.advance(time.Hour)
	a.setUsage(t, thisHour.Add(time.Hour), 100, 50)
	a.evaluate()
	texts := a.texts()
	if len(texts) != 2 || !strings.Contains(texts[1], "Error rate: 50.0% (50 of 100 requests)") {
		t.Fatalf("messages = %q", texts)
	}
}

func TestErrorSpikeRespectsItsRule(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	a.setUsage(t, a.clock.now().Truncate(time.Hour), 1000, 900)
	a.setRules(t, func(r *control.AlertRules) { r.ErrorSpike.Enabled = false })
	a.evaluate()
	a.setRules(t, func(r *control.AlertRules) {
		r.ErrorSpike = control.ErrorSpikeRule{Enabled: true, ThresholdPercent: 95, MinRequests: 50}
	})
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("messages = %q", a.texts())
	}
}

type fakeResolver struct {
	mu      sync.Mutex
	failing bool
	asked   []string
}

func (r *fakeResolver) lookup(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.asked = append(r.asked, host)
	if r.failing {
		return nil, errors.New("no such host")
	}
	return []net.IPAddr{{IP: net.ParseIP("203.0.113.1")}}, nil
}

func (r *fakeResolver) setFailing(failing bool) {
	r.mu.Lock()
	r.failing = failing
	r.mu.Unlock()
}

func TestDNSFailureAlertRunsEveryFifteenMinutes(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	if _, err := a.db.SetDomainMode(context.Background(), "example.com", control.DomainWildcard); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeResolver{failing: true}
	a.store.lookupIP = resolver.lookup

	a.evaluate()
	want := "🌐 <b>DNS lookup failing</b>\nDomain: <code>example.com</code>\nNames: <code>" + a.host + "</code>, <code>*.example.com</code>\n<a href=\"https://" + a.host + "/domains/example.com\">Open in Meerkat</a>"
	if texts := a.texts(); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "dns_failure")
	if len(events) != 1 || events[0].Subject != "example.com" || events[0].ResolvedAt != nil {
		t.Fatalf("events = %+v", events)
	}

	// Fixed before the next check: nothing happens until fifteen minutes pass.
	resolver.setFailing(false)
	a.clock.advance(14 * time.Minute)
	asked := len(resolver.asked)
	a.evaluate()
	if len(resolver.asked) != asked || len(a.telegram.messages()) != 1 {
		t.Fatal("DNS was checked before the interval")
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	texts := a.texts()
	if len(texts) != 2 || !strings.HasPrefix(texts[1], "🟢 <b>DNS lookup recovered</b>\nDomain: <code>example.com</code>\nAlerted: 2026-10-26 14:00 UTC\nOpen for: 15 min") {
		t.Fatalf("recovery = %q", texts)
	}
	if a.events(t, "dns_failure")[0].ResolvedAt == nil {
		t.Fatal("event still open")
	}
}

func TestDNSFailureHostsModeNamesRoutes(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	ctx := context.Background()
	if _, err := a.db.SetDomainMode(ctx, "example.com", control.DomainHosts); err != nil {
		t.Fatal(err)
	}
	connector, _ := a.db.CreateConnector(ctx, "edge-a")
	service, err := a.db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CreateHTTPRoute(ctx, service.ID, "app.example.com"); err != nil {
		t.Fatal(err)
	}
	// Only the route's name fails; the management host resolves.
	a.store.lookupIP = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "app.example.com" {
			return nil, errors.New("no such host")
		}
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.1")}}, nil
	}
	a.evaluate()
	texts := a.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "Names: <code>app.example.com</code>\n") || strings.Contains(texts[0], a.host+"</code>") {
		t.Fatalf("messages = %q", texts)
	}
}
