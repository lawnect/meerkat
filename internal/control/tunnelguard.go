package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"
)

var ErrInvalidTunnelGuard = errors.New("invalid tunnel guard")

const (
	settingTunnelGuard = "tunnel_guard"

	// TunnelGuardLimited holds unknown clients to per-client and overall
	// limits; TunnelGuardStrict admits only known Connectors and the
	// allowed ranges.
	TunnelGuardLimited = "limited"
	TunnelGuardStrict  = "strict"

	MaxTunnelGuardAllowed = 50
)

// TunnelGuardSettings say how the Outlet treats connections to the public
// tunnel port from clients it doesn't know. Allowed lists IP addresses and
// CIDR ranges that are always admitted like a known Connector.
type TunnelGuardSettings struct {
	Mode    string   `json:"mode"`
	Allowed []string `json:"allowed"`
}

// DefaultTunnelGuard limits unknown clients and allows no range.
func DefaultTunnelGuard() TunnelGuardSettings {
	return TunnelGuardSettings{Mode: TunnelGuardLimited, Allowed: []string{}}
}

// Normalize reports whether the settings are valid and puts the ranges in
// one form: lowercase, an address with no prefix length when it is a single
// one, a range with its host bits cleared, an IPv4-mapped IPv6 address as
// IPv4, and each range once, in the order given.
func (s TunnelGuardSettings) Normalize() (TunnelGuardSettings, bool) {
	if (s.Mode != TunnelGuardLimited && s.Mode != TunnelGuardStrict) || len(s.Allowed) > MaxTunnelGuardAllowed {
		return s, false
	}
	allowed := make([]string, 0, len(s.Allowed))
	seen := map[string]bool{}
	for _, raw := range s.Allowed {
		entry, ok := normalizeRange(strings.TrimSpace(raw))
		if !ok {
			return s, false
		}
		if !seen[entry] {
			seen[entry] = true
			allowed = append(allowed, entry)
		}
	}
	s.Allowed = allowed
	return s, true
}

func normalizeRange(raw string) (string, bool) {
	if !strings.Contains(raw, "/") {
		addr, err := netip.ParseAddr(raw)
		if err != nil || addr.Zone() != "" {
			return "", false
		}
		return addr.Unmap().String(), true
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Zone() != "" {
		return "", false
	}
	addr, bits := prefix.Addr(), prefix.Bits()
	if addr.Is4In6() {
		// An IPv4 range written as ::ffff:a.b.c.d/96+n.
		if bits < 96 {
			return "", false
		}
		addr, bits = addr.Unmap(), bits-96
	}
	prefix = netip.PrefixFrom(addr, bits).Masked()
	if bits == addr.BitLen() {
		return prefix.Addr().String(), true
	}
	return prefix.String(), true
}

// TunnelGuard returns the stored settings, or the defaults.
func (d *Database) TunnelGuard(ctx context.Context) (TunnelGuardSettings, error) {
	raw, err := getSetting(ctx, d.db, settingTunnelGuard)
	if err != nil || raw == "" {
		return DefaultTunnelGuard(), err
	}
	var settings TunnelGuardSettings
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return DefaultTunnelGuard(), nil
	}
	if settings, ok := settings.Normalize(); ok {
		return settings, nil
	}
	return DefaultTunnelGuard(), nil
}

func (d *Database) SetTunnelGuard(ctx context.Context, settings TunnelGuardSettings, now time.Time) (TunnelGuardSettings, error) {
	settings, ok := settings.Normalize()
	if !ok {
		return settings, ErrInvalidTunnelGuard
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	return settings, putSetting(ctx, d.db, settingTunnelGuard, string(raw), now)
}
