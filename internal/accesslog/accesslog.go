// Package accesslog keeps the requests access protection decided, in a
// SQLite file of its own: however many there are, they never grow the
// control database or wait on its one connection.
package accesslog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/securefs"

	"modernc.org/sqlite"
)

const (
	// Retention is how long entries are kept, and MaxEntries how many: the
	// oldest go first when either is reached. MaxBytes is the most the
	// database file may take, enforced by SQLite (max_page_count of 4 KiB
	// pages), and maxWAL the write-ahead log it keeps after a checkpoint.
	Retention  = 30 * 24 * time.Hour
	MaxEntries = 500_000
	MaxBytes   = 256 << 20
	maxWAL     = 16 << 20
	// MaxPage bounds one Query.
	MaxPage = 200
	// maxPath is the longest path kept; longer ones are cut.
	maxPath = 512
)

// ErrFull is Append's error when SQLite refuses the write because the
// database reached MaxBytes, or the disk is full; Shed makes room.
var ErrFull = errors.New("access log full")

// Entry is one request access protection decided.
type Entry struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Host     string    `json:"host"`
	Method   string    `json:"method"`
	Path     string    `json:"path"`
	Client   string    `json:"client"`
	Policy   string    `json:"policy"`
	Decision string    `json:"decision"`
	Reason   string    `json:"reason"`
	Email    string    `json:"email"`
	Token    string    `json:"token"`
}

// Filter narrows a Query: by decision and policy when set, entries older
// than Before (an ID) when set, at most Limit of them.
type Filter struct {
	Decision string
	Policy   string
	Before   int64
	Limit    int
}

// Log is the access log's database. keep is MaxEntries, smaller in tests.
type Log struct {
	db   *sql.DB
	keep int
}

// Open opens the access log at path, creating it.
func Open(ctx context.Context, path string) (*Log, error) {
	return open(ctx, path, MaxBytes)
}

// open opens the access log with a file of at most maxBytes; tests shrink it.
func open(ctx context.Context, path string, maxBytes int) (*Log, error) {
	if path == "" {
		return nil, errors.New("access log path is required")
	}
	if err := securefs.Directory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	// The pragmas go in the name so every connection SQLite opens has them,
	// the size cap above all.
	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=journal_size_limit(%d)&_pragma=max_page_count(%d)", path, maxWAL, maxBytes/4096))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS access_log (
			id INTEGER PRIMARY KEY,
			at TEXT NOT NULL,
			host TEXT NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			client TEXT NOT NULL,
			policy_id TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL,
			email TEXT NOT NULL,
			token_id TEXT NOT NULL
		)`,
		"CREATE INDEX IF NOT EXISTS access_log_at ON access_log(at)",
		"CREATE INDEX IF NOT EXISTS access_log_decision ON access_log(decision, id)",
		"CREATE INDEX IF NOT EXISTS access_log_policy ON access_log(policy_id, id)",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Log{db: db, keep: MaxEntries}, nil
}

func (l *Log) Close() error { return l.db.Close() }

// Append stores entries in one transaction.
func (l *Log) Append(ctx context.Context, entries []Entry) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert, err := tx.PrepareContext(ctx, `INSERT INTO access_log(at, host, method, path, client, policy_id, decision, reason, email, token_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, e := range entries {
		path := e.Path
		if len(path) > maxPath {
			path = strings.ToValidUTF8(path[:maxPath], "")
		}
		if _, err := insert.ExecContext(ctx, e.At.UTC().Format(time.RFC3339Nano), e.Host, e.Method, path, e.Client, e.Policy, e.Decision, e.Reason, e.Email, e.Token); err != nil {
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

// Shed forgets the oldest tenth of the entries, so writes fit again under
// MaxBytes: freed pages are reused rather than the file growing.
func (l *Log) Shed(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `DELETE FROM access_log WHERE id <= (SELECT MIN(id) + (MAX(id) - MIN(id)) / 10 FROM access_log)`)
	return err
}

// Prune forgets entries older than Retention before now, then the oldest
// beyond the newest MaxEntries.
func (l *Log) Prune(ctx context.Context, now time.Time) error {
	if _, err := l.db.ExecContext(ctx, `DELETE FROM access_log WHERE at < ?`, now.UTC().Add(-Retention).Format(time.RFC3339Nano)); err != nil {
		return err
	}
	_, err := l.db.ExecContext(ctx, `DELETE FROM access_log WHERE id <= (SELECT id FROM access_log ORDER BY id DESC LIMIT 1 OFFSET ?)`, l.keep)
	return err
}

// Query returns entries newest first, and the ID to pass as Before for the
// next page, 0 when there is none.
func (l *Log) Query(ctx context.Context, filter Filter) ([]Entry, int64, error) {
	limit := filter.Limit
	if limit <= 0 || limit > MaxPage {
		limit = MaxPage
	}
	var where []string
	var args []any
	for _, c := range []struct {
		column, value string
	}{{"decision", filter.Decision}, {"policy_id", filter.Policy}} {
		if c.value != "" {
			where, args = append(where, c.column+" = ?"), append(args, c.value)
		}
	}
	if filter.Before > 0 {
		where, args = append(where, "id < ?"), append(args, filter.Before)
	}
	query := `SELECT id, at, host, method, path, client, policy_id, decision, reason, email, token_id FROM access_log`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := l.db.QueryContext(ctx, query+" ORDER BY id DESC LIMIT ?", append(args, limit+1)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var e Entry
		var at string
		if err := rows.Scan(&e.ID, &at, &e.Host, &e.Method, &e.Path, &e.Client, &e.Policy, &e.Decision, &e.Reason, &e.Email, &e.Token); err != nil {
			return nil, 0, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(entries) > limit {
		entries, next = entries[:limit], entries[limit-1].ID
	}
	return entries, next, nil
}
