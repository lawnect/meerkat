package proxy

import (
	"strings"
	"testing"
)

func TestCaddyfileUsesStableOnDemandRouting(t *testing.T) {
	config := string(DynamicCaddyfile())
	if !strings.Contains(config, "metrics {\n\t\tper_host") || !strings.Contains(config, ":9090 {\n\tmetrics /metrics") {
		t.Fatalf("usage metrics endpoint missing from config: %q", config)
	}
	if !strings.Contains(config, "on_demand_tls {") || !strings.Contains(config, "ask http://outlet:8080/.well-known/meerkat/cert-ask") || !strings.Contains(config, "reverse_proxy http://outlet:8080") {
		t.Fatalf("dynamic certificate authorization or routing missing: %q", config)
	}
	if strings.Contains(config, "*.saeoo.com") || strings.Contains(config, "dns cloudflare") || strings.Contains(config, "app.saeoo.com") {
		t.Fatalf("route-specific or DNS-provider config remains: %q", config)
	}
	if count := strings.Count(config, "disable_tlsalpn_challenge"); count != 1 {
		t.Fatalf("expected HTTP-01 for on-demand certificates, got %d: %q", count, config)
	}
}

func TestEmptyCaddyfileHasNoPublicHost(t *testing.T) {
	config := string(EmptyCaddyfile())
	if !strings.Contains(config, ":9090 {\n\tmetrics /metrics") || strings.Contains(config, "issuer acme") {
		t.Fatalf("invalid pre-domain config: %q", config)
	}
}
