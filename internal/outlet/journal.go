package outlet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/securefs"
)

// auditWriter stores audit entries; *control.Database implements it.
type auditWriter interface {
	AppendAudits(ctx context.Context, entries []control.AuditEntry) error
}

// auditJournal writes audit entries behind the request: add never waits and
// never fails, so a slow or broken database cannot hold up or fail the action
// being recorded.
//
// Entries travel through three buffers. The queue takes them from requests;
// run moves them to a pending list in memory and writes that in batches. While
// the database refuses writes, pending grows until memoryCap, then spills to
// files in dir. When pending is empty and the queue is quiet, run writes the
// oldest file back and deletes it. Only when the queue is full or the spool
// holds maxFiles files is an entry dropped, and then it is counted.
//
// Everything below the limits is owned by the run goroutine.
type auditJournal struct {
	db      auditWriter
	dir     string
	queue   chan control.AuditEntry
	flushes chan chan struct{}
	dropped atomic.Int64 // entries lost; only ever grows

	// Limits and intervals. newAuditJournal sets them and tests shrink them
	// before starting run.
	batchSize    int           // entries per AppendAudits call
	memoryCap    int           // pending entries kept in memory before they spill
	fileEntries  int           // entries per spool file
	fileBytes    int           // encoded size of a spool file; securefs reads at most 1 MiB
	maxFiles     int           // spool files kept; a spill beyond this is dropped
	writeTimeout time.Duration // one AppendAudits call
	idleTick     time.Duration // spacing of spool replays, one file per tick
	retryMin     time.Duration // wait after the first failed write; doubles up to retryMax
	retryMax     time.Duration
	warnEvery    time.Duration // at most one warning about drops per interval

	pending  []control.AuditEntry
	backoff  time.Duration
	retryAt  time.Time // no write is attempted before this
	spoolSeq int
	reported int64 // dropped count already warned about
	warnedAt time.Time
}

func newAuditJournal(db auditWriter, dir string) *auditJournal {
	return &auditJournal{
		db:           db,
		dir:          dir,
		queue:        make(chan control.AuditEntry, 4096),
		flushes:      make(chan chan struct{}),
		batchSize:    256,
		memoryCap:    10000,
		fileEntries:  500,
		fileBytes:    512 << 10,
		maxFiles:     200,
		writeTimeout: 5 * time.Second,
		idleTick:     time.Second,
		retryMin:     time.Second,
		retryMax:     30 * time.Second,
		warnEvery:    time.Minute,
	}
}

