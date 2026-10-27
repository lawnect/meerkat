package outlet

import (
	"io"
	"net"
	"testing"
	"time"
)

const benchmarkBytes = 64 << 20

// newSink reads every connection to its end, then answers with one byte.
func newSink(b testing.TB) net.Listener {
	b.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
				_, _ = conn.Write([]byte{'k'})
			}()
		}
	}()
	return listener
}

// BenchmarkTunnelGuardThroughput moves 64 MiB to a sink directly and through
// the guard, on loopback; the difference is the cost of the extra hop.
func BenchmarkTunnelGuardThroughput(b *testing.B) {
	sink := newSink(b)
	g := startGuard(b, sink.Addr().String())
	g.learn("127.0.0.1")
	chunk := make([]byte, 256<<10)
	for name, address := range map[string]string{"direct": sink.Addr().String(), "guard": g.address().String()} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(benchmarkBytes)
			for range b.N {
				conn, err := net.Dial("tcp", address)
				if err != nil {
					b.Fatal(err)
				}
				for sent := 0; sent < benchmarkBytes; sent += len(chunk) {
					if _, err := conn.Write(chunk); err != nil {
						b.Fatal(err)
					}
				}
				_ = conn.(*net.TCPConn).CloseWrite()
				if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
					b.Fatal(err)
				}
				conn.Close()
			}
		})
	}
}

// BenchmarkTunnelGuardEcho sends 64 MiB through an echo and reads it back at
// the same time, so both directions of the proxy are busy.
func BenchmarkTunnelGuardEcho(b *testing.B) {
	rathole := newEchoRathole(b)
	g := startGuard(b, rathole.address())
	g.learn("127.0.0.1")
	chunk := make([]byte, 256<<10)
	for name, address := range map[string]string{"direct": rathole.address(), "guard": g.address().String()} {
		b.Run(name, func(b *testing.B) {
			b.SetBytes(2 * benchmarkBytes)
			for range b.N {
				conn, err := net.Dial("tcp", address)
				if err != nil {
					b.Fatal(err)
				}
				go func() {
					for sent := 0; sent < benchmarkBytes; sent += len(chunk) {
						if _, err := conn.Write(chunk); err != nil {
							return
						}
					}
					_ = conn.(*net.TCPConn).CloseWrite()
				}()
				if n, err := io.Copy(io.Discard, conn); err != nil || n < benchmarkBytes {
					b.Fatalf("read %d bytes: %v", n, err)
				}
				conn.Close()
			}
		})
	}
}

// BenchmarkTunnelGuardSetup is a new connection's time to its first answer:
// connect, send a byte, read it back from the echo. Direct is the floor; a
// known client adds the admission and the second connection; an unknown one
// also waits for its first bytes before rathole is dialled. The known
// client's guard is a second one in front of the same rathole.
func BenchmarkTunnelGuardSetup(b *testing.B) {
	rathole := newEchoRathole(b)
	// Each connection finds every bucket full, so limits don't get in the way.
	var tick time.Time
	g := startGuard(b, rathole.address(), func(g *tunnelGuard) {
		g.now = func() time.Time { tick = tick.Add(time.Minute); return tick }
	})
	known := startGuard(b, rathole.address())
	known.learn("127.0.0.1")
	for _, tc := range []struct{ name, address string }{
		{"direct", rathole.address()},
		{"known", known.address().String()},
		{"unknown", g.address().String()},
	} {
		b.Run(tc.name, func(b *testing.B) {
			buf := make([]byte, 1)
			for range b.N {
				conn, err := net.Dial("tcp", tc.address)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := conn.Write([]byte{'x'}); err != nil {
					b.Fatal(err)
				}
				if _, err := io.ReadFull(conn, buf); err != nil {
					b.Fatal(err)
				}
				conn.Close()
			}
		})
	}
}
