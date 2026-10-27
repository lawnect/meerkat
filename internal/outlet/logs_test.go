package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/maphash"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// budgetLog is one of the logs, as the budget tests drive it.
type budgetLog struct {
	turn    func(on bool)
	routine func(r *http.Request) // adds an entry anyone can make
	notable func(r *http.Request) // adds one that names someone, or a service in trouble
	slot    func(client string) *atomic.Uint64
	read    func() (logged int, suppressed int64)
}

// fromClient is a request that came from ip.
func fromClient(ip string) *http.Request {
	r := httptest.NewRequest("GET", "https://app.example.com/x", nil)
	r.RemoteAddr = net.JoinHostPort(ip, "4000")
	return r
}

// apart returns an address like format that has a budget of its own, other
// than those of the clients avoided: a slot is shared by chance, and a test
// of two clients must not be left to it.
func apart(slot func(string) *atomic.Uint64, format string, avoid ...string) string {
	for i := 1; ; i++ {
		ip := fmt.Sprintf(format, i)
		free := true
		for _, other := range avoid {
			free = free && slot(clientKey(ip)) != slot(clientKey(other))
		}
		if free {
			return ip
		}
	}
}

// Each client may add 60 routine entries a minute to a log, and the rest are
// counted rather than kept, so a flood neither fills the queue and the file
// nor pushes out other entries. It costs other clients nothing, and entries
// that matter, naming someone or a service in trouble, have no budget.
func TestRoutineBudget(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func(t *testing.T, f proxyFixture) budgetLog
	}{
		{"access", func(t *testing.T, f proxyFixture) budgetLog {
			l := f.store.logs.access
			deny := func(email string) func(r *http.Request) {
				return func(r *http.Request) {
					f.store.logAccess(r, "app.example.com", "policy", accessDenied, "not_allowed", email, "")
				}
			}
			return budgetLog{
				turn: func(on bool) {
					body := map[string]any{"enabled": on, "max_mib": 64, "retention_days": 7}
					if w := request(f.store.Handler(), "PUT", "/api/v1/observe/access-log", f.admin, body); w.Code != 200 {
						t.Fatalf("access log %v: %d %s", on, w.Code, w.Body.String())
					}
				},
				routine: deny(""),
				notable: deny("alice@corp.example.com"),
				slot:    l.slot,
				read: func() (int, int64) {
					view := readLog[accessEntry](t, f, "access-log", "")
					return view.Total, view.Suppressed
				},
			}
		}},
		{"failures", func(t *testing.T, f proxyFixture) budgetLog {
			return budgetLog{
				turn:    func(on bool) { f.failureLogging(t, on) },
				routine: func(r *http.Request) { f.store.logFailure(r, "app.example.com", time.Time{}, failBlocked, 403, "") },
				notable: func(r *http.Request) { f.store.logFailure(r, "app.example.com", time.Time{}, failAppError, 500, "svc") },
				slot:    f.store.logs.failures.slot,
				read: func() (int, int64) {
					view := f.failureLog(t, "")
					return view.Total, view.Suppressed
				},
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newProxyFixture(t)
			var minutes atomic.Int64
			start := time.Date(2026, 10, 27, 15, 0, 30, 0, time.UTC)
			f.store.now = func() time.Time { return start.Add(time.Duration(minutes.Load()) * time.Minute) }
			defer func() { f.store.now = time.Now }()
			l := c.build(t, f)
			// send adds n entries for a client and checks how many were kept and how many given up.
			send := func(what, client string, add func(*http.Request), n, kept int, suppressed int64) {
				t.Helper()
				logged, given := l.read()
				for range n {
					add(fromClient(client))
				}
				logged2, given2 := l.read()
				if logged2-logged != kept || given2-given != suppressed {
					t.Fatalf("%s: %d kept and %d suppressed, want %d and %d", what, logged2-logged, given2-given, kept, suppressed)
				}
			}
			a := "198.51.100.1"
			b := apart(l.slot, "198.51.100.%d", a)

			// With the log off nothing is kept or counted, and the budget stays whole.
			send("while off", a, l.routine, 100, 0, 0)
			l.turn(true)
			send("a client's budget", a, l.routine, 60, 60, 0)
			send("over the budget", a, l.routine, 1, 0, 1)
			send("far over the budget", a, l.routine, 40, 0, 40)
			send("another client", b, l.routine, 60, 60, 0)
			send("another client over", b, l.routine, 1, 0, 1)
			// A client over its budget still gets the entries that matter logged, and they don't
			// take from the budget.
			send("notable entries", a, l.notable, 100, 100, 0)
			send("routine again", a, l.routine, 1, 0, 1)
			// The next minute starts the budget over.
			minutes.Add(1)
			send("the next minute", a, l.routine, 61, 60, 1)
			// An IPv6 client is told by its /56, as the other per-client limits do.
			v6 := apart(l.slot, "2001:db8:%x::1", a, b)
			sameNetwork := strings.TrimSuffix(v6, "::1") + ":ff:ffff::9"
			otherNetwork := apart(l.slot, "2001:db8:%x::1", a, b, v6)
			send("an IPv6 client", v6, l.routine, 60, 60, 0)
			send("the same /56", sameNetwork, l.routine, 5, 0, 5)
			send("the same /56, notable", sameNetwork, l.notable, 5, 5, 0)
			send("another /56", otherNetwork, l.routine, 60, 60, 0)
		})
	}
}

