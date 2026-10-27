package outlet

import (
	"container/list"
	"context"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// The public tunnel port. rathole's Noise_NK server makes anyone who opens
// a connection pay for an X25519 handshake before it checks a token, and it
// has no limits of its own, so a flood of anonymous handshakes would take
// every tunnel down. The Outlet listens on the public port instead and
// admits a connection before rathole sees a byte; rathole listens on a
// loopback address in the Outlet's network namespace.
const (
	// DefaultTunnelInternalAddress is where rathole listens when outlet.json
	// doesn't say.
	DefaultTunnelInternalAddress = "127.0.0.1:2334"

	// A known client is a Connector's address: it gets only a sanity cap.
	guardKnownConcurrent = 4096
	guardKnownClients    = 4096
	guardKnownTTL        = 15 * time.Minute

	// An unknown client's limits, each of its own and all of them together.
	guardClientConcurrent = 32
	guardClientPerMinute  = 120
	guardTotalConcurrent  = 256
	guardTotalPerSecond   = 50
	guardTotalBurst       = 100
	// guardFirstBytes is how long an unknown client may stay silent before
	// it is closed, without rathole having been dialled.
	guardFirstBytes  = 10 * time.Second
	guardDialTimeout = 5 * time.Second
	// guardBuckets bounds the clients whose rate is remembered.
	guardBuckets = 16384
	// guardFirstRead is the most of its first bytes the guard holds back.
	guardFirstRead = 4096
)

// guardConfig is the compiled, immutable form of the settings.
type guardConfig struct {
	strict  bool
	allowed []netip.Prefix
}

func compileTunnelGuard(settings control.TunnelGuardSettings) *guardConfig {
	config := &guardConfig{strict: settings.Mode == control.TunnelGuardStrict}
	for _, entry := range settings.Allowed {
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			config.allowed = append(config.allowed, prefix)
		} else if addr, err := netip.ParseAddr(entry); err == nil {
			config.allowed = append(config.allowed, netip.PrefixFrom(addr, addr.BitLen()))
		}
	}
	return config
}

func (c *guardConfig) allows(addr netip.Addr) bool {
	for _, prefix := range c.allowed {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// clientPrefix is the client a connection belongs to: an IPv4 address or an
// IPv6 /64, which is what one subscriber is commonly given.
func clientPrefix(addr netip.Addr) netip.Prefix {
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return netip.PrefixFrom(addr, 32)
	}
	return netip.PrefixFrom(addr, 64).Masked()
}

// clientName is how the console shows a client.
func clientName(key netip.Prefix) string {
	if key.Addr().Is4() {
		return key.Addr().String()
	}
	return key.String()
}

type knownClient struct {
	key  netip.Prefix
	seen time.Time
}

// knownClients are the addresses that made an authenticated Connector API
// request lately. The table is bounded: the one seen longest ago goes first.
type knownClients struct {
	mu    sync.Mutex
	items map[netip.Prefix]*list.Element
	order *list.List // newest first
}

func newKnownClients() *knownClients {
	return &knownClients{items: map[netip.Prefix]*list.Element{}, order: list.New()}
}

func (k *knownClients) expire(now time.Time) {
	for back := k.order.Back(); back != nil && now.Sub(back.Value.(*knownClient).seen) >= guardKnownTTL; back = k.order.Back() {
		delete(k.items, back.Value.(*knownClient).key)
		k.order.Remove(back)
	}
}

func (k *knownClients) learn(key netip.Prefix, now time.Time) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.expire(now)
	if element, ok := k.items[key]; ok {
		element.Value.(*knownClient).seen = now
		k.order.MoveToFront(element)
		return
	}
	k.items[key] = k.order.PushFront(&knownClient{key: key, seen: now})
	for k.order.Len() > guardKnownClients {
		oldest := k.order.Back()
		delete(k.items, oldest.Value.(*knownClient).key)
		k.order.Remove(oldest)
	}
}

