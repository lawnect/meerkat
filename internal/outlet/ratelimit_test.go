package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

var t0 = time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)

func TestBucketMath(t *testing.T) {
	store := newBucketStore(4, 100, 10*time.Minute)
	ip := []limit{{key: "ip|1", rps: 2, burst: 4}}
	now := t0
	drain := func() int {
		allowed := 0
		for store.take(now, ip) < 0 {
			allowed++
			if allowed > 100 {
				t.Fatal("never denied")
			}
		}
		return allowed
	}
	if got := drain(); got != 4 {
		t.Fatalf("burst allowed %d, want 4", got)
	}
	now = now.Add(400 * time.Millisecond) // 0.8 token
	if store.take(now, ip) != 0 {
		t.Fatal("allowed with 0.8 tokens")
	}
	now = now.Add(100 * time.Millisecond) // 1.0 token
	if got := drain(); got != 1 {
		t.Fatalf("after 0.5s allowed %d, want 1", got)
	}
	now = now.Add(time.Hour) // refills are capped at the burst
	if got := drain(); got != 4 {
		t.Fatalf("after a long pause allowed %d, want 4", got)
	}
	// A smaller burst applies at once.
	now = now.Add(time.Hour)
	ip[0].burst = 2
	if got := drain(); got != 2 {
		t.Fatalf("after lowering the burst allowed %d, want 2", got)
	}
}

func TestBucketsAreAllOrNothing(t *testing.T) {
	store := newBucketStore(8, 100, 10*time.Minute)
	narrow, wide := limit{key: "net|a", rps: 1, burst: 1}, limit{key: "ip|b", rps: 1, burst: 5}
	if store.take(t0, []limit{wide, narrow}) != -1 {
		t.Fatal("first request rejected")
	}
	if got := store.take(t0, []limit{wide, narrow}); got != 1 {
		t.Fatalf("denied index = %d, want the narrow bucket (1)", got)
	}
	allowed := 0
	for store.take(t0, []limit{wide}) < 0 {
		allowed++
	}
	if allowed != 4 {
		t.Fatalf("the wide bucket lost tokens to a rejected request: %d left, want 4", allowed)
	}
	if got := store.take(t0, []limit{narrow, wide}); got != 0 {
		t.Fatalf("both empty: denied index = %d, want the first (0)", got)
	}
}

func TestBucketStoreEvictsLeastRecentlyUsedAndIdle(t *testing.T) {
	store := newBucketStore(1, 3, 10*time.Minute)
	one := func(key string, at time.Time) int { return store.take(at, []limit{{key: key, rps: 1, burst: 1}}) }
	if one("k1", t0) != -1 || one("k1", t0) != 0 {
		t.Fatal("k1 should hold a single token")
	}
	one("k2", t0)
	one("k3", t0)
	one("k2", t0.Add(time.Second)) // k2 is fresher than k1
	one("k4", t0.Add(2*time.Second))
	if store.size() != 3 {
		t.Fatalf("size = %d, want the capacity 3", store.size())
	}
	// k1 was the least recently used, so its empty bucket is gone and it starts full again.
	if one("k1", t0.Add(2*time.Second)) != -1 {
		t.Fatal("k1 was not evicted")
	}
	if store.size() != 3 {
		t.Fatalf("size = %d after re-adding", store.size())
	}
	store.sweep(t0.Add(10*time.Minute + 1500*time.Millisecond))
	if store.size() != 2 {
		t.Fatalf("size = %d after the idle window, want the two freshest buckets", store.size())
	}
	store.sweep(t0.Add(20 * time.Minute))
	if store.size() != 0 {
		t.Fatalf("size = %d, idle buckets should be gone", store.size())
	}
	// Untouched shards free memory only on sweep, but access expires them too.
	one("k5", t0)
	one("k6", t0.Add(11*time.Minute))
	if store.size() != 1 {
		t.Fatalf("size = %d, k5 should have expired on access", store.size())
	}
}

