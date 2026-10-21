package outlet

import (
	"strings"
	"testing"
)

func TestParseUsageMetricsPrefersRouteHandler(t *testing.T) {
	metrics := `process_start_time_seconds 1.79254607271e+09
caddy_http_requests_total{handler="subroute",host="app.example.test",server="srv0"} 4
caddy_http_request_size_bytes_sum{code="200",handler="subroute",host="app.example.test",method="GET",server="srv0"} 200
caddy_http_response_size_bytes_sum{code="200",handler="subroute",host="app.example.test",method="GET",server="srv0"} 800
caddy_http_response_size_bytes_count{code="502",handler="subroute",host="app.example.test",method="GET",server="srv0"} 1
caddy_http_requests_total{handler="reverse_proxy",host="app.example.test",server="srv0"} 4
caddy_http_requests_total{handler="subroute",host="other.example.test",server="srv0"} 99
`
	samples, generation, err := parseUsageMetrics(strings.NewReader(metrics), map[string]string{"app.example.test": "route-1"})
	if err != nil {
		t.Fatal(err)
	}
	if generation != "1.79254607271e+09" {
		t.Fatalf("generation = %q", generation)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %+v", samples)
	}
	got := samples["route-1"]
	if got.Requests != 4 || got.RequestBytes != 200 || got.ResponseBytes != 800 || got.ErrorResponses != 1 {
		t.Fatalf("usage = %+v", got)
	}
}

func TestParseUsageMetricsSupportsProxyHandler(t *testing.T) {
	metrics := `caddy_http_requests_total{handler="reverse_proxy",host="app.example.test",server="srv0"} 3` + "\n"
	samples, _, err := parseUsageMetrics(strings.NewReader(metrics), map[string]string{"app.example.test": "route-1"})
	if err != nil {
		t.Fatal(err)
	}
	if samples["route-1"].Requests != 3 {
		t.Fatalf("samples = %+v", samples)
	}
}
