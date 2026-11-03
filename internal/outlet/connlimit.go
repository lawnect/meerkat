package outlet

import (
	"bytes"
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

// shardOf picks the shard of a client's in-flight count. The counts are
// never evicted, so a client choosing its shard gains nothing.
func shardOf(key string, shards int) int {
	hash := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		hash = (hash ^ uint32(key[i])) * 16777619
	}
	return int(hash % uint32(shards))
}

// connLimits is the compiled, immutable form of control.ConnectionLimits.
type connLimits struct {
	control.ConnectionLimits
	maxUpload int64            // bytes; 0 is no limit
	uploads   map[string]int64 // per host, overriding maxUpload
	idle      time.Duration
	minRate   float64                  // upload bytes per second
	respond   time.Duration            // 0 is no limit
	responds  map[string]time.Duration // per host, overriding respond
	request   time.Duration            // 0 is no limit
	requests  map[string]time.Duration // per host, overriding request
}

func compileConnectionLimits(c control.ConnectionLimits) *connLimits {
	l := &connLimits{
		ConnectionLimits: c,
		maxUpload:        int64(c.MaxUploadMB) << 20,
		uploads:          make(map[string]int64, len(c.Hosts)),
		idle:             time.Duration(c.IdleTimeoutSeconds) * time.Second,
		minRate:          float64(c.MinUploadKBPerSecond) * 1024,
		respond:          time.Duration(c.ResponseTimeoutSeconds) * responseTimeoutUnit,
		responds:         make(map[string]time.Duration, len(c.ResponseTimeouts)),
		request:          time.Duration(c.RequestTimeoutMinutes) * requestTimeoutUnit,
		requests:         make(map[string]time.Duration, len(c.RequestTimeouts)),
	}
	for _, h := range c.Hosts {
		l.uploads[h.Host] = int64(h.MaxUploadMB) << 20
	}
	for _, h := range c.ResponseTimeouts {
		l.responds[h.Host] = time.Duration(h.Seconds) * responseTimeoutUnit
	}
	for _, h := range c.RequestTimeouts {
		l.requests[h.Host] = time.Duration(h.Minutes) * requestTimeoutUnit
	}
	return l
}

// responseTimeoutUnit is a second of the response timeouts. Tests shorten it.
var responseTimeoutUnit = time.Second

// responseTimeout is how long the services of host get to send their
// response headers; 0 is no limit.
func (l *connLimits) responseTimeout(host string) time.Duration {
	if d, ok := l.responds[host]; ok {
		return d
	}
	return l.respond
}

// requestTimeoutUnit is a minute of the request time limits. Tests shorten
// it.
var requestTimeoutUnit = time.Minute

// requestTimeout is how long a request to host may take in all; 0 is no
// limit.
func (l *connLimits) requestTimeout(host string) time.Duration {
	if d, ok := l.requests[host]; ok {
		return d
	}
	return l.request
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
// stored settings nothing is limited, but services still get the default
// time to answer and requests the default time in all.
func (s *Store) connectionLimits() *connLimits {
	if s.connections != nil {
		if limits := s.connections.config.Load(); limits != nil {
			return limits
		}
	}
	return &connLimits{respond: control.DefaultResponseTimeoutSeconds * responseTimeoutUnit, request: control.DefaultRequestTimeoutMinutes * requestTimeoutUnit}
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
	waited  time.Duration // spent in Read, waiting on the client
	limit   int64         // bytes; 0 is no limit
	read    int64
	done    bool

	complete atomic.Bool // read to the end
	cutOff   atomic.Bool // cut: no read may wait for the client again
	failed   atomic.Bool
	timedOut atomic.Bool
	tooLarge atomic.Bool
}

// deadline is when the next read must have returned: within the idle
// timeout, and in time to keep the minimum average speed over the time
// spent waiting on the client. Time the service took to read the body is
// not the client's: counted from when the request began, a service that
// paused reading would see the client cut for its own slowness.
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
		paced := now.Add(max(grace+time.Duration(float64(b.read)/b.minRate*float64(time.Second))-b.waited, 0))
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
	began := time.Now()
	if !b.done && (b.idle > 0 || b.minRate > 0) {
		_ = b.rc.SetReadDeadline(b.deadline(began))
		// A cut made while the deadline was set must not be undone by it.
		if b.cutOff.Load() {
			_ = b.rc.SetReadDeadline(time.Unix(1, 0))
		}
	}
	n, err := b.ReadCloser.Read(p)
	b.waited += time.Since(began)
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

// cut makes any read of the rest of an unfinished body fail at once, the
// next ones too: Read sets the deadline again before each read. It may run
// on another goroutine than the reads.
func (b *clientBody) cut() {
	if !b.complete.Load() {
		b.cutOff.Store(true)
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
		body = &clientBody{ReadCloser: r.Body, rc: rc, idle: limits.idle, minRate: limits.minRate}
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

// errTimeLimit ends a proxied request that ran past the request time limit
// of its hostname.
var errTimeLimit = errors.New("request time limit reached")

// timeLimit ends a proxied request at the request time limit of its
// hostname, counted from when the request came: the request to the service
// is cancelled, before the service answers or as the response streams, and
// the transfers with the client end with it. A nil timeLimit is no limit.
//
// The limit expires on a timer's goroutine. mu orders that with the request:
// once the request is done with the limit (finish, stop, an upgrade) it no
// longer expires, so it never touches the connection of a response that went
// out whole, or of the next request on it.
type timeLimit struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	rc     *http.ResponseController
	body   *clientBody

	mu        sync.Mutex
	over      bool // the request is done with the limit
	streaming bool // the response is under way
	cutWrite  bool // expire cut the write of the response
}

// limitTime starts the time limit of r, which the returned request carries.
func limitTime(w http.ResponseWriter, r *http.Request, body *clientBody, limit time.Duration, began time.Time) (*http.Request, *timeLimit) {
	if limit <= 0 {
		return r, nil
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	l := &timeLimit{ctx: ctx, cancel: cancel, rc: http.NewResponseController(w), body: body}
	l.timer = time.AfterFunc(limit-time.Since(began), l.expire)
	return r.WithContext(ctx), l
}

// expire ends the request. An unfinished upload is cut, since the transport
// waits for its read before giving up the request, and so is a write of the
// response under way: without the idle timeout, a stalled client would hold
// either for good.
func (l *timeLimit) expire() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.over {
		return
	}
	l.cancel(errTimeLimit)
	if l.body != nil {
		l.body.cut()
	}
	if l.streaming {
		_ = l.rc.SetWriteDeadline(time.Unix(1, 0))
		l.cutWrite = true
	}
}

// answered notes the service's answer: a response that streams until the
// limit is cut, and a connection upgraded off HTTP (WebSocket) is let be,
// as it is no longer a request and the idle timeout doesn't count it either.
func (l *timeLimit) answered(status int) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if status == http.StatusSwitchingProtocols {
		l.over = true
		l.timer.Stop()
	} else {
		l.streaming = true
	}
}

// finish ends the limit of a response that went out whole: one that expired
// as its last bytes left gets the rest of its write. The idle timeout, when
// on, sets the deadline again as the handler returns.
func (l *timeLimit) finish() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.over = true
	l.timer.Stop()
	if l.cutWrite {
		_ = l.rc.SetWriteDeadline(time.Time{})
	}
}

