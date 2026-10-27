package outlet

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// echoRathole stands in for rathole behind the guard. It echoes what it
// reads, and once the client has sent all it will it adds "!bye" and hangs
// up, which a proxy that loses the half-close never gets to hear.
type echoRathole struct {
	listener net.Listener
	accepted atomic.Int64
	wg       sync.WaitGroup

	mu    sync.Mutex
	first []string
	conns map[net.Conn]struct{}
}

func newEchoRathole(t testing.TB) *echoRathole {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	e := &echoRathole{listener: listener, conns: map[net.Conn]struct{}{}}
	e.wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			e.accepted.Add(1)
			e.mu.Lock()
			e.conns[conn] = struct{}{}
			e.mu.Unlock()
			e.wg.Go(func() { e.echo(conn.(*net.TCPConn)) })
		}
	})
	t.Cleanup(e.close)
	return e
}

func (e *echoRathole) address() string { return e.listener.Addr().String() }

func (e *echoRathole) echo(conn *net.TCPConn) {
	defer conn.Close()
	buf := make([]byte, 32<<10)
	for seen := false; ; {
		n, err := conn.Read(buf)
		if n > 0 {
			if !seen {
				e.mu.Lock()
				e.first = append(e.first, string(buf[:n]))
				e.mu.Unlock()
				seen = true
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				_, _ = conn.Write([]byte("!bye"))
				_ = conn.CloseWrite()
			}
			return
		}
	}
}

func (e *echoRathole) firstBytes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.first...)
}

func (e *echoRathole) close() {
	_ = e.listener.Close()
	e.mu.Lock()
	for conn := range e.conns {
		_ = conn.Close()
	}
	e.mu.Unlock()
	e.wg.Wait()
}

// startGuard runs a guard on a loopback port and waits until it listens.
func startGuard(t testing.TB, rathole string, tune ...func(*tunnelGuard)) *tunnelGuard {
	t.Helper()
	g := newTunnelGuard("127.0.0.1:0", rathole, time.Now)
	for _, change := range tune {
		change(g)
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		g.run(ctx)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		g.close()
		<-finished
	})
	select {
	case <-g.bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the guard didn't start listening")
	}
	return g
}

func connectGuard(t testing.TB, g *tunnelGuard) *net.TCPConn {
	t.Helper()
	conn, err := net.Dial("tcp", g.address().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn.(*net.TCPConn)
}

// echoes sends msg and expects it back through the guard.
func echoes(t *testing.T, conn *net.TCPConn, msg string) {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte(msg)); err != nil {
		t.Fatalf("write %q: %v", msg, err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != msg {
		t.Fatalf("echo of %q = %q, %v", msg, got, err)
	}
}

// closedByGuard reports whether the connection is cut (closed or reset)
// within the time, rather than left open.
func closedByGuard(conn *net.TCPConn, within time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(within))
	_, err := conn.Read(make([]byte, 1))
	return err != nil && !isTimeout(err)
}

// turnedAway dials the address and reports whether the connection is cut at
// once: closed or reset after the dial, or reset before it even returns,
// which a reset can win.
func turnedAway(t *testing.T, address string) bool {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return errors.Is(err, syscall.ECONNRESET)
	}
	defer conn.Close()
	return closedByGuard(conn.(*net.TCPConn), 3*time.Second)
}

// within runs f and fails the test if it hasn't returned in time, which a
// shutdown that waits for connections nobody cuts would otherwise hang on.
func within(t testing.TB, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s didn't return", what)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// settled fails the test if goroutines started since before are still
// running a moment later.
func settled(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if now := runtime.NumGoroutine(); now > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("goroutines leaked: %d before, %d after\n%s", before, now, buf[:runtime.Stack(buf, true)])
	}
}

