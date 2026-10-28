package outlet

import (
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/control"
)

// unstableTunnel is an enrolled Connector that posts heartbeats the way a real
// one does, so the events come from the handler.
type unstableTunnel struct {
	a         alertFixture
	connector control.Connector
	enrolled  contract.EnrollmentResponse
	// restarts is what the next heartbeat reports for the tunnel process.
	restarts int
}

// unstableGap is a silence long enough to be a reconnect.
const unstableGap = 20 * time.Second

func newUnstableTunnel(t *testing.T, a alertFixture) *unstableTunnel {
	t.Helper()
	return newUnstableTunnelNamed(t, a, "edge-a")
}

func newUnstableTunnelNamed(t *testing.T, a alertFixture, name string) *unstableTunnel {
	t.Helper()
	connector, enrolled := enrolledConnectorNamed(t, a.store.Handler(), a.admin, name)
	// The credential would run out within the hour the tests span.
	a.store.mu.Lock()
	record := a.store.managed[connector.ID]
	record.CredentialExpiresAt = a.clock.now().Add(24 * time.Hour)
	a.store.managed[connector.ID] = record
	a.store.mu.Unlock()
	return &unstableTunnel{a: a, connector: connector, enrolled: enrolled}
}

// post sends a heartbeat observed now and returns the status.
func (u *unstableTunnel) post() int {
	hb := contract.Heartbeat{ConnectorID: u.connector.ID, Revision: u.enrolled.Revision, Version: contract.Version, ProcessState: "running", ProcessRestarts: u.restarts, ServiceHealth: "unknown", ServiceHealths: map[string]string{u.enrolled.Services[0].ServiceID: "healthy"}, ObservedAt: u.a.clock.now()}
	return request(u.a.store.Handler(), "POST", "/connector/v1/heartbeat", u.enrolled.Credential, hb).Code
}

func (u *unstableTunnel) beat(t *testing.T) {
	t.Helper()
	if code := u.post(); code != 200 {
		t.Fatalf("heartbeat: %d", code)
	}
}

// flap goes silent for unstableGap and reports again, n times.
func (u *unstableTunnel) flap(t *testing.T, n int) {
	t.Helper()
	for range n {
		u.a.clock.advance(unstableGap)
		u.beat(t)
	}
}

func (u *unstableTunnel) counts() (reconnects, restarts int) {
	u.a.store.mu.Lock()
	defer u.a.store.mu.Unlock()
	history := u.a.store.tunnelEvents.recent(u.connector.ID, u.a.clock.now())
	return len(history.reconnects), len(history.restarts)
}

func (u *unstableTunnel) want(t *testing.T, reconnects, restarts int) {
	t.Helper()
	if r, p := u.counts(); r != reconnects || p != restarts {
		t.Fatalf("events = %d reconnects, %d restarts; want %d, %d", r, p, reconnects, restarts)
	}
}

// unstableTexts are the messages about unstable tunnels: the other rules speak
// in the same rounds.
func unstableTexts(a alertFixture) []string {
	var texts []string
	for _, text := range a.texts() {
		if strings.Contains(text, "<b>Tunnel unstable</b>") {
			texts = append(texts, text)
		}
	}
	return texts
}

func TestUnstableReconnectsAreHeartbeatsAfterAGap(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)

	// The first heartbeat of a Connector has nothing before it.
	u.beat(t)
	u.want(t, 0, 0)
	a.clock.advance(5 * time.Second)
	u.beat(t)
	u.want(t, 0, 0)
	// One second short of stale.
	a.clock.advance(14 * time.Second)
	u.beat(t)
	u.want(t, 0, 0)
	// Stale: the Console showed it as such in between.
	a.clock.advance(15 * time.Second)
	u.beat(t)
	u.want(t, 1, 0)
	// Disconnected for a while.
	a.clock.advance(10 * time.Minute)
	u.beat(t)
	u.want(t, 2, 0)
	a.clock.advance(5 * time.Second)
	u.beat(t)
	u.want(t, 2, 0)
}

