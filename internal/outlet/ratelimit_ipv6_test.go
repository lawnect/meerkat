package outlet

import (
	"fmt"
	"testing"
)

// A /64 holds 2^64 addresses, so a client that rotates through them must not
// get a bucket for each: per-IP limits, and endpoint limits per IP, count an
// IPv6 client by its /64. IPv4 clients, however written, still count one by
// one.
func TestPerIPLimitsCountAnIPv6ClientBySlash64(t *testing.T) {
	a := limitedFixture(t)
	a.setLimits(t, map[string]any{"per_ip": capacity(1, 2), "endpoints": []any{
		map[string]any{"host": "*", "path_prefix": "/login", "rps": 1, "burst": 2, "per": "ip"},
	}})
	codes := statuses(a, 5, a.host, "/healthz", func(i int) string { return fmt.Sprintf("2001:db8:1:2:%x::%x", i+1, i+1) })
	if countOf(codes, 200) != 2 || countOf(codes, 429) != 3 {
		t.Fatalf("per IP over a /64: %v", codes)
	}
	if w := a.get(a.host, "/healthz", "2001:db8:1:3::1"); w.Code != 200 {
		t.Fatalf("the next /64: %d", w.Code)
	}
	codes = statuses(a, 5, "app.example.com", "/login", func(i int) string { return fmt.Sprintf("2001:db8:5:6:%x::1", i+1) })
	// The address isn't routed here: 404 is a request the limits let by.
	if codes[0] == 429 || codes[1] == 429 || countOf(codes, 429) != 3 {
		t.Fatalf("endpoint per IP over a /64: %v", codes)
	}
	if w := a.get("app.example.com", "/login", "2001:db8:5:7::1"); w.Code == 429 {
		t.Fatalf("the next /64 at the endpoint: %d", w.Code)
	}
	// IPv4 stays per address, and its IPv4-mapped form is the same client.
	addresses := []string{"192.0.2.1", "::ffff:192.0.2.1", "192.0.2.1", "192.0.2.2"}
	codes = statuses(a, len(addresses), a.host, "/healthz", func(i int) string { return addresses[i] })
	if codes[0] != 200 || codes[1] != 200 || codes[2] != 429 || codes[3] != 200 {
		t.Fatalf("IPv4: %v", codes)
	}
}
