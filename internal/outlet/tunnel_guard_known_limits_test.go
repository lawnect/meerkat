package outlet

import (
	"net/netip"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// Every known address may hold many connections, but all of them together are
// bounded, so known addresses can't use up the Outlet's file descriptors.
func TestKnownConnectionsAreBoundedInTotal(t *testing.T) {
	f := newAdmissionGuard()
	f.g.knownTotal = 100
	a, b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	f.g.learnFor("connector-a", a.String())
	f.g.learnFor("connector-b", b.String())
	var releases []func()
	for i := range 100 {
		release, _, why := f.g.admit([]netip.Addr{a, b}[i%2])
		if why != admitted {
			t.Fatalf("connection %d: %v", i+1, why)
		}
		releases = append(releases, release)
	}
	for _, addr := range []netip.Addr{a, b} {
		if got := f.try(addr); got != refusedBusy {
			t.Fatalf("past the total from %v: %v", addr, got)
		}
	}
	// The refused attempts hold nothing, and a release frees a place for anyone.
	if got := f.g.knownConns.total.Load(); got != 100 || stressTunnelInflight(&f.g.knownConns) != 100 {
		t.Fatalf("%d held, want 100", got)
	}
	releases[0]()
	if got := f.pass(b); got != admitted {
		t.Fatalf("after a release: %v", got)
	}
	for _, release := range releases[1:] {
		release()
	}
	if got := f.g.knownConns.total.Load(); got != 0 || stressTunnelInflight(&f.g.knownConns) != 0 {
		t.Fatalf("slots left: %d", got)
	}
}

// One address can't take every place of the total: another Connector's
// connections still get in.
func TestOneKnownAddressCannotTakeEveryPlace(t *testing.T) {
	if guardKnownConcurrent >= guardKnownTotal {
		t.Fatalf("one known address may hold %d of %d", guardKnownConcurrent, guardKnownTotal)
	}
	f := newAdmissionGuard()
	a, b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	f.g.learn(a.String())
	f.g.learn(b.String())
	for i := range guardKnownConcurrent {
		if got := f.try(a); got != admitted {
			t.Fatalf("connection %d: %v", i+1, got)
		}
	}
	if got := f.try(a); got != refusedBusy {
		t.Fatalf("past one address's share: %v", got)
	}
	if got := f.try(b); got != admitted {
		t.Fatalf("another known address: %v", got)
	}
}

// A known address has its own rate limit, far above a Connector's needs.
func TestKnownAddressesAreRateLimited(t *testing.T) {
	f := newAdmissionGuard()
	known, other := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	f.g.learn(known.String())
	f.g.learn(other.String())
	// A Connector that reconnects everything at once after a restart.
	for i := range int(guardKnownBurst) {
		if got := f.pass(known); got != admitted {
			t.Fatalf("connection %d of the burst: %v", i+1, got)
		}
	}
	if got := f.pass(known); got != refusedRate {
		t.Fatalf("past the burst: %v", got)
	}
	// The next address has a bucket of its own.
	if got := f.pass(other); got != admitted {
		t.Fatalf("another known address: %v", got)
	}
	f.clock.advance(time.Second)
	for i := range int(guardKnownPerSecond) {
		if got := f.pass(known); got != admitted {
			t.Fatalf("connection %d of the first second: %v", i+1, got)
		}
	}
	if got := f.pass(known); got != refusedRate {
		t.Fatalf("past one second's worth: %v", got)
	}
	// The allowed ranges are held to it too.
	f.g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{"198.51.100.0/24"}}))
	inside := netip.MustParseAddr("198.51.100.9")
	for range int(guardKnownBurst) {
		f.pass(inside)
	}
	if got := f.pass(inside); got != refusedRate {
		t.Fatalf("an allowed address past the burst: %v", got)
	}
}
