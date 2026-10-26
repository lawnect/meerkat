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
type AuditEntry struct {
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

// AppendAudit adds one entry. Timestamps have a fixed width so they sort as text.
func (d *Database) AppendAudit(ctx context.Context, e AuditEntry, now time.Time) error {
	if e.Action == "" || e.ResourceType == "" || (e.Source != "public" && e.Source != "local") {
		return ErrInvalidAudit
	}
	var detail any
	if len(e.Detail) != 0 {
		raw, err := json.Marshal(e.Detail)
		if err != nil {
			return err
		}
		detail = string(raw)
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `INSERT INTO audit_events(id, actor_id, actor_email, action, resource_type, resource_id, source, ip, user_agent, detail, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullable(e.ActorID), nullable(e.ActorEmail), e.Action, e.ResourceType, e.ResourceID, e.Source, nullable(e.IP), nullable(e.UserAgent), detail,
		now.UTC().Format("2006-01-02T15:04:05.000Z"))
	return err
}

// likePattern escapes LIKE wildcards so user text matches literally.
func likePattern(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// Audit returns one page, newest first, and the number of matching entries.
func (d *Database) Audit(ctx context.Context, f AuditFilter) ([]AuditEvent, int, error) {
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
	var total int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT id, created_at, actor_email, action, resource_type, resource_id, COALESCE(source, ''), ip, user_agent, detail
		FROM audit_events`+where+" ORDER BY rowid DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var email, ip, agent, detail sql.NullString
		if err := rows.Scan(&e.ID, &e.CreatedAt, &email, &e.Action, &e.ResourceType, &e.ResourceID, &e.Source, &ip, &agent, &detail); err != nil {
			return nil, 0, err
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
	return events, total, rows.Err()
}