// reached reports whether the limit has ended the request.
func (l *timeLimit) reached() bool { return l != nil && context.Cause(l.ctx) == errTimeLimit }

// stop ends the limit with the request, reporting whether it ended the
// request.
func (l *timeLimit) stop() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	l.over = true
	l.mu.Unlock()
	l.timer.Stop()
	reached := l.reached()
	l.cancel(nil)
	return reached
}

// connectionLimitsRequest is the body of PUT connection-limits. Its per-host
// entries need every key: a missing or null value would read as zero, which
// lifts the limit. The request's strict decoding doesn't reach into a
// custom UnmarshalJSON, so the entries decode strictly themselves; stored
// limits read them leniently, as control.ConnectionLimits.
type connectionLimitsRequest struct {
	control.ConnectionLimits
	Hosts            []uploadEntry   `json:"hosts"`
	ResponseTimeouts []timeoutEntry  `json:"response_timeouts"`
	RequestTimeouts  []durationEntry `json:"request_timeouts"`
}

type uploadEntry control.HostUpload

func (e *uploadEntry) UnmarshalJSON(data []byte) error {
	type plain control.HostUpload
	return decodeEntry(data, (*plain)(e), "host", "max_upload_mb")
}

type timeoutEntry control.HostResponseTimeout

func (e *timeoutEntry) UnmarshalJSON(data []byte) error {
	type plain control.HostResponseTimeout
	return decodeEntry(data, (*plain)(e), "host", "response_timeout_seconds")
}

type durationEntry control.HostRequestTimeout

func (e *durationEntry) UnmarshalJSON(data []byte) error {
	type plain control.HostRequestTimeout
	return decodeEntry(data, (*plain)(e), "host", "request_timeout_minutes")
}

var errIncompleteEntry = errors.New("entry is missing a key")

func decodeEntry(data []byte, v any, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, key := range keys {
		if value, ok := fields[key]; !ok || bytes.Equal(value, []byte("null")) {
			return errIncompleteEntry
		}
	}
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	return strict.Decode(v)
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
	var body connectionLimitsRequest
	// Three lists of up to 50 hostnames of up to 253 characters.
	if !decodeLimited(w, r, &body, 64<<10, "max_upload_mb", "per_ip_concurrent", "total_concurrent", "idle_timeout_seconds", "min_upload_kb_per_second", "hosts",
		"response_timeout_seconds", "response_timeouts", "request_timeout_minutes", "request_timeouts") {
		return
	}
	req := body.ConnectionLimits
	req.Hosts = make([]control.HostUpload, len(body.Hosts))
	for i, h := range body.Hosts {
		req.Hosts[i] = control.HostUpload(h)
	}
	req.ResponseTimeouts = make([]control.HostResponseTimeout, len(body.ResponseTimeouts))
	for i, h := range body.ResponseTimeouts {
		req.ResponseTimeouts[i] = control.HostResponseTimeout(h)
	}
	req.RequestTimeouts = make([]control.HostRequestTimeout, len(body.RequestTimeouts))
	for i, h := range body.RequestTimeouts {
		req.RequestTimeouts[i] = control.HostRequestTimeout(h)
	}
	s.saving.Lock()
	saved, err := s.control.SetConnectionLimits(r.Context(), req, s.now())
	if err == nil {
		err = s.reloadConnectionLimits(context.WithoutCancel(r.Context()))
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