func TestBucketStoreStaysWithinCapacity(t *testing.T) {
	store := newBucketStore(8, 80, 10*time.Minute)
	for i := 0; i < 5000; i++ {
		store.take(t0, []limit{{key: fmt.Sprintf("ip|10.0.%d.%d", i/256, i%256), rps: 1, burst: 1}})
	}
	if store.size() > 80 {
		t.Fatalf("size = %d, capacity 80", store.size())
	}
	if def := newRateLimiter(); def.buckets.perShard*len(def.buckets.shards) > limiterCapacity {
		t.Fatalf("default capacity %d exceeds %d", def.buckets.perShard*len(def.buckets.shards), limiterCapacity)
	}
}

func TestBucketStoreIsSafeUnderContention(t *testing.T) {
	store := newBucketStore(limiterShards, limiterCapacity, limiterIdle)
	var allowed atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Go(func() {
			for i := 0; i < 100; i++ {
				// One shared bucket plus a private one: exactly burst requests pass.
				limits := []limit{{key: "host|shared", rps: 1, burst: 1000}, {key: fmt.Sprintf("ip|%d", g), rps: 1, burst: 1000}}
				if store.take(t0, limits) < 0 {
					allowed.Add(1)
				}
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 1000 {
		t.Fatalf("allowed %d, want exactly the burst of 1000", allowed.Load())
	}
}

func TestSubnetOf(t *testing.T) {
	for ip, want := range map[string]string{
		"203.0.113.7": "203.0.113.0/24", "203.0.113.250": "203.0.113.0/24", "::ffff:203.0.113.7": "203.0.113.0/24",
		"2001:db8:0:1::1": "2001:db8::/56", "2001:db8:0:ff:ffff::9": "2001:db8::/56", "2001:db8:0:100::1": "2001:db8:0:100::/56", "garbage": "",
	} {
		if got := subnetOf(ip); got != want {
			t.Errorf("subnetOf(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestEndpointRulesPreferLongestPrefixThenNamedHost(t *testing.T) {
	config := compileRateLimit(control.RateLimitSettings{Endpoints: []control.EndpointLimit{
		{Host: "*", PathPrefix: "/api", RPS: 1, Burst: 1, Per: "total"},
		{Host: "*", PathPrefix: "/api/v1/login", RPS: 2, Burst: 2, Per: "ip"},
		{Host: "app.example.com", PathPrefix: "/api", RPS: 3, Burst: 3, Per: "total"},
	}})
	for _, tc := range []struct{ host, path, want string }{
		{"app.example.com", "/api/v1/login", "*/api/v1/login"},
		{"app.example.com", "/api/users", "app.example.com/api"},
		{"other.example.com", "/api/users", "*/api"},
		{"other.example.com", "/API/Users", "*/api"},
		{"other.example.com", "//api//users/./x/../y", "*/api"},
		{"other.example.com", "/apiary", "*/api"},
		{"other.example.com", "/home", ""},
	} {
		got := ""
		if rule := config.match(tc.host, tc.path); rule != nil {
			got = rule.label
		}
		if got != tc.want {
			t.Errorf("%s %s matched %q, want %q", tc.host, tc.path, got, tc.want)
		}
	}
}

func rateLimitBody(overrides map[string]any) map[string]any {
	body := map[string]any{
		"enabled": true, "per_ip": map[string]any{"rps": 0, "burst": 0}, "per_subnet": map[string]any{"rps": 0, "burst": 0},
		"per_address": map[string]any{"rps": 0, "burst": 0}, "endpoints": []any{},
	}
	for key, value := range overrides {
		body[key] = value
	}
	return body
}

func capacity(rps, burst int) map[string]any { return map[string]any{"rps": rps, "burst": burst} }

// limited is a fixture that serves the dashboard on the management host.
func limitedFixture(t *testing.T) alertFixture {
	t.Helper()
	a := newAlertFixture(t)
	if err := a.store.reloadRateLimit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a alertFixture) setLimits(t *testing.T, overrides map[string]any) {
	t.Helper()
	if w := request(a.store.Handler(), "PUT", "/api/v1/security/rate-limit", a.admin, rateLimitBody(overrides)); w.Code != 200 {
		t.Fatalf("PUT rate limit: %d %s", w.Code, w.Body.String())
	}
}

// get sends a public request to host, as Caddy would forward it.
func (a alertFixture) get(host, path, forwardedFor string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = host
	if forwardedFor != "" {
		r.Header.Set("X-Forwarded-For", forwardedFor)
	}
	w := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(w, r)
	return w
}

func statuses(a alertFixture, n int, host, path string, ip func(i int) string) []int {
	codes := make([]int, n)
	for i := range codes {
		codes[i] = a.get(host, path, ip(i)).Code
	}
	return codes
}

func same(ip string) func(int) string { return func(int) string { return ip } }

func countOf(codes []int, want int) int {
	n := 0
	for _, code := range codes {
		if code == want {
			n++
		}
	}
	return n
}

func TestPerIPLimitRejectsWith429(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_ip": capacity(2, 3)})
	codes := statuses(a, 5, a.host, "/healthz", same("203.0.113.7"))
	if countOf(codes, 200) != 3 || countOf(codes, 429) != 2 {
		t.Fatalf("statuses = %v, want 3 passes then 429", codes)
	}
	w := a.get(a.host, "/healthz", "203.0.113.7")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") ||
		w.Body.String() != "Too many requests (ip limit)\n" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("429 response: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	// Another client is unaffected, and rotating earlier X-Forwarded-For entries does not help:
	// only the address Caddy appended counts.
	if w := a.get(a.host, "/healthz", "198.51.100.1"); w.Code != 200 {
		t.Fatalf("other client: %d", w.Code)
	}
	if w := a.get(a.host, "/healthz", "10.9.9.9, 203.0.113.7"); w.Code != 429 {
		t.Fatalf("forged leading entry escaped the limit: %d", w.Code)
	}
	a.clock.advance(time.Second) // two tokens come back
	codes = statuses(a, 3, a.host, "/healthz", same("203.0.113.7"))
	if countOf(codes, 200) != 2 || countOf(codes, 429) != 1 {
		t.Fatalf("after refill statuses = %v", codes)
	}
}

func TestLocalAdminPortIsNeverRateLimited(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1), "per_subnet": capacity(1, 1), "per_address": capacity(1, 1)})
	h := a.store.Handler()
	for i := 0; i < 50; i++ {
		r := httptest.NewRequest("GET", "/healthz", nil)
		r.Header.Set("X-Forwarded-For", "203.0.113.7")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
}

func TestPerSubnetLimitSharesIPv4Slash24AndIPv6Slash56(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_subnet": capacity(1, 3)})
	codes := statuses(a, 5, a.host, "/healthz", func(i int) string { return fmt.Sprintf("198.51.100.%d", i+1) })
	if countOf(codes, 200) != 3 || countOf(codes, 429) != 2 {
		t.Fatalf("one /24: %v", codes)
	}
	if w := a.get(a.host, "/healthz", "198.51.100.200"); w.Body.String() != "Too many requests (subnet limit)\n" {
		t.Fatalf("body = %q", w.Body.String())
	}
	if w := a.get(a.host, "/healthz", "198.51.101.1"); w.Code != 200 {
		t.Fatalf("next /24: %d", w.Code)
	}
	// Different /64s of one /56 share the bucket.
	v6 := []string{"2001:db8:0:1::1", "2001:db8:0:2::2", "2001:db8:0:ff:ffff::3", "2001:db8:0:1:1::4"}
	codes = statuses(a, 4, a.host, "/healthz", func(i int) string { return v6[i] })
	if countOf(codes, 200) != 3 || codes[3] != 429 {
		t.Fatalf("one /56: %v", codes)
	}
	if w := a.get(a.host, "/healthz", "2001:db8:0:100::1"); w.Code != 200 {
		t.Fatalf("next /56: %d", w.Code)
	}
}

func TestPerAddressLimitCoversAllClientsOfOneHost(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_address": capacity(1, 2)})
	codes := statuses(a, 4, a.host, "/healthz", func(i int) string { return fmt.Sprintf("203.0.%d.1", i+1) })
	if countOf(codes, 200) != 2 || countOf(codes, 429) != 2 {
		t.Fatalf("management host: %v", codes)
	}
	if w := a.get(a.host, "/healthz", "192.0.2.1"); w.Body.String() != "Too many requests (address limit)\n" {
		t.Fatalf("body = %q", w.Body.String())
	}
	// Another hostname has its own bucket (no route there, so it answers 404).
	if w := a.get("app.example.com", "/", "203.0.9.1"); w.Code != 404 {
		t.Fatalf("other host: %d", w.Code)
	}
	// Without a forwarded address the per-host bucket still applies.
	if w := a.get(a.host, "/healthz", ""); w.Code != 429 {
		t.Fatalf("no forwarded address: %d", w.Code)
	}
}

func TestEndpointLimitsTotalAndPerIP(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"endpoints": []any{
		map[string]any{"host": "*", "path_prefix": "/login", "rps": 1, "burst": 2, "per": "total"},
		map[string]any{"host": a.host, "path_prefix": "/healthz", "rps": 1, "burst": 2, "per": "ip"},
	}})
	// "total": two clients' requests all count against one bucket.
	codes := statuses(a, 4, a.host, "/login", func(i int) string { return fmt.Sprintf("203.0.%d.1", i+1) })
	if countOf(codes, 429) != 2 || codes[0] == 429 || codes[1] == 429 {
		t.Fatalf("total: %v", codes)
	}
	if w := a.get(a.host, "/login", "192.0.2.50"); w.Body.String() != "Too many requests (endpoint limit)\n" {
		t.Fatalf("body = %q", w.Body.String())
	}
	if w := a.get(a.host, "/other", "192.0.2.50"); w.Code == 429 {
		t.Fatalf("other path: %d", w.Code)
	}
	// Dressing the path up does not escape the rule; the "*" rule covers other hosts too.
	if w := a.get(a.host, "//LOGIN/", "192.0.2.51"); w.Code != 429 {
		t.Fatalf("disguised path: %d", w.Code)
	}
	if w := a.get("app.example.com", "/login/x", "192.0.2.52"); w.Code != 429 {
		t.Fatalf("wildcard host: %d", w.Code)
	}
	// "ip": every client has its own bucket.
	codes = statuses(a, 3, a.host, "/healthz", same("198.51.100.1"))
	if countOf(codes, 200) != 2 || codes[2] != 429 {
		t.Fatalf("per ip: %v", codes)
	}
	if w := a.get(a.host, "/healthz", "198.51.100.2"); w.Code != 200 {
		t.Fatalf("second client: %d", w.Code)
	}
	if w := a.get("app.example.com", "/healthz", "198.51.100.1"); w.Code == 429 {
		t.Fatal("a rule for the management host limited another host")
	}
}

func TestDefaultLimitsProtectLoginOnTheManagementHost(t *testing.T) {
	a := limitedFixture(t)
	var settings control.RateLimitSettings
	w := request(a.store.Handler(), "GET", "/api/v1/security/rate-limit", a.admin, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil || w.Code != 200 {
		t.Fatalf("GET: %d %s", w.Code, w.Body.String())
	}
	want := control.RateLimitSettings{
		Enabled: true, PerIP: control.Limit{RPS: 300, Burst: 1000}, PerSubnet: control.Limit{RPS: 1500, Burst: 5000},
		Endpoints: []control.EndpointLimit{{Host: a.host, PathPrefix: "/api/v1/session", RPS: 3, Burst: 30, Per: "ip"}},
	}
	if !strings.Contains(w.Body.String(), `"per_address":{"rps":0,"burst":0}`) || settings.Enabled != want.Enabled || settings.PerIP != want.PerIP || settings.PerSubnet != want.PerSubnet ||
		settings.PerAddress != want.PerAddress || !slices.Equal(settings.Endpoints, want.Endpoints) {
		t.Fatalf("defaults = %s", w.Body.String())
	}
	// 30 login attempts pass the endpoint bucket (the login handler has its own, separate limit);
	// the 31st is turned away before reaching it.
	login := func() *httptest.ResponseRecorder {
		r := jsonRequest("POST", "/api/v1/session", map[string]any{"email": "nobody@example.com", "password": "wrong password entirely"})
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", "203.0.113.7")
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		return w
	}
	blocked := 0
	for i := 0; i < 31; i++ {
		if w := login(); w.Body.String() == "Too many requests (endpoint limit)\n" {
			blocked++
			if i != 30 {
				t.Fatalf("blocked at attempt %d", i+1)
			}
		}
	}
	if blocked != 1 {
		t.Fatalf("blocked = %d, want only the 31st attempt", blocked)
	}
	if w := a.get(a.host, "/healthz", "203.0.113.7"); w.Code != 200 {
		t.Fatalf("the same client can still load pages: %d", w.Code)
	}
}

func TestOnlyTheCertificateAskIsExempt(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1), "per_subnet": capacity(1, 1), "per_address": capacity(1, 1),
		"endpoints": []any{map[string]any{"host": "*", "path_prefix": "/", "rps": 1, "burst": 1, "per": "total"}}})
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", certificateAskPath+"?domain="+a.host, nil)
		r.Host = "outlet:8080"
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		if w.Code != http.StatusNoContent {
			t.Fatalf("cert-ask %d: %d", i, w.Code)
		}
	}
	ask := httptest.NewRequest("GET", certificateAskPath, nil)
	ask.Host = "outlet:8080"
	public := httptest.NewRequest("GET", certificateAskPath, nil)
	public.Host = a.host
	// A client can send Host: outlet:8080 through Caddy, but Caddy then adds
	// X-Forwarded-For, which its own ask never carries.
	spoofed := httptest.NewRequest("GET", certificateAskPath+"?domain="+a.host, nil)
	spoofed.Host = "outlet:8080"
	spoofed.Header.Set("X-Forwarded-For", "203.0.113.7")
	if !exemptFromRateLimit(ask) || exemptFromRateLimit(public) || exemptFromRateLimit(spoofed) || exemptFromRateLimit(httptest.NewRequest("GET", "/.well-known/acme-challenge/x", nil)) {
		t.Fatal("exemptions are wrong")
	}
	w := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(w, spoofed)
	if w.Code == http.StatusNoContent {
		t.Fatalf("forwarded cert-ask answered: %d", w.Code)
	}
	// Caddy answers ACME challenges itself: here they are ordinary requests,
	// and so is the ask path on a public host.
	a.get(a.host, "/.well-known/acme-challenge/token", "203.0.113.7")
	if w := a.get(a.host, "/.well-known/acme-challenge/token", "203.0.113.7"); w.Code != 429 {
		t.Fatalf("ACME path: %d", w.Code)
	}
	if w := a.get(a.host, certificateAskPath, "203.0.113.7"); w.Code != 429 {
		t.Fatalf("ask path on a public host: %d", w.Code)
	}
	if w := a.get(a.host, "/healthz", "203.0.113.7"); w.Code != 429 {
		t.Fatalf("ordinary path: %d", w.Code)
	}
}

