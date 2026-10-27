package outlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	inflightShards = 256
	// minRateGrace is the head start of the minimum upload speed when the
	// idle timeout, which normally sets it, is off.
	minRateGrace = 30 * time.Second
)

var errUploadTooLarge = errors.New("request body too large")

// connLimits is the compiled, immutable form of control.ConnectionLimits.
type connLimits struct {
	control.ConnectionLimits
	maxUpload int64            // bytes; 0 is no limit
	uploads   map[string]int64 // per host, overriding maxUpload
	idle      time.Duration
	minRate   float64 // upload bytes per second
}

func compileConnectionLimits(c control.ConnectionLimits) *connLimits {
	l := &connLimits{
		ConnectionLimits: c,
		maxUpload:        int64(c.MaxUploadMB) << 20,
		uploads:          make(map[string]int64, len(c.Hosts)),
		idle:             time.Duration(c.IdleTimeoutSeconds) * time.Second,
		minRate:          float64(c.MinUploadKBPerSecond) * 1024,
	}
	for _, h := range c.Hosts {
		l.uploads[h.Host] = int64(h.MaxUploadMB) << 20
	}
	return l
}

func (l *connLimits) uploadLimit(host string) int64 {
	if n, ok := l.uploads[host]; ok {
		return n
	}
	return l.maxUpload
}

type inflightShard struct {
	mu     sync.Mutex
	counts map[string]int
}

// inflight counts requests in progress per client and, for proxied
// requests, in total. A client leaves the map when its last request ends,
// so memory follows the requests actually open.
type inflight struct {
	shards [inflightShards]inflightShard
	total  atomic.Int64
}

func (f *inflight) acquire(key string, max int) bool {
	shard := &f.shards[shardOf(key, inflightShards)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.counts[key] >= max {
		return false
	}
	if shard.counts == nil {
		shard.counts = map[string]int{}
	}
	shard.counts[key]++
	return true
}

func (f *inflight) release(key string) {
	shard := &f.shards[shardOf(key, inflightShards)]
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if n := shard.counts[key] - 1; n > 0 {
		shard.counts[key] = n
	} else {
		delete(shard.counts, key)
	}
}

func (f *inflight) acquireTotal(max int) bool {
	if f.total.Add(1) > int64(max) {
		f.total.Add(-1)
		return false
	}
	return true
}

// connectionLimiter is the state of the connection limits.
type connectionLimiter struct {
	config   atomic.Pointer[connLimits]
	inflight inflight
}

func (s *Store) reloadConnectionLimits(ctx context.Context) error {
	if s.control == nil || s.connections == nil {
		return nil
	}
	limits, err := s.control.ConnectionLimits(ctx)
	if err != nil {
		return err
	}
	s.connections.config.Store(compileConnectionLimits(limits))
	return nil
}

// connectionLimits never returns nil, so callers need no checks; without
// stored settings nothing is limited.
func (s *Store) connectionLimits() *connLimits {
	if s.connections != nil {
		if limits := s.connections.config.Load(); limits != nil {
			return limits
		}
	}
	return &connLimits{}
}

// refuse answers a request turned away before any work, like limitRequest.
func refuse(w http.ResponseWriter, status int, retryAfter, message string) {
	w.Header().Set("Retry-After", retryAfter)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, message)
}

func (s *Store) recordRefusal(kind, ip string) {
	if s.limiter == nil {
		return
	}
	hit := rejection{limit: kind, ip: ip}
	if ip != "" {
		hit.subnet = subnetOf(ip)
	}
	s.limiter.stats.record(s.now(), hit)
}

// admitClient holds a slot for the client's address, or answers 429 and
// reports false. A non-empty key must be released when the request ends.
func (s *Store) admitClient(w http.ResponseWriter, r *http.Request, limits *connLimits) (key string, ok bool) {
	ip, _ := requestOrigin(r)
	if limits.PerIPConcurrent == 0 || ip == "" || s.connections == nil {
		return "", true
	}
	key = clientKey(ip)
	if !s.connections.inflight.acquire(key, limits.PerIPConcurrent) {
		s.recordRefusal("concurrent", ip)
		refuse(w, http.StatusTooManyRequests, "1", "Too many requests in progress from this address\n")
		return "", false
	}
	return key, true
}

// admitProxy holds one of the slots shared by all proxied requests, or
// answers 503 and reports false. When held, release it when the request ends.
func (s *Store) admitProxy(w http.ResponseWriter, r *http.Request, limits *connLimits) (held, ok bool) {
	if limits.TotalConcurrent == 0 || s.connections == nil {
		return false, true
	}
	if !s.connections.inflight.acquireTotal(limits.TotalConcurrent) {
		ip, _ := requestOrigin(r)
		s.recordRefusal("capacity", ip)
		refuse(w, http.StatusServiceUnavailable, "5", "Server busy\n")
		return false, false
	}
	return true, true
}

