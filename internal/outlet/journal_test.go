package outlet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/securefs"
)

// fakeAuditDB records what the journal writes. It can be told to fail, and to
// hold every call until released.
type fakeAuditDB struct {
	mu      sync.Mutex
	err     error
	block   chan struct{}
	stored  []control.AuditEntry // entries of every successful call, in order
	batches []int                // size of every successful call
	calls   []time.Time          // start of every call
	once    sync.Once
}

func (f *fakeAuditDB) AppendAudits(ctx context.Context, entries []control.AuditEntry) error {
	f.mu.Lock()
	f.calls = append(f.calls, time.Now())
	err, block := f.err, f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored = append(f.stored, entries...)
	f.batches = append(f.batches, len(entries))
	return nil
}

func (f *fakeAuditDB) fail(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *fakeAuditDB) release() { f.once.Do(func() { close(f.block) }) }

func (f *fakeAuditDB) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeAuditDB) ids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return entryIDs(f.stored)
}

func entryIDs(entries []control.AuditEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

var journalEpoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// journalEntry is entry i. IDs are zero-padded, so sorted IDs are in order.
func journalEntry(i int) control.AuditEntry {
	return control.AuditEntry{
		ID: fmt.Sprintf("e%05d", i), At: journalEpoch.Add(time.Duration(i) * time.Minute),
		Action: "test.action", ResourceType: "test", ResourceID: fmt.Sprint(i), Source: "local",
		Detail: map[string]any{"n": i},
	}
}

func journalEntries(from, to int) []control.AuditEntry {
	var entries []control.AuditEntry
	for i := from; i < to; i++ {
		entries = append(entries, journalEntry(i))
	}
	return entries
}

func journalIDs(from, to int) []string { return entryIDs(journalEntries(from, to)) }

// startJournal runs a journal with short intervals until stop or the test
// ends. tune may shrink more limits before it starts.
func startJournal(t *testing.T, db auditWriter, dir string, tune ...func(*auditJournal)) (*auditJournal, func()) {
	t.Helper()
	j := newAuditJournal(db, dir)
	j.idleTick = 2 * time.Millisecond
	j.retryMin, j.retryMax = time.Millisecond, 4*time.Millisecond
	for _, f := range tune {
		f(j)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		j.run(ctx)
		close(done)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
	t.Cleanup(stop)
	return j, stop
}

func waitJournal(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func spoolNames(dir string) []string {
	items, _ := os.ReadDir(dir)
	var names []string
	for _, item := range items {
		if strings.HasSuffix(item.Name(), ".json") {
			names = append(names, item.Name())
		}
	}
	return names
}

// spooled returns every entry in the spool, in file-name order.
func spooled(t *testing.T, dir string) []control.AuditEntry {
	t.Helper()
	var all []control.AuditEntry
	for _, name := range spoolNames(dir) {
		var entries []control.AuditEntry
		if err := securefs.ReadJSON(filepath.Join(dir, name), &entries); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		all = append(all, entries...)
	}
	return all
}

func writeSpool(t *testing.T, dir, name string, entries ...control.AuditEntry) {
	t.Helper()
	if err := securefs.WriteJSON(filepath.Join(dir, name), entries); err != nil {
		t.Fatal(err)
	}
}

var errDown = errors.New("database is locked")

func TestJournalBatchesEntries(t *testing.T) {
	db := &fakeAuditDB{block: make(chan struct{})}
	j, _ := startJournal(t, db, t.TempDir(), func(j *auditJournal) { j.batchSize = 10 })
	t.Cleanup(db.release)
	j.add(journalEntry(0))
	waitJournal(t, "first write", func() bool { return db.callCount() == 1 })
	for i := 1; i <= 25; i++ {
		j.add(journalEntry(i))
	}
	db.release()
	j.flush()
	if !slices.Equal(db.batches, []int{1, 10, 10, 5}) {
		t.Fatalf("batches = %v", db.batches)
	}
	if !slices.Equal(db.ids(), journalIDs(0, 26)) {
		t.Fatalf("stored %v", db.ids())
	}
}

func TestJournalAssignsIDAndTimeAndRejectsInvalid(t *testing.T) {
	db := &fakeAuditDB{}
	j, _ := startJournal(t, db, t.TempDir())
	j.add(control.AuditEntry{Action: "a", ResourceType: "t", Source: "local"})
	j.add(control.AuditEntry{Action: "a", ResourceType: "t", Source: "public"})
	j.add(control.AuditEntry{Action: "a", ResourceType: "t", Source: "nowhere"})
	j.flush()
	if len(db.stored) != 2 || j.dropped.Load() != 0 {
		t.Fatalf("stored %d, dropped %d", len(db.stored), j.dropped.Load())
	}
	a, b := db.stored[0], db.stored[1]
	if a.ID == "" || a.ID == b.ID || time.Since(a.At) > time.Minute || time.Since(b.At) > time.Minute {
		t.Fatalf("id and time not assigned: %+v %+v", a, b)
	}
}

func TestJournalRetriesAfterTransientFailureWithoutLossOrDuplicates(t *testing.T) {
	db := &fakeAuditDB{}
	db.fail(errDown)
	j, _ := startJournal(t, db, t.TempDir())
	for i := 0; i < 5; i++ {
		j.add(journalEntry(i))
	}
	waitJournal(t, "retries", func() bool { return db.callCount() >= 3 })
	for i := 5; i < 8; i++ { // still accepted while the database is down
		j.add(journalEntry(i))
	}
	db.fail(nil)
	waitJournal(t, "recovery", func() bool { return len(db.ids()) >= 8 })
	j.flush()
	if !slices.Equal(db.ids(), journalIDs(0, 8)) {
		t.Fatalf("stored %v", db.ids())
	}
	if j.dropped.Load() != 0 || len(spoolNames(j.dir)) != 0 {
		t.Fatalf("dropped %d, spooled %v", j.dropped.Load(), spoolNames(j.dir))
	}
}

func TestJournalBackoffDoublesAndResets(t *testing.T) {
	db := &fakeAuditDB{}
	db.fail(errDown)
	j := newAuditJournal(db, "")
	j.retryMin, j.retryMax = time.Second, 8*time.Second
	batch := journalEntries(0, 1)
	for _, want := range []time.Duration{1, 2, 4, 8, 8} {
		if j.write(batch) || j.backoff != want*time.Second || j.ready() {
			t.Fatalf("backoff = %v, ready = %v, want %v", j.backoff, j.ready(), want*time.Second)
		}
		if wait := time.Until(j.retryAt); wait <= 0 || wait > want*time.Second {
			t.Fatalf("retry in %v, want at most %v", wait, want*time.Second)
		}
	}
	db.fail(nil)
	if !j.write(batch) || j.backoff != 0 || !j.ready() {
		t.Fatal("success must clear the backoff")
	}
}

func TestJournalSpillsAndKeepsOrderWhileDatabaseIsDown(t *testing.T) {
	db := &fakeAuditDB{}
	db.fail(errDown)
	dir := filepath.Join(t.TempDir(), "spool", "audit")
	j, stop := startJournal(t, db, dir, func(j *auditJournal) { j.memoryCap, j.fileEntries = 20, 5 })
	for i := 0; i < 30; i++ {
		j.add(journalEntry(i))
	}
	// Spilled because memory filled up, not because the journal stopped.
	waitJournal(t, "spill", func() bool { return len(spoolNames(dir)) > 0 })
	stop()
	if len(db.ids()) != 0 || j.dropped.Load() != 0 {
		t.Fatalf("stored %v, dropped %d", db.ids(), j.dropped.Load())
	}
	for _, name := range spoolNames(dir) {
		var entries []control.AuditEntry
		if err := securefs.ReadJSON(filepath.Join(dir, name), &entries); err != nil || len(entries) > 5 {
			t.Fatalf("%s: %d entries, %v", name, len(entries), err)
		}
	}
	if !slices.Equal(entryIDs(spooled(t, dir)), journalIDs(0, 30)) {
		t.Fatalf("spool holds %v", entryIDs(spooled(t, dir)))
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, spoolNames(dir)[0]): 0600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
}

func TestJournalReplaysSpoolWhenIdleAfterRecovery(t *testing.T) {
	db := &fakeAuditDB{}
	db.fail(errDown)
	dir := t.TempDir()
	j, _ := startJournal(t, db, dir, func(j *auditJournal) { j.memoryCap, j.fileEntries = 10, 4 })
	for i := 0; i < 30; i++ {
		j.add(journalEntry(i))
	}
	waitJournal(t, "spill", func() bool { return len(spoolNames(dir)) >= 2 })
	db.fail(nil)
	waitJournal(t, "replay", func() bool { return len(db.ids()) >= 30 && len(spoolNames(dir)) == 0 })
	j.flush()
	if got := slices.Sorted(slices.Values(db.ids())); !slices.Equal(got, journalIDs(0, 30)) {
		t.Fatalf("stored %v", got)
	}
	if j.dropped.Load() != 0 {
		t.Fatalf("dropped %d", j.dropped.Load())
	}
}

func TestJournalReplaysOneFilePerIdleTick(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 4; i++ {
		writeSpool(t, dir, fmt.Sprintf("%020d-000000.json", i), journalEntries(2*i, 2*i+2)...)
	}
	db := &fakeAuditDB{}
	tick := 20 * time.Millisecond
	startJournal(t, db, dir, func(j *auditJournal) { j.idleTick = tick })
	waitJournal(t, "replay", func() bool { return len(db.ids()) == 8 && len(spoolNames(dir)) == 0 })
	if len(db.calls) != 4 {
		t.Fatalf("%d writes for 4 files", len(db.calls))
	}
	for i := 1; i < len(db.calls); i++ {
		if gap := db.calls[i].Sub(db.calls[i-1]); gap < tick {
			t.Fatalf("files %d and %d replayed %v apart, want at least %v", i-1, i, gap, tick)
		}
	}
}

func TestJournalReplaysFilesLeftByPreviousProcess(t *testing.T) {
	dir := t.TempDir()
	// Written out of order on purpose: replay follows the file names.
	writeSpool(t, dir, "00000000000000000020-000000.json", journalEntries(4, 6)...)
	writeSpool(t, dir, "00000000000000000010-000000.json", journalEntries(0, 4)...)
	db := &fakeAuditDB{}
	j, _ := startJournal(t, db, dir)
	waitJournal(t, "startup replay", func() bool { return len(db.ids()) == 6 && len(spoolNames(dir)) == 0 })
	j.add(journalEntry(6))
	j.flush()
	if !slices.Equal(db.ids(), journalIDs(0, 7)) {
		t.Fatalf("stored %v", db.ids())
	}
}

func TestJournalSetsAsideUnusableFiles(t *testing.T) {
	dir := t.TempDir()
	garbage := []byte("{not json")
	if err := securefs.Write(filepath.Join(dir, "a.json"), garbage); err != nil {
		t.Fatal(err)
	}
	refused := control.AuditEntry{ID: "bad", Action: "a", ResourceType: "t", Source: "elsewhere"}
	writeSpool(t, dir, "b.json", journalEntry(0), refused)
	writeSpool(t, dir, "c.json", journalEntry(1))
	db := &fakeAuditDB{}
	j, _ := startJournal(t, db, dir)
	waitJournal(t, "good file", func() bool { return len(db.ids()) == 1 && len(spoolNames(dir)) == 0 })
	if db.ids()[0] != journalEntry(1).ID {
		t.Fatalf("stored %v", db.ids())
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.json.bad"))
	if err != nil || !bytes.Equal(got, garbage) {
		t.Fatalf("garbage file not kept as .bad: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.json.bad")); err != nil {
		t.Fatal("file with a refused entry not kept as .bad")
	}
	j.add(journalEntry(2))
	j.flush()
	if !slices.Equal(db.ids(), []string{journalEntry(1).ID, journalEntry(2).ID}) {
		t.Fatalf("journal stopped working: %v", db.ids())
	}
}

func TestJournalAddNeverBlocksAndCountsOverflow(t *testing.T) {
	db := &fakeAuditDB{block: make(chan struct{})}
	j, _ := startJournal(t, db, t.TempDir(), func(j *auditJournal) { j.queue = make(chan control.AuditEntry, 8) })
	t.Cleanup(db.release)
	j.add(journalEntry(0))
	waitJournal(t, "stuck write", func() bool { return db.callCount() == 1 })
	added := make(chan struct{})
	go func() {
		for i := 1; i <= 20; i++ {
			j.add(journalEntry(i))
		}
		close(added)
	}()
	select {
	case <-added:
	case <-time.After(5 * time.Second):
		t.Fatal("add blocked behind a stuck writer")
	}
	if got := j.dropped.Load(); got != 12 {
		t.Fatalf("dropped = %d, want 12", got)
	}
	db.release()
	j.flush()
	if !slices.Equal(db.ids(), journalIDs(0, 9)) {
		t.Fatalf("stored %v", db.ids())
	}
}

func TestJournalDropsWhenSpoolIsFull(t *testing.T) {
	dir := t.TempDir()
	j := newAuditJournal(&fakeAuditDB{}, dir)
	j.fileEntries, j.maxFiles = 2, 2
	j.pending = journalEntries(0, 12)
	j.spill()
	if got := j.dropped.Load(); got != 8 || len(j.pending) != 0 {
		t.Fatalf("dropped %d, pending %d", got, len(j.pending))
	}
	if len(spoolNames(dir)) != 2 || !slices.Equal(entryIDs(spooled(t, dir)), journalIDs(0, 4)) {
		t.Fatalf("spool = %v", entryIDs(spooled(t, dir)))
	}
	j.pending = journalEntries(12, 14)
	j.spill()
	if got := j.dropped.Load(); got != 10 || len(spoolNames(dir)) != 2 {
		t.Fatalf("full spool: dropped %d, files %d", got, len(spoolNames(dir)))
	}
}

func TestJournalSpoolFilesStayBelowSizeCap(t *testing.T) {
	dir := t.TempDir()
	j := newAuditJournal(&fakeAuditDB{}, dir)
	j.fileBytes = 2000
	j.pending = journalEntries(0, 10)
	for i := range j.pending {
		j.pending[i].Detail = map[string]any{"padding": strings.Repeat("x", 300)}
	}
	huge := journalEntry(10)
	huge.Detail = map[string]any{"padding": strings.Repeat("x", 5000)}
	j.pending = append(j.pending, huge)
	j.spill()
	names := spoolNames(dir)
	if len(names) < 3 || j.dropped.Load() != 1 {
		t.Fatalf("%d files, dropped %d", len(names), j.dropped.Load())
	}
	for _, name := range names {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() > int64(j.fileBytes) {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	if !slices.Equal(entryIDs(spooled(t, dir)), journalIDs(0, 10)) {
		t.Fatalf("spool holds %v", entryIDs(spooled(t, dir)))
	}
}

func TestJournalShutdownWritesEverythingWhenDatabaseIsUp(t *testing.T) {
	db := &fakeAuditDB{}
	dir := t.TempDir()
	j, stop := startJournal(t, db, dir, func(j *auditJournal) { j.batchSize = 7 })
	for i := 0; i < 50; i++ {
		j.add(journalEntry(i))
	}
	stop()
	if !slices.Equal(db.ids(), journalIDs(0, 50)) || len(spoolNames(dir)) != 0 {
		t.Fatalf("stored %d, spooled %v", len(db.ids()), spoolNames(dir))
	}
}

func TestJournalShutdownSpillsAndNextJournalReplays(t *testing.T) {
	dir := t.TempDir()
	down := &fakeAuditDB{}
	down.fail(errDown)
	j, stop := startJournal(t, down, dir)
	for i := 0; i < 20; i++ {
		j.add(journalEntry(i))
	}
	stop()
	if !slices.Equal(entryIDs(spooled(t, dir)), journalIDs(0, 20)) || len(down.ids()) != 0 {
		t.Fatalf("spool holds %v", entryIDs(spooled(t, dir)))
	}
	up := &fakeAuditDB{}
	startJournal(t, up, dir)
	waitJournal(t, "replay", func() bool { return len(up.ids()) >= 20 && len(spoolNames(dir)) == 0 })
	if !slices.Equal(up.ids(), journalIDs(0, 20)) {
		t.Fatalf("replayed %v", up.ids())
	}
}

func TestJournalFlushWritesAddedEntriesThenReplaysSpool(t *testing.T) {
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-000000.json", journalEntries(100, 102)...)
	writeSpool(t, dir, "00000000000000000002-000000.json", journalEntries(102, 104)...)
	db := &fakeAuditDB{}
	// An hour between ticks: only flush can have done the work.
	j, _ := startJournal(t, db, dir, func(j *auditJournal) { j.idleTick = time.Hour })
	for i := 0; i < 5; i++ {
		j.add(journalEntry(i))
	}
	j.flush()
	if got := slices.Sorted(slices.Values(db.ids())); !slices.Equal(got, append(journalIDs(0, 5), journalIDs(100, 104)...)) {
		t.Fatalf("stored %v", got)
	}
	if len(spoolNames(dir)) != 0 {
		t.Fatalf("spool not replayed: %v", spoolNames(dir))
	}
}

func TestJournalFlushReturnsWhenWriteFails(t *testing.T) {
	db := &fakeAuditDB{}
	db.fail(errDown)
	dir := t.TempDir()
	writeSpool(t, dir, "00000000000000000001-000000.json", journalEntries(100, 101)...)
	j, _ := startJournal(t, db, dir, func(j *auditJournal) { j.idleTick = time.Hour })
	j.add(journalEntry(0))
	flushed := make(chan struct{})
	go func() {
		j.flush()
		close(flushed)
	}()
	select {
	case <-flushed:
	case <-time.After(5 * time.Second):
		t.Fatal("flush hangs while the database is down")
	}
	if len(db.ids()) != 0 || len(spoolNames(dir)) != 1 {
		t.Fatalf("stored %v, spooled %v", db.ids(), spoolNames(dir))
	}
	db.fail(nil)
	j.flush()
	if got := slices.Sorted(slices.Values(db.ids())); !slices.Equal(got, []string{journalEntry(0).ID, journalEntry(100).ID}) {
		t.Fatalf("stored %v after recovery", got)
	}
}

// logCapture collects log records so a test can count warnings.
type logCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (l *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (l *logCapture) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logCapture) WithGroup(string) slog.Handler            { return l }
func (l *logCapture) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	l.records = append(l.records, r)
	l.mu.Unlock()
	return nil
}

// dropWarnings returns the "count" of each drop warning logged so far.
func (l *logCapture) dropWarnings() []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var counts []int64
	for _, r := range l.records {
		if r.Message != "audit entries dropped" || r.Level != slog.LevelWarn {
			continue
		}
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "count" {
				counts = append(counts, a.Value.Int64())
			}
			return true
		})
	}
	return counts
}

func TestJournalWarnsAboutDropsOncePerInterval(t *testing.T) {
	logs := &logCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(previous) })
	j := newAuditJournal(&fakeAuditDB{}, "")
	j.warnDrops()
	if got := logs.dropWarnings(); len(got) != 0 {
		t.Fatalf("warned without drops: %v", got)
	}
	j.dropped.Add(5)
	j.warnDrops()
	j.dropped.Add(3)
	j.warnDrops()
	j.warnDrops()
	if got := logs.dropWarnings(); !slices.Equal(got, []int64{5}) {
		t.Fatalf("warnings = %v, want one for 5", got)
	}
	j.warnedAt = j.warnedAt.Add(-2 * j.warnEvery)
	j.warnDrops()
	j.warnDrops()
	if got := logs.dropWarnings(); !slices.Equal(got, []int64{5, 3}) {
		t.Fatalf("warnings = %v, want 5 then 3", got)
	}
}

