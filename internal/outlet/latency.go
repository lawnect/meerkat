package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// latencyKey names the histogram of one kind of latency in one hour, by
// its Unix hour.
type latencyKey struct {
	kind string
	hour int64
}

// latencyCounter is a histogram requests add to without a lock; its count
// is the buckets' total, so the two can't disagree.
type latencyCounter struct {
	sum     atomic.Int64
	buckets [control.LatencyBuckets]atomic.Int64
}

// timing is how an attempt to forward a request went: began when the
// Outlet received the request, waited on attempts at other services that
// failed before this one, then ready when it had a connection to the
// service, sent when its headers were written, and written when its body
// was. The transport sets the last three from goroutines of its own.
type timing struct {
	began                time.Time
	waited               time.Duration
	ready, sent, written atomic.Int64
}

// latencies returns the Outlet's time and the services', once answered,
// at the final response's headers: the Outlet's until the request was on
// a connection, but for attempts that failed, and the services' from
// having sent the request, its body included, tunnel round trip included,
// with those attempts. A client's slow upload, or a 1xx before the answer,
// counts as neither.
func (t *timing) latencies(answered time.Time) (outlet, upstream time.Duration, ok bool) {
	ready, sent, written := t.ready.Load(), t.sent.Load(), t.written.Load()
	if ready == 0 || sent == 0 || t.began.IsZero() {
		return 0, 0, false
	}
	// An app can answer before the body has all been sent; then the
	// answer counts from the headers.
	from := sent
	if written != 0 && written <= answered.UnixNano() {
		from = written
	}
	outlet = max(time.Duration(ready-t.began.UnixNano())-t.waited, 0)
	upstream = max(time.Duration(answered.UnixNano()-from), 0) + t.waited
	return outlet, upstream, true
}

// countLatencies adds a forwarded request's latencies to this hour's
// histograms; flushLatencies saves them.
func (s *Store) countLatencies(outlet, upstream time.Duration) {
	hour := s.now().Unix() / 3600
	s.countLatency(latencyKey{control.LatencyOutlet, hour}, outlet)
	s.countLatency(latencyKey{control.LatencyUpstream, hour}, upstream)
}

func (s *Store) countLatency(key latencyKey, d time.Duration) {
	v, ok := s.latency.Load(key)
	if !ok {
		v, _ = s.latency.LoadOrStore(key, new(latencyCounter))
	}
	c, us := v.(*latencyCounter), d.Microseconds()
	c.sum.Add(us)
	c.buckets[control.LatencyBucket(us)].Add(1)
}

// flushLatencies saves the histograms counted since the last flush, and
// forgets those of hours gone by; what it couldn't save waits for the next.
func (s *Store) flushLatencies(ctx context.Context) {
	if s.control == nil {
		return
	}
	now := s.now()
	pending := map[control.LatencyKey]control.LatencyHistogram{}
	s.latency.Range(func(k, v any) bool {
		key, c := k.(latencyKey), v.(*latencyCounter)
		var h control.LatencyHistogram
		for i := range c.buckets {
			h.Buckets[i] = c.buckets[i].Swap(0)
			h.Count += h.Buckets[i]
		}
		h.SumUS = c.sum.Swap(0)
		if h.Count == 0 && h.SumUS == 0 {
			// An hour's histogram goes an hour after it ended: a request
			// that found it just before the hour turned may still add to it.
			if key.hour < now.Unix()/3600-1 {
				s.latency.Delete(key)
			}
			return true
		}
		pending[control.LatencyKey{Kind: key.kind, Hour: time.Unix(key.hour*3600, 0)}] = h
		return true
	})
	if len(pending) == 0 {
		return
	}
	if err := s.control.RecordLatencies(ctx, pending, now); err != nil {
		slog.Warn("latencies not saved, kept for the next try", "error", err)
		for key, h := range pending {
			hour := latencyKey{key.Kind, key.Hour.Unix() / 3600}
			v, _ := s.latency.LoadOrStore(hour, new(latencyCounter))
			c := v.(*latencyCounter)
			c.sum.Add(h.SumUS)
			for i, n := range h.Buckets {
				c.buckets[i].Add(n)
			}
		}
	}
}

// getLatency returns how long forwarded requests took over a range, the
// Outlet's time and the service's, in calendar buckets of a unit.
func (s *Store) getLatency(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	unit, from, to, location, ok := usageRange(w, r)
	if !ok {
		return
	}
	s.flushLatencies(r.Context())
	series, err := s.control.LatencySeries(r.Context(), unit, from, to, location)
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
