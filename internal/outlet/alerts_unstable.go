package outlet

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	// unstableWindow is how far back the rule counts events.
	unstableWindow = time.Hour
	// reconnectGap is the silence after which a Connector was stale, as
	// managedConnectorStatus sees it, and so reconnected when it spoke again;
	// for a Connector that reports less often, missedBeats of its heartbeats.
	reconnectGap = 15 * time.Second
	missedBeats  = 2.5
	// A shorter silence of quietBeats heartbeats or more, a missed one, shows
	// a tunnel was touched by an outage too.
	quietBeats = 1.6
	// Tunnels that went quiet this close together were cut off by one
	// outage: when that is most of them, the Outlet's side (Caddy
	// restarting, the host's network), not each tunnel.
	sharedWindow = 30 * time.Second
	// judgeLimit bounds the comparisons of one round: beyond it, so many
	// tunnels reconnect so often that it is the Outlet's side by any measure.
	judgeLimit = 4_000_000
	// maxTunnelEvents bounds what is kept per Connector and kind. An hour holds
	// at most 240 reconnects, one for each reconnectGap, so only restarts a
	// Connector reports in a burst are ever cut.
	maxTunnelEvents = 256
)

// tunnelEvents records reconnects and tunnel process restarts as heartbeats
// arrive. It is guarded by Store.mu, which the heartbeat handler holds.
type tunnelEvents struct {
	byConnector map[string]tunnelHistory
	// gaps are each Connector's latest gaps between heartbeats that weren't
	// reconnects, of a second or more (the least an interval can be set to).
	// Their median is its heartbeat interval: one early or late heartbeat
	// doesn't move it. They are kept while the rule is off too.
	gaps map[string][]time.Duration
}

// intervalSamples is how many gaps the heartbeat interval is the median of.
const intervalSamples = 9

func (e *tunnelEvents) interval(id string) time.Duration {
	gaps := e.gaps[id]
	if len(gaps) == 0 {
		return 0
	}
	return slices.Sorted(slices.Values(gaps))[(len(gaps)-1)/2]
}

func (e *tunnelEvents) noteGap(id string, gap time.Duration) {
	if e.gaps == nil {
		e.gaps = map[string][]time.Duration{}
	}
	gaps := append(slices.Clone(e.gaps[id]), gap)
	e.gaps[id] = gaps[max(0, len(gaps)-intervalSamples):]
}

// silence is a stretch without heartbeats, from the last one before it to
// the one that ended it, or to now while it lasts.
type silence struct{ from, to time.Time }

// tunnelHistory is one Connector's last hour, oldest first: the silences
// long enough to be reconnects, the shorter quiet ones, and when its tunnel
// process restarted.
type tunnelHistory struct {
	reconnects, quiet []silence
	restarts          []time.Time
}

// recentEvents drops the events older than the window, then adds n at now,
// keeping the newest maxTunnelEvents.
func recentEvents(events []time.Time, now time.Time, n int) []time.Time {
	events = slices.DeleteFunc(events, func(at time.Time) bool { return now.Sub(at) >= unstableWindow })
	for range min(n, maxTunnelEvents) {
		events = append(events, now)
	}
	if over := len(events) - maxTunnelEvents; over > 0 {
		events = slices.Delete(events, 0, over)
	}
	return events
}

// recentSilences drops the silences that ended before the window, then adds
// the ones given, keeping the newest maxTunnelEvents.
func recentSilences(silences []silence, now time.Time, add ...silence) []silence {
	silences = append(slices.DeleteFunc(silences, func(q silence) bool { return now.Sub(q.to) >= unstableWindow }), add...)
	return silences[max(0, len(silences)-maxTunnelEvents):]
}

