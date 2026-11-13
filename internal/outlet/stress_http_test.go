package outlet

// Opt-in, local-only socket stress tests. Run these sequentially in an isolated
// container (for example --network none --cpus=2 --memory=1g), never against a
// deployed Outlet. All servers, certificates, databases and traffic are synthetic.
//
// MEERKAT_STRESS=1 MEERKAT_CADDY_BINARY=/path/to/caddy go test ./internal/outlet \
//   -run '^TestStressHTTP' -count=1 -timeout=5m -v
// MEERKAT_STRESS_SMOKE=1 reduces counts/durations for harness verification; its
// output is explicitly labeled and does not provide full stress coverage.
// JSON records use schema meerkat-http-stress-v1. Latency includes response-body
// draining; fixed 100us histogram buckets report an upper bound (overflow >30s).

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
	"github.com/lawnect/meerkat/internal/proxy"
)

const stressHistogramBins = 300001

type stressStats struct {
	mu                        sync.Mutex
	bins                      []uint64
	count, errors, unexpected uint64
	statuses                  map[int]uint64
	firstError                string
}

func newStressStats() *stressStats {
	return &stressStats{bins: make([]uint64, stressHistogramBins), statuses: map[int]uint64{}}
}

func (s *stressStats) record(elapsed time.Duration, status, expected int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count++
	bin := min(int(elapsed/(100*time.Microsecond)), len(s.bins)-1)
	s.bins[bin]++
	if err != nil {
		s.errors++
		if s.firstError == "" {
			s.firstError = err.Error()
		}
		return
	}
	s.statuses[status]++
	if status != expected {
		s.unexpected++
	}
}

func (s *stressStats) summary(name string, elapsed time.Duration) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	percentile := func(p uint64) float64 {
		target := (s.count*p + 99) / 100
		var n uint64
		for i, count := range s.bins {
			n += count
			if n >= target {
				return float64(i+1) / 10
			}
		}
		return 0
	}
	return map[string]any{"name": name, "elapsed_seconds": elapsed.Seconds(), "requests": s.count,
		"requests_per_second": float64(s.count) / elapsed.Seconds(), "statuses": s.statuses,
		"transport_or_body_errors": s.errors, "unexpected_statuses": s.unexpected, "first_error": s.firstError,
		"p50_ms_upper": percentile(50), "p95_ms_upper": percentile(95), "p99_ms_upper": percentile(99)}
}

func (s *stressStats) assertHealthy(t *testing.T) {
	t.Helper()
	if s.count == 0 || s.errors != 0 || s.unexpected != 0 {
		t.Errorf("socket workload failed: %+v", s.summary("failed", time.Second))
	}
}

func stressJSON(t *testing.T, kind string, value any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"schema": "meerkat-http-stress-v1", "smoke": os.Getenv("MEERKAT_STRESS_SMOKE") == "1", "kind": kind, "data": value})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}

func stressEnabled(t *testing.T) bool {
	t.Helper()
	if os.Getenv("MEERKAT_STRESS") != "1" {
		t.Skip("set MEERKAT_STRESS=1 for bounded synthetic local socket stress")
	}
	return os.Getenv("MEERKAT_STRESS_SMOKE") == "1"
}

// Match cmd/outlet's public server, including streaming-friendly absence of
// global Read/WriteTimeout. Client deadlines bound every workload operation.
func stressPublicServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.IdleTimeout = 30 * time.Second
	s.Config.MaxHeaderBytes = 32 << 10
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func stressClient(t *testing.T, tlsConfig *tls.Config, concurrency int) *http.Client {
	t.Helper()
	tr := &http.Transport{TLSClientConfig: tlsConfig, MaxConnsPerHost: concurrency, MaxIdleConns: concurrency,
		MaxIdleConnsPerHost: concurrency, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 3 * time.Second}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 4 * time.Second}
}

func stressSend(client *http.Client, address, host, path, ip string, expected int, stats *stressStats) {
	began := time.Now()
	req, err := http.NewRequest(http.MethodGet, address+path, nil)
	if err != nil {
		stats.record(time.Since(began), 0, expected, err)
		return
	}
	req.Host = host
	// This is used ONLY by the trusted direct-Outlet fixture. Caddy workloads
	// never send XFF, and cannot choose another client's identity this way.
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	req.Header.Set("User-Agent", "meerkat-local-stress")
	resp, err := client.Do(req)
	status := 0
	if err == nil {
		status = resp.StatusCode
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		closeErr := resp.Body.Close()
		if err == nil {
			err = closeErr
		}
	}
	stats.record(time.Since(began), status, expected, err)
}

