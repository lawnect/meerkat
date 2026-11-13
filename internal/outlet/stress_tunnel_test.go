package outlet

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/rathole"
)

// TestStressTunnelGuardRealRathole is intentionally opt-in. Run it alone in a
// Linux container with --network none --cpus 2 --memory 1g (or 2g), using the
// real rathole 0.5.0 binary. All listeners and attack sources are loopback.
// The guard's production limits, clocks and timeouts remain unchanged.
func TestStressTunnelGuardRealRathole(t *testing.T) {
	if os.Getenv("MEERKAT_STRESS") != "1" || os.Getenv("MEERKAT_RATHOLE") == "" {
		t.Skip("set MEERKAT_STRESS=1 and MEERKAT_RATHOLE to opt into the isolated real-tunnel stress test")
	}
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux loopback source addresses and /proc resource measurements")
	}
	binary := os.Getenv("MEERKAT_RATHOLE")
	version, err := exec.Command(binary, "--version").CombinedOutput()
	versionFields := make(map[string]string)
	for line := range strings.SplitSeq(string(version), "\n") {
		if name, value, ok := strings.Cut(line, ":"); ok {
			versionFields[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
	}
	features := strings.Split(versionFields["cargo Features"], ",")
	if err != nil || versionFields["Build Version"] != "0.5.0" || !slices.Contains(features, "noise") || !slices.Contains(features, "client") || !slices.Contains(features, "server") {
		t.Fatalf("requires rathole Build Version 0.5.0 with noise, client, server features: version=%q err=%v", version, err)
	}
	const workers, knownSockets, junkWorkers = 16, 256, 32
	const knownAttemptsPerSocket, unknownOfferLimit = 8, 8192
	const baselineTime, attackTime, recoveryTime = 5 * time.Second, 35 * time.Second, 5 * time.Second
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	internal, service, token := freeAddress(t), freeAddress(t), Token()
	config, err := rathole.Server(internal, base64.StdEncoding.EncodeToString(key.Bytes()), []rathole.ServerService{{Name: "stress", Token: token, BindAddress: service}})
	if err != nil {
		t.Fatal(err)
	}
	startRathole(t, binary, "server", string(config))
	// The relay forwards bytes as they arrive, without buffering a handshake.
	// It observes a unique partial-frame marker; it cannot mask a regression
	// by shielding rathole's serial Noise accept loop from incomplete frames.
	partial := handshakeFrame(bytes.Repeat([]byte{0xa5}, 48))
	partial = partial[:len(partial)-1]
	observer := newStressTunnelObserver(t, internal, partial)
	g := startGuard(t, observer.listener.Addr().String())
	g.learn("127.0.0.1")
	logJSON := func(kind string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("STRESS_TUNNEL_%s %s", kind, data)
	}
	logJSON("CONFIG", map[string]any{
		"rathole": versionFields,
		"scope":   "real Noise tunnel component with transparent observer extra TCP hop; excludes Outlet HTTP and Caddy",
		"workers": workers, "known_sockets": knownSockets, "max_known_attempts": knownSockets * knownAttemptsPerSocket,
		"unknown_workers": junkWorkers, "max_unknown_offers": unknownOfferLimit, "unknown_offered_per_second": 200,
		"baseline_seconds": baselineTime.Seconds(), "attack_seconds": attackTime.Seconds(), "recovery_seconds": recoveryTime.Seconds(),
		"first_frame_timeout_seconds": g.firstBytes.Seconds(), "request_deadline_seconds": 2,
		"unknown_per_ip_concurrent": guardClientConcurrent, "unknown_total_concurrent": guardTotalConcurrent, "unknown_global_per_second": guardTotalPerSecond,
	})
	defer func() {
		logJSON("FINAL", map[string]any{"resources": stressTunnelResources(g), "observer_connections": observer.accepted.Load(), "partial_frames_forwarded": observer.partials.Load()})
	}()

	// With no real Connector yet, slots and upstream dials are unambiguous.
	// Explicit client close must free all known-address admission slots.
	var preflight []net.Conn
	for range 16 {
		conn, err := net.DialTimeout("tcp", g.address().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		preflight = append(preflight, conn)
		t.Cleanup(func() { conn.Close() })
		if _, err := conn.Write(partial); err != nil {
			t.Fatal(err)
		}
	}
	stressTunnelWait(t, "preflight partial admissions", 2*time.Second, func() bool { return stressTunnelInflight(&g.knownConns) == 16 })
	time.Sleep(100 * time.Millisecond)
	if got := observer.accepted.Load(); got != 0 {
		t.Fatalf("incomplete first frames opened %d real-rathole connections", got)
	}
	for _, conn := range preflight {
		conn.Close()
	}
	stressTunnelWait(t, "closed partial slots to be released", 2*time.Second, func() bool { return stressTunnelInflight(&g.knownConns) == 0 })
	logJSON("PREFLIGHT", map[string]any{"explicitly_closed_slots_released": len(preflight), "upstream_connections": observer.accepted.Load()})

	body := strings.Repeat("meerkat-noise-stress-body\n", 128)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
	defer app.Close()
	clientConfig := fmt.Sprintf("[client]\nremote_addr = %q\nretry_interval = 1\n\n[client.transport]\ntype = \"noise\"\n\n[client.transport.noise]\npattern = \"Noise_NK_25519_ChaChaPoly_BLAKE2s\"\nremote_public_key = %q\n\n[client.services.stress]\ntype = \"tcp\"\ntoken = %q\nlocal_addr = %q\n", g.address().String(), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), token, strings.TrimPrefix(app.URL, "http://"))
	startRathole(t, binary, "client", clientConfig)
	transport := &http.Transport{DisableKeepAlives: true, MaxConnsPerHost: workers}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Timeout: 2 * time.Second, Transport: transport}
	visit := func() error {
		response, err := httpClient.Get("http://" + service)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		got, err := io.ReadAll(io.LimitReader(response.Body, int64(len(body)+1)))
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK || string(got) != body {
			return fmt.Errorf("invalid response status=%d bytes=%d", response.StatusCode, len(got))
		}
		return nil
	}
	stressTunnelWait(t, "real Noise service", 20*time.Second, func() bool { return visit() == nil })
	logJSON("BEFORE", stressTunnelResources(g))
	baseline := stressTunnelRequests(baselineTime, workers, visit)
	logJSON("BASELINE", baseline)
	before := stressTunnelResources(g)
	logJSON("BEFORE_ATTACK", before)

	attackCtx, stopAttack := context.WithCancel(context.Background())
	defer stopAttack()
	var known, junk stressTunnelAttack
	var attackers sync.WaitGroup
	for range knownSockets {
		attackers.Go(func() {
			for attempt := 0; attempt < knownAttemptsPerSocket && attackCtx.Err() == nil; attempt++ {
				known.offered.Add(1)
				conn, err := net.DialTimeout("tcp", g.address().String(), time.Second)
				if err != nil {
					known.dialErrors.Add(1)
					continue
				}
				known.dialed.Add(1)
				cut := context.AfterFunc(attackCtx, func() { conn.Close() })
				began := time.Now()
				_ = conn.SetDeadline(began.Add(guardFirstBytes + 2*time.Second))
				if _, err := conn.Write(partial); err == nil {
					known.written.Add(1)
					_, err = conn.Read(make([]byte, 1))
					if err != nil && !isTimeout(err) && attackCtx.Err() == nil {
						known.remoteClosed.Add(1)
						if time.Since(began) >= guardFirstBytes-time.Second {
							known.timeoutClosed.Add(1)
						}
					} else if isTimeout(err) && attackCtx.Err() == nil {
						known.readTimeouts.Add(1)
					}
				}
				cut()
				conn.Close()
			}
		})
	}
	// A paced shared offer channel bounds work and worker count even when
	// the peer resets immediately. Unknown junk uses a distinct source IP.
	offers := make(chan struct{})
	attackers.Go(func() {
		defer close(offers)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for offered := 0; offered < unknownOfferLimit; offered++ {
			select {
			case <-attackCtx.Done():
				return
			case <-ticker.C:
				select {
				case offers <- struct{}{}:
				case <-attackCtx.Done():
					return
				}
			}
		}
	})
	for range junkWorkers {
		attackers.Go(func() {
			dialer := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2)}, Timeout: time.Second}
			frame := handshakeFrame(bytes.Repeat([]byte{0x5a}, 48))
			for range offers {
				junk.offered.Add(1)
				conn, err := dialer.DialContext(attackCtx, "tcp", g.address().String())
				if err != nil {
					junk.dialErrors.Add(1)
					continue
				}
				junk.dialed.Add(1)
				cut := context.AfterFunc(attackCtx, func() { conn.Close() })
				_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
				if _, err := conn.Write(frame); err == nil {
					junk.written.Add(1)
					_, err = conn.Read(make([]byte, 1))
					if err != nil && !isTimeout(err) && attackCtx.Err() == nil {
						junk.remoteClosed.Add(1)
					} else if isTimeout(err) && attackCtx.Err() == nil {
						junk.readTimeouts.Add(1)
					}
				}
				cut()
				conn.Close()
			}
		})
	}
	// Measurements run concurrently with continuous requests; only the
	// calling test goroutine emits logs and failures.
	var during []stressTunnelResource
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-attackCtx.Done():
				return
			case <-ticker.C:
				during = append(during, stressTunnelResources(g))
			}
		}
	}()
	underAttack := stressTunnelRequests(attackTime, workers, visit)
	stopAttack()
	attackers.Wait()
	<-monitorDone
	afterAttack := g.stats()
	logJSON("ATTACK", map[string]any{
		"requests": underAttack, "known_partial": known.snapshot(), "unknown_junk": junk.snapshot(),
		"unknown_junk_forwarded_to_rathole": observer.junk.Load(),
		"guard_refused_rate":                afterAttack.Refused.Rate - before.Guard.Refused.Rate,
		"guard_refused_busy":                afterAttack.Refused.Busy - before.Guard.Refused.Busy,
		"guard_refused_idle":                afterAttack.Refused.Idle - before.Guard.Refused.Idle,
		"resources_each_second":             during,
	})
	stressTunnelWait(t, "unknown guard slots after attack close", guardAfterHangup+3*time.Second, func() bool { return g.unknownConns.total.Load() == 0 })
	logJSON("COOLDOWN", stressTunnelResources(g))
	recovery := stressTunnelRequests(recoveryTime, workers, visit)
	logJSON("RECOVERY", recovery)
	stressTunnelWait(t, "all partial slots released, leaving only complete legitimate channels", 3*time.Second, func() bool {
		return g.unknownConns.total.Load() == 0 && stressTunnelInflight(&g.knownConns) == g.active.Load() && stressTunnelInflight(&g.knownConns) <= before.KnownInflight+workers*2
	})
	logJSON("AFTER", stressTunnelResources(g))
	if observer.partials.Load() != 0 {
		t.Errorf("%d known-address partial handshakes reached real rathole", observer.partials.Load())
	}
	if known.timeoutClosed.Load() < knownSockets || known.readTimeouts.Load() != 0 {
		t.Errorf("known partials did not expire and replenish normally: %+v", known.snapshot())
	}
	var maxKnown, maxUnknown int64
	for _, snapshot := range during {
		maxKnown, maxUnknown = max(maxKnown, snapshot.KnownInflight), max(maxUnknown, snapshot.UnknownInflight)
	}
	if maxKnown < knownSockets || maxUnknown > guardClientConcurrent {
		t.Errorf("unexpected attack inflight peaks known=%d unknown=%d", maxKnown, maxUnknown)
	}
	after := g.stats()
	if after.Refused.Rate+after.Refused.Busy <= before.Guard.Refused.Rate+before.Guard.Refused.Busy {
		t.Error("unknown junk load did not exercise admission refusals")
	}
	for name, phase := range map[string]stressTunnelRequestSummary{"baseline": baseline, "attack": underAttack, "recovery": recovery} {
		if phase.Errors != 0 || phase.Successes < workers {
			t.Errorf("%s requests failed liveness/body checks: %+v", name, phase)
		}
		for window, successes := range phase.SuccessesPerFiveSeconds {
			if successes == 0 {
				t.Errorf("%s had no successful requests in five-second window %d", name, window)
			}
		}
	}
}

