package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"
)

// ErrInvalidClientBans reports settings outside the limits below.
var ErrInvalidClientBans = errors.New("invalid client bans")

const (
	settingClientBans = "client_bans"

	MinBanMinutes = 1
	MaxBanMinutes = 7 * 24 * 60
	// DefaultBanMinutes is how long a ban lasts unless set otherwise.
	DefaultBanMinutes = 60
	MaxBanExempt      = 50
	// maxBanExemptBytes is the longest entry tried as an address or range;
	// the longest valid one, an IPv6 range, is 43 bytes.
	maxBanExemptBytes = 64
)

// ClientBanSettings say how long a client that matched a block rule with ban
// is refused (1 minute to 7 days), and which clients are never banned: up to
// 50 addresses or ranges (CIDR), IPv4 or IPv6. Entries are kept in canonical
// form: a range as its network address and length ("203.0.113.0/24"), a
// single address bare ("203.0.113.5", whether it was written with /32 or
// /128 or not), an IPv4 address written as IPv6 ("::ffff:203.0.113.5") as
// IPv4, duplicates dropped.
type ClientBanSettings struct {
	Minutes int      `json:"minutes"`
	Exempt  []string `json:"exempt"`
}

// DefaultClientBanSettings ban for an hour and exempt nobody.
func DefaultClientBanSettings() ClientBanSettings {
	return ClientBanSettings{Minutes: DefaultBanMinutes, Exempt: []string{}}
}

// Normalize checks the settings and puts the exempt entries in canonical
// form.
func (s ClientBanSettings) Normalize() (ClientBanSettings, bool) {
	if s.Minutes < MinBanMinutes || s.Minutes > MaxBanMinutes || len(s.Exempt) > MaxBanExempt {
		return s, false
	}
	exempt := make([]string, 0, len(s.Exempt))
	for _, entry := range s.Exempt {
		prefix, ok := ParseBanExempt(entry)
		if !ok {
			return s, false
		}
		canonical := prefix.String()
		if prefix.Bits() == prefix.Addr().BitLen() {
			canonical = prefix.Addr().String()
		}
		if !containsString(exempt, canonical) {
			exempt = append(exempt, canonical)
		}
	}
	s.Exempt = exempt
	return s, true
}

// ParseBanExempt reads an IP address or a CIDR range as the range it covers
// (a single address is a range of one). An IPv4 address written as IPv6 is
// the IPv4 one; zones are refused.
func ParseBanExempt(entry string) (netip.Prefix, bool) {
	entry = strings.TrimSpace(entry)
	if entry == "" || len(entry) > maxBanExemptBytes {
		return netip.Prefix{}, false
	}
	if !strings.Contains(entry, "/") {
		addr, err := netip.ParseAddr(entry)
		if err != nil || addr.Zone() != "" {
			return netip.Prefix{}, false
		}
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), true
	}
	prefix, err := netip.ParsePrefix(entry)
	if err != nil || prefix.Addr().Zone() != "" {
		return netip.Prefix{}, false
	}
	// "::ffff:203.0.113.0/120" is the IPv4 range; a shorter one covers more
	// than IPv4 and stays as written.
	if addr := prefix.Addr(); addr.Is4In6() && prefix.Bits() >= 96 {
		prefix = netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96)
	}
	return prefix.Masked(), true
}

// ClientBans returns the settings, the defaults until saved.
func (d *Database) ClientBans(ctx context.Context) (ClientBanSettings, error) {
	raw, err := getSetting(ctx, d.db, settingClientBans)
	if err != nil || raw == "" {
		return DefaultClientBanSettings(), err
	}
	// Saved settings that can't be read, or that fail the checks, are the
	// defaults.
	var settings ClientBanSettings
	if json.Unmarshal([]byte(raw), &settings) != nil {
		return DefaultClientBanSettings(), nil
	}
	if settings, ok := settings.Normalize(); ok {
		return settings, nil
	}
	return DefaultClientBanSettings(), nil
}

// SetClientBans saves the settings, normalized.
func (d *Database) SetClientBans(ctx context.Context, settings ClientBanSettings, now time.Time) (ClientBanSettings, error) {
	settings, ok := settings.Normalize()
	if !ok {
		return settings, ErrInvalidClientBans
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	return settings, putSetting(ctx, d.db, settingClientBans, string(raw), now)
}