func (k *knownClients) has(key netip.Prefix, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	element, ok := k.items[key]
	if !ok {
		return false
	}
	if now.Sub(element.Value.(*knownClient).seen) >= guardKnownTTL {
		delete(k.items, key)
		k.order.Remove(element)
		return false
	}
	return true
}

// list returns the known clients, newest first.
func (k *knownClients) list(now time.Time) []knownClient {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.expire(now)
	clients := make([]knownClient, 0, k.order.Len())
	for element := k.order.Front(); element != nil; element = element.Next() {
		clients = append(clients, *element.Value.(*knownClient))
	}
	return clients
}

// refusal is why the guard turned a connection away.
type refusal int

const (
	admitted refusal = iota
	refusedRate
	refusedBusy
	refusedStrict
)

// tunnelGuardStats count connections since the Outlet started.
type tunnelGuardStats struct {
	Active   int64 `json:"active"`
	Accepted int64 `json:"accepted"`
	Refused  struct {
		Rate   int64 `json:"rate"`
		Busy   int64 `json:"busy"`
		Idle   int64 `json:"idle"`
		Strict int64 `json:"strict"`
	} `json:"refused"`
}

// tunnelGuard admits connections to the public tunnel port and proxies the
// ones it admits to rathole.
type tunnelGuard struct {
	listen, internal string
	now              func() time.Time
	config           atomic.Pointer[guardConfig]
	known            *knownClients
	// knownConns and unknownConns count open connections per client; the
	// unknown ones also in total.
	knownConns, unknownConns inflight
	buckets                  *bucketStore

	active, accepted                                     atomic.Int64
	refusedRate, refusedBusy, refusedIdle, refusedStrict atomic.Int64
	// bindFailures counts the attempts to listen that failed.
	bindFailures atomic.Int64

	// These are the limits above; tests shorten them.
	firstBytes, dialTimeout time.Duration
	retryFrom, retryTo      time.Duration

	// bound is closed once the guard listens.
	bound     chan struct{}
	boundOnce sync.Once
	done      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	wg        sync.WaitGroup

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
	closed   bool

	lastDialWarning atomic.Int64
}

func newTunnelGuard(listen, internal string, now func() time.Time) *tunnelGuard {
	g := &tunnelGuard{
		listen: listen, internal: internal, now: now,
		known: newKnownClients(),
		// A client's bucket is full again within a minute, so forgetting it
		// after 2 minutes of silence changes nothing.
		buckets:    newBucketStore(64, guardBuckets, 2*time.Minute),
		firstBytes: guardFirstBytes, dialTimeout: guardDialTimeout,
		retryFrom: time.Second, retryTo: 30 * time.Second,
		bound: make(chan struct{}), done: make(chan struct{}),
		conns: map[net.Conn]struct{}{},
	}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.config.Store(compileTunnelGuard(control.DefaultTunnelGuard()))
	return g
}

// learn makes the address of an authenticated Connector request known.
func (g *tunnelGuard) learn(address string) {
	if addr, err := netip.ParseAddr(address); err == nil {
		g.known.learn(clientPrefix(addr), g.now())
	}
}

// admit decides about a connection from addr. When it admits, release must
// be called once the connection ends, and unknown says the client must prove
// itself by sending first.
func (g *tunnelGuard) admit(addr netip.Addr) (release func(), unknown bool, why refusal) {
	addr = addr.Unmap().WithZone("")
	key := clientPrefix(addr)
	name := key.String()
	config, now := g.config.Load(), g.now()
	if config.allows(addr) || g.known.has(key, now) {
		if !g.knownConns.acquire(name, guardKnownConcurrent) {
			return nil, false, refusedBusy
		}
		return func() { g.knownConns.release(name) }, false, admitted
	}
	if config.strict {
		return nil, true, refusedStrict
	}
	if !g.unknownConns.acquire(name, guardClientConcurrent) {
		return nil, true, refusedBusy
	}
	if !g.unknownConns.acquireTotal(guardTotalConcurrent) {
		g.unknownConns.release(name)
		return nil, true, refusedBusy
	}
	release = func() {
		g.unknownConns.total.Add(-1)
		g.unknownConns.release(name)
	}
	if g.buckets.take(now, []limit{
		{key: "client|" + name, rps: guardClientPerMinute / 60, burst: guardClientPerMinute},
		{key: "all", rps: guardTotalPerSecond, burst: guardTotalBurst},
	}) >= 0 {
		release()
		return nil, true, refusedRate
	}
	return release, true, admitted
}

