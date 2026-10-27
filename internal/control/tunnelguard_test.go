package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func openTunnelGuardDatabase(t *testing.T) *Database {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTunnelGuardRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openTunnelGuardDatabase(t)
	got, err := db.TunnelGuard(ctx)
	if err != nil || got.Mode != TunnelGuardLimited || got.Allowed == nil || len(got.Allowed) != 0 {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	saved, err := db.SetTunnelGuard(ctx, TunnelGuardSettings{Mode: TunnelGuardStrict, Allowed: []string{" 203.0.113.7 ", "2001:DB8::/32"}}, time.Now())
	if err != nil || saved.Mode != TunnelGuardStrict || !slices.Equal(saved.Allowed, []string{"203.0.113.7", "2001:db8::/32"}) {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if got, err := db.TunnelGuard(ctx); err != nil || got.Mode != TunnelGuardStrict || !slices.Equal(got.Allowed, saved.Allowed) {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	// A saved value that can't be used is the default, not a failure.
	if err := putSetting(ctx, db.db, settingTunnelGuard, `{"mode":"open","allowed":[]}`, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.TunnelGuard(ctx); err != nil || got.Mode != TunnelGuardLimited {
		t.Fatalf("unusable = %+v, %v", got, err)
	}
}

func TestTunnelGuardNormalizesAndRejects(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed []string
		want    []string // nil: invalid
	}{
		{"none", nil, []string{}},
		{"address", []string{"203.0.113.7"}, []string{"203.0.113.7"}},
		{"a host range is an address", []string{"203.0.113.7/32", "2001:db8::1/128"}, []string{"203.0.113.7", "2001:db8::1"}},
		{"host bits are cleared", []string{"203.0.113.77/24", "2001:db8:0:1:5::1/64"}, []string{"203.0.113.0/24", "2001:db8:0:1::/64"}},
		{"duplicates in any spelling", []string{"203.0.113.7", "203.0.113.7/32", "::ffff:203.0.113.7", "2001:DB8::1", "2001:db8:0::1"}, []string{"203.0.113.7", "2001:db8::1"}},
		{"an IPv4 range written as IPv6", []string{"::ffff:203.0.113.0/120"}, []string{"203.0.113.0/24"}},
		{"not an address", []string{"example.com"}, nil},
		{"empty entry", []string{" "}, nil},
		{"prefix too long", []string{"203.0.113.0/33"}, nil},
		{"zone", []string{"fe80::1%eth0"}, nil},
		{"mapped range wider than IPv4", []string{"::ffff:0.0.0.0/64"}, nil},
	} {
		got, ok := TunnelGuardSettings{Mode: TunnelGuardLimited, Allowed: tc.allowed}.Normalize()
		if ok != (tc.want != nil) || ok && !slices.Equal(got.Allowed, tc.want) {
			t.Errorf("%s: %q -> %q, ok=%v, want %q", tc.name, tc.allowed, got.Allowed, ok, tc.want)
		}
	}
	for _, mode := range []string{"", "open", "Strict"} {
		if _, ok := (TunnelGuardSettings{Mode: mode}).Normalize(); ok {
			t.Errorf("mode %q accepted", mode)
		}
	}
}

func TestTunnelGuardLimitsTheRanges(t *testing.T) {
	ctx := context.Background()
	db := openTunnelGuardDatabase(t)
	ranges := func(n int) []string {
		list := make([]string, n)
		for i := range list {
			list[i] = fmt.Sprintf("10.0.%d.0/24", i)
		}
		return list
	}
	if saved, err := db.SetTunnelGuard(ctx, TunnelGuardSettings{Mode: TunnelGuardLimited, Allowed: ranges(MaxTunnelGuardAllowed)}, time.Now()); err != nil || len(saved.Allowed) != MaxTunnelGuardAllowed {
		t.Fatalf("%d ranges: %v", MaxTunnelGuardAllowed, err)
	}
	// The list as sent counts, even where entries collapse into fewer.
	repeated := slices.Repeat([]string{"10.0.0.1"}, MaxTunnelGuardAllowed+1)
	for _, allowed := range [][]string{ranges(MaxTunnelGuardAllowed + 1), repeated} {
		if _, err := db.SetTunnelGuard(ctx, TunnelGuardSettings{Mode: TunnelGuardLimited, Allowed: allowed}, time.Now()); !errors.Is(err, ErrInvalidTunnelGuard) {
			t.Fatalf("%d ranges: %v", len(allowed), err)
		}
	}
}
