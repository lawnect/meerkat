package outlet

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// requestEntry is a public request to a routed address, as the request log
// keeps it: every one, whatever became of it, so that what was asked for,
// and by whom, can be counted and looked into later.
type requestEntry struct {
	ID         int64     `json:"id"`
	At         time.Time `json:"at"`
	Host       string    `json:"host"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Client     string    `json:"client"`
	Source     string    `json:"source"` // the client as its budget counts it: an IPv4 address, or an IPv6 /56
	Status     int       `json:"status"`
	Class      string    `json:"class"` // the status's class, "4xx"
	Cause      string    `json:"cause"`
	Service    string    `json:"service"`
	DurationMS int64     `json:"duration_ms"`
	// The Outlet's time and the service's for a forwarded request (see
	// timing), 0 for one that wasn't.
	OutletUS   int64  `json:"outlet_us"`
	UpstreamUS int64  `json:"upstream_us"`
	Bytes      int64  `json:"bytes"`
	Agent      string `json:"agent"`
	// Routine entries are 4xx: anyone can make them at will.
	Routine bool `json:"routine"`
}

// Anyone can make a 2xx as easily as a 4xx, so the request log sheds the
// oldest entries, whichever they are: a flood of cheap requests can't push
// out a scanner's first. Clients are filtered and tallied by source, as
// their budgets count them, so one rotating through its IPv6 addresses is
// still one.
var requestEntries = eventlog.Kind[requestEntry]{
	Table:   "request_log",
	Version: 1,
	Columns: []string{"host TEXT", "method TEXT", "path TEXT", "client TEXT", "source TEXT", "status INTEGER", "class TEXT", "cause TEXT", "service TEXT", "duration_ms INTEGER", "outlet_us INTEGER", "upstream_us INTEGER", "bytes INTEGER", "agent TEXT"},
	Filters: []string{"class", "host", "path", "source"},
	Search:  []string{"host", "path", "client", "service", "agent"},
	Fields: func(e *requestEntry) []any {
		return []any{&e.Host, &e.Method, &e.Path, &e.Client, &e.Source, &e.Status, &e.Class, &e.Cause, &e.Service, &e.DurationMS, &e.OutletUS, &e.UpstreamUS, &e.Bytes, &e.Agent}
	},
	Stamp:       func(e *requestEntry) (*int64, *time.Time) { return &e.ID, &e.At },
	Routine:     func(e *requestEntry) *bool { return &e.Routine },
	OldestFirst: true,
}

var statusClasses = []string{"1xx", "2xx", "3xx", "4xx", "5xx"}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return ""
	}
	return statusClasses[status/100-1]
}

// requestRecord is what became of a request the request log will keep, as
// the handler learns it: the status and body the client got, why the
// Outlet refused it or the service failed, and the service tried last. Only
// the request's own goroutine touches it.
type requestRecord struct {
	status               int
	bytes                int64
	cause                string
	service              string
	outletUS, upstreamUS int64
}

type requestRecordKey struct{}

// recordOf returns the record a request carries, if the request log keeps
// it.
func recordOf(r *http.Request) *requestRecord {
	record, _ := r.Context().Value(requestRecordKey{}).(*requestRecord)
	return record
}

// recordWriter notes the status and body bytes of a response.
type recordWriter struct {
	http.ResponseWriter
	record *requestRecord
}

func (w *recordWriter) WriteHeader(code int) {
	// 1xx before the response, 103 Early Hints, aren't its status; 101 is.
	if w.record.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
		w.record.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *recordWriter) Write(p []byte) (int, error) {
	if w.record.status == 0 {
		w.record.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.record.bytes += int64(n)
	return n, err
}

// Hijack is how the proxy takes over an upgraded connection, after the app
// switched protocols; it writes that answer to the connection itself.
func (w *recordWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil && w.record.status == 0 {
		w.record.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}

func (w *recordWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// recordRequest has the request log keep r, a request to host, a routed
// address, when the log is on: it returns the writer and request the
// handler goes on with, and a func to defer, which logs the request once
// answered. With the log off it returns them as they are and nil.
func (s *Store) recordRequest(w http.ResponseWriter, r *http.Request, host string, began time.Time) (http.ResponseWriter, *http.Request, func()) {
	if !s.logs.requests.on() {
		return w, r, nil
	}
	record := &requestRecord{}
	return &recordWriter{ResponseWriter: w, record: record}, r.WithContext(context.WithValue(r.Context(), requestRecordKey{}, record)), func() { s.logRequest(r, host, began, record) }
}

// logRequest adds a request to the request log, when it is on. Each client
// has a budget of entries; 4xx are routine, so a scanner's are fewer and go
// first when the file is full. It never waits and never fails the request.
func (s *Store) logRequest(r *http.Request, host string, began time.Time, record *requestRecord) {
	l := s.logs.requests
	if !l.on() {
		return
	}
	defer keepResponse()
	ip, _ := requestOrigin(r)
	source := clientKey(ip)
	now := s.now()
	// net/http answers 200 for a handler that wrote nothing.
	status := record.status
	if status == 0 {
		status = http.StatusOK
	}
	routine := status >= 400 && status < 500
	if !l.admit(source, now, routine) {
		return
	}
	var took time.Duration
	if !began.IsZero() {
		took = time.Since(began)
	}
	l.add(requestEntry{At: now, Host: host, Method: eventlog.Clip(r.Method, 16), Path: eventlog.Clip(r.URL.Path, 512), Client: ip, Source: source, Status: status, Class: statusClass(status), Cause: record.cause,
		Service: record.service, DurationMS: took.Milliseconds(), OutletUS: record.outletUS, UpstreamUS: record.upstreamUS, Bytes: record.bytes, Agent: eventlog.Clip(r.UserAgent(), 256), Routine: routine})
}

func (s *Store) getRequestLog(w http.ResponseWriter, r *http.Request) {
	serveLog(s, w, r, control.LogRequests, s.logs.requests, requestFilters...)
}

func (s *Store) putRequestLog(w http.ResponseWriter, r *http.Request) {
	saveLog(s, w, r, control.LogRequests, s.logs.requests)
}

// requestFilters filter the request log: a path or source, exact, comes
// from a row of its tally.
var requestFilters = []logFilter{{name: "class", values: statusClasses}, {name: "host"}, {name: "path", size: 512}, {name: "source"}}

// requestTally is what the request log is tallied by: by endpoint, a host
// and path, how many clients (sources) asked for it; by client, its source,
// for how many endpoints.
var requestTally = map[string]struct {
	by       []string
	distinct string
}{
	"endpoint": {[]string{"host", "path"}, "COUNT(DISTINCT source)"},
	"client":   {[]string{"source"}, "COUNT(DISTINCT host || ' ' || path)"},
}

// requestMeasures are a group's requests, 4xx, 5xx and distinct count, in
// the order of tallySorts.
var requestMeasures = []string{"COUNT(*)", "SUM(status BETWEEN 400 AND 499)", "SUM(status >= 500)"}

// tallySorts are the sort keys a tally takes, each a measure, and last_at
// the latest request.
var tallySorts = map[string]int{"requests": 0, "client_errors": 1, "server_errors": 2, "distinct": 3, "last_at": -1}

var tallyPeriods = map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "all": 0}

// requestTopRow is a group of the request log's tally.
type requestTopRow struct {
	Host         string    `json:"host,omitempty"`
	Path         string    `json:"path,omitempty"`
	Client       string    `json:"client,omitempty"`
	Requests     int64     `json:"requests"`
	ClientErrors int64     `json:"client_errors"`
	ServerErrors int64     `json:"server_errors"`
	Distinct     int64     `json:"distinct"`
	LastAt       time.Time `json:"last_at"`
}

// requestTopView is a page of the tally, with the log's settings: from is
// when the period began, null for all of the log.
type requestTopView struct {
	Settings  control.LogSettings `json:"settings"`
	Available bool                `json:"available"`
	From      *time.Time          `json:"from"`
	Total     int                 `json:"total"`
	Rows      []requestTopRow     `json:"rows"`
}

// getRequestTop answers GET for the request log's tally: the endpoints, or
// clients, with the most requests in a period, or sorted by another
// measure; with format=csv, every group as a file.
func (s *Store) getRequestTop(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) || !ownPage(w, r) {
		return
	}
	// A tally of a big file can take longer than the management port lets
	// a response wait.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(csvWriteTimeout))
	query := r.URL.Query()
	by, period, sort := query.Get("by"), query.Get("period"), query.Get("sort")
	if by == "" {
		by = "endpoint"
	}
	if period == "" {
		period = "24h"
	}
	if sort == "" {
		sort = "requests.desc"
	}
	key, direction, _ := strings.Cut(sort, ".")
	order, sorted := tallySorts[key]
	tally, grouped := requestTally[by]
	window, known := tallyPeriods[period]
	q, filtered := logQuery(query, []logFilter{{name: "host"}})
	offset, limit, paged := pageOf(query, eventlog.MaxPage)
	csv := query.Get("format") == "csv"
	zone, zoned := csvZone(query)
	if !sorted || direction != "asc" && direction != "desc" || !grouped || !known || !filtered || !paged || !zoned || query.Get("format") != "" && !csv {
		fail(w, r, 400, "invalid_log_filter")
		return
	}
	settings, err := s.control.LogSettings(r.Context(), control.LogRequests)
	if err != nil {
		fail(w, r, 503, "log_failed")
		return
	}
	view := requestTopView{Settings: settings, Rows: []requestTopRow{}}
	if window > 0 {
		from := s.now().Add(-window)
		view.From, q.Since = &from, from
	}
	l := s.logs.requests
	if l == nil {
		if csv {
			fail(w, r, 409, "log_unavailable")
			return
		}
		reply(w, 200, view)
		return
	}
	l.writer.Flush()
	q.Offset, q.Limit = offset, limit
	if csv {
		// A download reads the log in one of maxExports slots.
		release, slotted := s.exportSlot(r)
		if !slotted {
			return
		}
		defer release()
		q.Offset, q.Limit = 0, eventlog.MaxTally
	}
	groups, total, err := l.log.Tally(r.Context(), eventlog.Tally{Query: q, By: tally.by, Measures: append(append([]string{}, requestMeasures...), tally.distinct), Sort: order, Ascending: direction == "asc"})
	if err != nil {
		fail(w, r, 503, "log_failed")
		return
	}
	for _, group := range groups {
		row := requestTopRow{Requests: group.Measures[0], ClientErrors: group.Measures[1], ServerErrors: group.Measures[2], Distinct: group.Measures[3], LastAt: group.Last}
		if by == "endpoint" {
			row.Host, row.Path = group.Keys[0], group.Keys[1]
		} else {
			row.Client = group.Keys[0]
		}
		view.Rows = append(view.Rows, row)
	}
	if !csv {
		view.Available, view.Total = true, total
		reply(w, 200, view)
		return
	}
	header := []string{"host", "path", "requests", "client_errors", "server_errors", "clients", lastAtHeader(zone)}
	if by == "client" {
		header = []string{"client", "requests", "client_errors", "server_errors", "endpoints", lastAtHeader(zone)}
	}
	out := newCSV(w, "request-"+by+"s", header)
	for _, row := range view.Rows {
		counts := []string{strconv.FormatInt(row.Requests, 10), strconv.FormatInt(row.ClientErrors, 10), strconv.FormatInt(row.ServerErrors, 10), strconv.FormatInt(row.Distinct, 10), stamp(row.LastAt, zone)}
		keys := []string{cell(row.Host), cell(row.Path)}
		if by == "client" {
			keys = []string{cell(row.Client)}
		}
		if err := out.row(append(keys, counts...)); err != nil {
			out.finish(r, err, "")
			return
		}
	}
	out.finish(r, nil, "")
}

func lastAtHeader(zone *time.Location) string { return fmt.Sprintf("last_at (%s)", zoneName(zone)) }