func (g *tunnelGuard) stats() tunnelGuardStats {
	var stats tunnelGuardStats
	stats.Active, stats.Accepted = g.active.Load(), g.accepted.Load()
	stats.Refused.Rate, stats.Refused.Busy = g.refusedRate.Load(), g.refusedBusy.Load()
	stats.Refused.Idle, stats.Refused.Strict = g.refusedIdle.Load(), g.refusedStrict.Load()
	return stats
}

// run listens until ctx ends or the guard is closed. If the address is
// taken, which an older rathole still holding it after an upgrade causes,
// it tries again with growing pauses instead of giving up: the Outlet and
// its tunnels never depend on it.
func (g *tunnelGuard) run(ctx context.Context) {
	listener := g.listenUntilBound(ctx)
	if listener == nil {
		return
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		_ = listener.Close()
		return
	}
	g.listener = listener
	g.mu.Unlock()
	stop := context.AfterFunc(ctx, g.close)
	defer stop()
	g.boundOnce.Do(func() { close(g.bound) })
	slog.Info("tunnel port protection is on", "listen", listener.Addr().String(), "rathole", g.internal)
	g.accept(listener)
}

func (g *tunnelGuard) listenUntilBound(ctx context.Context) net.Listener {
	lc := net.ListenConfig{KeepAliveConfig: net.KeepAliveConfig{Enable: true, Idle: 30 * time.Second, Interval: 15 * time.Second, Count: 4}}
	delay := g.retryFrom
	for {
		listener, err := lc.Listen(ctx, "tcp", g.listen)
		if err == nil {
			return listener
		}
		g.bindFailures.Add(1)
		slog.Warn("the tunnel port can't be bound yet, trying again: until rathole has moved to its new address, the Outlet can't protect it", "listen", g.listen, "error", err.Error(), "retry_in", delay.String())
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-g.done:
			timer.Stop()
			return nil
		}
		delay = min(delay*2, g.retryTo)
	}
}

func (g *tunnelGuard) accept(listener net.Listener) {
	var pause time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			// Out of descriptors or the like: wait for some to free up.
			pause = min(max(pause*2, 5*time.Millisecond), time.Second)
			timer := time.NewTimer(pause)
			select {
			case <-timer.C:
			case <-g.done:
				timer.Stop()
				return
			}
			continue
		}
		pause = 0
		if tcp, ok := conn.(*net.TCPConn); ok {
			g.handle(tcp)
		} else {
			_ = conn.Close()
		}
	}
}

// handle admits a new connection or closes it at once. A reset instead of a
// close leaves no socket behind in the kernel while a flood lasts.
func (g *tunnelGuard) handle(conn *net.TCPConn) {
	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		_ = conn.Close()
		return
	}
	release, unknown, why := g.admit(remote.AddrPort().Addr())
	switch why {
	case refusedRate:
		g.refusedRate.Add(1)
	case refusedBusy:
		g.refusedBusy.Add(1)
	case refusedStrict:
		g.refusedStrict.Add(1)
	}
	if why != admitted {
		_ = conn.SetLinger(0)
		_ = conn.Close()
		return
	}
	if !g.start(conn) {
		release()
		_ = conn.Close()
		return
	}
	go g.serve(conn, release, unknown)
}

// start registers a connection and a goroutine for it, unless the guard is
// closing.
func (g *tunnelGuard) start(conn net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.conns[conn] = struct{}{}
	g.wg.Add(1)
	return true
}

// remember registers a connection that belongs to a goroutine already started.
func (g *tunnelGuard) remember(conn net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.conns[conn] = struct{}{}
	return true
}

