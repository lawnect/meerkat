// Package eventlog keeps a log of events, one kind per SQLite file of its
// own: however many there are, they never grow the control database or
// wait on its one connection. A file stays under a size cap SQLite itself
// enforces, and entries older than a retention go.
package eventlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lawnect/meerkat/internal/securefs"

	"modernc.org/sqlite"
)

const (
	// pageSize is SQLite's default page, the unit of the size cap.
	pageSize = 4096
	// maxWAL is the write-ahead log SQLite keeps after a checkpoint.
	maxWAL = 16 << 20
	// MaxPage bounds one Query.
	MaxPage = 200
	// layout numbers the table layout every log shares (id, at, routine and
	// the kind's columns); a change to it, or a Kind's Version, starts the
	// files over.
	layout = 1
	// stampLayout is how at is written: in UTC, to the nanosecond and fixed
	// in width, so its text sorts as the time does. RFC3339Nano trims zeros,
	// and "…:00Z" would sort after "…:00.5Z". Files from 0.5.1 hold such
	// trimmed times; they sort right to the second, and age out.
	stampLayout = "2006-01-02T15:04:05.000000000Z"
	// readers is how many reads may run at once beside the writes.
	readers = 4
	// vacuumStep is how many free pages one incremental vacuum gives back:
	// each is a transaction of its own, so the log behind it stays small.
	vacuumStep = 4096
	// tallyTimeout bounds one tally.
	tallyTimeout = time.Minute
	// checkpointWait is how long a checkpoint looks for a moment without
	// reads before it leaves it to the next write.
	checkpointWait = 2 * time.Second
)

// walLimit is how far the write-ahead log may grow, while overlapping
// reads keep SQLite from starting it over, before a checkpoint looks for a
// moment without any; walCeiling, before it makes one (see Checkpoint).
// Tests lower them.
var (
	walLimit   int64 = 4 * maxWAL
	walCeiling int64 = 16 * maxWAL
)

// ErrFull is Append's error when SQLite refuses the write because the file
// reached its cap, or the disk is full; Shed makes room.
var ErrFull = errors.New("event log full")

// Limits bound a log: the most its file may take, and how long entries stay.
type Limits struct {
	MaxBytes  int64
	Retention time.Duration
}

// Kind describes one log's entries: their table, the columns after id and
// at as "name TYPE", the columns queries filter on (each indexed), the ones
// a text search looks in, pointers to an entry's fields for the columns, in
// order, to its id and time, and to its routine flag.
//
// An entry is routine when anyone can make it happen at will, as a flood
// would; the rest are notable. The flag has a column of its own, so that
// shedding can keep notable entries longest.
//
// A log whose notable entries anyone can make as easily as routine ones,
// such as one of every request, sheds strictly oldest first: OldestFirst.
// Otherwise a flood of those would push out the routine entries first.
//
// Version numbers the columns: change them and change it, and files kept
// in the old ones start over empty rather than failing to open. Log entries
// are worth less than a log that works.
type Kind[E any] struct {
	Table       string
	Version     int
	Columns     []string
	Filters     []string
	Search      []string
	Fields      func(*E) []any
	Stamp       func(*E) (*int64, *time.Time)
	Routine     func(*E) *bool
	OldestFirst bool
}

// Names returns the names of the kind's columns, in order.
func (k Kind[E]) Names() []string {
	names := make([]string, len(k.Columns))
	for i, column := range k.Columns {
		names[i] = strings.Fields(column)[0]
	}
	return names
}

// Log is one log's database: db writes, on one connection, and read reads.
// The file is in WAL mode, so a long read, a tally or an export, never holds
// up the writes, nor a write the reads. Reads hold gate shared, and a
// checkpoint holds it alone; tallies, the longest reads, take turns. names
// are the columns' names.
type Log[E any] struct {
	db      *sql.DB
	read    *sql.DB
	gate    sync.RWMutex
	tallies chan struct{}
	// preempt cancels the tally under way, for a checkpoint; checkpointed
	// is closed when the checkpoint under way ends, nil while there is none.
	preemptMu    sync.Mutex
	preempt      context.CancelFunc
	checkpointed chan struct{}
	path         string
	kind         Kind[E]
	names        string
	limits       atomic.Pointer[Limits]
}