// A routine flood never reaches the queue: the budget is checked before
// the entry is handed over. With nothing writing, whatever got through
// would stay queued, and a flood would overflow the queue.
func TestRoutineFloodNeverReachesTheQueue(t *testing.T) {
	f := newProxyFixture(t)
	// This log's writer isn't started.
	l := openOutletLog(context.Background(), f.db, t.TempDir(), "failure-log.db", control.LogFailures, failureEntries)
	if l == nil {
		t.Fatal("log not opened")
	}
	defer l.close()
	l.enabled.Store(true)
	// Reading the log waits for the store's writer to be running, so it has read the log it runs.
	f.failureLog(t, "")
	logs := f.store.logs.failures
	f.store.logs.failures = l
	defer func() { f.store.logs.failures = logs }()
	const flood = 3 * 8192
	r := fromClient("203.0.113.9")
	for range flood {
		f.store.logFailure(r, "app.example.com", time.Time{}, failBlocked, 403, "")
	}
	if l.writer.Dropped() != 0 || l.suppressed.Load() != flood-routineBudget {
		t.Fatalf("%d given up for a full queue and %d suppressed, want 0 and %d", l.writer.Dropped(), l.suppressed.Load(), flood-routineBudget)
	}
	// Entries that matter still have the queue.
	for range 100 {
		f.store.logFailure(r, "app.example.com", time.Time{}, failTimeout, 504, "svc")
	}
	if l.writer.Dropped() != 0 || l.suppressed.Load() != flood-routineBudget {
		t.Fatalf("after notable entries: %d given up and %d suppressed", l.writer.Dropped(), l.suppressed.Load())
	}
}

// Many requests at once from one client take the budget exactly: no lock
// is held, and none of them takes more than its share.
func TestRoutineBudgetUnderContention(t *testing.T) {
	l := &outletLog[failureEntry]{seed: maphash.MakeSeed()}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	var allowed atomic.Int64
	for range 16 {
		wg.Go(func() {
			for range 100 {
				if l.allow("203.0.113.9", now) {
					allowed.Add(1)
				}
			}
		})
	}
	wg.Wait()
	if allowed.Load() != routineBudget || l.suppressed.Load() != 1600-routineBudget {
		t.Fatalf("%d allowed and %d suppressed", allowed.Load(), l.suppressed.Load())
	}
}

// Clients can share a slot, however many slots there are; they share its
// budget, which is stricter for them and never looser.
func TestRoutineBudgetSharedSlot(t *testing.T) {
	l := &outletLog[failureEntry]{seed: maphash.MakeSeed()}
	now := time.Date(2026, 10, 27, 15, 0, 0, 0, time.UTC)
	var other string
	for i := 0; other == ""; i++ {
		if ip := fmt.Sprintf("10.%d.%d.1", i/256, i%256); ip != "10.0.0.1" && l.slot(ip) == l.slot("10.0.0.1") {
			other = ip
		}
	}
	for range routineBudget {
		if !l.allow("10.0.0.1", now) {
			t.Fatal("refused within the budget")
		}
	}
	if l.allow(other, now) || l.suppressed.Load() != 1 {
		t.Fatalf("a client sharing the slot was let in: %d suppressed", l.suppressed.Load())
	}
}

// readLog reads a log through the console's API, with a query such as
// "?q=bob".
func readLog[E any](tb testing.TB, f proxyFixture, name, query string) logView[E] {
	tb.Helper()
	var view logView[E]
	w := request(f.store.Handler(), "GET", "/api/v1/observe/"+name+query, f.admin, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		tb.Fatalf("%s%s: %d %s", name, query, w.Code, w.Body.String())
	}
	return view
}

// BenchmarkRoutineBudget is what a routine entry pays for the budget: one
// atomic compare-and-swap, with no lock and, for an IPv4 client, no
// allocation (an IPv6 client's /56 takes a few to name).
func BenchmarkRoutineBudget(b *testing.B) {
	for _, ip := range []string{"203.0.113.9", "2001:db8:0:123:4:5:6:7"} {
		b.Run(ip, func(b *testing.B) {
			l := &outletLog[failureEntry]{seed: maphash.MakeSeed()}
			now := time.Now()
			b.ReportAllocs()
			for b.Loop() {
				l.allow(clientKey(ip), now)
			}
		})
	}
}
