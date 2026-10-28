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
	// managedConnectorStatus sees it, and so reconnected when it spoke again.
	reconnectGap = 15 * time.Second
	// maxTunnelEvents bounds what is kept per Connector and kind. An hour holds
	// at most 240 reconnects, one for each reconnectGap, so only restarts a
	// Connector reports in a burst are ever cut.
	maxTunnelEvents = 256
)

// tunnelEvents records reconnects and tunnel process restarts as heartbeats
// arrive. It is guarded by Store.mu, which the heartbeat handler holds.
type tunnelEvents struct {
	byConnector map[string]tunnelHistory
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

// count reports the events of the last hour, forgetting the older ones.
func (e *tunnelEvents) count(id string, now time.Time) (reconnects, restarts int) {
	history, ok := e.byConnector[id]
	if !ok {
		return 0, 0
	}
	history.reconnects, history.restarts = recentEvents(history.reconnects, now, 0), recentEvents(history.restarts, now, 0)
	if len(history.reconnects)+len(history.restarts) == 0 {
		delete(e.byConnector, id)
		return 0, 0
	}
	e.byConnector[id] = history
	return len(history.reconnects), len(history.restarts)
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
	if now.Sub(previous.LastSeen) >= reconnectGap {
		reconnects = 1
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
	var found []unstable
	s.mu.Lock()
	for id := range s.tunnelEvents.byConnector {
		// A deleted tunnel's events would otherwise stay for good.
		if _, ok := s.managed[id]; !ok {
			delete(s.tunnelEvents.byConnector, id)
		}
	}
	for _, c := range connectors {
		if record, ok := s.managed[c.ID]; !ok || !record.Enrolled {
			continue
		}
		if reconnects, restarts := s.tunnelEvents.count(c.ID, now); reconnects+restarts >= rule.MinReconnects {
			found = append(found, unstable{c, reconnects, restarts})
		}
	}
	s.mu.Unlock()
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