// Open opens the log at path, creating it.
func Open[E any](ctx context.Context, path string, kind Kind[E], limits Limits) (*Log[E], error) {
	if path == "" {
		return nil, errors.New("event log path is required")
	}
	if kind.Routine == nil {
		return nil, errors.New("event log kind needs its routine flag")
	}
	if err := securefs.Directory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// The pragmas go in the name so every connection SQLite opens has them;
	// incremental auto-vacuum lets a lowered cap shrink the file. The cap
	// itself is set on each write (see Append), as it can change.
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_auto_vacuum=INCREMENTAL&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(%d)", path, maxWAL))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	columns := make([]string, len(kind.Columns))
	for i, column := range kind.Columns {
		columns[i] = column + " NOT NULL"
	}
	// A file whose layout isn't this one, from an older build, starts over.
	version := layout<<16 | kind.Version
	var stored int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&stored); err != nil {
		db.Close()
		return nil, err
	}
	if stored != version {
		var tables int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?", kind.Table).Scan(&tables); err != nil {
			db.Close()
			return nil, err
		}
		if tables > 0 {
			slog.Warn("event log kept by another version starts over", "path", path, "version", stored, "want", version)
			if _, err := db.ExecContext(ctx, "DROP TABLE "+kind.Table); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	statements := []string{fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id INTEGER PRIMARY KEY, at TEXT NOT NULL, routine INTEGER NOT NULL, %s)", kind.Table, strings.Join(columns, ", ")),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %[1]s_at ON %[1]s(at)", kind.Table),
		// Shedding takes the oldest routine entries, and a list can show only them or only the rest.
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %[1]s_routine ON %[1]s(routine, id)", kind.Table)}
	for _, column := range kind.Filters {
		statements = append(statements, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %[1]s_%[2]s ON %[1]s(%[2]s, id)", kind.Table, column))
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		db.Close()
		return nil, err
	}
	read, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		db.Close()
		return nil, err
	}
	read.SetMaxOpenConns(readers)
	log := &Log[E]{db: db, read: read, tallies: make(chan struct{}, 1), path: path, kind: kind, names: strings.Join(kind.Names(), ", ")}
	log.SetLimits(limits)
	return log, nil
}

func (l *Log[E]) Close() error { return errors.Join(l.read.Close(), l.db.Close()) }

// SetLimits changes the log's limits; writes take the cap at once, and the
// next Prune shrinks the file to it.
func (l *Log[E]) SetLimits(limits Limits) { l.limits.Store(&limits) }

// Limits returns the log's limits.
func (l *Log[E]) Limits() Limits { return *l.limits.Load() }

// Append stores entries in one transaction, under the current cap.
func (l *Log[E]) Append(ctx context.Context, entries []E) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A cap below the file's size leaves it as it is; Prune shrinks it.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA max_page_count = %d", max(l.Limits().MaxBytes/pageSize, 1))); err != nil {
		return err
	}
	insert, err := tx.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %s(at, routine, %s) VALUES (?, ?%s)", l.kind.Table, l.names, strings.Repeat(", ?", len(l.kind.Columns))))
	if err != nil {
		return err
	}
	defer insert.Close()
	for i := range entries {
		_, at := l.kind.Stamp(&entries[i])
		if _, err := insert.ExecContext(ctx, append([]any{at.UTC().Format(stampLayout), *l.kind.Routine(&entries[i])}, l.kind.Fields(&entries[i])...)...); err != nil {
			return full(err)
		}
	}
	return full(tx.Commit())
}

// full turns SQLite's SQLITE_FULL into ErrFull.
func full(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) && e.Code()&0xff == 13 {
		return fmt.Errorf("%w: %v", ErrFull, err)
	}
	return err
}

// shedChunk bounds one delete: the writes have a single connection, and
// the entries waiting get it between chunks rather than after the whole lot.
const shedChunk = 10_000

// Shed forgets the oldest tenth of the entries, at most a chunk, so writes
// fit again: freed pages are reused rather than the file growing. A chunk
// at most keeps each pause of the writes short on a big file, which sheds
// again when it fills. The tenth is of the span of ids, read off the
// primary key rather than counted.
func (l *Log[E]) Shed(ctx context.Context) error {
	var span int64
	if err := l.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id) - MIN(id) + 1, 0) FROM "+l.kind.Table).Scan(&span); err != nil {
		return err
	}
	return l.shed(ctx, min(max(span/10, 1), shedChunk))
}

