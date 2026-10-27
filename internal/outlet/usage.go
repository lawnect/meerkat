package outlet

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	// The slim runtime image has no zoneinfo database for time.LoadLocation.
	_ "time/tzdata"

	"github.com/lawnect/meerkat/internal/control"
)

var (
	usageHost = regexp.MustCompile(`(?:\{|,)host="([a-z0-9.-]+)"`)
	usageCode = regexp.MustCompile(`(?:\{|,)code="([0-9]{3})"`)
)

// usageHosts maps each enabled hostname to the route its usage is stored
// on. Caddy counts per host, so an address several services share is
// counted on the first of them in failover order.
func usageHosts(plans []control.HTTPRoutePlan) map[string]string {
	hosts := make(map[string]string, len(plans))
	for _, plan := range plans {
		if _, seen := hosts[plan.Hostname]; plan.Enabled && !seen {
			hosts[plan.Hostname] = plan.ID
		}
	}
	return hosts
}

// CollectUsage reads only aggregate metrics from Caddy. It never stores paths,
// client addresses, headers, or individual requests.
func (s *Store) CollectUsage(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	plans, err := s.control.HTTPRoutePlans(ctx)
	if err != nil {
		return err
	}
	hosts := usageHosts(plans)
	if len(hosts) == 0 {
		return s.control.RecordUsage(ctx, nil, time.Now(), "")
	}
	client := &http.Client{Timeout: 4 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.config.CaddyMetrics+"/metrics", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return io.ErrUnexpectedEOF
	}
	samples, generation, err := parseUsageMetrics(resp.Body, hosts)
	if err != nil {
		return err
	}
	return s.control.RecordUsage(ctx, samples, time.Now(), generation)
}

func parseUsageMetrics(body io.Reader, hosts map[string]string) (map[string]control.UsageSample, string, error) {
	samples := map[string]control.UsageSample{}
	byHandler := map[string]map[string]control.UsageSample{}
	generation := ""
	limited := &io.LimitedReader{R: body, N: (16 << 20) + 1}
	scan := bufio.NewScanner(limited)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "process_start_time_seconds ") {
			generation = strings.TrimSpace(strings.TrimPrefix(line, "process_start_time_seconds "))
			continue
		}
		handler := ""
		switch {
		case strings.Contains(line, `handler="subroute"`):
			handler = "subroute"
		case strings.Contains(line, `handler="reverse_proxy"`):
			handler = "reverse_proxy"
		default:
			continue
		}
		hostMatch := usageHost.FindStringSubmatch(line)
		if len(hostMatch) != 2 || hosts[hostMatch[1]] == "" {
			continue
		}
		name, _, ok := strings.Cut(line, "{")
		if !ok {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil || value < 0 {
			continue
		}
		count := int64(value + 0.5)
		routeID := hosts[hostMatch[1]]
		if byHandler[handler] == nil {
			byHandler[handler] = map[string]control.UsageSample{}
		}
		item := byHandler[handler][routeID]
		switch name {
		case "caddy_http_requests_total":
			item.Requests += count
		case "caddy_http_request_size_bytes_sum":
			item.RequestBytes += count
		case "caddy_http_response_size_bytes_sum":
			item.ResponseBytes += count
		case "caddy_http_response_size_bytes_count":
			code := usageCode.FindStringSubmatch(line)
			if len(code) == 2 && code[1][0] == '5' {
				item.ErrorResponses += count
			}
		default:
			continue
		}
		byHandler[handler][routeID] = item
	}
	if err := scan.Err(); err != nil {
		return nil, "", err
	}
	if limited.N == 0 {
		return nil, "", io.ErrUnexpectedEOF
	}
	for routeID, sample := range byHandler["reverse_proxy"] {
		samples[routeID] = sample
	}
	for routeID, sample := range byHandler["subroute"] {
		samples[routeID] = sample
	}
	return samples, generation, nil
}

func (s *Store) RunUsageCollector(ctx context.Context) {
	collect := func() {
		call, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		if err := s.CollectUsage(call); err != nil && ctx.Err() == nil {
			slog.Warn("usage collection failed", "error", err)
		}
	}
	collect()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collect()
		}
	}
}

func (s *Store) connectorUsage(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "persistence_unavailable")
		return
	}
	exists, err := s.control.ConnectorExists(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if !exists {
		fail(w, r, 404, "connector_not_found")
		return
	}
	hours := 24
	switch r.URL.Query().Get("hours") {
	case "", "24":
	case "168":
		hours = 168
	case "720":
		hours = 720
	default:
		fail(w, r, 400, "invalid_usage_period")
		return
	}
	usage, err := s.control.ConnectorUsageFiltered(r.Context(), r.PathValue("id"), time.Now(), hours, r.URL.Query().Get("service_id"))
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, usage)
}

// usageRange reads a series request's unit, from, to and tz, the bounds
// RFC 3339 and the zone an IANA name, answering 400 when they aren't.
func usageRange(w http.ResponseWriter, r *http.Request) (unit string, from, to time.Time, location *time.Location, ok bool) {
	query := r.URL.Query()
	from, fromErr := time.Parse(time.RFC3339, query.Get("from"))
	to, toErr := time.Parse(time.RFC3339, query.Get("to"))
	location = time.UTC
	if zone := query.Get("tz"); zone != "" {
		var err error
		// "Local" would expose the server zone instead of an IANA name.
		if location, err = time.LoadLocation(zone); err != nil || zone == "Local" {
			fail(w, r, 400, "invalid_usage_range")
			return "", from, to, nil, false
		}
	}
	if fromErr != nil || toErr != nil {
		fail(w, r, 400, "invalid_usage_range")
		return "", from, to, nil, false
	}
	return query.Get("unit"), from, to, location, true
}

// outletUsage returns Outlet-wide request totals grouped into calendar buckets
// of the requested zone. The client supplies unit-aligned from/to bounds.
func (s *Store) outletUsage(w http.ResponseWriter, r *http.Request) {
	if !s.admin(w, r) {
		return
	}
	if s.control == nil {
		fail(w, r, 503, "persistence_unavailable")
		return
	}
	unit, from, to, location, ok := usageRange(w, r)
	if !ok {
		return
	}
	series, err := s.control.OutletUsageSeries(r.Context(), unit, from, to, location)
	if errors.Is(err, control.ErrInvalidUsageRange) {
		fail(w, r, 400, "invalid_usage_range")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, series)
}
