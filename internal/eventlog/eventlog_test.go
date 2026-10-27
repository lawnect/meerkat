package eventlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	if Clip("가나다", 4) != "가" || Clip("abc", 5) != "abc" || Clip("/x\xff", 512) != "/x\uFFFD" || Clip("/x\xfe", 512) != Clip("/x\xff", 512) || Clip("\xff가", 4) != "\uFFFD" {
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

// A file kept by a build with other columns would fail every write; it
// starts over instead, and a file in the current layout keeps its entries.
func TestLogFromAnotherVersionStartsOver(t *testing.T) {
	ctx := context.Background()
	limits := Limits{MaxBytes: 64 << 20, Retention: 24 * time.Hour}
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "events.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("CREATE TABLE events (id INTEGER PRIMARY KEY, at TEXT NOT NULL, path TEXT NOT NULL, state TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec("INSERT INTO events(at, path, state) VALUES (?, '/old', 'x')", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	old.Close()

	reopen := func(kind Kind[testEntry]) *Log[testEntry] {
		t.Helper()
		log, err := Open(ctx, path, kind, limits)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		return log
	}
	log := reopen(testKind)
	if entries, total, err := log.Query(ctx, Query{}); err != nil || total != 0 || len(entries) != 0 {
		t.Fatalf("old entries kept: %v %d %v", entries, total, err)
	}
	if err := log.Append(ctx, []testEntry{{At: now, Path: "/new", State: "ok"}}); err != nil {
		t.Fatal(err)
	}
	log.Close()
	log = reopen(testKind)
	if _, total, err := log.Query(ctx, Query{}); err != nil || total != 1 {
		t.Fatalf("same version lost its entries: %d %v", total, err)
	}
	log.Close()
	bumped := testKind
	bumped.Version++
	log = reopen(bumped)
	defer log.Close()
	if _, total, err := log.Query(ctx, Query{}); err != nil || total != 0 {
		t.Fatalf("a new version kept the old entries: %d %v", total, err)
	}
}

// Shedding more than a chunk takes every routine entry before any notable
// one, chunk after chunk; and a lowered cap is met by one Prune, not a
// tenth of the entries at a time.
func TestLogShedsInChunksAndMeetsALoweredCap(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 256 << 20, Retention: 24 * time.Hour})
	now := time.Now().UTC()
	padding := strings.Repeat("x", 200)
	var batch []testEntry
	for i := range 2*shedChunk + 500 {
		batch = append(batch, testEntry{At: now, Path: fmt.Sprintf("/%d/%s", i, padding), State: "r", Routine: i%100 != 0})
	}
	if err := log.Append(ctx, batch); err != nil {
		t.Fatal(err)
	}
	notable := int64(0)
	for _, e := range batch {
		if !e.Routine {
			notable++
		}
	}
	routine := int64(len(batch)) - notable
	if err := log.shed(ctx, routine+5); err != nil {
		t.Fatal(err)
	}
	left, total, err := log.Query(ctx, Query{Limit: MaxPage})
	if err != nil || int64(total) != notable-5 {
		t.Fatalf("after shedding: %d left, want %d (%v)", total, notable-5, err)
	}
	for _, e := range left {
		if e.Routine {
			t.Fatalf("a routine entry outlived the notable ones: %+v", e)
		}
	}

	if err := log.Append(ctx, batch); err != nil {
		t.Fatal(err)
	}
	size, _ := log.Size(ctx)
	limits := log.Limits()
	limits.MaxBytes = size / 4
	log.SetLimits(limits)
	if err := log.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	if after, _ := log.Size(ctx); after > limits.MaxBytes {
		t.Fatalf("one prune left %d bytes over a cap of %d", after, limits.MaxBytes)
	}
}

// Each hands over every matching entry once, newest first, across chunks,
// and leaves out what was added after it began.
func TestLogEachReadsInChunks(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 30 * 24 * time.Hour})
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	const n = 2*eachChunk + 7
	entries := make([]testEntry, n)
	for i := range entries {
		state := "passed"
		if i%3 == 0 {
			state = "denied"
		}
		entries[i] = testEntry{At: now.Add(time.Duration(i) * time.Second), Path: fmt.Sprintf("/%d", i), State: state}
	}
	if err := log.Append(ctx, entries); err != nil {
		t.Fatal(err)
	}
	var seen []string
	added := false
	if err := log.Each(ctx, Query{Where: map[string]string{"state": "denied"}, Offset: 5, Limit: 1}, func(e *testEntry) error {
		seen = append(seen, e.Path)
		if !added {
			added = true
			return log.Append(ctx, []testEntry{{At: now.Add(time.Hour), Path: "/late", State: "denied"}})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var want []string
	for i := n - 1; i >= 0; i-- {
		if i%3 == 0 {
			want = append(want, fmt.Sprintf("/%d", i))
		}
	}
	if !slices.Equal(seen, want) {
		t.Fatalf("%d entries, want %d; first %v", len(seen), len(want), seen[:min(3, len(seen))])
	}
	stop := errors.New("stop")
	calls := 0
	if err := log.Each(ctx, Query{}, func(*testEntry) error { calls++; return stop }); !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("an error didn't stop it: %v after %d", err, calls)
	}
	if err := log.Each(ctx, Query{Since: now.Add(50 * time.Minute)}, func(e *testEntry) error {
		if e.Path != "/late" {
			return fmt.Errorf("%s is before since", e.Path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Tally groups entries by columns, sorts by a measure or the latest entry,
// breaks ties by the columns, and pages with the number of groups.
func TestLogTally(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 30 * 24 * time.Hour})
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	var entries []testEntry
	add := func(path, state string, ago time.Duration, n int) {
		for range n {
			entries = append(entries, testEntry{At: now.Add(-ago), Path: path, State: state})
		}
	}
	add("/a", "passed", time.Minute, 5)
	add("/a", "denied", 2*time.Hour, 1)
	add("/b", "denied", 3*time.Minute, 4)
	add("/c", "passed", 2*time.Minute, 2)
	add("/d", "passed", 4*time.Minute, 2)
	if err := log.Append(ctx, entries); err != nil {
		t.Fatal(err)
	}
	measures := []string{"COUNT(*)", "SUM(state = 'denied')"}
	tally := func(t *testing.T, tally Tally) ([]string, int) {
		t.Helper()
		tally.By, tally.Measures = []string{"path"}, measures
		groups, total, err := log.Tally(ctx, tally)
		if err != nil {
			t.Fatal(err)
		}
		lines := make([]string, len(groups))
		for i, g := range groups {
			lines[i] = fmt.Sprintf("%s %d %d", g.Keys[0], g.Measures[0], g.Measures[1])
		}
		return lines, total
	}
	for _, c := range []struct {
		name  string
		tally Tally
		want  []string
		total int
	}{
		{"most first, ties by path", Tally{Sort: 0}, []string{"/a 6 1", "/b 4 4", "/c 2 0", "/d 2 0"}, 4},
		{"fewest first", Tally{Sort: 0, Ascending: true}, []string{"/c 2 0", "/d 2 0", "/b 4 4", "/a 6 1"}, 4},
		{"by another measure", Tally{Sort: 1}, []string{"/b 4 4", "/a 6 1", "/c 2 0", "/d 2 0"}, 4},
		{"latest first", Tally{Sort: -1}, []string{"/a 6 1", "/c 2 0", "/b 4 4", "/d 2 0"}, 4},
		{"a page", Tally{Query: Query{Offset: 1, Limit: 2}, Sort: 0}, []string{"/b 4 4", "/c 2 0"}, 4},
		{"past the last page", Tally{Query: Query{Offset: 10, Limit: 2}, Sort: 0}, []string{}, 4},
		{"since", Tally{Query: Query{Since: now.Add(-time.Hour)}, Sort: 0}, []string{"/a 5 0", "/b 4 4", "/c 2 0", "/d 2 0"}, 4},
		{"filtered", Tally{Query: Query{Where: map[string]string{"state": "denied"}}, Sort: 0}, []string{"/b 4 4", "/a 1 1"}, 2},
		{"searched", Tally{Query: Query{Text: "/c"}, Sort: 0}, []string{"/c 2 0"}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, total := tally(t, c.tally); !slices.Equal(got, c.want) || total != c.total {
				t.Fatalf("%v (%d groups), want %v (%d)", got, total, c.want, c.total)
			}
		})
	}
	groups, _, err := log.Tally(ctx, Tally{By: []string{"path"}, Measures: measures, Sort: 0, Query: Query{Limit: 1}})
	if err != nil || len(groups) != 1 || !groups[0].Last.Equal(now.Add(-time.Minute)) {
		t.Fatalf("latest entry of /a: %+v %v", groups, err)
	}
	if _, _, err := log.Tally(ctx, Tally{By: []string{"path"}, Measures: measures, Sort: 2}); err == nil {
		t.Fatal("sorted by a measure it doesn't have")
	}
}

// A long read, a tally or an export, holds up no write: reads have
// connections of their own beside the writer's.
func TestLogReadsDontHoldUpWrites(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 30 * 24 * time.Hour})
	if err := log.Append(ctx, []testEntry{{At: time.Now(), Path: "/a", State: "passed"}, {At: time.Now(), Path: "/b", State: "passed"}}); err != nil {
		t.Fatal(err)
	}
	// A read left open, as a slow tally would keep it.
	rows, err := log.read.QueryContext(ctx, "SELECT path FROM events")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no rows")
	}
	done := make(chan error, 1)
	go func() {
		write, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		done <- log.Append(write, []testEntry{{At: time.Now(), Path: "/c", State: "passed"}})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write beside a read: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a write waited for a read")
	}
	if _, err := log.read.ExecContext(ctx, "DELETE FROM events"); err == nil {
		t.Fatal("a read connection wrote")
	}
}

