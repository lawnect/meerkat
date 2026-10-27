package outlet

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// A log downloads as CSV, every entry its filters match, newest first. The
// file is written as it is read, a chunk at a time, so neither the Outlet's
// memory nor the log's one connection is held by a slow download.

// csvChunk is how many rows go out between flushes; each flush gives the
// download another csvWriteTimeout to be taken.
const (
	csvChunk        = 1000
	csvWriteTimeout = time.Minute
)

// maxExports is how many downloads read the logs at once; more wait.
const maxExports = 2

// ownPage reports whether a read that costs the Outlet, a search, a tally
// or a download, comes from the console's own page. The session cookie is
// SameSite=Strict, but every public address is same-site with the
// management host, so an image on a tunneled app's page would carry it;
// browsers name such a request cross-site or same-site in Sec-Fetch-Site,
// and the console's own reads same-origin. Clients without the header,
// scripts, are let through as before.
func ownPage(w http.ResponseWriter, r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	fail(w, r, http.StatusForbidden, "cross_origin_request")
	return false
}

// exportSlot waits for one of maxExports downloads, or for the client to
// leave; release gives it back.
func (s *Store) exportSlot(r *http.Request) (release func(), ok bool) {
	select {
	case s.exports <- struct{}{}:
		return func() { <-s.exports }, true
	case <-r.Context().Done():
		return nil, false
	}
}

// csvZone reads utc_offset, the minutes east of UTC the browser is in,
// for the times in a file: spreadsheets don't read zones, so times are
// written in the reader's, and the column's header names it.
func csvZone(query url.Values) (*time.Location, bool) {
	raw := query.Get("utc_offset")
	if raw == "" {
		return time.UTC, true
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes < -14*60 || minutes > 14*60 {
		return nil, false
	}
	if minutes == 0 {
		return time.UTC, true
	}
	return time.FixedZone(zoneLabel(minutes), minutes*60), true
}

func zoneLabel(minutes int) string {
	sign := "+"
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, minutes/60, minutes%60)
}

func zoneName(zone *time.Location) string {
	if zone == time.UTC {
		return "UTC"
	}
	return zone.String()
}

func stamp(t time.Time, zone *time.Location) string {
	return t.In(zone).Format("2006-01-02 15:04:05.000")
}

// cell keeps a text a spreadsheet would take for a formula as text. A
// path, an agent or a search is whatever a client sent, and a file opened
// in a spreadsheet must not run it.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r\n", rune(s[0])) {
		return "'" + s
	}
	return s
}

// csvOut is a CSV download. It starts with its first row, or when it ends
// with none, so a read that fails at once can still answer with an error.
type csvOut struct {
	w      http.ResponseWriter
	what   string
	header []string
	csv    *csv.Writer
	rc     *http.ResponseController
	rows   int
}

// newCSV is a download named after what with header as its first row.
func newCSV(w http.ResponseWriter, what string, header []string) *csvOut {
	return &csvOut{w: w, what: what, header: header}
}

