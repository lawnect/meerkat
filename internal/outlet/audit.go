package outlet

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/lawnect/meerkat/internal/control"
)

const maxAuditUserAgent = 256

// record appends an audit entry for the request. The actor is the session's
// administrator unless the caller names one; before the first administrator
// exists (bootstrap token) there is none. The source and address follow the
// same trust rule as the login alert: only PublicHandler vouches for a
// forwarded address, and the local admin port is just "local".
func (s *Store) record(r *http.Request, e control.AuditEntry) error {
	if s.control == nil {
		return nil
	}
	ip, public := requestOrigin(r)
	e.Source, e.IP = "local", ""
	if public {
		e.Source, e.IP = "public", ip
	}
	e.UserAgent = r.UserAgent()
	if utf8.RuneCountInString(e.UserAgent) > maxAuditUserAgent {
		e.UserAgent = string([]rune(e.UserAgent)[:maxAuditUserAgent])
	}
	if e.ActorID == "" && e.ActorEmail == "" {
		if cookie, err := r.Cookie("meerkat_session"); err == nil {
			if admin, err := s.control.Authenticate(r.Context(), cookie.Value); err == nil {
				e.ActorID, e.ActorEmail = admin.ID, admin.Email
			}
		}
	}
	// The mutation is already done; a dropped connection must not skip its record.
	return s.control.AppendAudit(context.WithoutCancel(r.Context()), e, s.now())
}

// audited records a completed mutation. When the record cannot be written the
// request fails with 503, so a missing entry is never silent.
func (s *Store) audited(w http.ResponseWriter, r *http.Request, action, resourceType, resourceID string, detail map[string]any) bool {
	return s.auditedEntry(w, r, action, control.AuditEntry{ResourceType: resourceType, ResourceID: resourceID, Detail: detail})
}

func (s *Store) auditedEntry(w http.ResponseWriter, r *http.Request, action string, e control.AuditEntry) bool {
	e.Action = action
	if err := s.record(r, e); err != nil {
		s.auditError(w, r, action, err)
		return false
	}
	return true
}

func (s *Store) auditError(w http.ResponseWriter, r *http.Request, action string, err error) {
	slog.Error("audit record failed", "action", action, "error", err)
	fail(w, r, 503, "audit_failed")
}

// auditFailure records a rejected attempt. The response is already decided,
// so a write failure is only logged.
func (s *Store) auditFailure(r *http.Request, action string, e control.AuditEntry) {
	e.Action = action
	if err := s.record(r, e); err != nil {
		slog.Error("audit record failed", "action", action, "error", err)
	}
}

// attemptedEmail keeps a failed login's address short enough to store.
func attemptedEmail(email string) string {
	if runes := []rune(email); len(runes) > 254 {
		return string(runes[:254])
	}
	return email
}

func (s *Store) connectorName(ctx context.Context, id string) string {
	connectors, err := s.control.ListConnectors(ctx)
	if err != nil {
		return ""
	}
	for _, c := range connectors {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

func (s *Store) routeHostname(ctx context.Context, serviceID, routeID string) string {
	routes, err := s.control.ListRoutes(ctx, serviceID)
	if err != nil {
		return ""
	}
	for _, route := range routes {
		if route.ID == routeID {
			return route.Hostname
		}
	}
	return ""
}

// getAudit lists the audit log, newest first. There is no way to change or
// delete entries through the API.
func (s *Store) getAudit(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	filter := control.AuditFilter{Limit: 50, Action: query.Get("action"), Query: query.Get("q")}
	for name, target := range map[string]*int{"limit": &filter.Limit, "offset": &filter.Offset} {
		if raw := query.Get(name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil {
				fail(w, r, 400, "invalid_audit_query")
				return
			}
			*target = value
		}
	}
	if filter.Limit < 1 || filter.Limit > control.MaxAuditLimit || filter.Offset < 0 || len(filter.Action) > 64 || len(filter.Query) > 128 {
		fail(w, r, 400, "invalid_audit_query")
		return
	}
	events, total, err := s.control.Audit(r.Context(), filter)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, map[string]any{"events": events, "total": total})
}