// Bytes pass both ways, an end of data from the client reaches rathole
// while rathole can still answer, the first bytes arrive in one piece, and
// a large transfer in both directions at once arrives intact.
func TestTunnelGuardPassesBytesBothWays(t *testing.T) {
	rathole := newEchoRathole(t)
	g := startGuard(t, rathole.address())

	conn := connectGuard(t, g)
	echoes(t, conn, "hello")
	echoes(t, conn, "second")
	if first := rathole.firstBytes(); len(first) != 1 || first[0] != "hello" {
		t.Fatalf("rathole saw %q first, want the client's first bytes", first)
	}

	payload := make([]byte, 4<<20)
	_, _ = rand.Read(payload)
	sent := make(chan error, 1)
	go func() {
		_, err := conn.Write(payload)
		sent <- err
	}()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	got := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("large echo: %v, equal %v", err, bytes.Equal(got, payload))
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}

	// The half-close: rathole's last words come after the client's end.
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	rest, err := io.ReadAll(conn)
	if err != nil || string(rest) != "!bye" {
		t.Fatalf("after CloseWrite read %q, %v; want rathole's !bye and a clean end", rest, err)
	}
	eventually(t, "the connection to count as ended", func() bool { return g.stats().Active == 0 })
	if stats := g.stats(); stats.Accepted != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// rathole ending first reaches the client too.
func TestTunnelGuardPassesRatholeClosing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_, _ = io.WriteString(conn, "goodbye")
		_ = conn.Close()
	}()
	g := startGuard(t, listener.Addr().String())
	conn := connectGuard(t, g)
	if _, err := conn.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if got, err := io.ReadAll(conn); string(got) != "goodbye" || err != nil {
		t.Fatalf("read %q, %v", got, err)
	}
}

