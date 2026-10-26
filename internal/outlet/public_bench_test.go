package outlet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// publicBench is an Outlet with one connected service on a local upstream,
// served through the real PublicHandler.
type publicBench struct {
	proxyFixture
}

func newPublicBench(tb testing.TB, routes int) publicBench {
	tb.Helper()
	f := newProxyFixture(tb, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	}))
	ctx := context.Background()
	if _, err := f.db.CreateHTTPRoute(ctx, f.services[0].ID, "app.example.com"); err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < routes; i++ {
		if _, err := f.db.CreateHTTPRoute(ctx, f.services[0].ID, "r"+strconv.Itoa(i)+".example.com"); err != nil {
			tb.Fatal(err)
		}
	}
	return publicBench{f}
}

// discardWriter is a ResponseWriter that keeps nothing but the status.
type discardWriter struct {
	header http.Header
	code   int
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(code int)        { w.code = code }

func (p publicBench) run(b *testing.B, host, path string, want int) {
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		w := &discardWriter{header: http.Header{}}
		for pb.Next() {
			r := httptest.NewRequest("GET", path, nil)
			r.Host = host
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

func BenchmarkPublicProxy(b *testing.B) {
	p := newPublicBench(b, 0)
	p.run(b, "app.example.com", "/", 200)
}

func BenchmarkPublicProxyManyRoutes(b *testing.B) {
	p := newPublicBench(b, 10000)
	b.ReportAllocs()
	var n atomic.Int64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		w := &discardWriter{header: http.Header{}}
		for pb.Next() {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = "r" + strconv.Itoa(int(n.Add(1)%10000)) + ".example.com"
			clear(w.header)
			w.code = 200
			p.handler.ServeHTTP(w, r)
			if w.code != 200 {
				b.Errorf("%s: %d", r.Host, w.code)
				return
			}
		}
	})
}

func BenchmarkPublicAdminAsset(b *testing.B) {
	p := newPublicBench(b, 0)
	p.run(b, p.adminHost, "/", 200)
}

func BenchmarkPublicUnknownHost(b *testing.B) {
	p := newPublicBench(b, 0)
	p.run(b, "unknown.example.com", "/", 404)
}

func BenchmarkCertificateAllowed(b *testing.B) {
	p := newPublicBench(b, 0)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if ok, err := p.store.certificateAllowed(ctx, "app.example.com"); !ok || err != nil {
				b.Errorf("allowed = %v, %v", ok, err)
				return
			}
		}
	})
}

// The limiter's allow path: one client, or a different one of 100k each time.
func BenchmarkRateLimiterTake(b *testing.B) {
	for _, ips := range []int{1, 100_000} {
		b.Run("ips="+strconv.Itoa(ips), func(b *testing.B) {
			store := newBucketStore(limiterShards, limiterCapacity, limiterIdle)
			keys := make([][2]string, ips)
			for i := range keys {
				ip := "10." + strconv.Itoa(i>>16&255) + "." + strconv.Itoa(i>>8&255) + "." + strconv.Itoa(i&255)
				keys[i] = [2]string{"ip|" + ip, "net|" + subnetOf(ip)}
			}
			var n atomic.Int64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					k := keys[int(n.Add(1))%ips]
					if store.take(time.Now(), []limit{{key: k[0], rps: 1e12, burst: 1e12}, {key: k[1], rps: 1e12, burst: 1e12}}) >= 0 {
						b.Error("denied")
						return
					}
				}
			})
			if size := store.size(); size > limiterCapacity {
				b.Fatalf("size %d over capacity", size)
			}
		})
	}
}

// BenchmarkRateLimitedFlood is the cost of turning one flooding client away.
func BenchmarkRateLimitedFlood(b *testing.B) {
	p := newPublicBench(b, 0)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		w := &discardWriter{header: http.Header{}}
		for pb.Next() {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = "app.example.com"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
			clear(w.header)
			w.code = 200
			p.handler.ServeHTTP(w, r)
		}
	})
}