// Bounded workers and a bounded channel: no per-request goroutine, no latency
// samples retained. A paced producer blocks if workers fall behind, so measured
// achieved rps and wall time expose overload instead of claiming an offered rate.
func stressRun(count, concurrency, rps int, duration time.Duration, send func(int)) time.Duration {
	start := time.Now()
	jobs := make(chan int, concurrency)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for i := range jobs {
				send(i)
			}
		})
	}
	var tick *time.Ticker
	if rps > 0 {
		tick = time.NewTicker(time.Second / time.Duration(rps))
		defer tick.Stop()
	}
	deadline := start.Add(duration)
	for i := 0; i < count; i++ {
		if duration > 0 && time.Now().After(deadline) {
			break
		}
		if tick != nil {
			<-tick.C
		}
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	return time.Since(start)
}

type stressResource struct {
	AtSeconds         float64 `json:"at_seconds"`
	HeapBytes         uint64  `json:"heap_bytes"`
	HeapObjects       uint64  `json:"heap_objects"`
	Goroutines        int     `json:"goroutines"`
	CaddyRSSBytes     uint64  `json:"caddy_rss_bytes,omitempty"`
	CgroupMemoryBytes uint64  `json:"cgroup_memory_bytes,omitempty"`
}

func stressResources(pid int) stressResource {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s := stressResource{HeapBytes: mem.HeapAlloc, HeapObjects: mem.HeapObjects, Goroutines: runtime.NumGoroutine()}
	if pid > 0 {
		data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					n, _ := strconv.ParseUint(fields[1], 10, 64)
					s.CaddyRSSBytes = n * 1024
				}
			}
		}
	}
	for _, path := range []string{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory/memory.usage_in_bytes"} {
		if data, err := os.ReadFile(path); err == nil {
			s.CgroupMemoryBytes, _ = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			break
		}
	}
	return s
}

func stressSample(t *testing.T, pid int) func() {
	t.Helper()
	start := time.Now()
	done, stopped := make(chan struct{}), make(chan struct{})
	var samples []stressResource
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			s := stressResources(pid)
			s.AtSeconds = time.Since(start).Seconds()
			if len(samples) < 360 {
				samples = append(samples, s)
			}
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { close(done); <-stopped; stressJSON(t, "resources", samples) }
}

// A subprocess can never leave an unbounded log file, even when testing a
// regressed Caddy binary. Retain the first 1MiB of startup diagnostics.
type stressCaddyOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *stressCaddyOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	keep := min(n, (1<<20)-len(w.data))
	w.data = append(w.data, p[:keep]...)
	return n, nil
}