func TestUnstableFirstHeartbeatAfterAnOutletRestartIsNoEvent(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	a.store.persist()
	a.clock.advance(10 * time.Minute)

	// The Outlet starts again with the last heartbeat kept but not when it
	// came, so neither the silence nor the restarts since count.
	reopened, err := OpenWithControl(a.cfg, a.dir, a.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	reopened.now = a.clock.now
	a.store = reopened
	u.a.store = reopened
	if reopened.managed[u.connector.ID].LastHeartbeat == nil || !reopened.managed[u.connector.ID].LastSeen.IsZero() {
		t.Fatalf("the restart did not leave the state this test needs: %+v", reopened.managed[u.connector.ID])
	}
	u.restarts = 3
	u.beat(t)
	u.want(t, 0, 0)

	// Recording works on the new Outlet: the next ones count.
	u.restarts = 4
	a.clock.advance(5 * time.Second)
	u.beat(t)
	u.want(t, 0, 1)
	u.flap(t, 1)
	u.want(t, 1, 1)
}

func TestUnstableTunnelProcessRestartsAreEvents(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	step := func(restarts, reconnects, processRestarts int) {
		t.Helper()
		u.restarts = restarts
		a.clock.advance(5 * time.Second)
		u.beat(t)
		u.want(t, reconnects, processRestarts)
	}
	step(0, 0, 0)
	// The count rises by what happened since the last heartbeat.
	step(2, 0, 2)
	step(2, 0, 2)
	step(3, 0, 3)
	// The Connector starting over reports a lower count: no restart of the
	// tunnel process, and no silence to count either.
	step(0, 0, 3)
	// Counted from the new start.
	step(1, 0, 4)
}

func TestUnstableEventsKeptPerConnectorAreBounded(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	// A Connector can report any number.
	for _, restarts := range []int{1_000_000, 2_000_000} {
		u.restarts = restarts
		a.clock.advance(5 * time.Second)
		u.beat(t)
		u.want(t, 0, maxTunnelEvents)
	}
}

func TestUnstableAlert(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	edge := newUnstableTunnel(t, a)
	edge.beat(t)

	edge.flap(t, 2)
	a.evaluate()
	if len(unstableTexts(a)) != 0 {
		t.Fatalf("alerted below the threshold: %q", unstableTexts(a))
	}

	// A tunnel process restart is the third event, once the reconnects are
	// past the time the others would have needed to come back too.
	edge.restarts = 1
	a.clock.advance(5 * time.Second)
	edge.beat(t)
	a.clock.advance(sharedWindow)
	a.evaluate()
	want := "🟠 <b>Tunnel unstable</b>\nTunnel: <code>edge-a</code>\nLast hour: 2 reconnects, 1 tunnel process restart\n<a href=\"https://" + a.host + "/connectors/" + edge.connector.ID + "\">Open in Meerkat</a>"
	if texts := unstableTexts(a); len(texts) != 1 || texts[0] != want {
		t.Fatalf("messages = %q", texts)
	}
	events := a.events(t, "tunnel_unstable")
	if len(events) != 1 || events[0].Key != "tunnel_unstable:"+edge.connector.ID || events[0].Subject != "edge-a" || !events[0].Sent || events[0].Message != "Tunnel edge-a unstable: 2 reconnects, 1 tunnel process restart in the last hour" || events[0].Ref != "/connectors/"+edge.connector.ID {
		t.Fatalf("events = %+v", events)
	}

	a.evaluate()
	if len(unstableTexts(a)) != 1 {
		t.Fatalf("alerted again in the same round: %q", unstableTexts(a))
	}

	// Still flapping half an hour later: it pages once an hour.
	a.clock.advance(30 * time.Minute)
	edge.flap(t, 3)
	a.evaluate()
	if len(unstableTexts(a)) != 1 || len(a.events(t, "tunnel_unstable")) != 1 {
		t.Fatalf("alerted again within the hour: %q", unstableTexts(a))
	}

	// After the hour, with the first events gone from the count.
	a.clock.advance(31 * time.Minute)
	a.evaluate()
	texts := unstableTexts(a)
	if len(texts) != 2 || !strings.Contains(texts[1], "\nLast hour: 3 reconnects\n") {
		t.Fatalf("messages after the hour = %q", texts)
	}
	for _, e := range a.events(t, "tunnel_unstable") {
		if e.Subject != "edge-a" {
			t.Fatalf("event for %q", e.Subject)
		}
	}
}

func TestUnstableEventsOlderThanAnHourDropOut(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	u.flap(t, 2)

	// The third event comes after the first two aged out: three in all, but
	// not within an hour.
	a.clock.advance(61 * time.Minute)
	u.beat(t)
	// Recording forgets them, not only reading.
	a.store.mu.Lock()
	kept := len(a.store.tunnelEvents.byConnector[u.connector.ID].reconnects)
	a.store.mu.Unlock()
	if kept != 1 {
		t.Fatalf("%d reconnects kept after recording a new one an hour later", kept)
	}
	a.evaluate()
	if len(unstableTexts(a)) != 0 {
		t.Fatalf("alerted on events from over an hour ago: %q", unstableTexts(a))
	}

	// Two more make three within the hour.
	u.flap(t, 2)
	a.clock.advance(sharedWindow)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 1 || !strings.Contains(texts[0], "\nLast hour: 3 reconnects\n") {
		t.Fatalf("messages = %q", texts)
	}
	// An event is out of the count when it is an hour old.
	a.clock.advance(time.Hour)
	u.want(t, 0, 0)
}

// A Connector that reports every 10 s isn't reconnecting when one heartbeat
// goes missing.
func TestUnstableReconnectsFollowTheHeartbeatInterval(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	a.clock.advance(10 * time.Second)
	u.beat(t)
	a.clock.advance(20 * time.Second)
	u.beat(t)
	u.want(t, 0, 0)
	a.clock.advance(25 * time.Second)
	u.beat(t)
	u.want(t, 1, 0)

	// One early heartbeat doesn't make it look like a 7 s Connector.
	v := newUnstableTunnelNamed(t, a, "edge-b")
	v.beat(t)
	for _, gap := range []time.Duration{10, 10, 7, 10, 10} {
		a.clock.advance(gap * time.Second)
		v.beat(t)
	}
	a.clock.advance(20 * time.Second)
	v.beat(t)
	v.want(t, 0, 0)
}

// Most tunnels going quiet together is the Outlet's side, Caddy restarting
// say, not each tunnel being unstable; a tunnel flapping alone still is.
func TestUnstableLeavesOutReconnectsMostTunnelsShare(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	tunnels := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-a"), newUnstableTunnelNamed(t, a, "edge-b"), newUnstableTunnelNamed(t, a, "edge-c")}
	for _, u := range tunnels {
		u.beat(t)
	}
	for range 3 {
		a.clock.advance(unstableGap)
		for _, u := range tunnels {
			u.beat(t)
		}
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("shared reconnects alerted: %q", texts)
	}
	// The others report every 5 s from here on while the first drops out
	// for 20 s, three times.
	steady := func(steps int, from int) {
		for range steps {
			a.clock.advance(5 * time.Second)
			for _, u := range tunnels[from:] {
				u.beat(t)
			}
		}
	}
	steady(12, 0)
	for range 3 {
		steady(4, 1)
		tunnels[0].beat(t)
		steady(12, 0)
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 1 || !strings.Contains(texts[0], "\nLast hour: 3 reconnects\n") {
		t.Fatalf("messages = %q", texts)
	}
}

// steadyTunnels report every 5 s for d, beside a.clock.
func steadyTunnels(t *testing.T, a alertFixture, d time.Duration, tunnels ...*unstableTunnel) {
	t.Helper()
	for range int(d / (5 * time.Second)) {
		a.clock.advance(5 * time.Second)
		for _, u := range tunnels {
			u.beat(t)
		}
	}
}

// Two of four tunnels flapping on their own, close together, aren't the
// Outlet's: that takes a majority.
func TestUnstableSharedReconnectsTakeAMajority(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	var tunnels []*unstableTunnel
	for _, name := range []string{"edge-a", "edge-b", "edge-c", "edge-d"} {
		tunnels = append(tunnels, newUnstableTunnelNamed(t, a, name))
	}
	for _, u := range tunnels {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, tunnels...)
	for range 3 {
		steadyTunnels(t, a, 20*time.Second, tunnels[2:]...)
		tunnels[0].beat(t)
		steadyTunnels(t, a, 25*time.Second, tunnels[2:]...)
		tunnels[1].beat(t)
		steadyTunnels(t, a, time.Minute, tunnels...)
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 2 {
		t.Fatalf("messages = %q", texts)
	}
}

// Enrolled tunnels that have been offline all hour can't share a reconnect,
// so they don't raise the majority the live ones need.
func TestUnstableOfflineTunnelsDontCountTowardTheMajority(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	for _, name := range []string{"off-a", "off-b", "off-c", "off-d"} {
		a.connector(t, name, a.clock.now().Add(-2*time.Hour))
	}
	tunnels := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-a"), newUnstableTunnelNamed(t, a, "edge-b"), newUnstableTunnelNamed(t, a, "edge-c")}
	for _, u := range tunnels {
		u.beat(t)
	}
	for range 3 {
		a.clock.advance(unstableGap)
		for _, u := range tunnels {
			u.beat(t)
		}
		steadyTunnels(t, a, time.Minute, tunnels...)
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("shared reconnects alerted: %q", texts)
	}
}

// A tunnel back first from an outage the others are still cut off by is
// judged with them: their silence under way counts.
func TestUnstableWaitsForTheOthersToComeBack(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	tunnels := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-a"), newUnstableTunnelNamed(t, a, "edge-b"), newUnstableTunnelNamed(t, a, "edge-c")}
	for _, u := range tunnels {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, tunnels...)
	// edge-a drops twice on its own.
	for range 2 {
		steadyTunnels(t, a, 20*time.Second, tunnels[1:]...)
		tunnels[0].beat(t)
		steadyTunnels(t, a, time.Minute, tunnels...)
	}
	// Then all three go quiet; edge-a is back first and a round runs.
	a.clock.advance(unstableGap)
	tunnels[0].beat(t)
	a.evaluate()
	a.clock.advance(3 * time.Second)
	for _, u := range tunnels[1:] {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, tunnels...)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("a shared reconnect counted for one tunnel: %q", texts)
	}
}

// Nothing is counted while alerts are off: Telegram turned on later doesn't
// page for what happened before.
func TestUnstableCountsNothingWhileAlertsAreOff(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	u.flap(t, 3)
	a.clock.advance(sharedWindow)
	a.evaluate()
	a.configureTelegram(t, false)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("paged for reconnects from before alerts were on: %q", texts)
	}
}

// Caddy restarting while half the tunnels are offline: the ones online all
// went quiet together, and the offline ones, cut off already, don't count
// against that.
func TestUnstableOfflineTunnelsDontHideASharedOutage(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	var tunnels []*unstableTunnel
	for _, name := range []string{"edge-a", "edge-b", "edge-c", "edge-d"} {
		tunnels = append(tunnels, newUnstableTunnelNamed(t, a, name))
	}
	for _, u := range tunnels {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, tunnels...)
	// edge-c and edge-d go offline; a while later Caddy restarts three times.
	steadyTunnels(t, a, time.Minute, tunnels[:2]...)
	for range 3 {
		a.clock.advance(unstableGap)
		for _, u := range tunnels[:2] {
			u.beat(t)
		}
		steadyTunnels(t, a, time.Minute, tunnels[:2]...)
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("a shared outage alerted: %q", texts)
	}
}

// An Outlet that stalls for 8 s leaves some tunnels silent long enough to
// reconnect and others only missing a heartbeat: all of them were cut off.
func TestUnstableNearMissesCountAsCutOff(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	var tunnels []*unstableTunnel
	for _, name := range []string{"edge-a", "edge-b", "edge-c", "edge-d"} {
		tunnels = append(tunnels, newUnstableTunnelNamed(t, a, name))
	}
	for _, u := range tunnels {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, tunnels...)
	for range 3 {
		a.clock.advance(10 * time.Second)
		for _, u := range tunnels[2:] {
			u.beat(t)
		}
		a.clock.advance(5 * time.Second)
		for _, u := range tunnels[:2] {
			u.beat(t)
		}
		steadyTunnels(t, a, time.Minute, tunnels...)
	}
	for _, u := range tunnels[:2] {
		u.want(t, 3, 0)
	}
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("a shared outage alerted: %q", texts)
	}
}