type stressTunnelAttack struct {
	offered, dialed, dialErrors, written, remoteClosed, timeoutClosed, readTimeouts atomic.Int64
}

func (a *stressTunnelAttack) snapshot() map[string]int64 {
	return map[string]int64{"offered": a.offered.Load(), "tcp_dialed": a.dialed.Load(), "dial_errors": a.dialErrors.Load(), "frame_written": a.written.Load(), "remote_closed": a.remoteClosed.Load(), "closed_after_guard_timeout": a.timeoutClosed.Load(), "read_deadline_expirations": a.readTimeouts.Load()}
}

type stressTunnelRequestSummary struct {
	Seconds                  float64  `json:"seconds"`
	Successes                int      `json:"successes"`
	Errors                   int      `json:"errors"`
	RequestsPerSecond        float64  `json:"requests_per_second"`
	P50UpperBoundMillis      float64  `json:"p50_upper_bound_ms"`
	P95UpperBoundMillis      float64  `json:"p95_upper_bound_ms"`
	P99UpperBoundMillis      float64  `json:"p99_upper_bound_ms"`
	MaxMillis                float64  `json:"max_ms"`
	HistogramResolutionMicro int64    `json:"histogram_resolution_us"`
	HistogramLimitMillis     int64    `json:"histogram_finite_limit_ms"`
	HistogramOverflowCount   uint64   `json:"histogram_overflow_count"`
	ErrorSamples             []string `json:"error_samples,omitempty"`
	SuccessesPerFiveSeconds  []int    `json:"successes_per_five_seconds"`
}