// shed forgets the n oldest entries, in chunks. The routine ones go first,
// and the notable ones only for what the routine ones can't make up, so a
// flood can't push out what matters; for a kind that is OldestFirst, the
// oldest go, whichever they are.
func (l *Log[E]) shed(ctx context.Context, n int64) error {
	order := []string{"routine = 1", "1 = 1"}
	if l.kind.OldestFirst {
		order = order[1:]
	}
	for _, which := range order {
		for n > 0 {
			chunk := min(n, shedChunk)
			done, err := l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %[1]s WHERE id IN (SELECT id FROM %[1]s WHERE %[2]s ORDER BY id LIMIT ?)", l.kind.Table, which), chunk)
			if err != nil {
				return err
			}
			gone, err := done.RowsAffected()
			if err != nil {
				return err
			}
			n -= gone
			if gone < chunk {
				break
			}
		}
	}
	return nil
}

// Prune forgets entries older than the retention before now, then sheds,
// routine entries first, until the file, its free pages given back, fits
// the cap. A lowered cap is met in one Prune: it sheds what the cap leaves
// no room for, with a tenth to spare, rather than a chunk at a time.
func (l *Log[E]) Prune(ctx context.Context, now time.Time) error {
	limits := l.Limits()
	for {
		// In the order of at, its index finds the old entries without a scan.
		done, err := l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %[1]s WHERE id IN (SELECT id FROM %[1]s WHERE at < ? ORDER BY at LIMIT %[2]d)", l.kind.Table, shedChunk), now.UTC().Add(-limits.Retention).Format(stampLayout))
		if err != nil {
			return err
		}
		if gone, err := done.RowsAffected(); err != nil || gone < shedChunk {
			if err != nil {
				return err
			}
			break
		}
	}
	for range 8 {
		if err := l.vacuum(ctx); err != nil {
			return err
		}
		size, err := l.size(ctx, l.db)
		if err != nil || size <= limits.MaxBytes {
			return err
		}
		var count int64
		if err := l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+l.kind.Table).Scan(&count); err != nil || count == 0 {
			return err
		}
		keep := int64(float64(count) * float64(limits.MaxBytes) / float64(size) * 0.9)
		if err := l.shed(ctx, max(count-keep, 1)); err != nil {
			return err
		}
	}
	return nil
}

// vacuum gives the free pages back, vacuumStep at a time, while each step
// gives some back: a file made elsewhere without incremental auto-vacuum
// gives none.
func (l *Log[E]) vacuum(ctx context.Context) error {
	last := int64(-1)
	for {
		var free int64
		if err := l.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&free); err != nil || free == 0 || last >= 0 && free >= last {
			return err
		}
		last = free
		if _, err := l.db.ExecContext(ctx, fmt.Sprintf("PRAGMA incremental_vacuum(%d)", vacuumStep)); err != nil {
			return err
		}
	}
}

// Size returns the bytes the file takes.
func (l *Log[E]) Size(ctx context.Context) (int64, error) {
	l.gate.RLock()
	defer l.gate.RUnlock()
	return l.size(ctx, l.read)
}

func (l *Log[E]) size(ctx context.Context, db *sql.DB) (int64, error) {
	var pages, size int64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return 0, err
	}
	if err := db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
		return 0, err
	}
	return pages * size, nil
}

