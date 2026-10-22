package validation

import (
	"github.com/lawnect/meerkat/internal/contract"
	"testing"
)

func TestTargetsAndHTTPS(t *testing.T) {
	for _, host := range []string{"app", "postgres", "app-name", "127.0.0.1", "::1", "localhost"} {
		if err := Target(contract.Target{Host: host, Port: 3000}); err != nil {
			t.Errorf("allowed host %q rejected", host)
		}
	}
	for _, host := range []string{"example.com", "8.8.8.8", "169.254.169.254", "192.168.1.1", "app\n[client]", "app:1234", "/bin/sh", "-app"} {
		if err := Target(contract.Target{Host: host, Port: 3000}); err == nil {
			t.Errorf("unsafe host %q accepted", host)
		}
	}
	if Target(contract.Target{Host: "app", Port: 65536}) == nil {
		t.Fatal("invalid port accepted")
	}
	for _, url := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?token=secret"} {
		if OutletURL(url, false) == nil {
			t.Errorf("invalid outlet URL accepted")
		}
	}
	if OutletURL("https://outlet.example.com", false) != nil {
		t.Fatal("HTTPS rejected")
	}
}

func TestTunnelAddress(t *testing.T) {
	for _, address := range []string{"meerkat.example.com:2333", "outlet:2333", "203.0.113.10:2333", "[2001:db8::1]:2333"} {
		if !TunnelAddress(address) {
			t.Errorf("tunnel address %q rejected", address)
		}
	}
	for _, address := range []string{"meerkat.example.com", "Meerkat.example.com:2333", "0.0.0.0:2333", "bad host:2333", "example.com:0", "example.com:65536", "a_b.example.com:2333", ":2333", "127.0.0.1:2333", "[::1]:2333", "localhost:2333"} {
		if TunnelAddress(address) {
			t.Errorf("invalid tunnel address %q accepted", address)
		}
	}
}
