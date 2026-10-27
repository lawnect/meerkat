package eventlog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type testEntry struct {
	ID      int64
	At      time.Time
	Path    string
	State   string
	Routine bool
}

var testKind = Kind[testEntry]{
	Table:   "events",
	Columns: []string{"path TEXT", "state TEXT"},
	Filters: []string{"state"},
	Search:  []string{"path", "state"},
	Fields:  func(e *testEntry) []any { return []any{&e.Path, &e.State} },
	Stamp:   func(e *testEntry) (*int64, *time.Time) { return &e.ID, &e.At },
	Routine: func(e *testEntry) *bool { return &e.Routine },
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

// kept names which of the entries appended as pattern are still in the log,
// oldest first: its letter if so, a dot if not.
func kept(t *testing.T, log *Log[testEntry], pattern string) string {
	t.Helper()
	entries, _, err := log.Query(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	out := []byte(strings.Repeat(".", len(pattern)))
	for _, e := range entries {
		i, _ := strconv.Atoi(strings.TrimPrefix(e.Path, "/"))
		out[i] = pattern[i]
	}
	return string(out)
}

// appendPattern appends one entry per letter of pattern, oldest first, with
// the index as its path: "r" is a routine entry and "n" a notable one.
func appendPattern(t *testing.T, log *Log[testEntry], pattern string) {
	t.Helper()
	batch := make([]testEntry, len(pattern))
	for i, c := range pattern {
		batch[i] = testEntry{At: time.Now(), Path: "/" + strconv.Itoa(i), State: "denied", Routine: c == 'r'}
	}
	if err := log.Append(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
}

// Shedding forgets the oldest tenth, the routine entries first: a flood of
// them can't push out the notable ones. Notable entries go only for what the
// routine ones can't make up.
func TestLogShedsRoutineFirst(t *testing.T) {
	for _, c := range []struct {
		name, entries, want string
	}{
		{"the oldest routine ones, past notable ones", "nnrrrrnrrrrnrrrrnrrr", "nn..rrnrrrrnrrrrnrrr"},
		{"every routine one when they are fewer than a tenth", "nnnnnnnnnnrrnnnnnnnn", "nnnnnnnnnn..nnnnnnnn"},
		{"the oldest notable one for what is missing", "nnnnnnnnnnrnnnnnnnnn", ".nnnnnnnnn.nnnnnnnnn"},
		{"the oldest notable ones when no routine one is left", "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnn", "...nnnnnnnnnnnnnnnnnnnnnnnnnnn"},
		{"at least one, from a short log", "nrnrr", "n.nrr"},
		{"nothing from an empty log", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
			if c.entries != "" {
				appendPattern(t, log, c.entries)
			}
			if err := log.Shed(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := kept(t, log, c.entries); got != c.want {
				t.Fatalf("after shedding %s:\n%s\nwant:\n%s", c.entries, got, c.want)
			}
		})
	}
}

// A file over its cap is pruned by forgetting routine entries, however old
// the notable ones are; a flood fills the file with routine entries and the
// notable ones stay, until none of the routine ones is left to give.
func TestLogPruneKeepsNotableEntries(t *testing.T) {
	ctx := context.Background()
	const limit = 256 << 10
	log, _ := openTest(t, Limits{MaxBytes: limit, Retention: time.Hour})
	// One entry in ten is notable, the first among them.
	batch := make([]testEntry, 50)
	for i := range batch {
		batch[i] = testEntry{At: time.Now(), Path: strings.Repeat("p", 400), State: "denied", Routine: i%10 != 0}
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
	count := func(routine bool) int {
		t.Helper()
		_, total, err := log.Query(ctx, Query{Routine: &routine})
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	notable, routine := count(false), count(true)
	if notable == 0 || routine < 5*notable {
		t.Fatalf("filled with %d notable and %d routine entries", notable, routine)
	}
	// Making room for writes forgets routine entries only.
	if err := log.Shed(ctx); err != nil {
		t.Fatal(err)
	}
	if count(false) != notable || count(true) >= routine {
		t.Fatalf("after shedding: %d notable, %d routine, from %d and %d", count(false), count(true), notable, routine)
	}
	// So does a lowered cap, which the notable entries fit.
	log.SetLimits(Limits{MaxBytes: limit / 4, Retention: time.Hour})
	if err := log.Prune(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if size, err := log.Size(ctx); err != nil || size > limit/4 {
		t.Fatalf("after lowering the cap: %d %v", size, err)
	}
	if count(false) != notable || count(true) == 0 {
		t.Fatalf("after pruning: %d notable, %d routine, from %d notable", count(false), count(true), notable)
	}
	// A cap they don't fit forgets the oldest of them as well, once the routine ones are gone.
	log.SetLimits(Limits{MaxBytes: limit / 8, Retention: time.Hour})
	if err := log.Prune(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	if size, err := log.Size(ctx); err != nil || size > limit/8 {
		t.Fatalf("after lowering the cap again: %d %v", size, err)
	}
	if count(true) != 0 || count(false) == 0 || count(false) >= notable {
		t.Fatalf("after pruning again: %d notable, %d routine, from %d notable", count(false), count(true), notable)
	}
}

// Text finds a substring of any search column, whatever its case, and a
// %, _ or \ in it means itself rather than a pattern; it combines with the
// other filters and the routine flag.
func TestLogQueryTextAndRoutine(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	now := time.Now()
	if err := log.Append(ctx, []testEntry{
		{At: now, Path: "/a%b", State: "denied", Routine: true},
		{At: now, Path: "/axb", State: "denied", Routine: true},
		{At: now, Path: "/a_b", State: "passed"},
		{At: now, Path: `/a\b`, State: "passed"},
		{At: now, Path: "/A.B", State: "denied"},
		{At: now, Path: "/ab", State: "passed", Routine: true},
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		query Query
		want  []string // newest first
	}{
		{"no text", Query{}, []string{"/ab", "/A.B", `/a\b`, "/a_b", "/axb", "/a%b"}},
		{"a percent is a percent", Query{Text: "a%b"}, []string{"/a%b"}},
		{"only a percent", Query{Text: "%"}, []string{"/a%b"}},
		{"an underscore is an underscore", Query{Text: "a_b"}, []string{"/a_b"}},
		{"only an underscore", Query{Text: "_"}, []string{"/a_b"}},
		{"a backslash is a backslash", Query{Text: `a\b`}, []string{`/a\b`}},
		{"only a backslash", Query{Text: `\`}, []string{`/a\b`}},
		{"a backslash before a percent", Query{Text: `\%`}, nil},
		{"a dot is a dot, and case does not matter", Query{Text: "a.b"}, []string{"/A.B"}},
		{"any search column", Query{Text: "PASSED"}, []string{"/ab", `/a\b`, "/a_b"}},
		{"a column that is not searched", Query{Text: "routine"}, nil},
		{"no match", Query{Text: "zzz"}, nil},
		{"with a filter", Query{Text: "a", Where: map[string]string{"state": "denied"}}, []string{"/A.B", "/axb", "/a%b"}},
		{"routine only", Query{Routine: new(true)}, []string{"/ab", "/axb", "/a%b"}},
		{"notable only", Query{Routine: new(false)}, []string{"/A.B", `/a\b`, "/a_b"}},
		{"text among routine ones", Query{Text: "a_b", Routine: new(true)}, nil},
		{"text among notable ones", Query{Text: "a", Routine: new(false)}, []string{"/A.B", `/a\b`, "/a_b"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, total, err := log.Query(ctx, c.query)
			if err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, e := range got {
				paths = append(paths, e.Path)
				if e.Routine != (e.Path == "/a%b" || e.Path == "/axb" || e.Path == "/ab") {
					t.Fatalf("%s came back as routine=%v", e.Path, e.Routine)
				}
			}
			if !slices.Equal(paths, c.want) || total != len(c.want) {
				t.Fatalf("%q (%d), want %q", paths, total, c.want)
			}
		})
	}
}

// A log can't tell routine entries from the rest without the flag.
func TestLogNeedsTheRoutineFlag(t *testing.T) {
	kind := testKind
	kind.Routine = nil
	if _, err := Open(context.Background(), filepath.Join(t.TempDir(), "events.db"), kind, Limits{MaxBytes: 1 << 20, Retention: time.Hour}); err == nil {
		t.Fatal("opened a log without the flag")
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