const (
	stressTunnelLatencyResolution = 100 * time.Microsecond
	stressTunnelLatencyLimit      = 2 * time.Second
)

// Fixed storage: 20,001 finite buckets (including zero) and one overflow
// bucket, independent of request throughput. Percentiles use nearest ranks;
// finite bucket values are upper bounds with <100us quantization error. An
// overflow percentile uses the exact observed maximum as its upper bound.
type stressTunnelLatencyHistogram struct {
	buckets [stressTunnelLatencyLimit/stressTunnelLatencyResolution + 2]uint64
	count   uint64
	maximum time.Duration
}

func (h *stressTunnelLatencyHistogram) add(latency time.Duration) {
	index := len(h.buckets) - 1
	if latency <= stressTunnelLatencyLimit {
		index = int((latency + stressTunnelLatencyResolution - 1) / stressTunnelLatencyResolution)
	}
	h.buckets[index]++
	h.count++
	h.maximum = max(h.maximum, latency)
}

func (h *stressTunnelLatencyHistogram) percentileUpperBound(percent int) time.Duration {
	if h.count == 0 {
		return 0
	}
	rank := (uint64(percent)*h.count + 99) / 100
	var cumulative uint64
	for index, count := range h.buckets {
		cumulative += count
		if cumulative >= rank {
			if index == len(h.buckets)-1 {
				return h.maximum
			}
			return time.Duration(index) * stressTunnelLatencyResolution
		}
	}
	return h.maximum
}