func (e *tunnelEvents) add(id string, now time.Time, reconnect, quiet []silence, restarts int) {
	if e.byConnector == nil {
		e.byConnector = map[string]tunnelHistory{}
	}
	history := e.byConnector[id]
	history.reconnects = recentSilences(history.reconnects, now, reconnect...)
	history.quiet = recentSilences(history.quiet, now, quiet...)
	history.restarts = recentEvents(history.restarts, now, restarts)
	e.byConnector[id] = history
}

// recent reports the events of the last hour, forgetting the older ones.
func (e *tunnelEvents) recent(id string, now time.Time) tunnelHistory {
	if _, ok := e.byConnector[id]; !ok {
		return tunnelHistory{}
	}
	e.add(id, now, nil, nil, 0)
	history := e.byConnector[id]
	if len(history.reconnects)+len(history.quiet)+len(history.restarts) == 0 {
		delete(e.byConnector, id)
		return tunnelHistory{}
	}
	return tunnelHistory{slices.Clone(history.reconnects), slices.Clone(history.quiet), slices.Clone(history.restarts)}
}

// recordTunnelEvents notes what an accepted heartbeat says about the
// Connector's stability. previous is the record before the heartbeat updates
// it. Called only while the store lock is held.
func (s *Store) recordTunnelEvents(id string, managed bool, previous Record, restarts int) {
	// Only managed Connectors have alerts. After an Outlet restart or a token
	// reset there is no earlier sighting in this run to measure from, and the
	// kept heartbeat is of unknown age: its restarts could be any number ago.
	if !managed || previous.LastSeen.IsZero() {
		return
	}
	now := s.now()
	var reconnect, quiet []silence
	var restarted int
	gap, interval := now.Sub(previous.LastSeen), s.tunnelEvents.interval(id)
	switch {
	case gap >= max(reconnectGap, time.Duration(missedBeats*float64(interval))):
		reconnect = []silence{{previous.LastSeen, now}}
	case gap >= quietGap(interval):
		quiet = []silence{{previous.LastSeen, now}}
	case gap >= time.Second:
		s.tunnelEvents.noteGap(id, gap)
	}
	// A lower count is the Connector starting over, an upgrade or a restart by
	// hand; a reconnect shows it when it was away long enough to be noticed.
	if previous.LastHeartbeat != nil && restarts > previous.LastHeartbeat.ProcessRestarts {
		restarted = restarts - previous.LastHeartbeat.ProcessRestarts
	}
	if len(reconnect)+len(quiet)+restarted != 0 {
		s.tunnelEvents.add(id, now, reconnect, quiet, restarted)
	}
}

// quietGap is the silence of a missed heartbeat at interval. While the
// interval isn't known yet nothing short of a reconnect is told apart: the
// gaps it is learned from would otherwise be taken for missed heartbeats.
func quietGap(interval time.Duration) time.Duration {
	if interval == 0 {
		return reconnectGap
	}
	return time.Duration(quietBeats * float64(interval))
}