// What happens between the last round with alerts off and the first one
// with them on isn't counted either.
func TestUnstableStartsAfreshWhenAlertsComeOn(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	a.evaluate()
	u.flap(t, 3)
	a.clock.advance(sharedWindow)
	a.configureTelegram(t, false)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("paged for reconnects from before alerts were on: %q", texts)
	}
}

// A Connector set to report less often is learned anew: its longer gaps
// aren't missed heartbeats for good.
func TestUnstableIntervalFollowsALongerSetting(t *testing.T) {
	a := newAlertFixture(t)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	steadyTunnels(t, a, time.Minute, u)
	for range 10 {
		a.clock.advance(10 * time.Second)
		u.beat(t)
	}
	a.store.mu.Lock()
	interval := a.store.tunnelEvents.interval(u.connector.ID)
	a.store.mu.Unlock()
	if interval != 10*time.Second {
		t.Fatalf("interval = %v, want 10s", interval)
	}
	// One missed heartbeat at 10 s isn't a reconnect.
	a.clock.advance(20 * time.Second)
	u.beat(t)
	u.want(t, 0, 0)
}

// A tunnel back first is judged only once peers reporting every 10 s have
// had time to show they were cut off too.
func TestUnstableWaitsForSlowerPeers(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	fast := newUnstableTunnelNamed(t, a, "edge-a")
	slow := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-b"), newUnstableTunnelNamed(t, a, "edge-c")}
	for _, u := range append([]*unstableTunnel{fast}, slow...) {
		u.beat(t)
	}
	beatSlow := func(d time.Duration) {
		for range int(d / (10 * time.Second)) {
			for range 2 {
				a.clock.advance(5 * time.Second)
				fast.beat(t)
			}
			for _, u := range slow {
				u.beat(t)
			}
		}
	}
	beatSlow(2 * time.Minute)
	// edge-a drops twice on its own.
	for range 2 {
		for range 2 {
			a.clock.advance(10 * time.Second)
			for _, u := range slow {
				u.beat(t)
			}
		}
		fast.beat(t)
		beatSlow(time.Minute)
	}
	// Then an outage cuts off all three, just after the slow ones reported:
	// edge-a is back first, while theirs is only a 15 s silence so far.
	a.clock.advance(time.Second)
	for _, u := range slow {
		u.beat(t)
	}
	a.clock.advance(14 * time.Second)
	fast.beat(t)
	a.clock.advance(time.Second)
	a.evaluate()
	a.clock.advance(5 * time.Second)
	for _, u := range slow {
		u.beat(t)
	}
	beatSlow(time.Minute)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("a shared reconnect counted for one tunnel: %q", texts)
	}
}

