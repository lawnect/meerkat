package outlet

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// failureEntry is a public request that failed, or that the Outlet refused
// before it reached an app, as its log keeps it.
type failureEntry struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	Host       string    `json:"host"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Client     string    `json:"client"`
	Cause      string    `json:"cause"`
	Status     int       `json:"status"`
	Service    string    `json:"service"`
	DurationMS int64     `json:"duration_ms"`
	// Routine entries are refusals: anyone can make them.
	Routine bool `json:"routine"`
}

var failureEntries = eventlog.Kind[failureEntry]{
	Table:   "failure_log",
	Columns: []string{"host TEXT", "method TEXT", "path TEXT", "client TEXT", "cause TEXT", "status INTEGER", "service TEXT", "duration_ms INTEGER"},
	Filters: []string{"cause", "host"},
	Search:  []string{"host", "path", "client", "service"},
	Fields: func(e *failureEntry) []any {
		return []any{&e.Host, &e.Method, &e.Path, &e.Client, &e.Cause, &e.Status, &e.Service, &e.DurationMS}
	},
	Stamp:   func(e *failureEntry) (*int64, *time.Time) { return &e.ID, &e.At },
	Routine: func(e *failureEntry) *bool { return &e.Routine },
}

// What went wrong, as the failure log and the console read it.
const (
	failAppError    = "app_error"    // the app answered with a 5xx
	failUnreachable = "unreachable"  // no service could be reached through its tunnel
	failTimeout     = "timeout"      // a service didn't answer in time
	failNoService   = "no_service"   // the address has no live service
	failRateLimited = "rate_limited" // a rate limit refused it
	failBlocked     = "blocked"      // a block rule refused it
	failTooLong     = "too_long"     // the path was over maxPublicPath
	failTooLarge    = "too_large"    // the body was over the upload limit
	failBusy        = "busy"         // a connection limit refused it
	failTrapped     = "trapped"      // a block rule that bans refused it, and banned the client
	failBanned      = "banned"       // the client was banned earlier
)

var failureCauses = []string{failAppError, failUnreachable, failTimeout, failNoService, failRateLimited, failBlocked, failTooLong, failTooLarge, failBusy, failTrapped, failBanned}

// refused says whether the Outlet turned the request away, which anyone can
// make it do at will, rather than something being wrong with a service.
func refused(cause string) bool {
	switch cause {
	case failRateLimited, failBlocked, failTooLong, failTooLarge, failBusy, failTrapped, failBanned:
		return true
	}
	return false
}

// logFailure records a public request that failed or was refused, when the
// failure log is on: began is when the request came, status what the client got,
// service the one that answered or was tried last, if any. Refusals are
// routine, so each client gets a budget of them. The cause goes in the
// request log's entry too. It never waits and never fails the request.
func (s *Store) logFailure(r *http.Request, host string, began time.Time, cause string, status int, service string) {
	l := s.logs.failures
	if !l.on() && !s.logs.requests.on() {
		return
	}
	defer keepResponse()
	if record := recordOf(r); record != nil {
		record.cause = cause
	}
	if !l.on() {
		return
	}
	ip, _ := requestOrigin(r)
	now := s.now()
	routine := refused(cause)
	if !l.admit(clientKey(ip), now, routine) {
		return
	}
	var took time.Duration
	if !began.IsZero() {
		took = time.Since(began)
	}
	l.add(failureEntry{At: now, Host: host, Method: eventlog.Clip(r.Method, 16), Path: eventlog.Clip(r.URL.Path, 512), Client: ip, Cause: cause, Status: status, Service: service, DurationMS: took.Milliseconds(), Routine: routine})
}

// logTurnedAway records a refusal made before the address is looked up, the
// rate limit and the per-client connection limit, in the failure and request
// logs that are on, when host is routed: a request for any other host is
// not logged. A route lookup reads the cached routing, and happens only for
// a request being refused.
func (s *Store) logTurnedAway(r *http.Request, host string, began time.Time, cause string, status int) {
	if !s.logs.failures.on() && !s.logs.requests.on() {
		return
	}
	defer keepResponse()
	routing, err := s.control.Routing(r.Context())
	if err != nil {
		return
	}
	if plans, _ := routing.Routes(host); len(plans) != 0 {
		s.logFailure(r, host, time.Time{}, cause, status, "")
		s.logRequest(r, host, began, &requestRecord{status: status, cause: cause})
	}
}

// keepResponse is deferred where a request logs, so a panic there is
// reported and never reaches the response.
func keepResponse() {
	if p := recover(); p != nil {
		slog.Error("a log skipped a request", "panic", p)
	}
}

var failureFilters = []logFilter{{name: "cause", values: failureCauses, groups: map[string]bool{"service": false, "refused": true}}, {name: "host"}}

func (s *Store) getFailureLog(w http.ResponseWriter, r *http.Request) {
	serveLog(s, w, r, control.LogFailures, s.logs.failures, failureFilters...)
}

func (s *Store) putFailureLog(w http.ResponseWriter, r *http.Request) {
	saveLog(s, w, r, control.LogFailures, s.logs.failures)
}
