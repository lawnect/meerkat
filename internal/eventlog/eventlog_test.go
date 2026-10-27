package eventlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testEntry struct {
	ID    int64
	At    time.Time
	Path  string
	State string
}

var testKind = Kind[testEntry]{
	Table:   "events",
	Columns: []string{"path TEXT", "state TEXT"},
	Filters: []string{"state"},
	Fields:  func(e *testEntry) []any { return []any{&e.Path, &e.State} },
	Stamp:   func(e *testEntry) (*int64, *time.Time) { return &e.ID, &e.At },
}

func openTest(t *testing.T, limits Limits) (*Log[testEntry], string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	log, err := Open(context.Background(), path, testKind, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	return log, path
}

// Entries come back newest first, filtered and paged with a total; old ones
// are forgotten.
func TestLogQueryAndRetention(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 30 * 24 * time.Hour})
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	if err := log.Append(ctx, []testEntry{
		{At: now.Add(-40 * 24 * time.Hour), Path: "/old", State: "denied"},
		{At: now.Add(-3 * time.Minute), Path: "/a", State: "passed"},
		{At: now.Add(-2 * time.Minute), Path: "/b", State: "denied"},
		{At: now.Add(-time.Minute), Path: "/c", State: "denied"},
	}); err != nil {
		t.Fatal(err)
	}
	got, total, err := log.Query(ctx, Query{Limit: 2})
	if err != nil || total != 4 || len(got) != 2 || got[0].Path != "/c" || !got[0].At.Equal(now.Add(-time.Minute)) || got[0].ID == 0 {
		t.Fatalf("first page: %+v %d %v", got, total, err)
	}
	if got, total, err = log.Query(ctx, Query{Limit: 2, Offset: 2}); err != nil || total != 4 || len(got) != 2 || got[0].Path != "/a" {
		t.Fatalf("second page: %+v %d %v", got, total, err)
	}
	if got, total, err = log.Query(ctx, Query{Where: map[string]string{"state": "denied", "path": "/c"}}); err != nil || total != 3 || len(got) != 3 {
		t.Fatalf("filtered on the filter column only: %+v %d %v", got, total, err)
	}
	if err := log.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, total, _ = log.Query(ctx, Query{}); total != 3 {
		t.Fatalf("after the retention: %d", total)
	}
	if Clip("가나다", 4) != "가" || Clip("abc", 5) != "abc" {
		t.Fatal("clip")
	}
}

// SQLite refuses to grow the file past its cap; shedding the oldest makes
// room without growing it, and a lowered cap shrinks the file.
func TestLogSizeCap(t *testing.T) {
	ctx := context.Background()
	const limit = 256 << 10
	log, path := openTest(t, Limits{MaxBytes: limit, Retention: time.Hour})
	batch := make([]testEntry, 50)
	for i := range batch {
		batch[i] = testEntry{At: time.Now(), Path: strings.Repeat("p", 400), State: "denied"}
	}
	var err error
	for range 400 {
		if err = log.Append(ctx, batch); err != nil {
			break
		}
	}
	if !errors.Is(err, ErrFull) {
		t.Fatalf("writes past the cap: %v", err)
	}
	if err := log.Shed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(ctx, batch[:5]); err != nil {
		t.Fatalf("after shedding: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > limit {
		t.Fatalf("file over the cap: %d %v", info.Size(), err)
	}
	log.SetLimits(Limits{MaxBytes: limit / 4, Retention: time.Hour})
	if err := log.Prune(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if size, err := log.Size(ctx); err != nil || size > limit/4 {
		t.Fatalf("after lowering the cap: %d %v", size, err)
	}
	if _, total, _ := log.Query(ctx, Query{}); total == 0 {
		t.Fatal("lowering the cap emptied the log")
	}
}

// fakeStore keeps entries in memory and fails Append as told.
type fakeStore struct {
	mu      sync.Mutex
	entries []testEntry
	fail    error
	full    int // Appends to refuse as full, each lifted by a Shed
	sheds   int
}

func (f *fakeStore) Append(_ context.Context, entries []testEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if f.full > f.sheds {
		return ErrFull
	}
	f.entries = append(f.entries, entries...)
	return nil
}

func (f *fakeStore) Shed(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sheds++
	return nil
}

func (f *fakeStore) Prune(context.Context, time.Time) error { return nil }

func (f *fakeStore) state() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries), f.sheds
}

// Adding never waits: a full queue gives entries up. While the log refuses
// writes, entries wait in the buffer, up to its cap, and are written once
// it takes them again; a full log sheds its oldest and is written to.
func TestWriterGivesUpRatherThanWaits(t *testing.T) {
	store := &fakeStore{}
	w := NewWriter[testEntry](store, "test", 3)
	w.BufferCap, w.BatchSize, w.Tick, w.RetryMax = 5, 2, time.Hour, time.Millisecond
	done := make(chan struct{})
	go func() {
		for range 10 {
			w.Add(testEntry{Path: "/x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Add waited for a writer that isn't running")
	}
	if w.Dropped() != 7 {
		t.Fatalf("given up with the queue full: %d", w.Dropped())
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	store.mu.Lock()
	store.fail = errors.New("disk on fire")
	store.mu.Unlock()
	go w.Run(ctx)
	w.Flush()
	for range 6 {
		w.Add(testEntry{Path: "/y"})
	}
	w.Flush()
	if written, _ := store.state(); written != 0 || w.Dropped() != 7+4 {
		t.Fatalf("while failing: %d written, %d given up", written, w.Dropped())
	}
	store.mu.Lock()
	store.fail, store.full = nil, 1
	store.mu.Unlock()
	w.Flush()
	if written, sheds := store.state(); written != 5 || sheds != 1 {
		t.Fatalf("after the log recovered: %d written, %d sheds", written, sheds)
	}
}

// BenchmarkLogAppend is how fast a log stores entries in batches of 500.
func BenchmarkLogAppend(b *testing.B) {
	log, err := Open(context.Background(), filepath.Join(b.TempDir(), "events.db"), testKind, Limits{MaxBytes: 1 << 30, Retention: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	defer log.Close()
	batch := make([]testEntry, 500)
	for i := range batch {
		batch[i] = testEntry{At: time.Now(), Path: "/v1/secret/data/app/" + strings.Repeat("x", 40), State: "denied"}
	}
	b.ResetTimer()
	for b.Loop() {
		if err := log.Append(context.Background(), batch); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N*len(batch))/b.Elapsed().Seconds(), "entries/s")
}