func openJournalDB(t *testing.T, path string) *control.Database {
	t.Helper()
	db, err := control.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestJournalReplayingTheSameFileTwiceStoresEntriesOnce(t *testing.T) {
	ctx := context.Background()
	db := openJournalDB(t, filepath.Join(t.TempDir(), "meerkat.db"))
	dir := filepath.Join(t.TempDir(), "spool")
	// A crash between the database write and deleting the file leaves it to
	// be replayed again; the second copy is the same file under a later name.
	oldest, middle, newest := journalEntry(1), journalEntry(2), journalEntry(3)
	writeSpool(t, dir, "00000000000000000001-000000.json", newest, oldest)
	j, _ := startJournal(t, db, dir, func(j *auditJournal) { j.idleTick = time.Hour })
	j.add(middle)
	j.flush()
	writeSpool(t, dir, "00000000000000000002-000000.json", newest, oldest)
	j.flush()
	events, total, err := db.Audit(ctx, control.AuditFilter{Limit: control.MaxAuditLimit})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	if total != 3 || !slices.Equal(ids, []string{newest.ID, middle.ID, oldest.ID}) {
		t.Fatalf("total %d, newest first: %v", total, ids)
	}
	if len(spoolNames(dir)) != 0 {
		t.Fatalf("spool left behind: %v", spoolNames(dir))
	}
}

func TestJournalSurvivesDatabaseOutage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "meerkat.db")
	dir := filepath.Join(t.TempDir(), "spool", "audit")
	closed := openJournalDB(t, path)
	closed.Close() // every write now fails
	j, stop := startJournal(t, closed, dir)
	for i := 0; i < 5; i++ {
		e := journalEntry(i)
		e.Detail = map[string]any{"n": i, "name": "서비스"}
		j.add(e)
	}
	stop()
	if len(spoolNames(dir)) == 0 {
		t.Fatal("nothing spooled while the database was closed")
	}
	reopened := openJournalDB(t, path)
	j, _ = startJournal(t, reopened, dir)
	j.flush()
	events, total, err := reopened.Audit(ctx, control.AuditFilter{Limit: control.MaxAuditLimit})
	if err != nil || total != 5 {
		t.Fatalf("total %d, %v", total, err)
	}
	if got := string(events[0].Detail); got != `{"n":4,"name":"서비스"}` {
		t.Fatalf("detail = %s", got)
	}
	if len(spoolNames(dir)) != 0 {
		t.Fatalf("spool left behind: %v", spoolNames(dir))
	}
}
