package outlet

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

func anonymousEntry(i int, action string) control.AuditEntry {
	e := journalEntry(i)
	e.Action, e.Source = action, "public"
	return e
}

func rowsOf(entries []control.AuditEntry, action string) (n int) {
	for _, e := range entries {
		if e.Action == action {
			n++
		}
	}
	return n
}

// What unauthenticated requests trigger shares one budget of audit rows per
// window, whoever sends them; past it nothing is stored per request, and the
// next window opens with one row saying how many were left out, and of what.
func TestAnonymousAuditRowsShareOneBudget(t *testing.T) {
	db := &fakeAuditDB{}
	j, _ := startJournal(t, db, "")
	now := journalEpoch
	for i := range 100 {
		action := "access.token_failed"
		if i%4 == 0 {
			action = "access.login_failed"
		}
		j.addAnonymous(anonymousEntry(i, action), now.Add(time.Duration(i)*time.Millisecond))
	}
	j.flush()
	if n := len(db.stored); n != anonymousAuditRows {
		t.Fatalf("%d rows stored within one window, want %d", n, anonymousAuditRows)
	}
	// An entry that isn't anonymous is never held back by the budget.
	j.add(journalEntry(500))
	j.flush()
	if n := len(db.stored); n != anonymousAuditRows+1 {
		t.Fatalf("an ordinary entry: %d rows", n)
	}
	// The next window: one summary, then the new entry.
	j.addAnonymous(anonymousEntry(1000, "access.token_failed"), now.Add(anonymousAuditWindow))
	j.flush()
	summaries := suppressedRows(db.stored)
	if len(summaries) != 1 {
		t.Fatalf("%d summaries", len(summaries))
	}
	raw, _ := json.Marshal(summaries[0].Detail)
	var detail suppressedDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	// Of the hundred, 25 were logins and 75 tokens; the first 60 in order
	// were stored: 15 logins, 45 tokens.
	if detail.Count != 40 || detail.WindowSeconds != int(anonymousAuditWindow.Seconds()) || detail.Actions["access.login_failed"] != 10 || detail.Actions["access.token_failed"] != 30 {
		t.Fatalf("summary: %+v", detail)
	}
	if summaries[0].Source != "public" || summaries[0].ResourceType != "audit" || summaries[0].ActorEmail != "" {
		t.Fatalf("summary entry: %+v", summaries[0])
	}
	if n := len(db.stored) - 1; n != anonymousAuditRows+2 {
		t.Fatalf("%d other rows, want %d", n, anonymousAuditRows+2)
	}
	// A quiet window leaves no summary behind.
	j.addAnonymous(anonymousEntry(1001, "access.token_failed"), now.Add(5*anonymousAuditWindow))
	j.flush()
	if len(suppressedRows(db.stored)) != 1 {
		t.Fatal("a window with nothing left out wrote a summary")
	}
}

// A flood that stops still gets its summary: nobody has to send the next
// entry for it to be written.
func TestSuppressedRowsAreSummarizedWithoutAnotherEntry(t *testing.T) {
	db := &fakeAuditDB{}
	j, _ := startJournal(t, db, "", func(j *auditJournal) { j.anonymousWindow = 30 * time.Millisecond })
	now := time.Now()
	for i := range anonymousAuditRows + 5 {
		j.addAnonymous(anonymousEntry(i, "access.token_failed"), now)
	}
	waitJournal(t, "the summary", func() bool {
		db.mu.Lock()
		defer db.mu.Unlock()
		return len(suppressedRows(db.stored)) == 1
	})
	db.mu.Lock()
	raw, _ := json.Marshal(suppressedRows(db.stored)[0].Detail)
	db.mu.Unlock()
	var detail suppressedDetail
	if err := json.Unmarshal(raw, &detail); err != nil || detail.Count != 5 {
		t.Fatalf("summary: %s %v", raw, err)
	}
}

func TestSuppressedRowsAreSummarizedAtShutdown(t *testing.T) {
	db := &fakeAuditDB{}
	j, stop := startJournal(t, db, "")
	for i := range anonymousAuditRows + 7 {
		j.addAnonymous(anonymousEntry(i, "access.login_failed"), journalEpoch)
	}
	stop()
	db.mu.Lock()
	defer db.mu.Unlock()
	if len(suppressedRows(db.stored)) != 1 || rowsOf(db.stored, "access.login_failed") != anonymousAuditRows {
		t.Fatalf("after shutdown: %d summaries, %d rows", len(suppressedRows(db.stored)), rowsOf(db.stored, "access.login_failed"))
	}
}
