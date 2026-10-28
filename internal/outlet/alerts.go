package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	alertInterval = time.Minute
	dnsInterval   = 15 * time.Minute
	alertTimeout  = 50 * time.Second
	lookupTimeout = 5 * time.Second
	lookupWorkers = 8
	// alertCooldown spaces re-opens of one key, so a flapping tunnel pages once an hour.
	alertCooldown = time.Hour
	// A spike key names its hour and is evaluated for two hours, so this blocks repeats.
	spikeCooldown = 2 * time.Hour
	// An alert that could not be delivered is retried for this long.
	retryWindow = time.Hour
)

// alertState belongs to the evaluator goroutine alone.
type alertState struct {
	downSince map[string]time.Time
	// serviceDownSince is when each service was first seen not serving.
	serviceDownSince map[string]time.Time
	lastDNS          time.Time
	// Each of these belongs to the rule in its own file.
	latency latencyAlertState
	address addressAlertState
	host    hostAlertState
}

// alertEnv is what one evaluation round reads up front.
type alertEnv struct {
	telegram control.Telegram
	rules    control.AlertRules
	outlet   control.Outlet
}

type alertRound struct {
	rule string
	run  func(context.Context, alertEnv, time.Time) error
}

// condition is an alert that holds right now.
type condition struct {
	key, subject string
	n            notice
}

// RunAlerts checks the alert rules every minute. The first round waits a
// minute so Connectors can reconnect after an Outlet restart.
func (s *Store) RunAlerts(ctx context.Context) {
	ticker := time.NewTicker(alertInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			call, cancel := context.WithTimeout(ctx, alertTimeout)
			s.evaluateAlerts(call)
			cancel()
		}
	}
}

// loadAlertEnv reports false when Telegram is not configured and enabled.
func (s *Store) loadAlertEnv(ctx context.Context) (alertEnv, bool, error) {
	var env alertEnv
	if s.control == nil {
		return env, false, nil
	}
	telegram, err := s.control.Telegram(ctx)
	if err != nil || !telegram.Configured() || !telegram.Enabled {
		return env, false, err
	}
	rules, err := s.control.AlertRules(ctx)
	if err != nil {
		return env, false, err
	}
	outlet, err := s.control.Outlet(ctx)
	return alertEnv{telegram, rules, outlet}, err == nil, err
}

func (s *Store) evaluateAlerts(ctx context.Context) {
	env, ok, err := s.loadAlertEnv(ctx)
	if err != nil {
		slog.Warn("alert settings unavailable", "error", err)
		return
	}
	if !ok {
		// Nothing is timed while alerts are off.
		s.alerts.downSince, s.alerts.serviceDownSince = nil, nil
		return
	}
	now := s.now()
	rounds := []alertRound{{"tunnel_down", s.tunnelAlerts}, {"service_down", s.serviceAlerts}, {"tunnel_unstable", s.unstableAlerts}, {"route_error", s.routeAlerts}, {"error_spike", s.spikeAlerts}, {"rate_limit", s.rateLimitAlerts}, {"latency", s.latencyAlerts}, {"outlet_restart", s.restartAlerts}, {"disk_low", s.diskAlerts}, {"memory_low", s.memoryAlerts}}
	if now.Sub(s.alerts.lastDNS) >= dnsInterval {
		rounds = append(rounds, alertRound{"dns_failure", s.dnsAlerts})
		s.alerts.lastDNS = now
	}
	for _, round := range rounds {
		if err := round.run(ctx, env, now); err != nil && ctx.Err() == nil {
			slog.Warn("alert evaluation failed", "rule", round.rule, "error", err)
		}
	}
}

// deliver sends a notice for an event and records whether it arrived.
func (s *Store) deliver(ctx context.Context, env alertEnv, id int64, n notice) {
	err := s.telegramSend(ctx, env.telegram.BotToken, env.telegram.ChatID, n.HTML(env.outlet.AdminHostname))
	failure := ""
	if err != nil {
		failure = telegramCode(err)
		slog.Warn("telegram alert not delivered", "error", failure)
	}
	if err := s.control.MarkAlertSent(ctx, id, failure == "", failure); err != nil {
		slog.Warn("alert delivery not recorded", "error", err)
	}
}