// An unknown client that sends nothing is closed after the time allowed
// and rathole is never dialled for it; one that connects and leaves, as a
// port check does, is not counted as refused.
func TestTunnelGuardClosesSilentClientsBeforeRathole(t *testing.T) {
	rathole := newEchoRathole(t)
	g := startGuard(t, rathole.address(), func(g *tunnelGuard) { g.firstBytes = 400 * time.Millisecond })

	silent := connectGuard(t, g)
	if !closedByGuard(silent, 3*time.Second) {
		t.Fatal("a silent client wasn't closed")
	}
	eventually(t, "the refusal to be counted", func() bool { return g.stats().Refused.Idle == 1 })

	connectGuard(t, g).Close()
	// A client that speaks in time is not idle, however slowly.
	slow := connectGuard(t, g)
	time.Sleep(100 * time.Millisecond)
	echoes(t, slow, "late but in time")

	if n := rathole.accepted.Load(); n != 1 {
		t.Fatalf("rathole was dialled %d times, want 1 (only for the client that spoke)", n)
	}
	eventually(t, "the check to be forgotten", func() bool { return g.unknownConns.total.Load() == 1 })
	if stats := g.stats(); stats.Refused.Idle != 1 || stats.Accepted != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// A known client's silence is its own business: rathole's slots are the
// reason for the time limit, and Connectors keep tunnels open quietly.
func TestTunnelGuardDoesNotTimeKnownClientsOut(t *testing.T) {
	rathole := newEchoRathole(t)
	g := startGuard(t, rathole.address(), func(g *tunnelGuard) { g.firstBytes = 100 * time.Millisecond })
	g.learn("127.0.0.1")
	conn := connectGuard(t, g)
	time.Sleep(300 * time.Millisecond)
	echoes(t, conn, "still here")
	if g.stats().Refused.Idle != 0 {
		t.Fatalf("stats = %+v", g.stats())
	}
}

// One unknown client holds 32 connections at once; a 33rd is closed until
// one ends. The 121st connection in a minute and the 101st in a burst are
// closed too.
func TestTunnelGuardLimitsAnUnknownClientOverSockets(t *testing.T) {
	t.Run("concurrent", func(t *testing.T) {
		rathole := newEchoRathole(t)
		g := startGuard(t, rathole.address())
		held := make([]*net.TCPConn, guardClientConcurrent)
		for i := range held {
			held[i] = connectGuard(t, g)
			echoes(t, held[i], "x")
		}
		if !turnedAway(t, g.address().String()) {
			t.Fatal("a 33rd connection from one client stayed open")
		}
		if stats := g.stats(); stats.Refused.Busy != 1 || stats.Active != guardClientConcurrent {
			t.Fatalf("stats = %+v", stats)
		}
		held[0].Close()
		eventually(t, "a slot to free up", func() bool { return g.unknownConns.total.Load() == guardClientConcurrent-1 })
		echoes(t, connectGuard(t, g), "again")
	})
	t.Run("burst", func(t *testing.T) {
		rathole := newEchoRathole(t)
		clock := &testClock{t: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
		g := startGuard(t, rathole.address(), func(g *tunnelGuard) { g.now = clock.now })
		for range guardTotalBurst {
			conn := connectGuard(t, g)
			echoes(t, conn, "x")
			conn.Close()
		}
		if !turnedAway(t, g.address().String()) {
			t.Fatal("a connection past the burst stayed open")
		}
		if stats := g.stats(); stats.Refused.Rate != 1 {
			t.Fatalf("stats = %+v", stats)
		}
		// Time passes, and tokens come back.
		clock.advance(time.Second)
		echoes(t, connectGuard(t, g), "after a pause")
	})
}

func guardClients(prefix string, n int) []netip.Addr {
	list := make([]netip.Addr, n)
	for i := range list {
		list[i] = netip.MustParseAddr(fmt.Sprintf("%s.%d.%d", prefix, i/250, i%250+1))
	}
	return list
}

type guardFixture struct {
	g     *tunnelGuard
	clock *testClock
}

func newAdmissionGuard() guardFixture {
	clock := &testClock{t: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	return guardFixture{g: newTunnelGuard("127.0.0.1:2333", "127.0.0.1:2334", clock.now), clock: clock}
}

// try asks for a connection and keeps it open when it is admitted.
func (f guardFixture) try(addr netip.Addr) refusal {
	_, _, why := f.g.admit(addr)
	return why
}

// pass asks for a connection that ends at once.
func (f guardFixture) pass(addr netip.Addr) refusal {
	release, _, why := f.g.admit(addr)
	if why == admitted {
		release()
	}
	return why
}

func TestTunnelGuardAdmission(t *testing.T) {
	allowed := []string{"198.51.100.0/24", "2001:db8:aaaa::/48", "203.0.113.9"}
	strict := control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: allowed}
	limited := control.TunnelGuardSettings{Mode: control.TunnelGuardLimited, Allowed: allowed}
	for _, tc := range []struct {
		name     string
		settings control.TunnelGuardSettings
		learned  string // an address that made an authenticated request
		client   string
		want     refusal
	}{
		{"unknown, limited", limited, "", "192.0.2.10", admitted},
		{"unknown, strict", strict, "", "192.0.2.10", refusedStrict},
		{"known, strict", strict, "192.0.2.10", "192.0.2.10", admitted},
		{"known by another address, strict", strict, "192.0.2.11", "192.0.2.10", refusedStrict},
		{"allowed address, strict", strict, "", "203.0.113.9", admitted},
		{"allowed IPv4 range, strict", strict, "", "198.51.100.77", admitted},
		{"allowed IPv6 range, strict", strict, "", "2001:db8:aaaa:5::1", admitted},
		{"just outside the IPv4 range, strict", strict, "", "198.51.101.1", refusedStrict},
		{"just outside the IPv6 range, strict", strict, "", "2001:db8:aaab::1", refusedStrict},
		{"IPv4 address mapped into IPv6, strict", strict, "", "::ffff:198.51.100.77", admitted},
		{"another address of a known IPv6 /64, strict", strict, "2001:db8:1:2::1", "2001:db8:1:2:ffff::9", admitted},
		{"the next IPv6 /64, strict", strict, "2001:db8:1:2::1", "2001:db8:1:3::1", refusedStrict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdmissionGuard()
			f.g.config.Store(compileTunnelGuard(tc.settings))
			if tc.learned != "" {
				f.g.learn(tc.learned)
			}
			if got := f.try(netip.MustParseAddr(tc.client)); got != tc.want {
				t.Fatalf("%s: %v, want %v", tc.client, got, tc.want)
			}
		})
	}
}

func TestTunnelGuardConcurrentLimits(t *testing.T) {
	t.Run("per client, IPv4", func(t *testing.T) {
		f := newAdmissionGuard()
		client, other := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
		for range guardClientConcurrent {
			if got := f.try(client); got != admitted {
				t.Fatalf("%v", got)
			}
		}
		if got := f.try(client); got != refusedBusy {
			t.Fatalf("33rd connection: %v", got)
		}
		if got := f.try(other); got != admitted {
			t.Fatalf("another address of the same /24: %v", got)
		}
	})
	t.Run("per client, an IPv6 /64 is one client", func(t *testing.T) {
		f := newAdmissionGuard()
		for i := range guardClientConcurrent {
			if got := f.try(netip.MustParseAddr(fmt.Sprintf("2001:db8:0:1::%x", i+1))); got != admitted {
				t.Fatalf("connection %d: %v", i, got)
			}
		}
		if got := f.try(netip.MustParseAddr("2001:db8:0:1:dead:beef::1")); got != refusedBusy {
			t.Fatalf("another address of the same /64: %v", got)
		}
		if got := f.try(netip.MustParseAddr("2001:db8:0:2::1")); got != admitted {
			t.Fatalf("the next /64: %v", got)
		}
	})
	t.Run("all unknown clients together", func(t *testing.T) {
		f := newAdmissionGuard()
		all := guardClients("10.1", guardTotalConcurrent+1)
		for i, addr := range all[:guardTotalConcurrent] {
			// Tokens come back as fast as the test spends them.
			f.clock.advance(100 * time.Millisecond)
			if got := f.try(addr); got != admitted {
				t.Fatalf("connection %d: %v", i, got)
			}
		}
		if got := f.try(all[guardTotalConcurrent]); got != refusedBusy {
			t.Fatalf("connection %d: %v", guardTotalConcurrent+1, got)
		}
	})
	t.Run("a connection refused for the overall limit holds nothing of its client's", func(t *testing.T) {
		f := newAdmissionGuard()
		var releases []func()
		for _, addr := range guardClients("10.4", guardTotalConcurrent) {
			f.clock.advance(100 * time.Millisecond)
			release, _, why := f.g.admit(addr)
			if why != admitted {
				t.Fatalf("%v", why)
			}
			releases = append(releases, release)
		}
		late := netip.MustParseAddr("192.0.2.9")
		for range 2 * guardClientConcurrent {
			if got := f.try(late); got != refusedBusy {
				t.Fatalf("with every place taken: %v", got)
			}
		}
		releases[0]()
		if got := f.try(late); got != admitted {
			t.Fatalf("after a place freed up: %v (its earlier refusals still held places of its own)", got)
		}
	})
	t.Run("a refused connection holds nothing", func(t *testing.T) {
		f := newAdmissionGuard()
		client := netip.MustParseAddr("192.0.2.1")
		for range guardClientConcurrent {
			f.try(client)
		}
		for range 50 {
			f.try(client)
		}
		if n := f.g.unknownConns.total.Load(); n != guardClientConcurrent {
			t.Fatalf("%d held, want %d", n, guardClientConcurrent)
		}
	})
	t.Run("known clients have a sanity cap only", func(t *testing.T) {
		f := newAdmissionGuard()
		f.g.learn("192.0.2.1")
		// Unknown clients fill their 256 places, and the known one is unaffected.
		for _, addr := range guardClients("10.2", guardTotalConcurrent) {
			f.clock.advance(100 * time.Millisecond)
			if got := f.try(addr); got != admitted {
				t.Fatalf("%v", got)
			}
		}
		if got := f.try(netip.MustParseAddr("192.0.2.2")); got != refusedBusy {
			t.Fatalf("an unknown client when all 256 places are taken: %v", got)
		}
		known := netip.MustParseAddr("192.0.2.1")
		for i := range guardKnownConcurrent {
			if got := f.try(known); got != admitted {
				t.Fatalf("connection %d of a known client: %v", i+1, got)
			}
		}
		if got := f.try(known); got != refusedBusy {
			t.Fatalf("past the cap: %v", got)
		}
	})
	t.Run("release frees the slot", func(t *testing.T) {
		f := newAdmissionGuard()
		client := netip.MustParseAddr("192.0.2.1")
		var releases []func()
		for range guardClientConcurrent {
			release, _, why := f.g.admit(client)
			if why != admitted {
				t.Fatal(why)
			}
			releases = append(releases, release)
		}
		releases[0]()
		if got := f.try(client); got != admitted {
			t.Fatalf("after a release: %v", got)
		}
	})
}

func TestTunnelGuardRateLimits(t *testing.T) {
	t.Run("per client", func(t *testing.T) {
		f := newAdmissionGuard()
		client, other := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
		// The overall burst is 100, so one client spends 100 at once...
		for i := range guardTotalBurst {
			if got := f.pass(client); got != admitted {
				t.Fatalf("connection %d: %v", i+1, got)
			}
		}
		// ...and after two seconds it has earned 4 of its 120 a minute back
		// (it had 20 left), while the overall limit is full again.
		f.clock.advance(2 * time.Second)
		for i := range 24 {
			if got := f.pass(client); got != admitted {
				t.Fatalf("connection %d after the pause: %v", i+1, got)
			}
		}
		if got := f.pass(client); got != refusedRate {
			t.Fatalf("the client's 121st-in-a-minute connection: %v", got)
		}
		if got := f.pass(other); got != admitted {
			t.Fatalf("another client while the first is limited: %v", got)
		}
		// A minute later the client has 120 again.
		f.clock.advance(time.Minute)
		if got := f.pass(client); got != admitted {
			t.Fatalf("a minute later: %v", got)
		}
	})
	t.Run("all unknown clients together", func(t *testing.T) {
		f := newAdmissionGuard()
		all := guardClients("10.3", guardTotalBurst+guardTotalPerSecond+2)
		for i := range guardTotalBurst {
			if got := f.pass(all[i]); got != admitted {
				t.Fatalf("connection %d: %v", i+1, got)
			}
		}
		if got := f.pass(all[guardTotalBurst]); got != refusedRate {
			t.Fatalf("the 101st connection in a burst: %v", got)
		}
		f.clock.advance(time.Second)
		for i := range guardTotalPerSecond {
			if got := f.pass(all[guardTotalBurst+i]); got != admitted {
				t.Fatalf("connection %d a second later: %v", i+1, got)
			}
		}
		if got := f.pass(all[guardTotalBurst+guardTotalPerSecond]); got != refusedRate {
			t.Fatalf("the 51st connection in a second: %v", got)
		}
	})
	t.Run("known clients are not counted", func(t *testing.T) {
		f := newAdmissionGuard()
		f.g.learn("192.0.2.1")
		known := netip.MustParseAddr("192.0.2.1")
		for i := range 5 * guardTotalBurst {
			if got := f.pass(known); got != admitted {
				t.Fatalf("connection %d of a known client: %v", i+1, got)
			}
		}
		if got := f.pass(netip.MustParseAddr("192.0.2.2")); got != admitted {
			t.Fatalf("an unknown client after a known one's flood: %v", got)
		}
	})
}

// A new setting applies to the next connection.
func TestTunnelGuardSettingsApplyAtOnce(t *testing.T) {
	f := newAdmissionGuard()
	addr := netip.MustParseAddr("192.0.2.1")
	if got := f.pass(addr); got != admitted {
		t.Fatal(got)
	}
	f.g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{}}))
	if got := f.pass(addr); got != refusedStrict {
		t.Fatalf("after strict: %v", got)
	}
	f.g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardStrict, Allowed: []string{"192.0.2.0/24"}}))
	if got := f.pass(addr); got != admitted {
		t.Fatalf("after allowing its range: %v", got)
	}
}