// unstableAlerts reports a tunnel that reconnected, or whose tunnel process
// restarted, at least MinReconnects times in the last hour: the tunnel_down
// rule never sees a tunnel that keeps dropping and returning within its wait.
func (s *Store) unstableAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.TunnelUnstable
	if !rule.Enabled {
		s.alerts.unstableOff = true
		return nil
	}
	connectors, err := s.control.ListConnectors(ctx)
	if err != nil {
		return err
	}
	type unstable struct {
		connector            control.Connector
		reconnects, restarts int
	}
	var tunnels []trackedTunnel
	s.mu.Lock()
	// What happened while the rule or alerts were off isn't counted.
	if s.alerts.unstableOff {
		s.tunnelEvents.byConnector = nil
	}
	for id := range s.tunnelEvents.byConnector {
		// A deleted tunnel's events would otherwise stay for good.
		if _, ok := s.managed[id]; !ok {
			delete(s.tunnelEvents.byConnector, id)
		}
	}
	for id := range s.tunnelEvents.gaps {
		if _, ok := s.managed[id]; !ok {
			delete(s.tunnelEvents.gaps, id)
		}
	}
	for _, c := range connectors {
		record, ok := s.managed[c.ID]
		if !ok || !record.Enrolled {
			continue
		}
		history := s.tunnelEvents.recent(c.ID, now)
		silences := append(slices.Clone(history.reconnects), history.quiet...)
		// A silence under way counts too: a tunnel still cut off by the
		// outage another came back from.
		if !record.LastSeen.IsZero() && now.Sub(record.LastSeen) >= quietGap(s.tunnelEvents.interval(c.ID)) {
			silences = append(silences, silence{record.LastSeen, now})
		}
		slices.SortFunc(silences, func(a, b silence) int { return a.from.Compare(b.from) })
		seen := !record.LastSeen.IsZero() && now.Sub(record.LastSeen) < unstableWindow
		tunnels = append(tunnels, trackedTunnel{c, history, silences, seen})
	}
	s.mu.Unlock()
	s.alerts.unstableOff = false
	judged := 0
	for _, t := range tunnels {
		judged += len(t.history.reconnects) * len(tunnels)
	}
	var found []unstable
	for i, t := range tunnels {
		reconnects := 0
		for _, r := range t.history.reconnects {
			if judged <= judgeLimit && !sharedOutage(tunnels, i, r) {
				reconnects++
			}
		}
		if restarts := len(t.history.restarts); reconnects+restarts >= rule.MinReconnects {
			found = append(found, unstable{t.connector, reconnects, restarts})
		}
	}
	for _, u := range found {
		name := connectorName(u.connector)
		n := tunnelUnstableNotice(name, u.connector.ID, u.reconnects, u.restarts)
		e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "tunnel_unstable:" + u.connector.ID, Kind: "tunnel_unstable", Subject: name, Message: n.summary, Ref: n.ref}, alertCooldown, now)
		if err != nil {
			return err
		}
		if opened {
			s.deliver(ctx, env, e.ID, n)
		}
	}
	return nil
}

// trackedTunnel is what a round knows of one tunnel: its last hour, its
// silences (reconnects, quiet ones and the one under way) by start, and
// whether it was heard from at all in the hour.
type trackedTunnel struct {
	connector control.Connector
	history   tunnelHistory
	silences  []silence
	seen      bool
}

// sharedOutage tells whether reconnect r of tunnel i was the Outlet's side
// going away: most of the tunnels that could have noticed (heard from in
// the hour, and not cut off already) went quiet within sharedWindow of it.
func sharedOutage(tunnels []trackedTunnel, i int, r silence) bool {
	together, steady := 1, 0
	for j, t := range tunnels {
		if j == i || !t.seen {
			continue
		}
		k, _ := slices.BinarySearchFunc(t.silences, r.from.Add(-sharedWindow), func(q silence, at time.Time) int { return q.from.Compare(at) })
		switch {
		case k < len(t.silences) && !t.silences[k].from.After(r.from.Add(sharedWindow)):
			together++
		case k > 0 && t.silences[k-1].to.After(r.from):
			// Already cut off before it: it couldn't tell either way.
		default:
			steady++
		}
	}
	return together >= 2 && 2*together > together+steady
}

// tunnelUnstableNotice counts what happened in the last hour, leaving out
// the kind that didn't.
func tunnelUnstableNotice(name, connectorID string, reconnects, restarts int) notice {
	var parts []string
	for _, p := range []struct {
		n    int
		noun string
	}{{reconnects, "reconnect"}, {restarts, "tunnel process restart"}} {
		if p.n == 0 {
			continue
		}
		part := strconv.Itoa(p.n) + " " + p.noun
		if p.n != 1 {
			part += "s"
		}
		parts = append(parts, part)
	}
	detail := strings.Join(parts, ", ")
	return notice{icon: "🟠", title: "Tunnel unstable", ref: connectorRef(connectorID),
		summary: "Tunnel " + name + " unstable: " + detail + " in the last hour"}.
		line("Tunnel", code(name)).line("Last hour", esc(detail))
}