// syncAlerts brings the open events of one kind in line with the conditions
// that hold now. Events whose key is in healthy get a recovery message when
// they clear; others (the subject was deleted, the rule was switched off)
// close quietly. Events whose key is in held stay open as they are.
func (s *Store) syncAlerts(ctx context.Context, env alertEnv, kind string, active []condition, healthy, held map[string]bool, now time.Time) error {
	open, err := s.control.OpenAlerts(ctx, kind)
	if err != nil {
		return err
	}
	pending := make(map[string]control.AlertEvent, len(open))
	for _, e := range open {
		pending[e.Key] = e
	}
	for _, c := range active {
		if e, found := pending[c.key]; found {
			delete(pending, c.key)
			if opened, err := time.Parse(time.RFC3339, e.OpenedAt); !e.Sent && err == nil && now.Sub(opened) < retryWindow {
				s.deliver(ctx, env, e.ID, c.n)
			}
			continue
		}
		e, opened, err := s.control.OpenAlert(ctx, control.AlertEvent{Key: c.key, Kind: kind, Subject: c.subject, Message: c.n.summary, Ref: c.n.ref}, alertCooldown, now)
		if err != nil {
			return err
		}
		if opened {
			s.deliver(ctx, env, e.ID, c.n)
		}
	}
	for _, e := range pending {
		if held[e.Key] {
			continue
		}
		if err := s.control.ResolveAlert(ctx, e.ID, now); err != nil {
			return err
		}
		if n, ok := recoveryNotice(e, now); ok && e.Sent && healthy[e.Key] {
			if err := s.telegramSend(ctx, env.telegram.BotToken, env.telegram.ChatID, n.HTML(env.outlet.AdminHostname)); err != nil {
				slog.Warn("telegram recovery not delivered", "error", telegramCode(err))
			}
		}
	}
	return nil
}

func (s *Store) tunnelAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.TunnelDown
	if !rule.Enabled {
		return s.syncAlerts(ctx, env, "tunnel_down", nil, nil, nil, now)
	}
	connectors, err := s.control.ListConnectors(ctx)
	if err != nil {
		return err
	}
	var active []condition
	healthy, held := map[string]bool{}, map[string]bool{}
	down := map[string]time.Time{}
	s.mu.Lock()
	for _, c := range connectors {
		record, ok := s.managed[c.ID]
		if !ok || !record.Enrolled {
			continue
		}
		key := "tunnel_down:" + c.ID
		if managedConnectorStatus(record, now) == "connected" {
			healthy[key] = true
			continue
		}
		// The Connector is down since it was last heard, or since the Outlet
		// first noticed after a restart cleared that.
		since := record.LastSeen
		if since.IsZero() {
			if since = s.alerts.downSince[c.ID]; since.IsZero() {
				since = now
			}
		}
		down[c.ID] = since
		if elapsed := now.Sub(since); elapsed >= time.Duration(rule.AfterMinutes)*time.Minute {
			name := connectorName(c)
			active = append(active, condition{key, name, tunnelDownNotice(name, c.ID, elapsed, record.LastSeen)})
		} else {
			// Still down, timed afresh after an Outlet restart: an alert
			// already open stays open.
			held[key] = true
		}
	}
	s.mu.Unlock()
	s.alerts.downSince = down
	return s.syncAlerts(ctx, env, "tunnel_down", active, healthy, held, now)
}

// connectorName is the tunnel name the console shows.
func connectorName(c control.Connector) string {
	if c.DisplayName != "" {
		return c.DisplayName
	}
	return c.Name
}

// serviceAlerts reports the services a connected tunnel isn't serving: the
// Connector can't reach the app, or the Outlet gets no answer from it through
// the tunnel. While a tunnel is down, that is the tunnel's alert: the alerts
// of its services are held as they are, neither opened nor cleared.
func (s *Store) serviceAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.ServiceDown
	if !rule.Enabled {
		s.alerts.serviceDownSince = nil
		return s.syncAlerts(ctx, env, "service_down", nil, nil, nil, now)
	}
	connectors, err := s.control.ListConnectors(ctx)
	if err != nil {
		return err
	}
	services := make([][]control.Service, len(connectors))
	for i, c := range connectors {
		// A tunnel deleted meanwhile has no services left to report.
		if services[i], err = s.control.ListServices(ctx, c.ID); err != nil && !errors.Is(err, control.ErrConnectorNotFound) {
			return err
		}
	}
	var active []condition
	healthy, held := map[string]bool{}, map[string]bool{}
	down := map[string]time.Time{}
	s.mu.Lock()
	for i, c := range connectors {
		record, ok := s.managed[c.ID]
		if !ok || !record.Enrolled {
			continue
		}
		connected := managedConnectorStatus(record, now) == "connected"
		for _, service := range services[i] {
			if !service.Enabled {
				continue
			}
			key := "service_down:" + service.ID
			// Time with the tunnel down isn't the service's: its wait starts
			// again once the tunnel is back.
			if !connected {
				held[key] = true
				continue
			}
			status := managedServiceStatus(record, service.ID, now)
			if status == "connected" {
				healthy[key] = true
				continue
			}
			since, seen := s.alerts.serviceDownSince[service.ID]
			if !seen {
				since = now
			}
			down[service.ID] = since
			if elapsed := now.Sub(since); elapsed >= time.Duration(rule.AfterMinutes)*time.Minute {
				tunnel := connectorName(c)
				active = append(active, condition{key, tunnel + " / " + service.Name, serviceDownNotice(tunnel, service.Name, c.ID, status, elapsed)})
			} else {
				// Timed afresh, after a restart or the tunnel's return: an
				// alert already open stays open.
				held[key] = true
			}
		}
	}
	s.mu.Unlock()
	s.alerts.serviceDownSince = down
	return s.syncAlerts(ctx, env, "service_down", active, healthy, held, now)
}