// An allowed range is known without a heartbeat, and limited mode lifts the
// unknown clients' limits for it.
func TestTunnelGuardAllowedRangesAreNotLimited(t *testing.T) {
	f := newAdmissionGuard()
	f.g.config.Store(compileTunnelGuard(control.TunnelGuardSettings{Mode: control.TunnelGuardLimited, Allowed: []string{"192.0.2.0/24"}}))
	inside := netip.MustParseAddr("192.0.2.1")
	for i := range 2 * guardClientConcurrent {
		if got := f.try(inside); got != admitted {
			t.Fatalf("connection %d from the allowed range: %v", i+1, got)
		}
	}
	outside := netip.MustParseAddr("192.0.3.1")
	for range guardClientConcurrent {
		f.try(outside)
	}
	if got := f.try(outside); got != refusedBusy {
		t.Fatalf("outside the range: %v", got)
	}
}

func TestKnownClientsExpireAndStayBounded(t *testing.T) {
	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	key := func(i int) netip.Prefix {
		return clientPrefix(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}))
	}

	t.Run("after 15 minutes", func(t *testing.T) {
		known := newKnownClients()
		known.learn(key(1), now)
		known.learn(key(2), now.Add(10*time.Minute))
		if !known.has(key(1), now.Add(15*time.Minute-time.Second)) {
			t.Fatal("forgotten after 14:59")
		}
		if known.has(key(1), now.Add(15*time.Minute)) {
			t.Fatal("still known after 15 minutes")
		}
		if !known.has(key(2), now.Add(15*time.Minute)) {
			t.Fatal("an address seen later was forgotten with the first")
		}
		// Another authenticated request starts the 15 minutes again.
		known.learn(key(2), now.Add(24*time.Minute))
		if !known.has(key(2), now.Add(38*time.Minute)) || known.has(key(2), now.Add(39*time.Minute)) {
			t.Fatal("a later request didn't extend the time")
		}
		if got := known.list(now.Add(time.Hour)); len(got) != 0 {
			t.Fatalf("expired entries listed: %v", got)
		}
	})
	t.Run("listing and learning drop what expired without a lookup", func(t *testing.T) {
		known := newKnownClients()
		known.learn(key(1), now)
		known.learn(key(2), now.Add(10*time.Minute))
		if got := known.list(now.Add(20 * time.Minute)); len(got) != 1 || got[0].key != key(2) {
			t.Fatalf("list = %+v, want only the address seen 10 minutes after the first", got)
		}
		known.learn(key(3), now.Add(30*time.Minute))
		if got := known.list(now.Add(30 * time.Minute)); len(got) != 1 || got[0].key != key(3) || len(known.items) != 1 {
			t.Fatalf("after learning: list = %+v, %d kept", got, len(known.items))
		}
	})
	t.Run("4096, the oldest seen goes first", func(t *testing.T) {
		known := newKnownClients()
		for i := range guardKnownClients {
			known.learn(key(i), now.Add(time.Duration(i)*time.Millisecond))
		}
		// Seeing the oldest again makes the next one the oldest.
		known.learn(key(0), now.Add(10*time.Second))
		known.learn(key(guardKnownClients), now.Add(11*time.Second))
		if got := known.list(now.Add(12 * time.Second)); len(got) != guardKnownClients {
			t.Fatalf("%d known, want %d", len(got), guardKnownClients)
		}
		if !known.has(key(0), now.Add(12*time.Second)) || known.has(key(1), now.Add(12*time.Second)) || !known.has(key(2), now.Add(12*time.Second)) {
			t.Fatal("the entry seen longest ago wasn't the one dropped")
		}
	})
	t.Run("newest first, IPv6 by /64", func(t *testing.T) {
		known := newKnownClients()
		known.learn(clientPrefix(netip.MustParseAddr("192.0.2.1")), now)
		known.learn(clientPrefix(netip.MustParseAddr("2001:db8:0:1::5")), now.Add(time.Minute))
		known.learn(clientPrefix(netip.MustParseAddr("2001:db8:0:1:9::7")), now.Add(2*time.Minute))
		got := known.list(now.Add(3 * time.Minute))
		if len(got) != 2 || clientName(got[0].key) != "2001:db8:0:1::/64" || clientName(got[1].key) != "192.0.2.1" || !got[0].seen.Equal(now.Add(2*time.Minute)) {
			t.Fatalf("list = %+v", got)
		}
	})
}

