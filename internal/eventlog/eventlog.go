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
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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
type Kind[E any] struct {
	Table   string
	Columns []string
	Filters []string
	Search  []string
	Fields  func(*E) []any
	Stamp   func(*E) (*int64, *time.Time)
	Routine func(*E) *bool
}

// Log is one log's database; names are its columns' names.
type Log[E any] struct {
	db     *sql.DB
	kind   Kind[E]
	names  string
	limits atomic.Pointer[Limits]
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
	columns, names := make([]string, len(kind.Columns)), make([]string, len(kind.Columns))
	for i, column := range kind.Columns {
		columns[i], names[i] = column+" NOT NULL", strings.Fields(column)[0]
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
	log := &Log[E]{db: db, kind: kind, names: strings.Join(names, ", ")}
	log.SetLimits(limits)
	return log, nil
}

func (l *Log[E]) Close() error { return l.db.Close() }

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
		if _, err := insert.ExecContext(ctx, append([]any{at.UTC().Format(time.RFC3339Nano), *l.kind.Routine(&entries[i])}, l.kind.Fields(&entries[i])...)...); err != nil {
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

// Shed forgets the oldest tenth of the entries, so writes fit again: freed
// pages are reused rather than the file growing. The routine ones go first,
// and the notable ones only for what the routine ones can't make up, so a
// flood can't push out what matters.
func (l *Log[E]) Shed(ctx context.Context) error {
	var count int64
	if err := l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+l.kind.Table).Scan(&count); err != nil {
		return err
	}
	tenth := max(count/10, 1)
	done, err := l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %[1]s WHERE id IN (SELECT id FROM %[1]s WHERE routine = 1 ORDER BY id LIMIT ?)", l.kind.Table), tenth)
	if err != nil {
		return err
	}
	gone, err := done.RowsAffected()
	if err != nil || gone >= tenth {
		return err
	}
	_, err = l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %[1]s WHERE id IN (SELECT id FROM %[1]s ORDER BY id LIMIT ?)", l.kind.Table), tenth-gone)
	return err
}

// Prune forgets entries older than the retention before now, then sheds,
// routine entries first, until the file, its free pages given back, fits
// the cap.
func (l *Log[E]) Prune(ctx context.Context, now time.Time) error {
	limits := l.Limits()
	if _, err := l.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE at < ?", l.kind.Table), now.UTC().Add(-limits.Retention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	for range 20 {
		if _, err := l.db.ExecContext(ctx, "PRAGMA incremental_vacuum"); err != nil {
			return err
		}
		size, err := l.Size(ctx)
		if err != nil || size <= limits.MaxBytes {
			return err
		}
		if err := l.Shed(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Size returns the bytes the file takes.
func (l *Log[E]) Size(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := l.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return 0, err
	}
	if err := l.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
		return 0, err
	}
	return pages * size, nil
}

// Query is a page of entries, newest first: those whose Where columns, all
// filter columns, have the given values, that contain Text in a search
// column, and are routine or not as Routine says (nil: either), skipping
// Offset, at most Limit.
type Query struct {
	Where   map[string]string
	Text    string
	Routine *bool
	Offset  int
	Limit   int
}

// likeEscaper makes text match itself in a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Query returns a page of entries and how many match in all.
func (l *Log[E]) Query(ctx context.Context, q Query) ([]E, int, error) {
	limit := q.Limit
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
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
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := l.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+l.kind.Table+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := l.db.QueryContext(ctx, fmt.Sprintf("SELECT id, at, routine, %s FROM %s%s ORDER BY id DESC LIMIT ? OFFSET ?", l.names, l.kind.Table, clause), append(args, limit, max(q.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := []E{}
	for rows.Next() {
		var entry E
		var at string
		id, stamp := l.kind.Stamp(&entry)
		if err := rows.Scan(append([]any{id, &at, l.kind.Routine(&entry)}, l.kind.Fields(&entry)...)...); err != nil {
			return nil, 0, err
		}
		*stamp, _ = time.Parse(time.RFC3339Nano, at)
		entries = append(entries, entry)
	}
	return entries, total, rows.Err()
}

// Clip cuts s to at most n bytes, on a character boundary.
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