func TestStressHTTPCaddyHostWaves(t *testing.T) {
	smoke := stressEnabled(t)
	binary := os.Getenv("MEERKAT_CADDY_BINARY")
	if binary == "" {
		t.Skip("set MEERKAT_CADDY_BINARY to the patched production Caddy executable")
	}
	version, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil || !strings.HasPrefix(string(version), "v2.11.7") {
		t.Fatalf("expected pinned Caddy2.11.7: %s %v", version, err)
	}
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/500" {
			w.WriteHeader(500)
		}
		_, _ = io.WriteString(w, "synthetic")
	}))
	t.Cleanup(f.store.Close)
	f.route(t, 0, "app.example.com")
	outlet := stressPublicServer(t, f.handler)
	dir := t.TempDir()
	cert, key := caddyTestCertificate(t, dir)
	httpsAddress, httpAddress, metricsAddress := caddyTestAddress(t), caddyTestAddress(t), caddyTestAddress(t)
	config := string(proxy.DynamicCaddyfile(strings.TrimPrefix(outlet.URL, "http://")))
	config = strings.Replace(config, "{\n", "{\n\tadmin off\n\tauto_https off\n\tdefault_bind 127.0.0.1\n", 1)
	config = strings.Replace(config, ":9090 {", ":"+strings.Split(metricsAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "http:// {", "http://:"+strings.Split(httpAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "https:// {", "https://:"+strings.Split(httpsAddress, ":")[1]+" {", 1)
	config = strings.Replace(config, "\ttls {\n\t\ton_demand\n\t\tissuer acme {\n\t\t\tdisable_tlsalpn_challenge\n\t\t}\n\t}", "\ttls "+cert+" "+key, 1)
	configPath := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	output := &stressCaddyOutput{}
	cmd := exec.Command(binary, "run", "--config", configPath, "--adapter", "caddyfile")
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dir, "XDG_CONFIG_HOME="+dir)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("Caddy diagnostics: %s", output.data)
		}
	})
	client := stressClient(t, &tls.Config{InsecureSkipVerify: true, ServerName: "app.example.com"}, 32) // synthetic certificate
	metricsClient := stressClient(t, nil, 1)
	readMetrics := func() (string, error) {
		r, err := metricsClient.Get("http://" + metricsAddress + "/metrics")
		if err != nil {
			return "", err
		}
		defer r.Body.Close()
		data, err := io.ReadAll(io.LimitReader(r.Body, (16<<20)+1))
		if len(data) > 16<<20 {
			return "", fmt.Errorf("metrics exceeded16MiB")
		}
		if r.StatusCode != 200 {
			return "", fmt.Errorf("metrics status%d", r.StatusCode)
		}
		return string(data), err
	}
	for deadline := time.Now().Add(10 * time.Second); ; {
		if _, err = readMetrics(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer stressSample(t, cmd.Process.Pid)()
	warm := newStressStats()
	stressSend(client, "https://"+httpsAddress, "app.example.com", "/", "", 200, warm)
	stressSend(client, "https://"+httpsAddress, "app.example.com", "/500", "", 500, warm)
	stressSend(client, "https://"+httpsAddress, "app.example.com:invalid-warm", "/", "", 404, warm)
	stressSend(stressClient(t, nil, 1), "http://"+httpAddress, "unknown.example.com", "/", "", 404, warm)
	warm.assertHealthy(t)
	phaseDuration := 5 * time.Second
	variants := 20000
	if smoke {
		phaseDuration = 100 * time.Millisecond
		variants = 100
	}
	stressJSON(t, "scenario", map[string]any{"runtime": strings.TrimSpace(string(version)), "limiter": "production defaults enabled", "normal_rps": 100, "attack_rps": 1000, "attack_workers": 32, "unique_variants_per_wave": variants, "waves": 2, "baseline_cooldown_seconds": phaseDuration.Seconds()})
	// Normal traffic is independently paced throughout the host flood. No
	// XFF is sent; the actual Caddy socket source remains loopback. Each
	// request belongs to the phase selected at its start, even if it finishes
	// after a phase boundary; durations are actual wall time, including the
	// short metric snapshot at the end of each phase.
	phaseNames := []string{"baseline", "wave_1", "wave_2", "recovery"}
	phaseStats := []*stressStats{newStressStats(), newStressStats(), newStressStats(), newStressStats()}
	var normalPhase atomic.Int32
	var phaseElapsed [4]time.Duration
	phaseStart := time.Now()
	switchPhase := func(next int) {
		now := time.Now()
		phaseElapsed[normalPhase.Load()] += now.Sub(phaseStart)
		phaseStart = now
		normalPhase.Store(int32(next))
	}
	stopNormal := make(chan struct{})
	normalDone := make(chan struct{})
	go func() {
		defer close(normalDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-stopNormal:
				return
			case <-ticker.C:
				path, want := "/", 200
				if i%10 == 0 {
					path, want = "/500", 500
				}
				selected := normalPhase.Load()
				stressSend(client, "https://"+httpsAddress, "app.example.com", path, "", want, phaseStats[selected])
				i++
			}
		}
	}()
	defer func() {
		close(stopNormal)
		<-normalDone
		phaseElapsed[normalPhase.Load()] += time.Since(phaseStart)
		for phase, stats := range phaseStats {
			if phaseElapsed[phase] == 0 {
				continue
			}
			stressJSON(t, "requests", stats.summary("caddy_normal_"+phaseNames[phase], phaseElapsed[phase]))
			stats.assertHealthy(t)
		}
	}()
	time.Sleep(phaseDuration)
	hostLabel := regexp.MustCompile(`(?:\{|,)host="([^"]*)"`)
	var baseBytes, baseSeries int
	metricsSnapshot := func(name string) {
		t.Helper()
		metrics, err := readMetrics()
		if err != nil {
			t.Fatal(err)
		}
		hosts := map[string]bool{}
		series := 0
		for _, line := range strings.Split(metrics, "\n") {
			if strings.HasPrefix(line, "caddy_http_") {
				series++
				if m := hostLabel.FindStringSubmatch(line); len(m) == 2 {
					hosts[m[1]] = true
				}
			}
		}
		if len(hosts) != 2 || !hosts["app.example.com"] || !hosts["_other"] {
			t.Errorf("unbounded metric hosts: %+v", hosts)
		}
		if baseBytes == 0 {
			baseBytes, baseSeries = len(metrics), series
		}
		// Dynamic runtime gauges and decimal counter widths may change;
		// request metric series must not grow with authority variants.
		if series != baseSeries || len(metrics) > baseBytes+(64<<10) {
			t.Errorf("metric growth: series%d->%d bytes%d->%d", baseSeries, series, baseBytes, len(metrics))
		}
		stressJSON(t, "caddy_metrics", map[string]any{"name": name, "bytes": len(metrics), "http_series": series, "hosts": hosts, "resources": stressResources(cmd.Process.Pid)})
	}
	metricsSnapshot("baseline")
	for wave := range 2 {
		switchPhase(wave + 1)
		stats := newStressStats()
		elapsed := stressRun(variants, 32, 1000, 0, func(i int) {
			n := wave*variants + i
			port := fmt.Sprintf("invalid-%d", n)
			if n%2 == 0 {
				port = fmt.Sprintf("%09d", n)
			}
			stressSend(client, "https://"+httpsAddress, "app.example.com:"+port, "/", "", 404, stats)
		})
		stats.assertHealthy(t)
		stressJSON(t, "requests", stats.summary(fmt.Sprintf("caddy_host_wave_%d", wave+1), elapsed))
		metricsSnapshot(fmt.Sprintf("wave_%d", wave+1))
	}
	switchPhase(3)
	time.Sleep(phaseDuration)
	metricsSnapshot("cooldown")
}

