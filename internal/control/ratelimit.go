package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/lawnect/meerkat/internal/validation"
)

var ErrInvalidRateLimit = errors.New("invalid rate limit settings")

const (
	settingRateLimit = "rate_limit"

	// RateEndpointTotal caps an endpoint across all clients; RateEndpointIP
	// caps it per client address.
	RateEndpointTotal = "total"
	RateEndpointIP    = "ip"

	MaxRateEndpoints = 50
	maxRateRPS       = 10000
	maxRateBurst     = 100000
	maxPathPrefix    = 256
)

// Limit is a token bucket. Zero for both fields turns it off.
type Limit struct {
	RPS   int `json:"rps"`
	Burst int `json:"burst"`
}

func (l Limit) On() bool { return l.RPS > 0 }

// EndpointLimit throttles requests whose host and path prefix match. Host is
// a hostname or "*" for every host.
type EndpointLimit struct {
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix"`
	RPS        int    `json:"rps"`
	Burst      int    `json:"burst"`
	Per        string `json:"per"`
}

// RateLimitSettings is the request flood protection of the public listener.
// A request is rejected when any of its buckets is empty.
type RateLimitSettings struct {
	Enabled    bool            `json:"enabled"`
	PerIP      Limit           `json:"per_ip"`
	PerSubnet  Limit           `json:"per_subnet"`
	PerAddress Limit           `json:"per_address"`
	Endpoints  []EndpointLimit `json:"endpoints"`
}

// DefaultRateLimit is generous enough for normal browsing, including offices
// behind one address, and strict on the login endpoint of the admin host.
func DefaultRateLimit(adminHostname string) RateLimitSettings {
	settings := RateLimitSettings{
		Enabled:   true,
		PerIP:     Limit{RPS: 300, Burst: 1000},
		PerSubnet: Limit{RPS: 1500, Burst: 5000},
		Endpoints: []EndpointLimit{},
	}
	if adminHostname != "" {
		settings.Endpoints = append(settings.Endpoints, EndpointLimit{Host: adminHostname, PathPrefix: "/api/v1/session", RPS: 3, Burst: 30, Per: RateEndpointIP})
	}
	return settings
}

func (l Limit) valid() bool {
	if l.RPS == 0 && l.Burst == 0 {
		return true
	}
	return l.RPS >= 1 && l.RPS <= maxRateRPS && l.Burst >= l.RPS && l.Burst <= maxRateBurst
}

func validPathPrefix(prefix string) bool {
	if !strings.HasPrefix(prefix, "/") || len(prefix) > maxPathPrefix {
		return false
	}
	for _, r := range prefix {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Normalize lowercases hosts and prefixes, and reports whether the settings
// are within range. Rules are matched case-insensitively, so two that differ
// only in case count as duplicates.
func (s RateLimitSettings) Normalize() (RateLimitSettings, bool) {
	if !s.PerIP.valid() || !s.PerSubnet.valid() || !s.PerAddress.valid() || len(s.Endpoints) > MaxRateEndpoints {
		return s, false
	}
	endpoints := make([]EndpointLimit, 0, len(s.Endpoints))
	seen := map[string]bool{}
	for _, e := range s.Endpoints {
		e.Host, e.PathPrefix = strings.ToLower(strings.TrimSpace(e.Host)), strings.ToLower(e.PathPrefix)
		key := e.Host + "\x00" + e.PathPrefix
		if (e.Host != "*" && !validation.Hostname(e.Host)) || !validPathPrefix(e.PathPrefix) || (e.Per != RateEndpointTotal && e.Per != RateEndpointIP) ||
			e.RPS < 1 || !(Limit{RPS: e.RPS, Burst: e.Burst}).valid() || seen[key] {
			return s, false
		}
		seen[key] = true
		endpoints = append(endpoints, e)
	}
	s.Endpoints = endpoints
	return s, true
}

// RateLimit returns the stored settings, or the defaults for this Outlet.
func (d *Database) RateLimit(ctx context.Context, adminHostname string) (RateLimitSettings, error) {
	raw, err := getSetting(ctx, d.db, settingRateLimit)
	if err != nil || raw == "" {
		return DefaultRateLimit(adminHostname), err
	}
	var settings RateLimitSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return DefaultRateLimit(adminHostname), nil
	}
	if settings, ok := settings.Normalize(); ok {
		return settings, nil
	}
	return DefaultRateLimit(adminHostname), nil
}

func (d *Database) SetRateLimit(ctx context.Context, settings RateLimitSettings, now time.Time) (RateLimitSettings, error) {
	settings, ok := settings.Normalize()
	if !ok {
		return settings, ErrInvalidRateLimit
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	return settings, putSetting(ctx, d.db, settingRateLimit, string(raw), now)
}

// RenameRateLimitHost keeps saved endpoint rules on the management host when
// a new primary domain changes its name. Defaults follow it on their own.
func (d *Database) RenameRateLimitHost(ctx context.Context, from, to string, now time.Time) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	raw, err := getSetting(ctx, d.db, settingRateLimit)
	if err != nil || raw == "" {
		return err
	}
	var settings RateLimitSettings
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return nil
	}
	changed := false
	for i := range settings.Endpoints {
		if settings.Endpoints[i].Host == from {
			settings.Endpoints[i].Host, changed = to, true
		}
	}
	if !changed {
		return nil
	}
	_, err = d.SetRateLimit(ctx, settings, now)
	return err
}
