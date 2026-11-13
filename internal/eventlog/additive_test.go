package eventlog

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type evidenceEntry struct {
	testEntry
	RequestID string
	Truncated bool
}

func TestAdditiveUpgradePreservesHistoryAndRollbackWriters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.db")
	limits := Limits{MaxBytes: 64 << 20, Retention: 24 * time.Hour}
	old, err := Open(ctx, path, testKind, limits)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := old.Append(ctx, []testEntry{{At: at, Path: "/before", State: "kept", Routine: true}}); err != nil {
		t.Fatal(err)
	}
	old.Close()
	additions := []string{"request_id TEXT DEFAULT ''", "truncated INTEGER DEFAULT 0"}
	kind := Kind[evidenceEntry]{Table: testKind.Table, Version: testKind.Version,
		Columns: append(append([]string{}, testKind.Columns...), additions...), AddColumns: additions,
		Filters: testKind.Filters, Search: []string{"path", "request_id"},
		Fields: func(e *evidenceEntry) []any { return append(testKind.Fields(&e.testEntry), &e.RequestID, &e.Truncated) },
		Stamp:  func(e *evidenceEntry) (*int64, *time.Time) { return &e.ID, &e.At }, Routine: func(e *evidenceEntry) *bool { return &e.Routine }}
	upgraded, err := Open(ctx, path, kind, limits)
	if err != nil {
		t.Fatal(err)
	}
	entries, total, err := upgraded.Query(ctx, Query{})
	if err != nil || total != 1 || entries[0].ID != 1 || !entries[0].At.Equal(at) || entries[0].Path != "/before" || !entries[0].Routine || entries[0].RequestID != "" || entries[0].Truncated {
		t.Fatalf("history after upgrade: %+v %d %v", entries, total, err)
	}
	if err := upgraded.Append(ctx, []evidenceEntry{{testEntry: testEntry{At: at, Path: "/new", State: "kept"}, RequestID: "new-id", Truncated: true}}); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := upgraded.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != layout<<16|testKind.Version {
		t.Fatalf("rollback version changed: %d %v", version, err)
	}
	upgraded.Close()
	// The old binary's exact Kind and explicit INSERT can still append;
	// new columns' defaults supply its absent evidence fields.
	old, err = Open(ctx, path, testKind, limits)
	if err != nil {
		t.Fatal(err)
	}
	if _, total, err := old.Query(ctx, Query{}); err != nil || total != 2 {
		t.Fatalf("rollback lost history: %d %v", total, err)
	}
	if err := old.Append(ctx, []testEntry{{At: at, Path: "/rollback", State: "kept"}}); err != nil {
		t.Fatal(err)
	}
	old.Close()
	upgraded, err = Open(ctx, path, kind, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	entries, total, err = upgraded.Query(ctx, Query{})
	if err != nil || total != 3 || entries[0].Path != "/rollback" || entries[0].RequestID != "" || entries[0].Truncated || entries[1].RequestID != "new-id" || !entries[1].Truncated || entries[2].Path != "/before" {
		t.Fatalf("history after re-upgrade: %+v %d %v", entries, total, err)
	}
	if entries, total, err := upgraded.Query(ctx, Query{Text: "new-id"}); err != nil || total != 1 || entries[0].Path != "/new" {
		t.Fatalf("evidence search: %+v %d %v", entries, total, err)
	}
}

func TestAdditiveUpgradeFailureDoesNotPartiallyChangeHistory(t *testing.T) {
	ctx := context.Background()
	log, path := openTest(t, Limits{MaxBytes: 64 << 20, Retention: time.Hour})
	if err := log.Append(ctx, []testEntry{{At: time.Now(), Path: "/kept", State: "kept"}}); err != nil {
		t.Fatal(err)
	}
	kind := testKind
	kind.Columns = append(append([]string{}, kind.Columns...), "first TEXT DEFAULT ''", "bad TEXT")
	kind.AddColumns = kind.Columns[len(testKind.Columns):]
	if bad, err := Open(ctx, path, kind, log.Limits()); err == nil {
		bad.Close()
		t.Fatal("upgrade without a safe default accepted")
	}
	var schema string
	if err := log.db.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name = 'events'").Scan(&schema); err != nil || strings.Contains(schema, "first") {
		t.Fatalf("partial upgrade: %s %v", schema, err)
	}
	if entries, total, err := log.Query(ctx, Query{}); err != nil || total != 1 || entries[0].Path != "/kept" {
		t.Fatalf("failed upgrade lost history: %+v %d %v", entries, total, err)
	}
}
