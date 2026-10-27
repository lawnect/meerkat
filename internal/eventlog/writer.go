package eventlog

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Store keeps entries; *Log implements it.
type Store[E any] interface {
	Append(ctx context.Context, entries []E) error
	Shed(ctx context.Context) error
	Prune(ctx context.Context, now time.Time) error
}

// checkpointer is a Store whose write-ahead log Run keeps in bounds, as
// *Log's (see Log.Checkpoint).
type checkpointer interface {
	WALSize() int64
	Checkpoint(ctx context.Context) error
}

// Writer writes a log behind the requests: Add never waits and never
// fails, so a slow, full or broken log file can't hold up or fail a
// request.
//
// Entries go from the queue to a write buffer, which Run stores in
// batches. While the log refuses writes the buffer is kept, up to
// BufferCap entries, and tried again after a growing pause; a log that
// reached its cap first sheds its oldest entries. With the queue or the
// buffer full, an entry is given up and counted. Nothing is spilled to
// disk.
type Writer[E any] struct {
	store   Store[E]
	name    string
	queue   chan E
	flushes chan chan struct{}
	dropped atomic.Int64

	// Limits and intervals; tests shrink them before starting Run.
	BatchSize    int           // entries per Append
	BufferCap    int           // entries kept while writes fail
	Tick         time.Duration // how often Run writes what it holds
	RetryMax     time.Duration // the longest pause after failed writes
	WriteTimeout time.Duration // one Append
	ShedTimeout  time.Duration // one Shed
	PruneEvery   time.Duration // how often Run prunes

	// A checkpoint runs beside Run, one at a time.
	checkpointing atomic.Bool
	checkpoints   sync.WaitGroup

	// Owned by Run.
	buffer   []E
	backoff  time.Duration
	retryAt  time.Time
	prunedAt time.Time
	prune    chan struct{}
}

// NewWriter returns a writer for store, its queue holding queue entries;
// name names the log in warnings.
func NewWriter[E any](store Store[E], name string, queue int) *Writer[E] {
	return &Writer[E]{
		store: store, name: name, queue: make(chan E, queue), flushes: make(chan chan struct{}), prune: make(chan struct{}, 1),
		BatchSize: 5000, BufferCap: 20000, Tick: time.Second, RetryMax: 30 * time.Second, WriteTimeout: 5 * time.Second, ShedTimeout: 2 * time.Minute, PruneEvery: time.Hour,
	}
}

// Add hands an entry to Run, or gives it up when the queue is full.
func (w *Writer[E]) Add(entry E) {
	select {
	case w.queue <- entry:
	default:
		w.dropped.Add(1)
	}
}

// Dropped is how many entries were given up since the writer started.
func (w *Writer[E]) Dropped() int64 { return w.dropped.Load() }

// Flush has Run write what it holds, waiting at most two seconds.
func (w *Writer[E]) Flush() {
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

// PruneSoon has Run prune at its next tick, as after the limits changed.
func (w *Writer[E]) PruneSoon() {
	select {
	case w.prune <- struct{}{}:
	default:
	}
}

// Run writes until ctx ends, then writes what is left.
func (w *Writer[E]) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Tick)
	defer ticker.Stop()
	for {
		select {
		case entry := <-w.queue:
			w.take(entry)
			if len(w.buffer) >= w.BatchSize {
				w.write(ctx, false)
			}
		case done := <-w.flushes:
			w.drain()
			w.write(ctx, true)
			close(done)
		case <-w.prune:
			w.pruneNow(ctx)
		case <-ticker.C:
			w.write(ctx, false)
			if time.Since(w.prunedAt) >= w.PruneEvery {
				w.pruneNow(ctx)
			}
		case <-ctx.Done():
			w.drain()
			w.write(context.Background(), true)
			w.checkpoints.Wait()
			return
		}
	}
}

func (w *Writer[E]) pruneNow(ctx context.Context) {
	w.prunedAt = time.Now()
	if err := w.store.Prune(ctx, w.prunedAt); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("log not pruned", "log", w.name, "error", err)
	}
	w.checkpointSoon(ctx)
}

// checkpointSoon starts a checkpoint beside Run when the write-ahead log
// grew past its limit, and none is running.
func (w *Writer[E]) checkpointSoon(ctx context.Context) {
	c, ok := w.store.(checkpointer)
	if !ok || c.WALSize() <= walLimit || !w.checkpointing.CompareAndSwap(false, true) {
		return
	}
	w.checkpoints.Go(func() {
		defer w.checkpointing.Store(false)
		if err := c.Checkpoint(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("log not checkpointed", "log", w.name, "error", err)
		}
	})
}

func (w *Writer[E]) take(entry E) {
	if len(w.buffer) >= w.BufferCap {
		w.dropped.Add(1)
		return
	}
	w.buffer = append(w.buffer, entry)
}

func (w *Writer[E]) drain() {
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
func (w *Writer[E]) write(ctx context.Context, now bool) {
	if len(w.buffer) == 0 || !now && time.Now().Before(w.retryAt) {
		return
	}
	for len(w.buffer) > 0 {
		n := min(len(w.buffer), w.BatchSize)
		attempt, cancel := context.WithTimeout(ctx, w.WriteTimeout)
		err := w.store.Append(attempt, w.buffer[:n])
		cancel()
		// A full file sheds a tenth of its entries, which takes longer than
		// a write on a big one; it has a time of its own, and the write
		// another try after it.
		if errors.Is(err, ErrFull) {
			shedding, stop := context.WithTimeout(ctx, w.ShedTimeout)
			err = w.store.Shed(shedding)
			stop()
			if err == nil {
				attempt, cancel = context.WithTimeout(ctx, w.WriteTimeout)
				err = w.store.Append(attempt, w.buffer[:n])
				cancel()
			}
		}
		if err != nil {
			w.backoff = min(max(2*w.backoff, time.Second), w.RetryMax)
			w.retryAt = time.Now().Add(w.backoff)
			slog.Warn("log not written", "log", w.name, "error", err, "buffered", len(w.buffer))
			return
		}
		w.buffer, w.backoff = w.buffer[n:], 0
	}
	w.buffer = nil
	w.checkpointSoon(ctx)
}
