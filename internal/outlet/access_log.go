package outlet

import (
	"net/http"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// accessEntry is a request access protection decided, as its log keeps it.
type accessEntry struct {
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
	// Routine entries name no one: anyone can make them.
	Routine bool `json:"routine"`
}

var accessEntries = eventlog.Kind[accessEntry]{
	Table:   "access_log",
	Columns: []string{"host TEXT", "method TEXT", "path TEXT", "client TEXT", "policy TEXT", "decision TEXT", "reason TEXT", "email TEXT", "token TEXT"},
	Filters: []string{"decision", "policy"},
	Search:  []string{"host", "path", "client", "email", "token"},
	Fields: func(e *accessEntry) []any {
		return []any{&e.Host, &e.Method, &e.Path, &e.Client, &e.Policy, &e.Decision, &e.Reason, &e.Email, &e.Token}
	},
	Stamp:   func(e *accessEntry) (*int64, *time.Time) { return &e.ID, &e.At },
	Routine: func(e *accessEntry) *bool { return &e.Routine },
}

// decisions names what a policy decided, as the access log and the console
// read it.
var decisions = [...]string{accessPassed: "passed", accessChallenged: "challenged", accessDenied: "denied"}

// logAccess records what a policy decided for a request, when the access
// log is on: every refusal and sign-in asked for, and requests let in that
// name someone, a person or a token. Requests let in anonymously, a bypass
// without a token, are only counted. Entries that name no one are routine,
// so each client gets a budget of them.
func (s *Store) logAccess(r *http.Request, host, policy string, outcome int, reason, email, token string) {
	l := s.logs.access
	routine := email == "" && token == ""
	if !l.on() || outcome == accessPassed && routine {
		return
	}
	ip, _ := requestOrigin(r)
	now := s.now()
	if !l.admit(clientKey(ip), now, routine) {
		return
	}
	l.add(accessEntry{At: now, Host: host, Method: eventlog.Clip(r.Method, 16), Path: eventlog.Clip(r.URL.Path, 512), Client: ip, Policy: policy, Decision: decisions[outcome], Reason: reason, Email: email, Token: token, Routine: routine})
}

var accessFilters = []logFilter{{name: "decision", values: decisions[:]}, {name: "policy"}, {name: "who", groups: map[string]bool{"named": false, "anonymous": true}}}

func (s *Store) getAccessLog(w http.ResponseWriter, r *http.Request) {
	serveLog(s, w, r, control.LogAccess, s.logs.access, accessFilters...)
}

func (s *Store) putAccessLog(w http.ResponseWriter, r *http.Request) {
	saveLog(s, w, r, control.LogAccess, s.logs.access)
}
