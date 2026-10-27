package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrInvalidAudit reports an entry the log refuses to store.
var ErrInvalidAudit = errors.New("invalid audit entry")

// MaxAuditLimit bounds one page of the audit log.
const MaxAuditLimit = 200

// AuditEntry is one administrator action. Empty strings are stored as NULL.
// Detail holds only non-secret fields: never tokens, passwords or codes.
// ID and At are fixed when the action happens, so an entry written late
// still sorts by its own time and a retried write cannot store it twice.
type AuditEntry struct {
	ID           string
	At           time.Time
	ActorID      string
	ActorEmail   string
	Action       string
	ResourceType string
	ResourceID   string
	Source       string
	IP           string
	UserAgent    string
	Detail       map[string]any
}

// AuditEvent is a stored entry. The log is append-only: triggers abort every
// UPDATE and DELETE, and nothing prunes it.
type AuditEvent struct {
	ID           string          `json:"id"`
	CreatedAt    string          `json:"created_at"`
	ActorEmail   *string         `json:"actor_email"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Source       string          `json:"source"`
	IP           *string         `json:"ip"`
	UserAgent    *string         `json:"user_agent"`
	Detail       json.RawMessage `json:"detail"`
}

type AuditFilter struct {
	Limit, Offset int
	// Action matches the start of the action name, e.g. "connector.".
	Action string
	// Query matches the actor email, resource id or detail text.
	Query string
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Valid reports whether the log accepts the entry.
func (e AuditEntry) Valid() bool {
	return e.Action != "" && e.ResourceType != "" && (e.Source == "public" || e.Source == "local")
}

// NewAuditID returns an ID for an entry that is written later.
func NewAuditID() (string, error) { return randomID() }

// AppendAudit adds one entry at now.
func (d *Database) AppendAudit(ctx context.Context, e AuditEntry, now time.Time) error {
	e.At = now
	return d.AppendAudits(ctx, []AuditEntry{e})
}

// AppendAudits adds entries in one transaction. An entry whose ID is already
// stored is skipped, so a batch can be retried or replayed safely. Missing IDs
// and times are filled in. Timestamps have a fixed width so they sort as text.
func (d *Database) AppendAudits(ctx context.Context, entries []AuditEntry) error {
	for _, e := range entries {
		if !e.Valid() {
			return ErrInvalidAudit
		}
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	insert, err := tx.PrepareContext(ctx, `INSERT INTO audit_events(id, actor_id, actor_email, action, resource_type, resource_id, source, ip, user_agent, detail, created_at)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? WHERE NOT EXISTS (SELECT 1 FROM audit_events WHERE id = ?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, e := range entries {
		if e.ID == "" {
			if e.ID, err = randomID(); err != nil {
				return err
			}
		}
		if e.At.IsZero() {
			e.At = time.Now()
		}
		var detail any
		if len(e.Detail) != 0 {
			raw, err := json.Marshal(e.Detail)
			if err != nil {
				return err
			}
			detail = string(raw)
		}
		if _, err := insert.ExecContext(ctx, e.ID, nullable(e.ActorID), nullable(e.ActorEmail), e.Action, e.ResourceType, e.ResourceID, e.Source, nullable(e.IP), nullable(e.UserAgent), detail,
			e.At.UTC().Format("2006-01-02T15:04:05.000Z"), e.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// likePattern escapes LIKE wildcards so user text matches literally.
func likePattern(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// auditWhere is the WHERE clause, and its arguments, for the events f
// matches.
func auditWhere(f AuditFilter) (string, []any) {
	where, args := " WHERE 1 = 1", []any{}
	if f.Action != "" {
		where += ` AND action LIKE ? ESCAPE '\'`
		args = append(args, likePattern(f.Action)+"%")
	}
	if f.Query != "" {
		where += ` AND (actor_email LIKE ? ESCAPE '\' OR resource_id LIKE ? ESCAPE '\' OR detail LIKE ? ESCAPE '\')`
		pattern := "%" + likePattern(f.Query) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	return where, args
}

const auditColumns = `id, created_at, actor_email, action, resource_type, resource_id, COALESCE(source, ''), ip, user_agent, detail`

// scanAudit reads the events a SELECT of auditColumns, and anything after
// them into more, returns.
func scanAudit(rows *sql.Rows, more ...any) ([]AuditEvent, error) {
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var email, ip, agent, detail sql.NullString
		if err := rows.Scan(append([]any{&e.ID, &e.CreatedAt, &email, &e.Action, &e.ResourceType, &e.ResourceID, &e.Source, &ip, &agent, &detail}, more...)...); err != nil {
			return nil, err
		}
		for text, target := range map[*sql.NullString]**string{&email: &e.ActorEmail, &ip: &e.IP, &agent: &e.UserAgent} {
			if text.Valid {
				*target = &text.String
			}
		}
		if detail.Valid && json.Valid([]byte(detail.String)) {
			e.Detail = json.RawMessage(detail.String)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// Audit returns one page, newest first, and the number of matching entries.
func (d *Database) Audit(ctx context.Context, f AuditFilter) ([]AuditEvent, int, error) {
	where, args := auditWhere(f)
	var total int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.db.QueryContext(ctx, "SELECT "+auditColumns+" FROM audit_events"+where+" ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	events, err := scanAudit(rows)
	return events, total, err
}

// auditChunk is how many events EachAudit reads at a time.
const auditChunk = 500

// EachAudit calls fn with every event f matches, newest first as Audit
// lists them; f's Limit and Offset don't apply. It reads a chunk at a time,
// each after the last one read rather than at an offset, and calls fn
// between reads, so the database's one connection is never held while fn
// writes to a client. Events added meanwhile are left out; fn's error stops
// it.
func (d *Database) EachAudit(ctx context.Context, f AuditFilter, fn func(AuditEvent) error) error {
	where, args := auditWhere(f)
	var last int64
	if err := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(rowid), 0) FROM audit_events").Scan(&last); err != nil {
		return err
	}
	where, args = where+" AND rowid <= ?", append(args, last)
	after, cursor := "", []any{}
	for {
		var row int64
		rows, err := d.db.QueryContext(ctx, "SELECT "+auditColumns+", rowid FROM audit_events"+where+after+" ORDER BY created_at DESC, rowid DESC LIMIT ?", append(append(append([]any{}, args...), cursor...), auditChunk)...)
		if err != nil {
			return err
		}
		events, err := scanAudit(rows, &row)
		if err != nil {
			return err
		}
		for _, e := range events {
			if err := fn(e); err != nil {
				return err
			}
		}
		if len(events) < auditChunk {
			return nil
		}
		// row holds the last event's: the next chunk starts after it.
		at := events[len(events)-1].CreatedAt
		// Bounded by created_at first, so the index starts at the cursor.
		after, cursor = " AND created_at <= ? AND (created_at < ? OR rowid < ?)", []any{at, at, row}
	}
}