// start sends the headers, a byte order mark, as spreadsheets take UTF-8
// for what it is after one, and the header row.
func (o *csvOut) start() {
	if o.csv != nil {
		return
	}
	o.w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	o.w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="meerkat-%s-%s.csv"`, o.what, time.Now().UTC().Format("20060102-150405")))
	o.csv, o.rc = csv.NewWriter(o.w), http.NewResponseController(o.w)
	_ = o.rc.SetWriteDeadline(time.Now().Add(csvWriteTimeout))
	_, _ = o.w.Write([]byte("\xEF\xBB\xBF"))
	_ = o.csv.Write(o.header)
}

// row writes a row; an error means the download is gone.
func (o *csvOut) row(fields []string) error {
	o.start()
	if err := o.csv.Write(fields); err != nil {
		return err
	}
	if o.rows++; o.rows%csvChunk == 0 {
		o.csv.Flush()
		if err := o.csv.Error(); err != nil {
			return err
		}
		_ = o.rc.SetWriteDeadline(time.Now().Add(csvWriteTimeout))
	}
	return nil
}

// finish ends the download once err, the reads' and writes' error, is
// known. Without one the file is whole, the header alone if nothing
// matched. A read that failed before any row answers 503 with code. Once
// the file has begun, 200 is gone: the connection is cut instead, and the
// browser reports a failed download rather than keep a short file as whole.
func (o *csvOut) finish(r *http.Request, err error, code string) {
	switch {
	case err == nil:
		o.start()
		o.csv.Flush()
	case o.csv == nil:
		fail(o.w, r, 503, code)
	default:
		panic(http.ErrAbortHandler)
	}
}

// entryRow is an entry's id, time and columns as text.
func entryRow(id int64, at time.Time, fields []any, zone *time.Location) []string {
	row := make([]string, 0, len(fields)+2)
	row = append(row, strconv.FormatInt(id, 10), stamp(at, zone))
	for _, field := range fields {
		switch v := field.(type) {
		case *string:
			row = append(row, cell(*v))
		case *int:
			row = append(row, strconv.Itoa(*v))
		case *int64:
			row = append(row, strconv.FormatInt(*v, 10))
		case *bool:
			row = append(row, strconv.FormatBool(*v))
		default:
			row = append(row, cell(fmt.Sprint(reflect.Indirect(reflect.ValueOf(v)).Interface())))
		}
	}
	return row
}

// exportLog answers GET for a log's CSV: every entry filters match.
func exportLog[E any](s *Store, w http.ResponseWriter, r *http.Request, what string, l *outletLog[E], kind eventlog.Kind[E], filters ...logFilter) {
	if !s.alertsAdmin(w, r) || !ownPage(w, r) {
		return
	}
	q, filtered := logQuery(r.URL.Query(), filters)
	zone, zoned := csvZone(r.URL.Query())
	if !filtered || !zoned {
		fail(w, r, 400, "invalid_log_filter")
		return
	}
	if l == nil {
		fail(w, r, 409, "log_unavailable")
		return
	}
	release, ok := s.exportSlot(r)
	if !ok {
		return
	}
	defer release()
	l.writer.Flush()
	out := newCSV(w, what, append([]string{"id", fmt.Sprintf("at (%s)", zoneName(zone))}, kind.Names()...))
	out.finish(r, l.log.Each(r.Context(), q, func(e *E) error {
		id, at := kind.Stamp(e)
		return out.row(entryRow(*id, *at, kind.Fields(e), zone))
	}), "log_failed")
}

func (s *Store) exportAccessLog(w http.ResponseWriter, r *http.Request) {
	exportLog(s, w, r, "access-log", s.logs.access, accessEntries, accessFilters...)
}

func (s *Store) exportFailureLog(w http.ResponseWriter, r *http.Request) {
	exportLog(s, w, r, "failure-log", s.logs.failures, failureEntries, failureFilters...)
}

func (s *Store) exportRequestLog(w http.ResponseWriter, r *http.Request) {
	exportLog(s, w, r, "request-log", s.logs.requests, requestEntries, requestFilters...)
}

// exportAudit answers GET for the audit log's CSV: every event the filters
// match, as the console lists them, up to the newest when it began.
func (s *Store) exportAudit(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) || !ownPage(w, r) {
		return
	}
	query := r.URL.Query()
	filter := control.AuditFilter{Action: query.Get("action"), Query: query.Get("q")}
	zone, zoned := csvZone(query)
	if len(filter.Action) > 64 || len(filter.Query) > 128 || !zoned {
		fail(w, r, 400, "invalid_audit_query")
		return
	}
	release, ok := s.exportSlot(r)
	if !ok {
		return
	}
	defer release()
	out := newCSV(w, "audit-log", []string{"id", fmt.Sprintf("at (%s)", zoneName(zone)), "action", "actor_email", "resource_type", "resource_id", "source", "ip", "user_agent", "detail"})
	text := func(s *string) string {
		if s == nil {
			return ""
		}
		return cell(*s)
	}
	out.finish(r, s.control.EachAudit(r.Context(), filter, func(e control.AuditEvent) error {
		at := e.CreatedAt
		if t, err := time.Parse(time.RFC3339Nano, e.CreatedAt); err == nil {
			at = stamp(t, zone)
		}
		return out.row([]string{cell(e.ID), at, cell(e.Action), text(e.ActorEmail), cell(e.ResourceType), cell(e.ResourceID), cell(e.Source), text(e.IP), text(e.UserAgent), cell(string(e.Detail))})
	}), "persistence_failed")
}