func TestRateLimitCanBeSwitchedOffAndAppliesLive(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1)})
	a.get(a.host, "/healthz", "203.0.113.7")
	if w := a.get(a.host, "/healthz", "203.0.113.7"); w.Code != 429 {
		t.Fatalf("limit not enforced: %d", w.Code)
	}
	a.setLimits(t, map[string]any{"enabled": false, "per_ip": capacity(1, 1)})
	for i := 0; i < 10; i++ {
		if w := a.get(a.host, "/healthz", "203.0.113.7"); w.Code != 200 {
			t.Fatalf("limit still applies when disabled: %d", w.Code)
		}
	}
	a.setLimits(t, map[string]any{"per_ip": capacity(100, 100)})
	a.clock.advance(time.Second) // the emptied bucket refills at the new rate
	for i := 0; i < 50; i++ {
		if w := a.get(a.host, "/healthz", "203.0.113.7"); w.Code != 200 {
			t.Fatalf("raised limit not applied: %d", w.Code)
		}
	}
}

func TestRateLimitSettingsAPI(t *testing.T) {
	a := limitedFixture(t)
	h := a.store.Handler()
	valid := rateLimitBody(map[string]any{
		"per_ip": capacity(10000, 100000), "per_subnet": capacity(5, 5),
		"endpoints": []any{map[string]any{"host": "API.Example.com", "path_prefix": "/Login", "rps": 5, "burst": 10, "per": "total"}},
	})
	w := request(h, "PUT", "/api/v1/security/rate-limit", a.admin, valid)
	var saved control.RateLimitSettings
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || w.Code != 200 || len(saved.Endpoints) != 1 || saved.Endpoints[0].Host != "api.example.com" || saved.Endpoints[0].PathPrefix != "/login" {
		t.Fatalf("valid: %d %s", w.Code, w.Body.String())
	}
	if got := request(h, "GET", "/api/v1/security/rate-limit", a.admin, nil); got.Body.String() != w.Body.String() {
		t.Fatalf("GET differs from PUT:\n%s\n%s", got.Body.String(), w.Body.String())
	}
	many := make([]any, 51)
	for i := range many {
		many[i] = map[string]any{"host": "*", "path_prefix": fmt.Sprintf("/p%d", i), "rps": 1, "burst": 1, "per": "ip"}
	}
	endpoint := func(change map[string]any) []any {
		e := map[string]any{"host": "*", "path_prefix": "/login", "rps": 5, "burst": 10, "per": "ip"}
		for k, v := range change {
			e[k] = v
		}
		return []any{e}
	}
	for name, overrides := range map[string]map[string]any{
		"rps zero with burst":     {"per_ip": capacity(0, 5)},
		"burst zero with rps":     {"per_ip": capacity(5, 0)},
		"rps too high":            {"per_ip": capacity(10001, 10001)},
		"burst below rps":         {"per_subnet": capacity(10, 9)},
		"burst too high":          {"per_address": capacity(10, 100001)},
		"negative":                {"per_ip": capacity(-1, 5)},
		"too many rules":          {"endpoints": many},
		"bad host":                {"endpoints": endpoint(map[string]any{"host": "not a host"})},
		"bare label host":         {"endpoints": endpoint(map[string]any{"host": "localhost"})},
		"wildcard subdomain host": {"endpoints": endpoint(map[string]any{"host": "*.example.com"})},
		"prefix without slash":    {"endpoints": endpoint(map[string]any{"path_prefix": "login"})},
		"prefix with space":       {"endpoints": endpoint(map[string]any{"path_prefix": "/log in"})},
		"prefix too long":         {"endpoints": endpoint(map[string]any{"path_prefix": "/" + strings.Repeat("a", 300)})},
		"unknown per":             {"endpoints": endpoint(map[string]any{"per": "both"})},
		"endpoint rps zero":       {"endpoints": endpoint(map[string]any{"rps": 0, "burst": 0})},
		"endpoint burst < rps":    {"endpoints": endpoint(map[string]any{"rps": 10, "burst": 5})},
		"duplicate rule":          {"endpoints": append(endpoint(nil), endpoint(map[string]any{"path_prefix": "/LOGIN", "per": "total"})...)},
	} {
		if w := request(h, "PUT", "/api/v1/security/rate-limit", a.admin, rateLimitBody(overrides)); w.Code != 400 || errorCode(t, w) != "invalid_rate_limit" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for _, key := range []string{"enabled", "per_ip", "per_subnet", "per_address", "endpoints"} {
		incomplete := rateLimitBody(nil)
		delete(incomplete, key)
		if w := request(h, "PUT", "/api/v1/security/rate-limit", a.admin, incomplete); w.Code != 400 || errorCode(t, w) != "invalid_request" {
			t.Errorf("without %s: %d %s", key, w.Code, w.Body.String())
		}
	}
	if w := request(h, "PUT", "/api/v1/security/rate-limit", "wrong", valid); w.Code != 401 {
		t.Errorf("unauthenticated: %d", w.Code)
	}
	if got := request(h, "GET", "/api/v1/security/rate-limit", a.admin, nil); got.Body.String() != w.Body.String() {
		t.Fatal("a rejected update changed the settings")
	}
	if e := a.lastAudit(t); e.Action != "security.rate_limit" || e.ResourceType != "security" || e.ResourceID != "rate_limit" || !strings.Contains(string(e.Detail), `"host":"api.example.com"`) {
		t.Fatalf("audit = %+v", e)
	}
}

func TestSavedLoginRuleFollowsThePrimaryDomain(t *testing.T) {
	a := limitedFixture(t)
	h := a.store.Handler()
	a.setLimits(t, map[string]any{"endpoints": []any{
		map[string]any{"host": a.host, "path_prefix": "/api/v1/session", "rps": 1, "burst": 5, "per": "ip"},
		map[string]any{"host": "app.example.com", "path_prefix": "/x", "rps": 1, "burst": 5, "per": "ip"},
	}})
	if w := request(h, "PUT", "/api/v1/outlet", a.admin, map[string]any{"domain": "example.org"}); w.Code != 200 {
		t.Fatalf("domain: %d %s", w.Code, w.Body.String())
	}
	label := strings.SplitN(a.host, ".", 2)[0]
	var settings control.RateLimitSettings
	_ = json.Unmarshal(request(h, "GET", "/api/v1/security/rate-limit", a.admin, nil).Body.Bytes(), &settings)
	if len(settings.Endpoints) != 2 || settings.Endpoints[0].Host != label+".example.org" || settings.Endpoints[1].Host != "app.example.com" {
		t.Fatalf("endpoints = %+v", settings.Endpoints)
	}
	// The renamed rule is live.
	codes := statuses(a, 7, label+".example.org", "/api/v1/session", same("203.0.113.7"))
	if countOf(codes, 429) < 2 {
		t.Fatalf("renamed rule not enforced: %v", codes)
	}
}

func TestRejectionsAreCountedPerHourAndExposedInUsage(t *testing.T) {
	a := limitedFixture(t)
	ctx := context.Background()
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1)})
	for i := 0; i < 6; i++ {
		a.get(a.host, "/healthz", "203.0.113.7") // 1 passes, 5 are rejected
	}
	a.clock.advance(time.Hour)
	a.get(a.host, "/healthz", "198.51.100.1")
	for i := 0; i < 2; i++ {
		a.get(a.host, "/healthz", "198.51.100.1")
	}
	a.store.flushRejections(ctx)
	a.store.flushRejections(ctx) // nothing left to add the second time

	w := request(a.store.Handler(), "GET", "/api/v1/usage?unit=hour&from=2026-10-26T14:00:00Z&to=2026-10-26T16:00:00Z", a.admin, nil)
	var series control.UsageSeries
	if err := json.Unmarshal(w.Body.Bytes(), &series); err != nil || w.Code != 200 || len(series.Buckets) != 2 {
		t.Fatalf("usage: %d %s", w.Code, w.Body.String())
	}
	if series.Buckets[0].RateLimited != 5 || series.Buckets[1].RateLimited != 2 {
		t.Fatalf("rate_limited = %d, %d, want 5, 2", series.Buckets[0].RateLimited, series.Buckets[1].RateLimited)
	}
	if !strings.Contains(w.Body.String(), `"rate_limited":5`) {
		t.Fatalf("field missing from the response: %s", w.Body.String())
	}
	// Usage collection adds requests to the same row without losing the count.
	if err := a.db.RecordUsage(ctx, nil, t0.Add(time.Hour+time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	if series, _ := a.db.OutletUsageSeries(ctx, "hour", t0, t0.Add(2*time.Hour), time.UTC); series.Buckets[0].RateLimited != 5 {
		t.Fatalf("count lost after a usage scrape: %+v", series.Buckets)
	}
}

func TestRateLimitAlert(t *testing.T) {
	a := limitedFixture(t)
	a.configureTelegram(t, false)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1), "per_subnet": capacity(1000, 1000), "endpoints": []any{
		map[string]any{"host": "*", "path_prefix": "/login", "rps": 1, "burst": 1, "per": "total"},
	}})
	rules := control.DefaultAlertRules()
	rules.RateLimit.MinBlocked = 10
	if err := a.db.SetAlertRules(context.Background(), rules, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	flood := func(n int) {
		for i := 0; i < n; i++ {
			a.get(a.host, "/healthz", "203.0.113.7")
			a.get(a.host, "/healthz", "198.51.100.9")
		}
	}
	flood(3) // 2 IPs x (3 requests - 1 allowed) = 4 rejected
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("alerted below the threshold: %q", a.texts())
	}
	flood(5) // 4 + 2 x 5 = 14 rejected
	for i := 0; i < 3; i++ {
		a.get(a.host, "/login", fmt.Sprintf("192.0.2.%d", 77+i)) // the 2nd and 3rd meet the endpoint's single token
	}
	a.clock.advance(time.Minute)
	a.evaluate()
	texts := a.texts()
	if len(texts) != 1 {
		t.Fatalf("messages = %q", texts)
	}
	for _, want := range []string{
		"🛑 <b>Requests rate limited</b>\nBlocked: 16 in 10 min (ip 14, endpoint 2)\n",
		"Top IPs: <code>198.51.100.9</code> (7), <code>203.0.113.7</code> (7), <code>192.0.2.78</code> (1)\n",
		"Top subnets: <code>198.51.100.0/24</code> (7), <code>203.0.113.0/24</code> (7), <code>192.0.2.0/24</code> (2)\n",
		"Top endpoints: <code>*/login</code> (2)\n",
		"<a href=\"https://" + a.host + "/\">Open in Meerkat</a>",
	} {
		if !strings.Contains(texts[0], want) {
			t.Errorf("message lacks %q:\n%s", want, texts[0])
		}
	}
	if events := a.events(t, "rate_limit"); len(events) != 1 || events[0].ResolvedAt == nil || !events[0].Sent || events[0].Message != "Blocked 16 requests in 10 min (ip 14, endpoint 2)" {
		t.Fatalf("events = %+v", events)
	}

	// Once per 30 minutes while it continues.
	a.clock.advance(20 * time.Minute)
	flood(8)
	a.evaluate()
	if len(a.telegram.messages()) != 1 {
		t.Fatalf("alerted again inside 30 minutes: %q", a.texts())
	}
	a.clock.advance(11 * time.Minute) // 31 minutes after the first alert
	flood(8)
	a.evaluate()
	if len(a.telegram.messages()) != 2 {
		t.Fatalf("no alert after 30 minutes of continued flooding: %q", a.texts())
	}
	// Old rejections fall out of the ten-minute window.
	a.clock.advance(time.Hour)
	a.evaluate()
	if len(a.telegram.messages()) != 2 {
		t.Fatal("alerted for a flood that ended")
	}
}

func TestRateLimitAlertCanBeSwitchedOff(t *testing.T) {
	a := limitedFixture(t)
	a.configureTelegram(t, false)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 1)})
	rules := control.DefaultAlertRules()
	rules.RateLimit = control.RateLimitAlertRule{Enabled: false, MinBlocked: 1}
	if err := a.db.SetAlertRules(context.Background(), rules, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		a.get(a.host, "/healthz", "203.0.113.7")
	}
	a.evaluate()
	if len(a.telegram.messages()) != 0 {
		t.Fatalf("messages = %q", a.texts())
	}
}