func (g *tunnelGuard) forget(conn net.Conn) {
	g.mu.Lock()
	delete(g.conns, conn)
	g.mu.Unlock()
}

func isTimeout(err error) bool {
	var timeout net.Error
	return errors.As(err, &timeout) && timeout.Timeout()
}

// serve proxies one admitted connection to rathole. An unknown client's
// first bytes are read before rathole is dialled, so a client that connects
// and says nothing never holds one of rathole's slots, and are then passed
// on.
func (g *tunnelGuard) serve(client *net.TCPConn, release func(), unknown bool) {
	defer g.wg.Done()
	defer g.forget(client)
	defer release()
	defer client.Close()
	var first []byte
	if unknown {
		buf := make([]byte, guardFirstRead)
		_ = client.SetReadDeadline(time.Now().Add(g.firstBytes))
		n, err := client.Read(buf)
		if n == 0 {
			// Silence is refused; a client that connects and leaves, as a port
			// check does, is not.
			if isTimeout(err) {
				g.refusedIdle.Add(1)
				_ = client.SetLinger(0)
			}
			return
		}
		first = buf[:n]
		_ = client.SetReadDeadline(time.Time{})
	}
	g.accepted.Add(1)
	g.active.Add(1)
	defer g.active.Add(-1)
	upstream, err := g.dial()
	if err != nil {
		g.warnDial(err)
		return
	}
	defer upstream.Close()
	defer g.forget(upstream)
	if !g.remember(upstream) {
		return
	}
	if len(first) > 0 {
		_ = upstream.SetWriteDeadline(time.Now().Add(g.dialTimeout))
		if _, err := upstream.Write(first); err != nil {
			return
		}
		_ = upstream.SetWriteDeadline(time.Time{})
	}
	pipe(client, upstream)
}

func (g *tunnelGuard) dial() (*net.TCPConn, error) {
	dialer := net.Dialer{Timeout: g.dialTimeout, KeepAliveConfig: net.KeepAliveConfig{Enable: true, Idle: 30 * time.Second, Interval: 15 * time.Second, Count: 4}}
	conn, err := dialer.DialContext(g.ctx, "tcp", g.internal)
	if err != nil {
		return nil, err
	}
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		_ = conn.Close()
		return nil, errors.New("not a TCP connection")
	}
	return tcp, nil
}

// warnDial says that rathole can't be reached, at most once a minute: while
// it is down every connection fails the same way.
func (g *tunnelGuard) warnDial(err error) {
	now := time.Now().UnixNano()
	last := g.lastDialWarning.Load()
	if now-last < int64(time.Minute) || !g.lastDialWarning.CompareAndSwap(last, now) {
		return
	}
	slog.Warn("the tunnel port can't reach rathole", "rathole", g.internal, "error", err.Error())
}

// pipe copies both ways between two TCP connections, which the runtime
// splices in the kernel on Linux. An end of data goes on as a half-close so
// the other direction can finish; a failure closes both.
func pipe(a, b *net.TCPConn) {
	forward := func(dst, src *net.TCPConn) {
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			_ = src.Close()
			return
		}
		_ = dst.CloseWrite()
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		forward(b, a)
	}()
	forward(a, b)
	<-finished
}

// close stops listening and cuts every connection, waiting for the
// goroutines to end.
func (g *tunnelGuard) close() {
	if g == nil {
		return
	}
	g.closeOnce.Do(func() {
		close(g.done)
		g.cancel()
		g.mu.Lock()
		g.closed = true
		listener := g.listener
		conns := slices.Collect(maps.Keys(g.conns))
		g.mu.Unlock()
		if listener != nil {
			_ = listener.Close()
		}
		for _, conn := range conns {
			_ = conn.Close()
		}
		g.wg.Wait()
	})
}

// address is where the guard listens, once it does.
func (g *tunnelGuard) address() net.Addr {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.listener == nil {
		return nil
	}
	return g.listener.Addr()
}
