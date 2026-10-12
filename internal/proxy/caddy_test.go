package proxy

import (
	"strings"
	"testing"
)

func TestCaddyfileRendersOnlyValidatedRoutes(t *testing.T) {
	config, err := Caddyfile("saeoo.com", []HTTPRoute{{Hostname: "app.saeoo.com", Upstream: "outlet:24000"}})
	if err != nil || !strings.Contains(string(config), "app.saeoo.com") || !strings.Contains(string(config), "outlet:24000") {
		t.Fatalf("config = %q, %v", config, err)
	}
	if _, err := Caddyfile("saeoo.com", []HTTPRoute{{Hostname: "outside.test", Upstream: "outlet:24000"}}); err == nil {
		t.Fatal("outside hostname accepted")
	}
	if _, err := Caddyfile("saeoo.com", []HTTPRoute{{Hostname: "app.saeoo.com", Upstream: "127.0.0.1:24000"}}); err == nil {
		t.Fatal("unsafe upstream accepted")
	}
}