// clientBody is a public request's body. It enforces the upload size and
// speed limits, and records how reading failed so the proxy can tell the
// client's fault from the service's. Only the goroutine reading the body
// touches the plain fields.
type clientBody struct {
	io.ReadCloser
	rc      *http.ResponseController
	idle    time.Duration
	minRate float64
	start   time.Time
	limit   int64 // bytes; 0 is no limit
	read    int64
	done    bool

	complete atomic.Bool // read to the end
	failed   atomic.Bool
	timedOut atomic.Bool
	tooLarge atomic.Bool
}

// deadline is when the next read must have returned: within the idle
// timeout, and in time to keep the minimum average speed since the request
// began.
func (b *clientBody) deadline(now time.Time) time.Time {
	var at time.Time
	if b.idle > 0 {
		at = now.Add(b.idle)
	}
	if b.minRate > 0 {
		grace := b.idle
		if grace == 0 {
			grace = minRateGrace
		}
		paced := b.start.Add(grace + time.Duration(float64(b.read)/b.minRate*float64(time.Second)))
		if at.IsZero() || paced.Before(at) {
			at = paced
		}
	}
	return at
}

func (b *clientBody) Read(p []byte) (int, error) {
	if b.tooLarge.Load() {
		return 0, errUploadTooLarge
	}
	if b.limit > 0 && int64(len(p)) > b.limit-b.read+1 {
		p = p[:b.limit-b.read+1]
	}
	// Once the body ends net/http reads the connection in the background
	// and clears the deadline itself; setting one then would cut it.
	if !b.done && (b.idle > 0 || b.minRate > 0) {
		_ = b.rc.SetReadDeadline(b.deadline(time.Now()))
	}
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if b.limit > 0 && b.read > b.limit {
		n -= int(b.read - b.limit)
		b.read = b.limit
		b.tooLarge.Store(true)
		return n, errUploadTooLarge
	}
	if err != nil {
		b.done = true
		if err == io.EOF {
			b.complete.Store(true)
		} else {
			b.failed.Store(true)
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				b.timedOut.Store(true)
			}
		}
	}
	return n, err
}

// Close cuts an unfinished body instead of letting net/http drain it, as
// finishTransfer does; the proxy closes the body before the handler returns.
func (b *clientBody) Close() error {
	b.cut()
	return b.ReadCloser.Close()
}

// cut makes any read of the rest of an unfinished body fail at once.
func (b *clientBody) cut() {
	if !b.complete.Load() {
		_ = b.rc.SetReadDeadline(time.Unix(1, 0))
	}
}

// stallWriter gives every write to the client the idle timeout to finish,
// so a client that stops reading is cut. Pauses between writes, while the
// service prepares more, do not count.
type stallWriter struct {
	http.ResponseWriter
	rc   *http.ResponseController
	idle time.Duration
}

func (w *stallWriter) arm() { _ = w.rc.SetWriteDeadline(time.Now().Add(w.idle)) }

func (w *stallWriter) WriteHeader(code int) {
	w.arm()
	w.ResponseWriter.WriteHeader(code)
}

func (w *stallWriter) Write(p []byte) (int, error) {
	w.arm()
	return w.ResponseWriter.Write(p)
}

func (w *stallWriter) FlushError() error {
	w.arm()
	return w.rc.Flush()
}

func (w *stallWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// guardTransfer installs the body and response deadlines. The caller defers
// finishTransfer with its results.
func guardTransfer(w http.ResponseWriter, r *http.Request, limits *connLimits) (http.ResponseWriter, *clientBody) {
	rc := http.NewResponseController(w)
	var body *clientBody
	if r.Body != nil && r.Body != http.NoBody {
		body = &clientBody{ReadCloser: r.Body, rc: rc, idle: limits.idle, minRate: limits.minRate, start: time.Now()}
		r.Body = body
	}
	if limits.idle > 0 {
		w = &stallWriter{ResponseWriter: w, rc: rc, idle: limits.idle}
	}
	return w, body
}

// finishTransfer runs as the handler returns. net/http then writes the end
// of the response and drains what the handler left of the request body,
// under the deadlines set here. The write gets the idle timeout. An
// unfinished body gets none: nothing counts the connection once the handler
// has returned, so a client that sent part of a body and stopped is cut
// rather than waited for, and the connection closes after the response.
func finishTransfer(w http.ResponseWriter, body *clientBody) {
	if stall, ok := w.(*stallWriter); ok {
		stall.arm()
	}
	if body != nil {
		body.cut()
	}
}

func (s *Store) getConnectionLimits(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	limits, err := s.control.ConnectionLimits(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, limits)
}

func (s *Store) putConnectionLimits(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.ConnectionLimits
	if !decode(w, r, &req, "max_upload_mb", "per_ip_concurrent", "total_concurrent", "idle_timeout_seconds", "min_upload_kb_per_second", "hosts") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetConnectionLimits(r.Context(), req, s.now())
	if err == nil {
		err = s.reloadConnectionLimits(r.Context())
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidConnectionLimits) {
		fail(w, r, 400, "invalid_connection_limits")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	raw, _ := json.Marshal(saved)
	var detail map[string]any
	_ = json.Unmarshal(raw, &detail)
	s.audited(r, "security.connection_limits", "security", "connection_limits", detail)
	reply(w, 200, saved)
}