// A tunnel first heard from after an outage began couldn't have told
// either way: it doesn't count against the tunnels that went quiet.
func TestUnstableLateTunnelsDontCount(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	early := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-a"), newUnstableTunnelNamed(t, a, "edge-b")}
	late := []*unstableTunnel{newUnstableTunnelNamed(t, a, "edge-c"), newUnstableTunnelNamed(t, a, "edge-d")}
	for _, u := range early {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, early...)
	for range 3 {
		a.clock.advance(unstableGap)
		for _, u := range early {
			u.beat(t)
		}
		steadyTunnels(t, a, time.Minute, early...)
	}
	for _, u := range late {
		u.beat(t)
	}
	steadyTunnels(t, a, time.Minute, append(early, late...)...)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 0 {
		t.Fatalf("shared reconnects alerted: %q", texts)
	}
}

func TestUnstableRuleSettings(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	u.flap(t, 3)

	a.setRules(t, func(r *control.AlertRules) { r.TunnelUnstable.Enabled = false })
	a.evaluate()
	if len(unstableTexts(a)) != 0 || len(a.events(t, "tunnel_unstable")) != 0 {
		t.Fatalf("alerted with the rule off: %q", unstableTexts(a))
	}

	// What happened before the rule was switched back on isn't counted,
	// and a higher threshold takes more.
	a.setRules(t, func(r *control.AlertRules) {
		r.TunnelUnstable = control.TunnelUnstableRule{Enabled: true, MinReconnects: 4}
	})
	a.evaluate()
	u.flap(t, 3)
	a.evaluate()
	if len(unstableTexts(a)) != 0 {
		t.Fatalf("alerted below MinReconnects: %q", unstableTexts(a))
	}
	u.flap(t, 1)
	a.clock.advance(sharedWindow)
	a.evaluate()
	if texts := unstableTexts(a); len(texts) != 1 || !strings.Contains(texts[0], "\nLast hour: 4 reconnects\n") {
		t.Fatalf("messages = %q", texts)
	}
}