func (s *Store) dnsAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	if !env.rules.DNSFailure.Enabled {
		return s.syncAlerts(ctx, env, "dns_failure", nil, nil, nil, now)
	}
	var active []condition
	healthy := map[string]bool{}
	for _, domain := range env.outlet.Domains {
		hostnames, err := s.dnsHostnames(ctx, env.outlet, domain)
		if err != nil {
			return err
		}
		key := "dns_failure:" + domain
		failing := s.unresolved(ctx, hostnames)
		if len(failing) == 0 {
			healthy[key] = true
			continue
		}
		for i, hostname := range failing {
			// Wildcard mode probes a fresh random name; show the pattern instead.
			if env.outlet.Modes[domain] != control.DomainHosts && strings.HasPrefix(hostname, "check-") && strings.HasSuffix(hostname, "."+domain) {
				failing[i] = "*." + domain
			}
		}
		active = append(active, condition{key, domain, dnsFailureNotice(domain, failing)})
	}
	return s.syncAlerts(ctx, env, "dns_failure", active, healthy, nil, now)
}

// unresolved returns the hostnames that do not resolve, in order.
func (s *Store) unresolved(ctx context.Context, hostnames []string) []string {
	lookup := s.lookupIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupIPAddr
	}
	failed := make([]bool, len(hostnames))
	var wg sync.WaitGroup
	slots := make(chan struct{}, lookupWorkers)
	for i, hostname := range hostnames {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			call, cancel := context.WithTimeout(ctx, lookupTimeout)
			defer cancel()
			addresses, err := lookup(call, hostname)
			failed[i] = err != nil || len(addresses) == 0
		})
	}
	wg.Wait()
	var names []string
	for i, hostname := range hostnames {
		if failed[i] {
			names = append(names, hostname)
		}
	}
	return names
}

// spikeAlerts looks at the current and previous UTC hour. Each hour alerts at most once.
func (s *Store) spikeAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	rule := env.rules.ErrorSpike
	if !rule.Enabled {
		return nil
	}
	hour := now.UTC().Truncate(time.Hour)
	series, err := s.control.OutletUsageSeries(ctx, "hour", hour.Add(-time.Hour), hour.Add(time.Hour), time.UTC)
	if err != nil {
		return err
	}
	for _, bucket := range series.Buckets {
		if bucket.Requests < int64(rule.MinRequests) || float64(bucket.ErrorResponses)*100 < rule.ThresholdPercent*float64(bucket.Requests) {
			continue
		}
		start, err := time.Parse(time.RFC3339, bucket.Start)
		if err != nil {
			return err
		}
		n := errorSpikeNotice(start, bucket.ErrorResponses, bucket.Requests)
		e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "error_spike:" + bucket.Start, Kind: "error_spike", Subject: bucket.Start, Message: n.summary, Ref: n.ref}, spikeCooldown, now)
		if err != nil {
			return err
		}
		if opened {
			s.deliver(ctx, env, e.ID, n)
		}
	}
	return nil
}

// sendLoginNotice records an administrator login and sends it to Telegram.
func (s *Store) sendLoginNotice(ctx context.Context, n notice, email string) {
	env, ok, err := s.loadAlertEnv(ctx)
	if err != nil || !ok || !env.rules.Login.Enabled {
		return
	}
	e, opened, err := s.control.RecordAlert(ctx, control.AlertEvent{Key: "login:" + Token(), Kind: "login", Subject: email, Message: n.summary, Ref: n.ref}, 0, s.now())
	if err != nil {
		// The notice matters more than its line in the alert history.
		slog.Warn("login alert not recorded", "error", err)
	} else if !opened {
		return
	}
	s.deliver(ctx, env, e.ID, n)
}