// Closing the guard stops the listener and cuts proxied connections, and
// nothing keeps running afterwards.
func TestTunnelGuardShutdownCutsConnectionsAndLeaksNothing(t *testing.T) {
	before := runtime.NumGoroutine()
	rathole := newEchoRathole(t)
	g := newTunnelGuard("127.0.0.1:0", rathole.address(), time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		g.run(ctx)
		close(finished)
	}()
	<-g.bound
	address := g.address().String()

	proxied := connectGuard(t, g)
	echoes(t, proxied, "up")
	waiting := connectGuard(t, g) // an unknown client that hasn't spoken
	eventually(t, "both to be admitted as unknown", func() bool { return g.unknownConns.total.Load() == 2 })
	g.learn("127.0.0.1")
	known := connectGuard(t, g)
	echoes(t, known, "known")
	eventually(t, "connections to be admitted", func() bool { return g.stats().Active == 2 })

	within(t, "closing the guard", g.close)
	for name, conn := range map[string]*net.TCPConn{"proxied": proxied, "waiting": waiting, "known": known} {
		if !closedByGuard(conn, 3*time.Second) {
			t.Errorf("the %s connection survived the shutdown", name)
		}
	}
	if conn, err := net.DialTimeout("tcp", address, time.Second); err == nil {
		conn.Close()
		t.Error("the port still accepts connections")
	}
	<-finished
	if stats := g.stats(); stats.Active != 0 {
		t.Fatalf("stats = %+v", stats)
	}
	proxied.Close()
	waiting.Close()
	known.Close()
	rathole.close()
	settled(t, before)
}

