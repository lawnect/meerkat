package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/accesslog"
)

// accessLogWriter writes the access log behind the requests: add never
// waits and never fails, so a slow, full or broken log file can't hold up
// or fail a request.
//
// Entries go from the queue to a write buffer, which run stores in
// batches. While the log refuses writes the buffer is kept, up to
// bufferCap entries, and tried again with a growing pause; a log that
// reached its size cap first sheds its oldest entries. With the queue or
// the buffer full, an entry is given up and counted. Nothing is spilled
// to disk.
type accessLogWriter struct {
	log     accessLogStore
	queue   chan accesslog.Entry
	flushes chan chan struct{}
	dropped atomic.Int64 // entries given up; only ever grows

	// Limits and intervals; tests shrink them before starting run.
	batchSize    int           // entries per Append
	bufferCap    int           // entries kept while writes fail
	tick         time.Duration // how often run writes what it holds
	retryMax     time.Duration // the longest pause after failed writes
	writeTimeout time.Duration // one Append

	// Owned by run.
	buffer   []accesslog.Entry
	backoff  time.Duration
	retryAt  time.Time
	prunedAt time.Time
}

// accessLogStore keeps the access log; *accesslog.Log implements it.
type accessLogStore interface {
	Append(ctx context.Context, entries []accesslog.Entry) error
	Shed(ctx context.Context) error
	Prune(ctx context.Context, now time.Time) error
	Query(ctx context.Context, filter accesslog.Filter) ([]accesslog.Entry, int64, error)
	Close() error
}

func newAccessLogWriter(log accessLogStore) *accessLogWriter {
	return &accessLogWriter{
		log:          log,
		queue:        make(chan accesslog.Entry, 8192),
		flushes:      make(chan chan struct{}),
		batchSize:    500,
		bufferCap:    20000,
		tick:         time.Second,
		retryMax:     30 * time.Second,
		writeTimeout: 5 * time.Second,
	}
}

// add hands an entry to run, or gives it up when the queue is full.
func (w *accessLogWriter) add(entry accesslog.Entry) {
	select {
	case w.queue <- entry:
	default:
		w.dropped.Add(1)
	}
}

// flush has run write what it holds, waiting at most two seconds, so the
// console sees the requests just made.
func (w *accessLogWriter) flush() {
	done := make(chan struct{})
	timeout := time.After(2 * time.Second)
	select {
	case w.flushes <- done:
		select {
		case <-done:
		case <-timeout:
		}
	case <-timeout:
	}
}

func (w *accessLogWriter) run(ctx context.Context) {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case entry := <-w.queue:
			w.take(entry)
			if len(w.buffer) >= w.batchSize {
				w.write(ctx, false)
			}
		case done := <-w.flushes:
			w.drain()
			w.write(ctx, true)
			close(done)
		case <-ticker.C:
			w.write(ctx, false)
			if now := time.Now(); now.Sub(w.prunedAt) >= time.Hour {
				w.prunedAt = now
				if err := w.log.Prune(ctx, now); err != nil {
					slog.Warn("access log not pruned", "error", err)
				}
			}
		case <-ctx.Done():
			w.drain()
			w.write(context.Background(), true)
			return
		}
	}
}

func (w *accessLogWriter) take(entry accesslog.Entry) {
	if len(w.buffer) >= w.bufferCap {
		w.dropped.Add(1)
		return
	}
	w.buffer = append(w.buffer, entry)
}

func (w *accessLogWriter) drain() {
	for {
		select {
		case entry := <-w.queue:
			w.take(entry)
		default:
			return
		}
	}
}

// write stores the buffer in batches, unless a failed write asked to wait
// and now isn't set. A failure keeps what is left for the next try.
func (w *accessLogWriter) write(ctx context.Context, now bool) {
	if len(w.buffer) == 0 || !now && time.Now().Before(w.retryAt) {
		return
	}
	for len(w.buffer) > 0 {
		n := min(len(w.buffer), w.batchSize)
		attempt, cancel := context.WithTimeout(ctx, w.writeTimeout)
		err := w.log.Append(attempt, w.buffer[:n])
		if errors.Is(err, accesslog.ErrFull) {
			if err = w.log.Shed(attempt); err == nil {
				err = w.log.Append(attempt, w.buffer[:n])
			}
		}
		cancel()
		if err != nil {
			w.backoff = min(max(2*w.backoff, time.Second), w.retryMax)
			w.retryAt = time.Now().Add(w.backoff)
			slog.Warn("access log not written", "error", err, "buffered", len(w.buffer))
			return
		}
		w.buffer, w.backoff = w.buffer[n:], 0
	}
	w.buffer = nil
}

// decisions names what a policy decided, as the access log and the console
// read it.
var decisions = [...]string{accessPassed: "passed", accessChallenged: "challenged", accessDenied: "denied"}

// logAccess records what a policy decided for a request, when the access
// log is on: every refusal and sign-in asked for, and requests let in that
// name someone, a person or a token. Requests let in anonymously, a bypass
// without a token, are only counted.
func (s *Store) logAccess(r *http.Request, host, policy string, outcome int, reason, email, token string) {
	writer := s.access.log
	if writer == nil || !s.access.logging.Load() || outcome == accessPassed && email == "" && token == "" {
		return
	}
	ip, _ := requestOrigin(r)
	method := r.Method
	if len(method) > 16 {
		method = method[:16]
	}
	writer.add(accesslog.Entry{At: s.now(), Host: host, Method: method, Path: r.URL.Path, Client: ip, Policy: policy, Decision: decisions[outcome], Reason: reason, Email: email, Token: token})
}

type accessLogView struct {
	// Enabled is the switch; Available whether the log file could be
	// opened. Next is the ID to ask for the page after, 0 for none, and
	// Dropped how many entries were given up since the Outlet started.
	Enabled   bool              `json:"enabled"`
	Available bool              `json:"available"`
	Entries   []accesslog.Entry `json:"entries"`
	Next      int64             `json:"next"`
	Dropped   int64             `json:"dropped"`
}

func (s *Store) getAccessLog(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	filter := accesslog.Filter{Decision: query.Get("decision"), Policy: query.Get("policy"), Limit: 50}
	switch filter.Decision {
	case "", "passed", "challenged", "denied":
	default:
		fail(w, r, 400, "invalid_access_log_filter")
		return
	}
	if value := query.Get("before"); value != "" {
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			fail(w, r, 400, "invalid_access_log_filter")
			return
		}
		filter.Before = n
	}
	if value := query.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > accesslog.MaxPage {
			fail(w, r, 400, "invalid_access_log_filter")
			return
		}
		filter.Limit = n
	}
	view := accessLogView{Enabled: s.access.logging.Load(), Entries: []accesslog.Entry{}}
	if writer := s.access.log; writer != nil {
		writer.flush()
		entries, next, err := writer.log.Query(r.Context(), filter)
		if err != nil {
			fail(w, r, 503, "access_log_failed")
			return
		}
		view.Available, view.Entries, view.Next, view.Dropped = true, entries, next, writer.dropped.Load()
	}
	reply(w, 200, view)
}

func (s *Store) putAccessLog(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decode(w, r, &req, "enabled") {
		return
	}
	if req.Enabled && s.access.log == nil {
		fail(w, r, 409, "access_log_unavailable")
		return
	}
	s.saving.Lock()
	err := s.control.SetAccessLog(r.Context(), req.Enabled, s.now())
	if err == nil {
		s.access.logging.Store(req.Enabled)
	}
	s.saving.Unlock()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.access_log", "security", "access_log", map[string]any{"enabled": req.Enabled})
	s.getAccessLog(w, r)
}
