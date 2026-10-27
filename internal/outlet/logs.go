package outlet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/eventlog"
)

// outletLog is one of the Outlet's logs: its file, the writer that fills it
// behind the requests, and its switch. A nil *outletLog, a file that
// couldn't be opened, logs nothing.
type outletLog[E any] struct {
	name    string // control.LogAccess, control.LogFailures
	log     *eventlog.Log[E]
	writer  *eventlog.Writer[E]
	enabled atomic.Bool
}

// openOutletLog opens a log's file in dir and applies its saved settings;
// the caller starts its writer. It returns nil, with a warning, when the
// file can't be opened.
func openOutletLog[E any](ctx context.Context, db *control.Database, dir, file, name string, kind eventlog.Kind[E]) *outletLog[E] {
	settings, err := db.LogSettings(ctx, name)
	if err != nil {
		slog.Warn("log settings unreadable, the log stays off", "log", name, "error", err)
		settings = control.DefaultLogSettings()
	}
	log, err := eventlog.Open(ctx, filepath.Join(dir, file), kind, logLimits(settings))
	if err != nil {
		slog.Warn("log unavailable", "log", name, "error", err)
		return nil
	}
	l := &outletLog[E]{name: name, log: log, writer: eventlog.NewWriter[E](log, name, 8192)}
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
// dropped how many entries were given up since the Outlet started, and
// total how many match the filters.
type logView[E any] struct {
	Settings  control.LogSettings `json:"settings"`
	Available bool                `json:"available"`
	SizeBytes int64               `json:"size_bytes"`
	Dropped   int64               `json:"dropped"`
	Total     int                 `json:"total"`
	Entries   []E                 `json:"entries"`
}

// logFilter is a query parameter that filters a log, by the column of the
// same name, and the values it takes; none means any.
type logFilter struct {
	name   string
	values []string
}

// serveLog answers GET for a log: a page of entries matching filters.
func serveLog[E any](s *Store, w http.ResponseWriter, r *http.Request, name string, l *outletLog[E], filters ...logFilter) {
	if !s.alertsAdmin(w, r) {
		return
	}
	query := r.URL.Query()
	page, perPage := 1, 50
	for _, p := range []struct {
		name     string
		into     *int
		min, max int
	}{{"page", &page, 1, 1 << 20}, {"per_page", &perPage, 1, eventlog.MaxPage}} {
		if value := query.Get(p.name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < p.min || n > p.max {
				fail(w, r, 400, "invalid_log_filter")
				return
			}
			*p.into = n
		}
	}
	where := map[string]string{}
	for _, f := range filters {
		value := query.Get(f.name)
		if value != "" && (len(value) > 255 || f.values != nil && !slices.Contains(f.values, value)) {
			fail(w, r, 400, "invalid_log_filter")
			return
		}
		where[f.name] = value
	}
	view, err := logPageOf(r.Context(), s, name, l, eventlog.Query{Where: where, Offset: (page - 1) * perPage, Limit: perPage})
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
	view.Available, view.Dropped = true, l.writer.Dropped()
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
