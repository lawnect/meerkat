package connector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/health"
	"github.com/lawnect/meerkat/internal/validation"
)

// targetRule is one MEERKAT_ALLOWED_TARGETS entry: a host, with a port or, at
// port 0, any port.
type targetRule struct {
	host string
	port int
}

// parseAllowedTargets reads host or host:port entries. An entry that
// validation.Target would refuse can never match, so it is an error rather
// than a silent no-op.
func parseAllowedTargets(entries []string) ([]targetRule, error) {
	rules := make([]targetRule, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		host, port := strings.Trim(entry, "[]"), 0
		if h, p, err := net.SplitHostPort(entry); err == nil {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("invalid allowed target %q", entry)
			}
			host, port = h, n
		}
		if validation.Target(contract.Target{Host: host, Port: 1}) != nil {
			return nil, fmt.Errorf("invalid allowed target %q: use a Compose service name or a loopback address", entry)
		}
		rules = append(rules, targetRule{normalizeHost(host), port})
	}
	return rules, nil
}

func normalizeHost(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return strings.ToLower(host)
}

// allowedTargetsFromEnvironment splits MEERKAT_ALLOWED_TARGETS, or reports nil
// when it is unset. A value without any entry is an error: "restrict to
// nothing" must not read as "no restriction".
func allowedTargetsFromEnvironment(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var entries []string
	for _, entry := range strings.Split(value, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("MEERKAT_ALLOWED_TARGETS lists no target")
	}
	return entries, nil
}

// permits reports whether the local allowlist lets the Connector reach target.
// Without an allowlist every target the Outlet configures is allowed, as before.
// An allowlist that cannot be read refuses everything.
func (c Config) permits(target contract.Target) bool {
	if len(c.AllowedTargets) == 0 {
		return true
	}
	rules, err := parseAllowedTargets(c.AllowedTargets)
	if err != nil {
		return false
	}
	host := normalizeHost(target.Host)
	for _, rule := range rules {
		if rule.host == host && (rule.port == 0 || rule.port == target.Port) {
			return true
		}
	}
	return false
}

// screen splits services by the allowlist. Refused services are never
// resolved, probed or handed to rathole.
func (c Config) screen(services []contract.ServiceConfiguration) (permitted, refused []contract.ServiceConfiguration) {
	for _, service := range services {
		if c.permits(service.Target) {
			permitted = append(permitted, service)
		} else {
			refused = append(refused, service)
		}
	}
	return permitted, refused
}

// checkAll is health.CheckAll for the permitted services; a refused one reports
// unhealthy, the only error state the Outlet accepts.
func (c Config) checkAll(ctx context.Context, services []contract.ServiceConfiguration) map[string]string {
	permitted, refused := c.screen(services)
	healths := health.CheckAll(ctx, permitted)
	for _, service := range refused {
		healths[service.ServiceID] = "unhealthy"
	}
	return healths
}