func stressTunnelRequests(duration time.Duration, workers int, visit func() error) stressTunnelRequestSummary {
	began := time.Now()
	end := began.Add(duration)
	var mu sync.Mutex
	var latencies stressTunnelLatencyHistogram
	summary := stressTunnelRequestSummary{
		SuccessesPerFiveSeconds:  make([]int, int((duration+5*time.Second-1)/(5*time.Second))),
		HistogramResolutionMicro: stressTunnelLatencyResolution.Microseconds(),
		HistogramLimitMillis:     stressTunnelLatencyLimit.Milliseconds(),
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for time.Now().Before(end) {
				start := time.Now()
				err := visit()
				took := time.Since(start)
				mu.Lock()
				latencies.add(took)
				if err != nil {
					summary.Errors++
					if len(summary.ErrorSamples) < 5 {
						summary.ErrorSamples = append(summary.ErrorSamples, err.Error())
					}
				} else {
					summary.Successes++
					window := min(int(start.Sub(began)/(5*time.Second)), len(summary.SuccessesPerFiveSeconds)-1)
					summary.SuccessesPerFiveSeconds[window]++
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	summary.Seconds = time.Since(began).Seconds()
	summary.RequestsPerSecond = float64(summary.Successes) / summary.Seconds
	summary.P50UpperBoundMillis = float64(latencies.percentileUpperBound(50)) / float64(time.Millisecond)
	summary.P95UpperBoundMillis = float64(latencies.percentileUpperBound(95)) / float64(time.Millisecond)
	summary.P99UpperBoundMillis = float64(latencies.percentileUpperBound(99)) / float64(time.Millisecond)
	summary.MaxMillis = float64(latencies.maximum) / float64(time.Millisecond)
	summary.HistogramOverflowCount = latencies.buckets[len(latencies.buckets)-1]
	return summary
}

func TestStressTunnelLatencyHistogram(t *testing.T) {
	var histogram stressTunnelLatencyHistogram
	if got := histogram.percentileUpperBound(99); got != 0 {
		t.Fatalf("empty histogram p99 = %v", got)
	}
	for _, latency := range []time.Duration{time.Nanosecond, 100 * time.Microsecond, 100*time.Microsecond + time.Nanosecond, 2 * time.Second, 2*time.Second + time.Nanosecond, 3 * time.Second} {
		histogram.add(latency)
	}
	if histogram.count != 6 || histogram.buckets[1] != 2 || histogram.buckets[2] != 1 || histogram.buckets[20000] != 1 || histogram.buckets[20001] != 2 {
		t.Fatalf("incorrect boundary buckets: count=%d <=100us=%d <=200us=%d <=2s=%d overflow=%d", histogram.count, histogram.buckets[1], histogram.buckets[2], histogram.buckets[20000], histogram.buckets[20001])
	}
	for _, test := range []struct {
		percent int
		upper   time.Duration
	}{{50, 200 * time.Microsecond}, {66, 2 * time.Second}, {95, 3 * time.Second}, {99, 3 * time.Second}} {
		if got := histogram.percentileUpperBound(test.percent); got != test.upper {
			t.Errorf("p%d upper bound = %v, want %v", test.percent, got, test.upper)
		}
	}
}

func stressTunnelWait(t *testing.T, what string, timeout time.Duration, ready func() bool) {
	t.Helper()
	for end := time.Now().Add(timeout); ; time.Sleep(20 * time.Millisecond) {
		if ready() {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func stressTunnelInflight(inflight *inflight) int64 {
	var total int64
	for index := range inflight.shards {
		shard := &inflight.shards[index]
		shard.mu.Lock()
		for _, n := range shard.counts {
			total += int64(n)
		}
		shard.mu.Unlock()
	}
	return total
}

type stressTunnelResource struct {
	At              string           `json:"at"`
	Goroutines      int              `json:"goroutines"`
	HeapBytes       uint64           `json:"heap_bytes"`
	HeapInuseBytes  uint64           `json:"heap_inuse_bytes"`
	RuntimeSysBytes uint64           `json:"runtime_sys_bytes"`
	SelfFDs         int              `json:"self_fds"`
	SelfRSSBytes    int64            `json:"self_rss_bytes"`
	ChildFDs        int              `json:"child_fds"`
	ChildRSSBytes   int64            `json:"child_rss_bytes"`
	CgroupBytes     int64            `json:"cgroup_memory_bytes"`
	KnownInflight   int64            `json:"known_inflight"`
	UnknownInflight int64            `json:"unknown_inflight"`
	Guard           tunnelGuardStats `json:"guard"`
}

func stressTunnelResources(g *tunnelGuard) stressTunnelResource {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	resource := stressTunnelResource{At: time.Now().UTC().Format(time.RFC3339Nano), Goroutines: runtime.NumGoroutine(), HeapBytes: memory.HeapAlloc, HeapInuseBytes: memory.HeapInuse, RuntimeSysBytes: memory.Sys, KnownInflight: stressTunnelInflight(&g.knownConns), UnknownInflight: g.unknownConns.total.Load(), Guard: g.stats()}
	processResource := func(pid int) (int, int64) {
		fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
		statm, _ := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid))
		var pages int64
		if fields := strings.Fields(string(statm)); len(fields) > 1 {
			pages, _ = strconv.ParseInt(fields[1], 10, 64)
		}
		return len(fds), pages * int64(os.Getpagesize())
	}
	resource.SelfFDs, resource.SelfRSSBytes = processResource(os.Getpid())
	// rathole processes are direct children of the test; collect their
	// descriptors and RSS separately from the Go runtime's measurements.
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
		if len(fields) < 2 || fields[1] != strconv.Itoa(os.Getpid()) {
			continue
		}
		fds, rss := processResource(pid)
		resource.ChildFDs += fds
		resource.ChildRSSBytes += rss
	}
	current, _ := os.ReadFile("/sys/fs/cgroup/memory.current")
	resource.CgroupBytes, _ = strconv.ParseInt(strings.TrimSpace(string(current)), 10, 64)
	return resource
}

type stressTunnelObserver struct {
	listener net.Listener
	accepted atomic.Int64
	partials atomic.Int64
	junk     atomic.Int64
}

func newStressTunnelObserver(t *testing.T, upstream string, marker []byte) *stressTunnelObserver {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	observer := &stressTunnelObserver{listener: listener}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			observer.accepted.Add(1)
			wg.Go(func() {
				defer conn.Close()
				cutClient := context.AfterFunc(ctx, func() { conn.Close() })
				defer cutClient()
				target, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", upstream)
				if err != nil {
					return
				}
				defer target.Close()
				cutTarget := context.AfterFunc(ctx, func() { target.Close() })
				defer cutTarget()
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					reader := &stressTunnelMarkerReader{Reader: conn, marker: marker, partials: &observer.partials, junk: &observer.junk}
					_, _ = io.Copy(target, reader)
					_ = target.(*net.TCPConn).CloseWrite()
				}()
				_, _ = io.Copy(conn, target)
				_ = conn.(*net.TCPConn).CloseWrite()
				_ = conn.SetReadDeadline(time.Now().Add(guardAfterHangup))
				<-finished
			})
		}
	})
	t.Cleanup(func() { cancel(); listener.Close(); wg.Wait() })
	return observer
}