// Connections that end in every way leave no goroutines behind.
func TestTunnelGuardLeavesNoGoroutinesBehind(t *testing.T) {
	before := runtime.NumGoroutine()
	rathole := newEchoRathole(t)
	g := newTunnelGuard("127.0.0.1:0", rathole.address(), time.Now)
	g.firstBytes = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		g.run(ctx)
		close(finished)
	}()
	<-g.bound

	for i := range 40 {
		conn, err := net.Dial("tcp", g.address().String())
		if err != nil {
			t.Fatal(err)
		}
		tcp := conn.(*net.TCPConn)
		switch i % 4 {
		case 0: // talks, then both sides finish politely
			echoes(t, tcp, "x")
			_ = tcp.CloseWrite()
			_, _ = io.Copy(io.Discard, tcp)
		case 1: // talks, then vanishes
			echoes(t, tcp, "x")
		case 2: // never speaks
		case 3: // resets with data unread
			_, _ = tcp.Write(bytes.Repeat([]byte("y"), 100000))
			_ = tcp.SetLinger(0)
		}
		tcp.Close()
	}
	eventually(t, "every connection to end", func() bool { return g.stats().Active == 0 && g.unknownConns.total.Load() == 0 })
	cancel()
	g.close()
	<-finished
	rathole.close()
	settled(t, before)
}