// WALSize returns the bytes the write-ahead log takes.
func (l *Log[E]) WALSize() int64 {
	info, err := os.Stat(l.path + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}

// Checkpoint moves what the write-ahead log holds into the file and empties
// it, once past walLimit. SQLite starts the log over only at a moment no
// read holds a snapshot of it; reads that overlap, tallies and exports
// against a busy log, would let it grow without end and outside the cap.
// Past walLimit it takes the first moment without reads, up to
// checkpointWait, and otherwise leaves it to the next write: reads never
// wait for it. Past walCeiling it makes that moment: it stops the tally
// under way, which starts over once it is done, and new reads wait for the
// ones under way to finish. It is for a goroutine of its own: the writes go
// on meanwhile, but for the moment of the checkpoint.
func (l *Log[E]) Checkpoint(ctx context.Context) error {
	size := l.WALSize()
	if size <= walLimit {
		return nil
	}
	if size <= walCeiling {
		deadline := time.Now().Add(checkpointWait)
		for !l.gate.TryLock() {
			if time.Now().After(deadline) || ctx.Err() != nil {
				return ctx.Err()
			}
			time.Sleep(5 * time.Millisecond)
		}
		defer l.gate.Unlock()
		_, err := l.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		return err
	}
	slog.Warn("event log reads kept its write-ahead log from starting over; they wait for a checkpoint", "path", l.path, "wal_bytes", size)
	done := make(chan struct{})
	l.preemptMu.Lock()
	l.checkpointed = done
	l.preemptMu.Unlock()
	defer func() {
		l.preemptMu.Lock()
		l.checkpointed = nil
		l.preemptMu.Unlock()
		close(done)
	}()
	l.preemptMu.Lock()
	if l.preempt != nil {
		l.preempt()
	}
	l.preemptMu.Unlock()
	l.gate.Lock()
	defer l.gate.Unlock()
	_, err := l.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

// Query is a page of entries, newest first: those whose Where columns, all
// filter columns, have the given values, that contain Text in a search
// column, are routine or not as Routine says (nil: either), and are from
// Since on (zero: any time), skipping Offset, at most Limit.
type Query struct {
	Where   map[string]string
	Text    string
	Routine *bool
	Since   time.Time
	Offset  int
	Limit   int
}

// likeEscaper makes text match itself in a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// filter returns the WHERE clause, empty or with a leading space, and its
// arguments for the entries q matches.
func (l *Log[E]) filter(q Query) (string, []any) {
	var where []string
	var args []any
	for _, column := range l.kind.Filters {
		if value, ok := q.Where[column]; ok && value != "" {
			where, args = append(where, column+" = ?"), append(args, value)
		}
	}
	if q.Text != "" && len(l.kind.Search) > 0 {
		match := make([]string, len(l.kind.Search))
		for i, column := range l.kind.Search {
			match[i] = column + ` LIKE ? ESCAPE '\'`
			args = append(args, "%"+likeEscaper.Replace(q.Text)+"%")
		}
		where = append(where, "("+strings.Join(match, " OR ")+")")
	}
	if q.Routine != nil {
		where, args = append(where, "routine = ?"), append(args, *q.Routine)
	}
	if !q.Since.IsZero() {
		where, args = append(where, "at >= ?"), append(args, q.Since.UTC().Format(stampLayout))
	}
	if len(where) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// Query returns a page of entries and how many match in all.
func (l *Log[E]) Query(ctx context.Context, q Query) ([]E, int, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	clause, args := l.filter(q)
	l.gate.RLock()
	defer l.gate.RUnlock()
	var total int
	if err := l.read.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+l.kind.Table+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	entries, err := l.scan(ctx, fmt.Sprintf("SELECT id, at, routine, %s FROM %s%s ORDER BY id DESC LIMIT ? OFFSET ?", l.names, l.kind.Table, clause), append(args, limit, max(q.Offset, 0))...)
	return entries, total, err
}

// scan reads the entries a SELECT of id, at, routine and the columns
// returns.
func (l *Log[E]) scan(ctx context.Context, query string, args ...any) ([]E, error) {
	rows, err := l.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []E{}
	for rows.Next() {
		var entry E
		var at string
		id, stamp := l.kind.Stamp(&entry)
		if err := rows.Scan(append([]any{id, &at, l.kind.Routine(&entry)}, l.kind.Fields(&entry)...)...); err != nil {
			return nil, err
		}
		*stamp, _ = time.Parse(time.RFC3339Nano, at)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// eachChunk is how many entries Each reads at a time.
const eachChunk = 1000

// Each calls fn with every entry q matches, newest first; q's Offset and
// Limit don't apply. It reads a chunk at a time and calls fn between reads,
// so a slow fn, one writing to a client, holds no read open. Entries added
// meanwhile are left out; fn's error stops it.
func (l *Log[E]) Each(ctx context.Context, q Query, fn func(*E) error) error {
	clause, args := l.filter(q)
	if clause == "" {
		clause = " WHERE id < ?"
	} else {
		clause += " AND id < ?"
	}
	var last int64
	if err := l.read.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) + 1 FROM "+l.kind.Table).Scan(&last); err != nil {
		return err
	}
	query := fmt.Sprintf("SELECT id, at, routine, %s FROM %s%s ORDER BY id DESC LIMIT %d", l.names, l.kind.Table, clause, eachChunk)
	for {
		l.gate.RLock()
		entries, err := l.scan(ctx, query, append(args, last)...)
		l.gate.RUnlock()
		if err != nil {
			return err
		}
		for i := range entries {
			if err := fn(&entries[i]); err != nil {
				return err
			}
		}
		if len(entries) < eachChunk {
			return nil
		}
		id, _ := l.kind.Stamp(&entries[len(entries)-1])
		last = *id
	}
}

// MaxTally bounds the groups one Tally returns.
const MaxTally = 10_000

// Tally groups the entries its Query matches by the By columns. Measures
// are integer SQL aggregates each group gets, written in code and never
// taken from a request; Sort is the measure groups are ordered by, or -1
// for their latest entry. Its Query's Offset and Limit page the groups.
type Tally struct {
	Query
	By        []string
	Measures  []string
	Sort      int
	Ascending bool
}

// Group is one of a Tally's groups: its By columns' values, its measures,
// and when its latest entry was.
type Group struct {
	Keys     []string
	Measures []int64
	Last     time.Time
}

// Tally returns a page of groups and how many there are in all.
func (l *Log[E]) Tally(ctx context.Context, t Tally) ([]Group, int, error) {
	limit := t.Limit
	if limit <= 0 || limit > MaxTally {
		limit = MaxTally
	}
	if len(t.By) == 0 || t.Sort < -1 || t.Sort >= len(t.Measures) {
		return nil, 0, errors.New("event log tally needs columns and a measure to sort by")
	}
	select {
	case l.tallies <- struct{}{}:
		defer func() { <-l.tallies }()
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	// A checkpoint may stop the tally once; it starts over once the
	// checkpoint is done.
	var wait chan struct{}
	for attempt := 0; ; attempt++ {
		run, cancel := context.WithTimeout(ctx, tallyTimeout)
		l.preemptMu.Lock()
		l.preempt = cancel
		l.preemptMu.Unlock()
		groups, total, err := l.tally(run, t, limit)
		l.preemptMu.Lock()
		l.preempt, wait = nil, l.checkpointed
		l.preemptMu.Unlock()
		preempted := err != nil && errors.Is(run.Err(), context.Canceled) && ctx.Err() == nil
		cancel()
		if !preempted || attempt > 0 {
			return groups, total, err
		}
		if wait != nil {
			select {
			case <-wait:
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
		}
	}
}

func (l *Log[E]) tally(ctx context.Context, t Tally, limit int) ([]Group, int, error) {
	l.gate.RLock()
	defer l.gate.RUnlock()
	clause, args := l.filter(t.Query)
	// Grouping by columns with an index of their own would have SQLite walk
	// that index through the whole file and drop what is out of the period;
	// a period with no column to filter on reads the period's entries alone.
	from := l.kind.Table
	if !t.Since.IsZero() && !slices.ContainsFunc(l.kind.Filters, func(column string) bool { return t.Where[column] != "" }) {
		from = fmt.Sprintf("%[1]s INDEXED BY %[1]s_at", l.kind.Table)
	}
	by := strings.Join(t.By, ", ")
	order := "last"
	if t.Sort >= 0 {
		order = fmt.Sprintf("m%d", t.Sort)
	}
	direction := "DESC"
	if t.Ascending {
		direction = "ASC"
	}
	measures := make([]string, len(t.Measures))
	for i, measure := range t.Measures {
		measures[i] = fmt.Sprintf("%s AS m%d", measure, i)
	}
	// The window counts the groups, before the page is taken; ties go in
	// the columns' order, so pages don't overlap.
	rows, err := l.read.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s, MAX(at) AS last, COUNT(*) OVER () FROM %s%s GROUP BY %s ORDER BY %s %s, %s LIMIT ? OFFSET ?",
		by, strings.Join(measures, ", "), from, clause, by, order, direction, by), append(args, limit, max(t.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	groups, total := []Group{}, 0
	for rows.Next() {
		group := Group{Keys: make([]string, len(t.By)), Measures: make([]int64, len(t.Measures))}
		var last string
		into := make([]any, 0, len(t.By)+len(t.Measures)+2)
		for i := range group.Keys {
			into = append(into, &group.Keys[i])
		}
		for i := range group.Measures {
			into = append(into, &group.Measures[i])
		}
		if err := rows.Scan(append(into, &last, &total)...); err != nil {
			return nil, 0, err
		}
		group.Last, _ = time.Parse(time.RFC3339Nano, last)
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	// A page past the last has no row to count the groups on.
	if len(groups) == 0 && t.Offset > 0 {
		err = l.read.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM (SELECT 1 FROM %s%s GROUP BY %s)", from, clause, by), args...).Scan(&total)
	}
	return groups, total, err
}

// Clip cuts s to at most n bytes, on a character boundary, after making
// bytes that aren't UTF-8 U+FFFD: a path can hold any bytes, and two that
// JSON would show alike must not be kept, counted or filtered apart.
func Clip(s string, n int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
