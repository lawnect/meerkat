package outlet

import (
	"context"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lawnect/meerkat/internal/control"
)

const maxAuditUserAgent = 256

// audit records an action without waiting for the database: the entry is
// queued, and a slow or failing log never holds up or fails the request. The
// actor is the session's administrator unless the entry names one; before the
// first administrator exists (bootstrap token) there is none. The source and
// address follow the same trust rule as the login alert: only PublicHandler
// vouches for a forwarded address, and the local admin port is just "local".
func (s *Store) audit(r *http.Request, action string, e control.AuditEntry) {
	if s.journal == nil {
		return
	}
	e.Action, e.At = action, s.now()
	ip, public := requestOrigin(r)
	e.Source, e.IP = "local", ""
	if public {
		e.Source, e.IP = "public", ip
	}
	e.UserAgent = strings.Map(func(c rune) rune {
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, r.UserAgent())
	if utf8.RuneCountInString(e.UserAgent) > maxAuditUserAgent {
		e.UserAgent = string([]rune(e.UserAgent)[:maxAuditUserAgent])
	}
	if e.ActorID == "" && e.ActorEmail == "" {
		if admin, ok := s.sessionAdmin(r); ok {
			e.ActorID, e.ActorEmail = admin.ID, admin.Email
		}
	}
	s.journal.add(e)
}

// audited records a completed change to a resource.
func (s *Store) audited(r *http.Request, action, resourceType, resourceID string, detail map[string]any) {
	s.audit(r, action, control.AuditEntry{ResourceType: resourceType, ResourceID: resourceID, Detail: detail})
}

// attemptedEmail keeps a failed login's address only when it is a plain
// email address; any other text is a stranger's input to a permanent log.
func attemptedEmail(email string) string {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 254 || strings.IndexFunc(email, unicode.IsControl) >= 0 {
		return ""
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