type stressTunnelMarkerReader struct {
	io.Reader
	marker   []byte
	first    []byte
	partials *atomic.Int64
	junk     *atomic.Int64
	done     bool
}

func (r *stressTunnelMarkerReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if !r.done {
		r.first = append(r.first, p[:min(n, len(r.marker)-len(r.first))]...)
		if len(r.first) == len(r.marker) {
			if bytes.Equal(r.first, r.marker) {
				r.partials.Add(1)
			}
			if r.first[0] == 48 && r.first[1] == 0 && bytes.Equal(r.first[2:], bytes.Repeat([]byte{0x5a}, len(r.marker)-2)) {
				r.junk.Add(1)
			}
			r.done = true
		}
	}
	return n, err
}

// The measurement hop must pass incomplete frames through immediately, or
// the stress test could pass even when the guard forwards them prematurely.
func TestStressTunnelObserverForwardsPartialImmediately(t *testing.T) {
	target := newEchoRathole(t)
	marker := handshakeFrame(bytes.Repeat([]byte{0xa5}, 48))
	marker = marker[:len(marker)-1]
	observer := newStressTunnelObserver(t, target.address(), marker)
	conn, err := net.DialTimeout("tcp", observer.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	for _, fragment := range [][]byte{marker[:1], marker[1:9], marker[9:]} {
		if _, err := conn.Write(fragment); err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(fragment))
		if _, err := io.ReadFull(conn, got); err != nil || !bytes.Equal(got, fragment) {
			t.Fatalf("incomplete frame wasn't immediately forwarded: got=%x err=%v", got, err)
		}
	}
	if observer.accepted.Load() != 1 || observer.partials.Load() != 1 || observer.junk.Load() != 0 {
		t.Fatalf("observer counts accepted=%d partials=%d junk=%d", observer.accepted.Load(), observer.partials.Load(), observer.junk.Load())
	}
}