// Times are kept to the nanosecond in text that sorts as they do, so a
// period starts where it says and a group's latest entry is its latest.
func TestLogTimesSortWithinASecond(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 30 * 24 * time.Hour})
	second := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	if err := log.Append(ctx, []testEntry{
		{At: second, Path: "/a", State: "passed"},
		{At: second.Add(900 * time.Millisecond), Path: "/a", State: "passed"},
		{At: second.Add(510 * time.Millisecond), Path: "/a", State: "passed"},
	}); err != nil {
		t.Fatal(err)
	}
	got, total, err := log.Query(ctx, Query{Since: second.Add(500 * time.Millisecond)})
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("since half a second in: %d %v", total, err)
	}
	groups, _, err := log.Tally(ctx, Tally{By: []string{"path"}, Measures: []string{"COUNT(*)"}, Query: Query{Where: map[string]string{"state": "passed"}, Since: second}})
	if err != nil || len(groups) != 1 || !groups[0].Last.Equal(second.Add(900*time.Millisecond)) {
		t.Fatalf("latest: %+v %v", groups, err)
	}
}

// A kind that is OldestFirst sheds the oldest entries, routine or not.
func TestLogShedsOldestFirst(t *testing.T) {
	kind := testKind
	kind.Table, kind.OldestFirst = "fair", true
	log, err := Open(context.Background(), filepath.Join(t.TempDir(), "fair.db"), kind, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx := context.Background()
	if err := log.Append(ctx, []testEntry{{At: time.Now(), Path: "/notable"}, {At: time.Now(), Path: "/routine", Routine: true}}); err != nil {
		t.Fatal(err)
	}
	if err := log.shed(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := log.Query(ctx, Query{}); len(got) != 1 || got[0].Path != "/routine" {
		t.Fatalf("kept %+v", got)
	}
}

// slowShedStore sheds slower than a write may take.
type slowShedStore struct {
	fakeStore
	took time.Duration
}

func (s *slowShedStore) Shed(ctx context.Context) error {
	select {
	case <-time.After(s.took):
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.fakeStore.Shed(ctx)
}

// Making room in a full file has a time of its own: a shed that takes
// longer than a write still lets the write through.
func TestWriterShedsOnItsOwnTime(t *testing.T) {
	store := &slowShedStore{fakeStore: fakeStore{full: 1}, took: 300 * time.Millisecond}
	w := NewWriter[testEntry](store, "test", 4)
	w.WriteTimeout, w.Tick = 100*time.Millisecond, time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)
	w.Add(testEntry{Path: "/x"})
	w.Flush()
	if written, sheds := store.state(); written != 1 || sheds != 1 {
		t.Fatalf("%d written after %d sheds", written, sheds)
	}
}

// Reads that overlap writes keep SQLite from starting its write-ahead log
// over; past its limit, a checkpoint makes new reads wait for the ones under
// way, and empties it.
func TestLogCheckpointEmptiesTheWriteAheadLog(t *testing.T) {
	previous := walLimit
	walLimit = 64 << 10
	defer func() { walLimit = previous }()
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	if err := log.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	// A read holds a snapshot while entries are written.
	log.gate.RLock()
	rows, err := log.read.QueryContext(ctx, "SELECT path FROM events")
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		entries := make([]testEntry, 200)
		for i := range entries {
			entries[i] = testEntry{At: time.Now(), Path: strings.Repeat("x", 200)}
		}
		if err := log.Append(ctx, entries); err != nil {
			t.Fatal(err)
		}
	}
	if log.WALSize() <= walLimit {
		t.Fatalf("write-ahead log %d bytes", log.WALSize())
	}
	done := make(chan error, 1)
	go func() { done <- log.Checkpoint(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("checkpointed under a read: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	rows.Close()
	log.gate.RUnlock()
	if err := <-done; err != nil || log.WALSize() != 0 {
		t.Fatalf("after the read: %d bytes, %v", log.WALSize(), err)
	}
	if got, total, err := log.Query(ctx, Query{Limit: 1}); err != nil || total != 4000 || len(got) != 1 {
		t.Fatalf("entries: %d %v", total, err)
	}
}

// checkpointStore is a store with a write-ahead log too big.
type checkpointStore struct {
	fakeStore
	checkpoints atomic.Int32
}

func (c *checkpointStore) WALSize() int64 { return walLimit + 1 }

func (c *checkpointStore) Checkpoint(context.Context) error {
	c.checkpoints.Add(1)
	return nil
}

// The writer checkpoints beside its writes once the log grew too big.
func TestWriterCheckpoints(t *testing.T) {
	store := &checkpointStore{}
	w := NewWriter[testEntry](store, "test", 4)
	w.Tick = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	w.Add(testEntry{Path: "/x"})
	w.Flush()
	cancel()
	<-done
	if store.checkpoints.Load() == 0 {
		t.Fatal("no checkpoint")
	}
}

// A big file sheds a chunk at a time, so a write waits for one chunk only.
func TestLogShedsAChunkAtMost(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 256 << 20, Retention: time.Hour})
	const n = 12 * shedChunk
	for i := 0; i < n; i += 20_000 {
		entries := make([]testEntry, 20_000)
		for j := range entries {
			entries[j] = testEntry{At: time.Now(), Path: "/x", Routine: true}
		}
		if err := log.Append(ctx, entries); err != nil {
			t.Fatal(err)
		}
	}
	if err := log.Shed(ctx); err != nil {
		t.Fatal(err)
	}
	if _, total, _ := log.Query(ctx, Query{Limit: 1}); total != n-shedChunk {
		t.Fatalf("%d left of %d", total, n)
	}
}

// A file made without incremental auto-vacuum gives no pages back; Prune
// stops trying rather than loop.
func TestLogPruneEndsOnAFileWithoutIncrementalVacuum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	plain, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Exec("CREATE TABLE events (id INTEGER PRIMARY KEY, at TEXT NOT NULL, routine INTEGER NOT NULL, path TEXT NOT NULL, state TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Exec(fmt.Sprintf("PRAGMA user_version = %d", layout<<16|testKind.Version)); err != nil {
		t.Fatal(err)
	}
	plain.Close()
	log, err := Open(context.Background(), path, testKind, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	old := make([]testEntry, 2000)
	for i := range old {
		old[i] = testEntry{At: time.Now().Add(-2 * time.Hour), Path: strings.Repeat("x", 100)}
	}
	if err := log.Append(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := log.Prune(ctx, time.Now()); err != nil {
		t.Fatalf("prune: %v", err)
	}
}

// Past its limit, a checkpoint never makes reads wait or stops a tally: with
// a read that won't end it gives up, for the next write to try again. Past
// the ceiling, it stops the tally under way, which starts over and still
// answers, and new reads wait for it.
func TestCheckpointYieldsToReadsAndPreemptsTallies(t *testing.T) {
	limit, ceiling := walLimit, walCeiling
	walLimit, walCeiling = 1, 1<<40
	defer func() { walLimit, walCeiling = limit, ceiling }()
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 256 << 20, Retention: time.Hour})
	entries := make([]testEntry, 200_000)
	for i := range entries {
		entries[i] = testEntry{At: time.Now(), Path: fmt.Sprintf("/%d", i%50_000), State: fmt.Sprintf("s%d", i%7)}
	}
	if err := log.Append(ctx, entries); err != nil {
		t.Fatal(err)
	}
	tally := func() <-chan error {
		done := make(chan error, 1)
		go func() {
			groups, _, err := log.Tally(ctx, Tally{By: []string{"path"}, Measures: []string{"COUNT(DISTINCT state)"}, Sort: 0})
			if err == nil && len(groups) != 10_000 {
				err = fmt.Errorf("%d groups", len(groups))
			}
			done <- err
		}()
		return done
	}

	// Past the limit, with a read that won't end and a tally running.
	log.gate.RLock()
	tallied := tally()
	time.Sleep(20 * time.Millisecond)
	for range 2 {
		start := time.Now()
		if err := log.Checkpoint(ctx); err != nil || time.Since(start) > checkpointWait+time.Second {
			t.Fatalf("checkpoint: %v after %v", err, time.Since(start))
		}
	}
	start := time.Now()
	if _, _, err := log.Query(ctx, Query{Limit: 1}); err != nil || time.Since(start) > time.Second {
		t.Fatalf("a read waited %v for a checkpoint: %v", time.Since(start), err)
	}
	log.gate.RUnlock()
	if err := <-tallied; err != nil {
		t.Fatalf("a tally stopped under the ceiling: %v", err)
	}

	// Past the ceiling.
	walCeiling = 1
	tallied = tally()
	time.Sleep(20 * time.Millisecond)
	if err := log.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-tallied; err != nil {
		t.Fatalf("tally after a checkpoint: %v", err)
	}
	if log.WALSize() != 0 {
		t.Fatalf("write-ahead log %d bytes after the checkpoint", log.WALSize())
	}
}

// Prune finds old entries through the index on at, not a scan of the file.
func TestPruneUsesTheTimeIndex(t *testing.T) {
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	rows, err := log.db.Query("EXPLAIN QUERY PLAN SELECT id FROM events WHERE at < ? ORDER BY at LIMIT 10", time.Now().Format(stampLayout))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if text := strings.Join(plan, "; "); !strings.Contains(text, "events_at") || strings.Contains(text, "TEMP B-TREE") {
		t.Fatalf("plan: %s", text)
	}
}

// An export reads entries by the log's own times, not the clock's: entries
// stamped ahead of it, by a clock set back meanwhile, are still exported.
func TestLogEachIgnoresTheClock(t *testing.T) {
	ctx := context.Background()
	log, _ := openTest(t, Limits{MaxBytes: 64 << 20, Retention: 365 * 24 * time.Hour})
	ahead := time.Now().Add(24 * time.Hour)
	if err := log.Append(ctx, []testEntry{{At: ahead, Path: "/a"}, {At: ahead.Add(time.Second), Path: "/b"}}); err != nil {
		t.Fatal(err)
	}
	var seen []string
	if err := log.Each(ctx, Query{}, func(e *testEntry) error { seen = append(seen, e.Path); return nil }); err != nil || !slices.Equal(seen, []string{"/b", "/a"}) {
		t.Fatalf("%v %v", seen, err)
	}
}
