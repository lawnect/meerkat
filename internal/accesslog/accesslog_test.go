package accesslog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Entries come back newest first, filtered and paged; old ones and those
// beyond the cap are forgotten.
func TestAccessLog(t *testing.T) {
	ctx := context.Background()
	log, err := Open(ctx, filepath.Join(t.TempDir(), "access-log.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	entries := []Entry{
		{At: now.Add(-40 * 24 * time.Hour), Host: "app.example.com", Method: "GET", Path: "/old", Policy: "staff", Decision: "denied", Reason: "not_allowed"},
		{At: now.Add(-3 * time.Minute), Host: "app.example.com", Method: "GET", Path: "/", Client: "198.51.100.7", Policy: "staff", Decision: "challenged", Reason: "no_session"},
		{At: now.Add(-2 * time.Minute), Host: "app.example.com", Method: "POST", Path: "/v1/" + strings.Repeat("x", 600), Policy: "api", Decision: "passed", Reason: "token", Token: "ci"},
		{At: now.Add(-time.Minute), Host: "app.example.com", Method: "GET", Path: "/admin", Policy: "staff", Decision: "denied", Reason: "not_allowed", Email: "bob@corp.example.com"},
	}
	if err := log.Append(ctx, entries); err != nil {
		t.Fatal(err)
	}
	got, next, err := log.Query(ctx, Filter{Limit: 2})
	if err != nil || len(got) != 2 || got[0].Path != "/admin" || got[0].Email != "bob@corp.example.com" || !got[0].At.Equal(entries[3].At) || len(got[1].Path) != maxPath || next != got[1].ID {
		t.Fatalf("first page: %+v %d %v", got, next, err)
	}
	if got, next, err = log.Query(ctx, Filter{Limit: 2, Before: next}); err != nil || len(got) != 2 || got[0].Path != "/" || got[1].Path != "/old" || next != 0 {
		t.Fatalf("second page: %+v %d %v", got, next, err)
	}
	if got, _, err = log.Query(ctx, Filter{Decision: "denied", Policy: "staff"}); err != nil || len(got) != 2 {
		t.Fatalf("filtered: %+v %v", got, err)
	}
	if err := log.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = log.Query(ctx, Filter{}); len(got) != 3 {
		t.Fatalf("after the retention: %d", len(got))
	}
	log.keep = 2
	if err := log.Prune(ctx, now); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = log.Query(ctx, Filter{}); len(got) != 2 || got[1].Path[:4] != "/v1/" {
		t.Fatalf("after the cap: %+v", got)
	}
}

// SQLite refuses to grow the file past its cap; shedding the oldest
// entries makes room again without the file growing.
func TestAccessLogSizeCap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "access-log.db")
	const limit = 128 << 10
	log, err := open(ctx, path, limit)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	batch := make([]Entry, 50)
	for i := range batch {
		batch[i] = Entry{At: time.Now(), Host: "app.example.com", Method: "GET", Path: "/" + strings.Repeat("p", 400), Policy: "staff", Decision: "denied", Reason: "not_allowed"}
	}
	for range 200 {
		if err = log.Append(ctx, batch); err != nil {
			break
		}
	}
	if !errors.Is(err, ErrFull) {
		t.Fatalf("writes past the cap: %v", err)
	}
	if err := log.Shed(ctx); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(ctx, batch[:5]); err != nil {
		t.Fatalf("after shedding: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() > limit {
		t.Fatalf("file size %d over %d: %v", info.Size(), limit, err)
	}
}