// add queues e and returns at once. Past every buffer the entry is dropped
// and counted; the request it describes goes ahead regardless.
func (j *auditJournal) add(e control.AuditEntry) {
	if !e.Valid() {
		slog.Error("audit entry rejected", "action", e.Action)
		return
	}
	if e.ID == "" {
		id, err := control.NewAuditID()
		if err != nil {
			j.dropped.Add(1)
			return
		}
		e.ID = id
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	select {
	case j.queue <- e:
	default:
		j.dropped.Add(1)
	}
}

// run is the journal's only writer: call it once, in its own goroutine. After
// ctx ends it writes what is left, spills what the database will not take, and
// returns; entries added after that are lost, so stop callers first.
func (j *auditJournal) run(ctx context.Context) {
	for {
		j.warnDrops()
		select {
		case <-ctx.Done():
			if !j.writeAll() {
				j.spill()
			}
			return
		case e := <-j.queue:
			j.pending = append(j.pending, e)
			j.take()
			j.settle()
		case done := <-j.flushes:
			if j.writeAll() {
				for j.replay() {
				}
			}
			close(done)
		case <-time.After(j.wait()):
			j.tick()
		}
	}
}

// flush returns once every entry added before the call has been written, and
// then the spool has been replayed. If the database refuses a write it returns
// at once, leaving the rest pending or in the spool. It is for tests and
// tooling; requests never call it, and run must be running or it blocks.
func (j *auditJournal) flush() {
	done := make(chan struct{})
	j.flushes <- done
	<-done
}

// wait is how long run sleeps with nothing to do: until the next retry if
// entries are waiting for the database, otherwise until the next replay.
func (j *auditJournal) wait() time.Duration {
	if len(j.pending) > 0 {
		return time.Until(j.retryAt)
	}
	return j.idleTick
}

func (j *auditJournal) tick() {
	switch {
	case len(j.pending) > 0:
		j.settle()
	case len(j.queue) == 0 && j.ready():
		j.replay()
	}
}

// take moves everything queued so far to pending.
func (j *auditJournal) take() {
	for n := len(j.queue); n > 0; n-- {
		j.pending = append(j.pending, <-j.queue)
	}
}

// settle writes pending in batches until the database refuses or it is empty,
// and spills it if the database is not keeping up with memory.
func (j *auditJournal) settle() {
	for len(j.pending) > 0 && j.ready() && j.writeNext() {
	}
	if len(j.pending) > j.memoryCap {
		j.spill()
	}
}

// writeAll ignores any backoff and writes everything added so far, stopping
// at the first failure.
func (j *auditJournal) writeAll() bool {
	j.take()
	j.retryAt = time.Time{}
	for len(j.pending) > 0 {
		if !j.writeNext() {
			return false
		}
	}
	return true
}

func (j *auditJournal) ready() bool { return !time.Now().Before(j.retryAt) }

func (j *auditJournal) writeNext() bool {
	n := min(len(j.pending), j.batchSize)
	if !j.write(j.pending[:n]) {
		return false
	}
	if j.pending = j.pending[n:]; len(j.pending) == 0 {
		j.pending = nil
	}
	return true
}

// write makes one AppendAudits call. A failure starts or lengthens the backoff
// that gates every later write. AppendAudits skips stored IDs, so a batch that
// did commit but reported an error is harmless to send again.
func (j *auditJournal) write(batch []control.AuditEntry) bool {
	ctx, cancel := context.WithTimeout(context.Background(), j.writeTimeout)
	defer cancel()
	if err := j.db.AppendAudits(ctx, batch); err != nil {
		j.backoff = min(max(2*j.backoff, j.retryMin), j.retryMax)
		j.retryAt = time.Now().Add(j.backoff)
		slog.Error("audit write failed", "entries", len(batch), "retry_in", j.backoff, "error", err)
		return false
	}
	j.backoff, j.retryAt = 0, time.Time{}
	return true
}

// spill moves pending into spool files, oldest first, and frees the memory.
// Whatever the spool has no room for is dropped.
func (j *auditJournal) spill() {
	entries := j.pending
	j.pending = nil
	room := 0
	if j.dir != "" {
		room = j.maxFiles - len(j.spoolFiles())
	}
	// File names sort chronologically: one timestamp per spill, then a
	// counter that never repeats within the process.
	stamp := time.Now().UnixNano()
	failed := false
	for i, chunk := range j.chunks(entries) {
		if i >= room || failed {
			j.dropped.Add(int64(len(chunk)))
			continue
		}
		path := filepath.Join(j.dir, fmt.Sprintf("%020d-%06d.json", stamp, j.spoolSeq))
		j.spoolSeq++
		data, err := json.Marshal(chunk)
		if err == nil {
			err = securefs.Write(path, data)
		}
		if err != nil {
			slog.Error("audit spool write failed", "entries", len(chunk), "error", err)
			failed = true
			j.dropped.Add(int64(len(chunk)))
		}
	}
}

// chunks encodes entries into groups that each fit one spool file. An entry
// that cannot be encoded, or is too big for any file, is dropped.
func (j *auditJournal) chunks(entries []control.AuditEntry) [][]json.RawMessage {
	var all [][]json.RawMessage
	var chunk []json.RawMessage
	size := 2 // the brackets
	for _, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil || len(raw)+3 > j.fileBytes {
			j.dropped.Add(1)
			continue
		}
		if len(chunk) == j.fileEntries || size+len(raw)+1 > j.fileBytes {
			all, chunk, size = append(all, chunk), nil, 2
		}
		chunk, size = append(chunk, raw), size+len(raw)+1
	}
	if len(chunk) > 0 {
		all = append(all, chunk)
	}
	return all
}

// spoolFiles lists the spool file names, oldest first.
func (j *auditJournal) spoolFiles() []string {
	items, err := os.ReadDir(j.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Error("audit spool unreadable", "dir", j.dir, "error", err)
	}
	var names []string
	for _, item := range items {
		if !item.IsDir() && strings.HasSuffix(item.Name(), ".json") {
			names = append(names, item.Name())
		}
	}
	return names
}

// replay writes the oldest spool file to the database and deletes it. It
// reports whether that file is dealt with, so a caller can go on to the next.
// A file that cannot be read, or holds an entry the log would refuse, is
// renamed to *.bad for an operator rather than retried or deleted.
func (j *auditJournal) replay() bool {
	files := j.spoolFiles()
	if len(files) == 0 {
		return false
	}
	path := filepath.Join(j.dir, files[0])
	var entries []control.AuditEntry
	err := securefs.ReadJSON(path, &entries)
	if err == nil && slices.ContainsFunc(entries, func(e control.AuditEntry) bool { return e.ID == "" || !e.Valid() }) {
		err = errors.New("invalid audit entry")
	}
	if err != nil {
		slog.Error("audit spool file set aside", "file", path, "error", err)
		if err := os.Rename(path, path+".bad"); err != nil {
			slog.Error("audit spool file stuck", "file", path, "error", err)
			return false
		}
		return true
	}
	if len(entries) > 0 && !j.write(entries) {
		return false
	}
	if err := os.Remove(path); err != nil {
		slog.Error("audit spool file not removed", "file", path, "error", err)
		return false
	}
	return true
}

// warnDrops reports entries dropped since the last report, at most once per
// warnEvery, so an outage that drops thousands writes one log line.
func (j *auditJournal) warnDrops() {
	n := j.dropped.Load()
	if n == j.reported || time.Since(j.warnedAt) < j.warnEvery {
		return
	}
	slog.Warn("audit entries dropped", "count", n-j.reported)
	j.reported, j.warnedAt = n, time.Now()
}
