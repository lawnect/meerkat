package outlet

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

// runAs is publicBench.run for a client, as Caddy forwards it.
func (p publicBench) runAs(b *testing.B, ip, host, path string, want int) {
	// One client sends them all, which the default rate limit would refuse.
	p.store.limiter = nil
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		w := &discardWriter{header: http.Header{}}
		for pb.Next() {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = host
			r.Header.Set("X-Forwarded-For", ip)
			clear(w.header)
			w.code = 200
			p.handler.ServeHTTP(w, r)
			if w.code != want {
				b.Errorf("%s%s: %d, want %d", host, path, w.code, want)
				return
			}
		}
	})
}

// BenchmarkPublicBans is what banning costs the public path: nothing while
// nobody is banned, one map lookup while some are, and the 403 itself for a
// banned client, compared with the 403 of a block rule that does not ban.
func BenchmarkPublicBans(b *testing.B) {
	const host, client = "app.example.com", "203.0.113.7"
	bans := func(p publicBench, n int) {
		for i := range n {
			key := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
			p.store.bans.add(key, "honeypot", host, "/.env", time.Now(), time.Hour)
		}
	}
	b.Run("no_bans", func(b *testing.B) {
		p := newPublicBench(b, 0)
		p.runAs(b, client, host, "/", 200)
	})
	b.Run("10k_bans_client_not_banned", func(b *testing.B) {
		p := newPublicBench(b, 0)
		bans(p, 10000)
		p.runAs(b, client, host, "/", 200)
	})
	b.Run("client_banned", func(b *testing.B) {
		p := newPublicBench(b, 0)
		bans(p, 10000)
		key, _, _ := banKey(client)
		p.store.bans.add(key, "honeypot", host, "/.env", time.Now(), time.Hour)
		p.runAs(b, client, host, "/", 403)
	})
	b.Run("blocked_by_rule", func(b *testing.B) {
		p := newPublicBench(b, 0)
		p.runAs(b, client, host, "/.hidden", 403)
	})
}

// BenchmarkBanFind is the lookup alone: with no bans an atomic load, with
// bans a parse of the address and a map lookup under a read lock.
func BenchmarkBanFind(b *testing.B) {
	now := time.Now()
	for _, c := range []struct {
		name   string
		bans   int
		client string
		banned bool
	}{
		{"no_bans", 0, "203.0.113.7", false},
		{"10k_bans_client_not_banned", 10000, "203.0.113.7", false},
		{"10k_bans_client_banned", 10000, "10.0.5.5", true},
		{"10k_bans_ipv6_client_not_banned", 10000, "2001:db8:1:2:3:4:5:6", false},
	} {
		b.Run(c.name, func(b *testing.B) {
			var bans banState
			for i := range c.bans {
				key := netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
				bans.add(key, "honeypot", "app.example.com", "/.env", now, time.Hour)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if (bans.find(c.client, now) != nil) != c.banned {
						b.Error("wrong answer")
						return
					}
				}
			})
		})
	}
}
