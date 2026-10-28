package outlet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// Every public address is checked this often; the rest of the rounds
	// use the last result.
	addressInterval = 5 * time.Minute
	addressWorkers  = 16
	// addressBudget bounds one pass, so a Caddy that hangs can't use up the
	// round the other rules share.
	addressBudget = 20 * time.Second
	// A new address gets its first certificate on the first handshake, which
	// can outlast the check, so one failure proves nothing.
	addressFailures = 2
	// Caddy renews with a third of the lifetime left (30 days of 90), so a
	// certificate this close to its end means renewal isn't happening.
	addressExpiryWindow = 14 * 24 * time.Hour
	// Checks further apart than this aren't consecutive: alerts were off.
	addressStale = 3 * addressInterval
)

// Seams for tests: the connection to Caddy, the time one check may take, and
// the roots certificates must chain to. Nothing else about the chain is
// configurable: the generated Caddyfile only ever uses Caddy's default ACME
// issuer, a public CA that the system roots (the image installs
// ca-certificates) cover. The root-ca.pem that Compose mounts into Caddy is
// the admin port's private CA, which no public address uses, so there is no
// private-CA setup to exempt.
var (
	addressDial = func(ctx context.Context, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}
	addressTimeout = 10 * time.Second
	addressRoots   *x509.CertPool
)

// addressAlertState is the address check's, kept between rounds.
type addressAlertState struct {
	lastCheck time.Time
	// checks holds the latest result of each address served.
	checks map[string]addressCheck
}

type addressCheck struct {
	// problem is empty when the address passed.
	problem string
	// failed counts the consecutive checks that did not.
	failed int
}

func (s *Store) routeAlerts(ctx context.Context, env alertEnv, now time.Time) error {
	if !env.rules.RouteError.Enabled {
		// Nothing is checked or counted while the rule is off.
		s.alerts.address = addressAlertState{}
		return s.syncAlerts(ctx, env, "route_error", nil, nil, nil, now)
	}
	plans, err := s.control.HTTPRoutePlans(ctx)
	if err != nil {
		return err
	}
	var active []condition
	healthy, held := map[string]bool{}, map[string]bool{}
	for _, plan := range plans {
		key := "route_error:" + plan.ID
		if plan.LastError == "" {
			healthy[key] = true
			continue
		}
		active = append(active, condition{key, plan.Hostname, routeErrorNotice(plan.Hostname, plan.ConnectorID, plan.LastError)})
	}
	failing, err := s.addressConditions(ctx, now, healthy, held)
	if err != nil {
		return err
	}
	return s.syncAlerts(ctx, env, "route_error", append(active, failing...), healthy, held, now)
}

// addressConditions checks the public addresses when the last check is old
// enough, then reports the latest results: the addresses that failed
// addressFailures checks in a row are returned as conditions, those that
// passed go into healthy. An address not confirmed either way yet goes into
// held, so an alert already open (from before a restart) stays as it is.
func (s *Store) addressConditions(ctx context.Context, now time.Time, healthy, held map[string]bool) ([]condition, error) {
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return nil, err
	}
	// The enabled routes of enabled services, and the console's own address.
	hostnames := routing.Hostnames()
	if admin := routing.Outlet.AdminHostname; admin != "" && !slices.Contains(hostnames, admin) {
		hostnames = slices.Insert(hostnames, 0, admin)
	}
	state := &s.alerts.address
	if now.Sub(state.lastCheck) >= addressInterval {
		s.checkAddresses(ctx, hostnames, now)
	}
	var active []condition
	for _, hostname := range hostnames {
		key := "route_error:tls:" + hostname
		check, checked := state.checks[hostname]
		switch {
		case !checked || check.problem != "" && check.failed < addressFailures:
			held[key] = true
		case check.problem == "":
			healthy[key] = true
		default:
			ref := "/"
			if plan, ok := routing.Route(hostname); ok {
				ref = connectorRef(plan.ConnectorID)
			}
			active = append(active, condition{key, hostname, addressNotice(hostname, ref, check.problem)})
		}
	}
	return active, nil
}