func TestStressHTTPOutletLogs(t *testing.T) {
	smoke := stressEnabled(t)
	var protectedUpstream atomic.Int64
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.env" || strings.HasPrefix(r.URL.Path, "/deny/") {
			protectedUpstream.Add(1)
		}
		if r.URL.Path == "/500" {
			w.WriteHeader(500)
		}
		_, _ = io.WriteString(w, "synthetic")
	}))
	t.Cleanup(f.store.Close)
	f.route(t, 0, "app.example.com")
	policies := control.AccessPolicies{Policies: []control.AccessPolicy{{ID: "stress-deny", Name: "Synthetic deny", Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: conditions(onPaths(control.BlockStartsWith, "/deny/"))}}}
	if _, err := f.db.SetAccessPolicies(context.Background(), policies, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := stressPublicServer(t, f.handler)
	client := stressClient(t, nil, 64)
	defer stressSample(t, 0)()
	phaseDuration := 10 * time.Second
	phaseRequestCap := 1_000_000
	if smoke {
		phaseDuration = 100 * time.Millisecond
		phaseRequestCap = 100_000
	}
	stressJSON(t, "scenario", map[string]any{"name": "outlet_logs", "concurrency": []int{16, 64}, "phase_seconds": phaseDuration.Seconds(), "phase_request_cap": phaseRequestCap, "mix": "60% success,20% app500,10% trap/banned,10% access deny", "throughput_limiter": "intentionally disabled only during four throughput phases", "isolation_limiter": "production defaults enabled", "identity": "synthetic trusted direct-Outlet XFF; distinct normal/app500/trap/access clients"})
	for _, enabled := range []bool{false, true} {
		f.requestLogging(t, enabled)
		f.failureLogging(t, enabled)
		accessLogging(t, f, enabled)
		// Throughput intentionally bypasses only the rate limiter. Block,
		// access, connection and logging budgets remain production defaults.
		cfg := control.DefaultRateLimit(f.adminHost)
		cfg.Enabled = false
		f.store.limiter.config.Store(compileRateLimit(cfg))
		for _, concurrency := range []int{16, 64} {
			stats := newStressStats()
			elapsed := stressRun(phaseRequestCap, concurrency, 0, phaseDuration, func(i int) {
				path, ip, want := "/", "198.51.100.10", 200
				switch i % 10 {
				case 6, 7:
					path, ip, want = "/500", "198.51.100.20", 500
				case 8:
					path, ip, want = "/.env", "198.51.100.30", 403
				case 9:
					path, ip, want = "/deny/private", "198.51.100.40", 403
				}
				stressSend(client, server.URL, "app.example.com", path, ip, want, stats)
			})
			stats.assertHealthy(t)
			stressJSON(t, "requests", stats.summary(fmt.Sprintf("outlet_logs_%t_concurrency_%d_limiter_disabled", enabled, concurrency), elapsed))
			if !enabled && (f.requestLog(t, "").Total != 0 || f.failureLog(t, "").Total != 0 || readLog[accessEntry](t, f, "access-log", "").Total != 0) {
				t.Fatal("disabled logs retained traffic")
			}
		}
	}
	// Restore default protection and prove isolation/recovery through real
	// sockets. Use unused budget slots so this deterministic correlation
	// check is independent of randomized slot collisions under the flood.
	cfg := control.DefaultRateLimit(f.adminHost)
	cfg.Endpoints = append(cfg.Endpoints, control.EndpointLimit{Host: "app.example.com", PathPrefix: "/protected-limit/", RPS: 1, Burst: 1, Per: control.RateEndpointIP})
	f.store.limiter.config.Store(compileRateLimit(cfg))
	isolation := newStressStats()
	isolationStart := time.Now()
	stressSend(client, server.URL, "app.example.com", "/protected-limit/first", "192.0.2.70", 200, isolation)
	// A 1rps endpoint removes timing dependence from protection verification;
	// the production global IP/subnet limits remain enabled throughout.
	stressSend(client, server.URL, "app.example.com", "/protected-limit/second", "192.0.2.70", 429, isolation)
	stressSend(client, server.URL, "app.example.com", "/protected-limit/other-client", "192.0.2.71", 200, isolation)
	stressSend(client, server.URL, "app.example.com", "/normal-after-rejection", "192.0.2.70", 200, isolation)
	isolation.assertHealthy(t)
	stressJSON(t, "requests", isolation.summary("outlet_default_globals_endpoint_isolation", time.Since(isolationStart)))
	f.store.limiter.config.Store(compileRateLimit(control.DefaultRateLimit(f.adminHost)))
	freshIP := func(start int) string {
		for n := start; n < start+10000; n++ {
			ip := fmt.Sprintf("203.%d.%d.%d", (n>>16)&255, (n>>8)&255, n&255)
			key := clientKey(ip)
			minute := uint64(time.Now().Unix()/60) << 32
			available := func(packed uint64) bool { return packed&^uint64(0xffffffff) != minute || packed&0xffffffff == 0 }
			if available(f.store.logs.requests.budget[f.store.logs.requests.slot(key)].Load()) && available(f.store.logs.requests.notable[f.store.logs.requests.slot(key)].Load()) && available(f.store.logs.failures.budget[f.store.logs.failures.slot(key)].Load()) && available(f.store.logs.failures.notable[f.store.logs.failures.slot(key)].Load()) && available(f.store.logs.access.budget[f.store.logs.access.slot(key)].Load()) {
				return ip
			}
		}
		t.Fatal("no unused synthetic client log budget slot")
		return ""
	}
	probe := newStressStats()
	type recoveryProbe struct {
		path, ip string
		status   int
	}
	probes := []recoveryProbe{{"/recovered", freshIP(100), 200}, {"/500", freshIP(200), 500}, {"/.env", freshIP(300), 403}, {"/deny/recovered", freshIP(400), 403}}
	for _, p := range probes {
		stressSend(client, server.URL, "app.example.com", p.path, p.ip, p.status, probe)
	}
	probe.assertHealthy(t)
	if protectedUpstream.Load() != 0 {
		t.Errorf("protected probes reached upstream %d times", protectedUpstream.Load())
	}
	requests := f.requestLog(t, "")
	failures := f.failureLog(t, "")
	access := readLog[accessEntry](t, f, "access-log", "")
	for _, p := range probes {
		query := "?source=" + p.ip
		r := f.requestLog(t, query)
		if r.Total != 1 || len(r.Entries) != 1 {
			t.Fatalf("recovery request missing %s: total%d", p.path, r.Total)
		}
		id := r.Entries[0].RequestID
		assertRequestID(t, id)
		if p.path == "/500" || p.path == "/.env" {
			failure := f.failureLog(t, "?q="+id)
			if failure.Total != 1 || failure.Entries[0].RequestID != id {
				t.Errorf("failure recovery correlation missing for%s", p.path)
			}
		}
		if strings.HasPrefix(p.path, "/deny/") {
			a := readLog[accessEntry](t, f, "access-log", "?q="+id)
			if a.Total != 1 || a.Entries[0].RequestID != id {
				t.Errorf("access recovery correlation missing")
			}
		}
	}
	if !smoke && (requests.Suppressed == 0 || failures.Suppressed == 0 || access.Suppressed == 0) {
		t.Error("full load did not exercise all three log suppression budgets")
	}
	for _, log := range []struct {
		name                      string
		total                     int
		suppressed, dropped, size int64
		limits                    eventlog.Limits
		wal                       int64
	}{
		{"request", requests.Total, requests.Suppressed, requests.Dropped, requests.SizeBytes, f.store.logs.requests.log.Limits(), f.store.logs.requests.log.WALSize()},
		{"failure", failures.Total, failures.Suppressed, failures.Dropped, failures.SizeBytes, f.store.logs.failures.log.Limits(), f.store.logs.failures.log.WALSize()},
		{"access", access.Total, access.Suppressed, access.Dropped, access.SizeBytes, f.store.logs.access.log.Limits(), f.store.logs.access.log.WALSize()},
	} {
		if log.size > log.limits.MaxBytes {
			t.Errorf("%s log size%d exceeds cap%d", log.name, log.size, log.limits.MaxBytes)
		}
		stressJSON(t, "log", map[string]any{"name": log.name, "stored": log.total, "suppressed": log.suppressed, "dropped": log.dropped, "size_bytes": log.size, "max_bytes": log.limits.MaxBytes, "wal_bytes": log.wal, "queue_capacity": 8192, "retry_buffer_capacity": 20000})
	}
	// Scan all stored rows in SQLite, not just the API's newest page. This
	// keeps QA memory bounded and catches blank evidence anywhere in the run.
	dir := filepath.Dir(f.store.path)
	for _, name := range []string{"control", "request-log", "failure-log", "access-log"} {
		path := filepath.Join(dir, name+".db")
		db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var check string
		err = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check)
		if err != nil || check != "ok" {
			t.Errorf("%s quick_check=%q: %v", name, check, err)
		}
		if name != "control" {
			table := strings.ReplaceAll(name, "-", "_")
			var blank int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE request_id='' OR request_id IS NULL").Scan(&blank); err != nil || blank != 0 {
				t.Errorf("%s blank request IDs=%d: %v", name, blank, err)
			}
			if name != "request-log" {
				if _, err := db.ExecContext(ctx, "ATTACH DATABASE ? AS requests", "file:"+filepath.Join(dir, "request-log.db")+"?mode=ro"); err != nil {
					t.Fatal(err)
				}
				var matched, unmatched int
				query := "SELECT COALESCE(SUM(r.request_id IS NOT NULL),0), COALESCE(SUM(r.request_id IS NULL),0) FROM " + table + " e LEFT JOIN requests.request_log r ON e.request_id=r.request_id"
				if err := db.QueryRowContext(ctx, query).Scan(&matched, &unmatched); err != nil {
					t.Fatal(err)
				}
				if matched == 0 {
					t.Errorf("%s has no stored request correlations", name)
				}
				stressJSON(t, "correlation", map[string]any{"log": name, "matched_request_ids": matched, "unmatched_request_ids": unmatched, "note": "budgets and queue drops are independent per log; recovery probes must correlate exactly"})
			}
		}
		cancel()
		_ = db.Close()
		stressJSON(t, "sqlite", map[string]any{"database": name, "quick_check": check})
	}
	// Check the writer's public fixed caps rather than inspecting its private
	// buffer concurrently. Writer queue overflow is separately reported above.
	if f.store.logs.requests.writer.BufferCap != 20000 || f.store.logs.failures.writer.BufferCap != 20000 || f.store.logs.access.writer.BufferCap != 20000 {
		t.Error("unexpected writer buffer bounds")
	}
}