func TestUnstableIgnoresTunnelsThatAreGone(t *testing.T) {
	t.Run("deleted", func(t *testing.T) {
		a := newAlertFixture(t)
		a.configureTelegram(t, false)
		u := newUnstableTunnel(t, a)
		u.beat(t)
		u.flap(t, 3)
		if w := request(a.store.Handler(), "DELETE", "/api/v1/connectors/"+u.connector.ID, a.admin, nil); w.Code != 204 {
			t.Fatalf("delete: %d %s", w.Code, w.Body.String())
		}
		a.evaluate()
		if len(unstableTexts(a)) != 0 || len(a.events(t, "tunnel_unstable")) != 0 {
			t.Fatalf("alerted for a deleted tunnel: %q", unstableTexts(a))
		}
		a.store.mu.Lock()
		_, kept := a.store.tunnelEvents.byConnector[u.connector.ID]
		a.store.mu.Unlock()
		if kept {
			t.Fatal("the events of a deleted tunnel are still kept")
		}
	})
	t.Run("unenrolled", func(t *testing.T) {
		a := newAlertFixture(t)
		a.configureTelegram(t, false)
		u := newUnstableTunnel(t, a)
		u.beat(t)
		u.flap(t, 3)
		// A new token starts the tunnel over as a draft until it enrolls.
		if w := request(a.store.Handler(), "POST", "/api/v1/connectors/"+u.connector.ID+"/enrollment-tokens?reset=true", a.admin, nil); w.Code != 201 {
			t.Fatalf("reset: %d %s", w.Code, w.Body.String())
		}
		a.evaluate()
		if len(unstableTexts(a)) != 0 || len(a.events(t, "tunnel_unstable")) != 0 {
			t.Fatalf("alerted for an unenrolled tunnel: %q", unstableTexts(a))
		}
	})
}