// checkAddresses handshakes every hostname and records the results. An
// address the pass didn't get to keeps its earlier result.
func (s *Store) checkAddresses(ctx context.Context, hostnames []string, now time.Time) {
	state := &s.alerts.address
	previous := state.checks
	if now.Sub(state.lastCheck) > addressStale {
		previous = nil
	}
	state.lastCheck = now
	call, cancel := context.WithTimeout(ctx, addressBudget)
	defer cancel()
	problems, done := s.probeAddresses(call, hostnames, now)
	next := make(map[string]addressCheck, len(hostnames))
	for i, hostname := range hostnames {
		check, known := previous[hostname]
		switch {
		case !done[i]:
		case problems[i] == "":
			check, known = addressCheck{}, true
		default:
			check, known = addressCheck{problems[i], check.failed + 1}, true
		}
		if known {
			next[hostname] = check
		}
	}
	state.checks = next
}

// probeAddresses runs the handshakes a few at a time. done is false for the
// ones that ctx cut short, whose problem says nothing about the address.
func (s *Store) probeAddresses(ctx context.Context, hostnames []string, now time.Time) (problems []string, done []bool) {
	address := caddyHTTPSAddress(s.config.CaddyMetrics)
	problems, done = make([]string, len(hostnames)), make([]bool, len(hostnames))
	var wg sync.WaitGroup
	slots := make(chan struct{}, addressWorkers)
	for i, hostname := range hostnames {
		slots <- struct{}{}
		wg.Go(func() {
			defer func() { <-slots }()
			problems[i] = probeAddress(ctx, address, hostname, now)
			done[i] = ctx.Err() == nil
		})
	}
	wg.Wait()
	return problems, done
}

// caddyHTTPSAddress is where the Outlet reaches Caddy's TLS listener. The
// metrics address is the only one the Outlet knows for Caddy (the caddy
// service on the Compose network), and the generated Caddyfile serves every
// public address on 443 of that host. Going through Caddy, not out to the
// internet and back, shows the certificate a visitor gets.
func caddyHTTPSAddress(metrics string) string {
	// Open validated metrics as host:port.
	host, _, _ := net.SplitHostPort(metrics)
	return net.JoinHostPort(host, "443")
}

// probeAddress is the handshake a browser would make for hostname, verifying
// the chain and the name. It returns what is wrong, or "".
func probeAddress(ctx context.Context, address, hostname string, now time.Time) string {
	call, cancel := context.WithTimeout(ctx, addressTimeout)
	defer cancel()
	conn, err := addressDial(call, address)
	if err != nil {
		return addressProblem(err)
	}
	defer conn.Close()
	client := tls.Client(conn, &tls.Config{ServerName: hostname, RootCAs: addressRoots, MinVersion: tls.VersionTLS12, Time: func() time.Time { return now }})
	if err := client.HandshakeContext(call); err != nil {
		return addressProblem(err)
	}
	if end := client.ConnectionState().PeerCertificates[0].NotAfter; end.Sub(now) < addressExpiryWindow {
		return "Certificate expires " + end.UTC().Format("2006-01-02") + " (in " + daysLeft(end.Sub(now)) + ")"
	}
	return ""
}

func daysLeft(d time.Duration) string {
	switch days := int(d / (24 * time.Hour)); days {
	case 0:
		return "less than a day"
	case 1:
		return "1 day"
	default:
		return fmt.Sprintf("%d days", days)
	}
}

// addressProblem says in a few words why a handshake failed. It repeats the
// cause only, never the addresses or paths around it.
func addressProblem(err error) string {
	var wrongName x509.HostnameError
	var invalid x509.CertificateInvalidError
	var unknown x509.UnknownAuthorityError
	var timeout net.Error
	var errno syscall.Errno
	var lookup *net.DNSError
	var reason string
	switch {
	case errors.As(err, &wrongName):
		return "Certificate isn't valid for this address"
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired && invalid.Cert != nil:
		return "Certificate expired " + invalid.Cert.NotAfter.UTC().Format("2006-01-02")
	case errors.As(err, &invalid):
		return "Certificate isn't valid"
	case errors.As(err, &unknown):
		return "Certificate isn't trusted"
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
		reason = "timed out"
	case errors.As(err, &errno):
		reason = errno.Error()
	case errors.As(err, &lookup):
		reason = lookup.Err
	default:
		reason = strings.TrimPrefix(err.Error(), "remote error: ")
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if runes := []rune(reason); len(runes) > 80 {
		reason = string(runes[:80]) + "…"
	}
	return "TLS handshake failed: " + reason
}

func addressNotice(hostname, ref, problem string) notice {
	return notice{icon: "🔴", title: "Public address failing", ref: ref,
		summary: "Public address " + hostname + " failing: " + problem}.
		line("Address", code(hostname)).line("Problem", esc(problem))
}
