package outlet

import (
	"context"
	"errors"
	"hash/maphash"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// logBudget is how many routine entries, and how many notable ones, one
// client may add to a log in a minute.
type logBudget struct{ routine, notable uint64 }

var (
	// refusalBudget is the access and failure logs'. Routine entries are
	// enough for a person's mistakes, far from a flood. Anyone can make
	// some notable ones too, a 5xx from an app or a request to a service
	// that is down, so they have a budget, one a busy person stays under.
	refusalBudget = logBudget{routine: 60, notable: 600}
	// requestBudget is the request log's, which keeps every request: a
	// scanner's 4xx up to ten a second, and what a busy office behind one
	// address asks for.
	requestBudget = logBudget{routine: 600, notable: 6000}
)

// budgetSlots is how many clients' budgets a log tells apart. More clients
// share slots, which only makes them stricter.
const budgetSlots = 1024

// outletLog is one of the Outlet's logs: its file, the writer that fills it
// behind the requests, and its switch. A nil *outletLog, a file that
// couldn't be opened, logs nothing.
//
// Anyone can make routine entries at will, so each client has a budget of
// them, and the rest are only counted. Without it a flood would fill the
// queue, and then the file, and push out the entries that matter.
type outletLog[E any] struct {
	name    string // control.LogAccess, control.LogFailures, control.LogRequests
	log     *eventlog.Log[E]
	writer  *eventlog.Writer[E]
	enabled atomic.Bool
	limits  logBudget

	// budget holds, per slot, the minute (high half) and how many routine
	// entries were added in it (low half); a slot is picked by a hash with
	// a seed of this process, so a client can't aim at another's slot.
	budget     [budgetSlots]atomic.Uint64
	notable    [budgetSlots]atomic.Uint64 // the same for notable entries
	seed       maphash.Seed
	suppressed atomic.Int64 // entries given up for a budget
}

// openOutletLog opens a log's file in dir and applies its saved settings;
// the caller starts its writer. It returns nil, with a warning, when the
// file can't be opened.
func openOutletLog[E any](ctx context.Context, db *control.Database, dir, file, name string, kind eventlog.Kind[E], limits logBudget) *outletLog[E] {
	settings, err := db.LogSettings(ctx, name)
	if err != nil {
		slog.Warn("log settings unreadable, the log stays off", "log", name, "error", err)
		settings = control.DefaultLogSettings(name)
	}
	log, err := eventlog.Open(ctx, filepath.Join(dir, file), kind, logLimits(settings))
	if err != nil {
		slog.Warn("log unavailable", "log", name, "error", err)
		return nil
	}
	l := &outletLog[E]{name: name, log: log, writer: eventlog.NewWriter[E](log, name, 8192), limits: limits, seed: maphash.MakeSeed()}
	l.enabled.Store(settings.Enabled)
	return l
}

func logLimits(settings control.LogSettings) eventlog.Limits {
	return eventlog.Limits{MaxBytes: int64(settings.MaxMiB) << 20, Retention: time.Duration(settings.RetentionDays) * 24 * time.Hour}
}

// add hands an entry to the writer when the log is on; it never waits.
func (l *outletLog[E]) add(entry E) {
	if l != nil && l.enabled.Load() {
		l.writer.Add(entry)
	}
}

// slot returns where client, a key from clientKey, keeps its budgets.
func (l *outletLog[E]) slot(client string) uint64 {
	return maphash.String(l.seed, client) % budgetSlots
}

// admit takes one of client's entries for the minute of now, from the
// routine budget or the notable one, or counts the entry as suppressed when
// that budget is spent.
func (l *outletLog[E]) admit(client string, now time.Time, routine bool) bool {
	if routine {
		return l.allow(client, now)
	}
	return l.take(&l.notable, l.limits.notable, client, now)
}

// allow takes one of client's routine entries for the minute of now.
func (l *outletLog[E]) allow(client string, now time.Time) bool {
	return l.take(&l.budget, l.limits.routine, client, now)
}

// take spends one of the limit entries client may add in the minute of now.
// It takes no lock and never allocates; a minute that passed starts the
// budget over.
func (l *outletLog[E]) take(slots *[budgetSlots]atomic.Uint64, limit uint64, client string, now time.Time) bool {
	slot, minute := &slots[l.slot(client)], uint64(now.Unix()/60)<<32
	for {
		packed := slot.Load()
		used := uint64(0)
		if packed&^math.MaxUint32 == minute {
			used = packed & math.MaxUint32
		}
		if used >= limit {
			l.suppressed.Add(1)
			return false
		}
		if slot.CompareAndSwap(packed, minute|(used+1)) {
			return true
		}
	}
}

// on reports whether the log keeps entries, so a caller can skip building
// one.
func (l *outletLog[E]) on() bool { return l != nil && l.enabled.Load() }

// apply takes saved settings: the switch and limits at once, the file
// shrunk to a lowered cap at the writer's next turn.
func (l *outletLog[E]) apply(settings control.LogSettings) {
	l.enabled.Store(settings.Enabled)
	l.log.SetLimits(logLimits(settings))
	l.writer.PruneSoon()
}

func (l *outletLog[E]) close() {
	if l != nil {
		_ = l.log.Close()
	}
}

// logView is a page of a log, newest first, with its settings: available
// says whether its file could be opened, size_bytes how much it takes,
// dropped how many entries were given up since the Outlet started because
// the queue was full, suppressed how many because a client was over its
// budget, and total how many match the filters.
type logView[E any] struct {
	Settings   control.LogSettings `json:"settings"`
	Available  bool                `json:"available"`
	SizeBytes  int64               `json:"size_bytes"`
	Dropped    int64               `json:"dropped"`
	Suppressed int64               `json:"suppressed"`
	Total      int                 `json:"total"`
	Entries    []E                 `json:"entries"`
}

// maxLogText is the longest text a log is searched for.
const maxLogText = 128

// logFilter is a query parameter that filters a log, by the column of the
// same name, and the values it takes; none means any. A group is a value
// that stands for all the routine entries (true) or all the notable ones
// (false) and is no column's value; a filter with groups takes nothing else.
// A value takes at most size bytes, 255 when zero.
type logFilter struct {
	name   string
	values []string
	groups map[string]bool
	size   int
}

// pageOf reads the page and per_page parameters, at most most a page.
func pageOf(query url.Values, most int) (offset, limit int, ok bool) {
	page, perPage := 1, 50
	for _, p := range []struct {
		name     string
		into     *int
		min, max int
	}{{"page", &page, 1, 1 << 20}, {"per_page", &perPage, 1, most}} {
		if value := query.Get(p.name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < p.min || n > p.max {
				return 0, 0, false
			}
			*p.into = n
		}
	}
	return (page - 1) * perPage, perPage, true
}

// logQuery reads a log's search text and filters from query.
func logQuery(query url.Values, filters []logFilter) (eventlog.Query, bool) {
	q := eventlog.Query{Where: map[string]string{}, Text: query.Get("q")}
	if len(q.Text) > maxLogText {
		return q, false
	}
	for _, f := range filters {
		value := query.Get(f.name)
		routine, group := f.groups[value]
		size := f.size
		if size == 0 {
			size = 255
		}
		if value != "" && (len(value) > size || !group && (f.values != nil || f.groups != nil) && !slices.Contains(f.values, value)) {
			return q, false
		}
		if group {
			q.Routine = &routine
		} else {
			q.Where[f.name] = value
		}
	}
	return q, true
}

// serveLog answers GET for a log: a page of entries matching filters.
func serveLog[E any](s *Store, w http.ResponseWriter, r *http.Request, name string, l *outletLog[E], filters ...logFilter) {
	if !s.alertsAdmin(w, r) || !ownPage(w, r) {
		return
	}
	// A search of a big file can take longer than the management port lets
	// a response wait.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(csvWriteTimeout))
	q, ok := logQuery(r.URL.Query(), filters)
	offset, limit, paged := pageOf(r.URL.Query(), eventlog.MaxPage)
	if !ok || !paged {
		fail(w, r, 400, "invalid_log_filter")
		return
	}
	q.Offset, q.Limit = offset, limit
	view, err := logPageOf(r.Context(), s, name, l, q)
	if err != nil {
		fail(w, r, 503, "log_failed")
		return
	}
	reply(w, 200, view)
}

// logPageOf reads a page of a log, what was waiting written first so the
// console sees requests just made.
func logPageOf[E any](ctx context.Context, s *Store, name string, l *outletLog[E], q eventlog.Query) (logView[E], error) {
	settings, err := s.control.LogSettings(ctx, name)
	if err != nil {
		return logView[E]{}, err
	}
	view := logView[E]{Settings: settings, Entries: []E{}}
	if l == nil {
		return view, nil
	}
	l.writer.Flush()
	if view.Entries, view.Total, err = l.log.Query(ctx, q); err != nil {
		return view, err
	}
	view.SizeBytes, err = l.log.Size(ctx)
	view.Available, view.Dropped, view.Suppressed = true, l.writer.Dropped(), l.suppressed.Load()
	return view, err
}

// saveLog answers PUT for a log: saves and applies its settings.
func saveLog[E any](s *Store, w http.ResponseWriter, r *http.Request, name string, l *outletLog[E]) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var settings control.LogSettings
	if !decode(w, r, &settings, "enabled", "max_mib", "retention_days") {
		return
	}
	if settings.Enabled && l == nil {
		fail(w, r, 409, "log_unavailable")
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetLogSettings(r.Context(), name, settings, s.now())
	if err == nil && l != nil {
		l.apply(saved)
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidLogSettings) {
		fail(w, r, 400, "invalid_log_settings")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "observe.log_settings", "observe", name, map[string]any{"log": name, "enabled": saved.Enabled, "max_mib": saved.MaxMiB, "retention_days": saved.RetentionDays})
	view, err := logPageOf(r.Context(), s, name, l, eventlog.Query{Limit: 50})
	if err != nil {
		fail(w, r, 503, "log_failed")
		return
	}
	reply(w, 200, view)
}