// Heartbeats and rounds run on different goroutines; the race detector fails
// this if either side reads or writes the events without the store lock.
func TestUnstableEventsAreSafeAcrossHeartbeatsAndRounds(t *testing.T) {
	a := newAlertFixture(t)
	a.configureTelegram(t, false)
	u := newUnstableTunnel(t, a)
	u.beat(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 40 {
			u.a.clock.advance(unstableGap)
			u.restarts++
			if code := u.post(); code != 200 {
				t.Errorf("heartbeat: %d", code)
				return
			}
		}
	}()
	for range 40 {
		a.evaluate()
	}
	<-done
	a.evaluate()
	if len(a.events(t, "tunnel_unstable")) != 1 {
		t.Fatalf("events = %+v", a.events(t, "tunnel_unstable"))
	}
}

func TestUnstableNoticeLeavesOutWhatDidNotHappen(t *testing.T) {
	for _, c := range []struct {
		reconnects, restarts int
		want                 string
	}{
		{1, 0, "1 reconnect"},
		{0, 1, "1 tunnel process restart"},
		{4, 2, "4 reconnects, 2 tunnel process restarts"},
		{0, 3, "3 tunnel process restarts"},
	} {
		n := tunnelUnstableNotice("edge-a", "c1", c.reconnects, c.restarts)
		if got := string(n.lines[1].value); n.lines[1].label != "Last hour" || got != c.want {
			t.Errorf("%d reconnects, %d restarts: %q", c.reconnects, c.restarts, got)
		}
		if !strings.Contains(n.summary, ": "+c.want+" in the last hour") {
			t.Errorf("summary = %q", n.summary)
		}
	}
}

// Only managed Connectors have alerts; the ones from the bootstrap
// configuration are not kept track of.
func TestUnstableOnlyManagedConnectorsAreRecorded(t *testing.T) {
	f := setup(t)
	response := registered(t, f)
	clock := time.Now().UTC()
	f.store.now = func() time.Time { return clock }
	for range 3 {
		hb := contract.Heartbeat{ConnectorID: response.ConnectorID, ServiceID: response.ServiceID, Revision: response.Revision, Version: contract.Version, ProcessState: "running", ServiceHealth: "unknown", ObservedAt: clock}
		if w := request(f.store.Handler(), "POST", "/connector/v1/heartbeat", response.Credential, hb); w.Code != 200 {
			t.Fatalf("heartbeat: %d %s", w.Code, w.Body.String())
		}
		clock = clock.Add(time.Minute)
	}
	if len(f.store.tunnelEvents.byConnector) != 0 {
		t.Fatalf("events kept for a bootstrap Connector: %+v", f.store.tunnelEvents.byConnector)
	}
}
