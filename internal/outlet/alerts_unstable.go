package outlet

import (
	"context"
	"iter"
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
	// Reconnects of most tunnels this close together are the Outlet's side
	// going away (Caddy restarting, the host's network), not the tunnels'.
	sharedWindow = 30 * time.Second
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

// tunnelHistory is when one Connector reconnected and when its tunnel process
// restarted, oldest first, each list holding the last hour at most.
type tunnelHistory struct {
	reconnects, restarts []time.Time
}

// recentEvents drops the events older than the window, then adds n at now, keeping
// the newest maxTunnelEvents.
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

func (e *tunnelEvents) add(id string, now time.Time, reconnects, restarts int) {
	if e.byConnector == nil {
		e.byConnector = map[string]tunnelHistory{}
	}
	history := e.byConnector[id]
	history.reconnects, history.restarts = recentEvents(history.reconnects, now, reconnects), recentEvents(history.restarts, now, restarts)
	e.byConnector[id] = history
}

// recent reports the events of the last hour, forgetting the older ones.
func (e *tunnelEvents) recent(id string, now time.Time) tunnelHistory {
	history, ok := e.byConnector[id]
	if !ok {
		return tunnelHistory{}
	}
	history.reconnects, history.restarts = recentEvents(history.reconnects, now, 0), recentEvents(history.restarts, now, 0)
	if len(history.reconnects)+len(history.restarts) == 0 {
		delete(e.byConnector, id)
		return tunnelHistory{}
	}
	e.byConnector[id] = history
	return tunnelHistory{slices.Clone(history.reconnects), slices.Clone(history.restarts)}
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
	var reconnects, restarted int
	gap := now.Sub(previous.LastSeen)
	if gap >= max(reconnectGap, time.Duration(missedBeats*float64(s.tunnelEvents.interval(id)))) {
		reconnects = 1
	} else if gap >= time.Second {
		s.tunnelEvents.noteGap(id, gap)
	}
	// A lower count is the Connector starting over, an upgrade or a restart by
	// hand; a reconnect shows it when it was away long enough to be noticed.
	if previous.LastHeartbeat != nil && restarts > previous.LastHeartbeat.ProcessRestarts {
		restarted = restarts - previous.LastHeartbeat.ProcessRestarts
	}
	if reconnects+restarted != 0 {
		s.tunnelEvents.add(id, now, reconnects, restarted)
	}
}

// unstableAlerts reports a tunnel that reconnected, or whose tunnel process
// restarted, at least MinReconnects times in the last hour: the tunnel_down
// rule never sees a tunnel that keeps dropping and returning within its wait.
func (s *Store) unstableAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.TunnelUnstable
	if !rule.Enabled {
		// What happened while the rule was off isn't counted once it is on.
		s.mu.Lock()
		s.tunnelEvents.byConnector = nil
		s.mu.Unlock()
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
	type tracked struct {
		connector control.Connector
		history   tunnelHistory
	}
	var tunnels []tracked
	live := 0
	s.mu.Lock()
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
		// Tunnels long offline can't share a reconnect, so they don't count
		// toward the majority that makes one the Outlet's.
		if !record.LastSeen.IsZero() && now.Sub(record.LastSeen) < unstableWindow {
			live++
		}
		tunnels = append(tunnels, tracked{c, s.tunnelEvents.recent(c.ID, now)})
	}
	s.mu.Unlock()
	shared := sharedReconnects(func(yield func(int, []time.Time) bool) {
		for i, t := range tunnels {
			if !yield(i, t.history.reconnects) {
				return
			}
		}
	}, max(2, live/2+1))
	var found []unstable
	for i, t := range tunnels {
		reconnects := 0
		for _, at := range t.history.reconnects {
			// A reconnect is judged once the others have had time to come
			// back too.
			if now.Sub(at) >= sharedWindow && !shared[i][at] {
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

// sharedReconnects finds, for each tunnel, its reconnects that at least
// quorum tunnels (it among them) had within sharedWindow of each other: the
// Outlet's side going away, not the tunnel's. One sorted sweep keeps this
// cheap with many tunnels flapping together.
func sharedReconnects(tunnels iter.Seq2[int, []time.Time], quorum int) map[int]map[time.Time]bool {
	type stamp struct {
		at     time.Time
		tunnel int
	}
	var all []stamp
	count := 0
	for i, reconnects := range tunnels {
		count = max(count, i+1)
		for _, at := range reconnects {
			all = append(all, stamp{at, i})
		}
	}
	slices.SortFunc(all, func(a, b stamp) int { return a.at.Compare(b.at) })
	shared := map[int]map[time.Time]bool{}
	mark := make([]int, count)
	lo, hi := 0, 0
	for k, e := range all {
		for all[lo].at.Before(e.at.Add(-sharedWindow)) {
			lo++
		}
		for hi < len(all) && !all[hi].at.After(e.at.Add(sharedWindow)) {
			hi++
		}
		together := 0
		for _, o := range all[lo:hi] {
			if mark[o.tunnel] != k+1 {
				mark[o.tunnel] = k + 1
				together++
			}
		}
		if together >= quorum {
			if shared[e.tunnel] == nil {
				shared[e.tunnel] = map[time.Time]bool{}
			}
			shared[e.tunnel][e.at] = true
		}
	}
	return shared
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