// When rathole isn't there the client is closed and the guard goes on.
func TestTunnelGuardClosesClientsWhenRatholeIsDown(t *testing.T) {
	down, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := down.Addr().String()
	down.Close()
	g := startGuard(t, address)
	conn := connectGuard(t, g)
	_, _ = conn.Write([]byte("hello"))
	if !closedByGuard(conn, 3*time.Second) {
		t.Fatal("a client stayed connected to a guard whose rathole is down")
	}
	eventually(t, "the connection to end", func() bool { return g.stats().Active == 0 })
}

// An address that is taken, as it is while the old rathole still holds the
// port after an upgrade, is retried until it is free; the guard never ends.
func TestTunnelGuardRetriesABusyPort(t *testing.T) {
	rathole := newEchoRathole(t)
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := holder.Addr().String()
	g := newTunnelGuard(address, rathole.address(), time.Now)
	g.retryFrom, g.retryTo = 10*time.Millisecond, 40*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		g.run(ctx)
		close(finished)
	}()
	defer g.close()
	eventually(t, "retries", func() bool { return g.bindFailures.Load() >= 3 })
	select {
	case <-finished:
		t.Fatal("the guard gave up on a busy port")
	case <-g.bound:
		t.Fatal("the guard bound a port that is taken")
	default:
	}
	holder.Close()
	select {
	case <-g.bound:
	case <-time.After(3 * time.Second):
		t.Fatal("the guard didn't take the port once it was free")
	}
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	echoes(t, conn.(*net.TCPConn), "through")

	// Closing during the wait also ends it.
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	waiting := newTunnelGuard(busy.Addr().String(), rathole.address(), time.Now)
	waiting.retryFrom = 10 * time.Millisecond
	ended := make(chan struct{})
	go func() {
		waiting.run(context.Background())
		close(ended)
	}()
	eventually(t, "a failed attempt", func() bool { return waiting.bindFailures.Load() >= 1 })
	waiting.close()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("closing didn't end the wait for the port")
	}
}
