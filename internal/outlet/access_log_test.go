package outlet

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/accesslog"
	"github.com/lawnect/meerkat/internal/control"
)

// fakeAccessLog keeps entries in memory and fails Append as told.
type fakeAccessLog struct {
	mu      sync.Mutex
	entries []accesslog.Entry
	fail    error // Append's error while set
	full    int   // Appends to refuse as full, each lifted by a Shed
	sheds   int
}

func (f *fakeAccessLog) Append(_ context.Context, entries []accesslog.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if f.full > f.sheds {
		return accesslog.ErrFull
	}
	f.entries = append(f.entries, entries...)
	return nil
}

func (f *fakeAccessLog) Shed(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sheds++
	return nil
}

func (f *fakeAccessLog) Prune(context.Context, time.Time) error { return nil }
func (f *fakeAccessLog) Close() error                           { return nil }
func (f *fakeAccessLog) Query(context.Context, accesslog.Filter) ([]accesslog.Entry, int64, error) {
	return nil, 0, nil
}

func (f *fakeAccessLog) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

// Adding never waits: a full queue gives entries up. While the log refuses
// writes, entries wait in the buffer, up to its cap, and are written once
// it takes them again; a full log sheds its oldest and is written to.
func TestAccessLogWriterGivesUpRatherThanWaits(t *testing.T) {
	store := &fakeAccessLog{}
	w := newAccessLogWriter(store)
	w.queue = make(chan accesslog.Entry, 3)
	w.bufferCap, w.batchSize, w.tick, w.retryMax = 5, 2, time.Hour, time.Millisecond
	done := make(chan struct{})
	go func() {
		for range 10 {
			w.add(accesslog.Entry{Path: "/x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("add waited for a writer that isn't running")
	}
	if w.dropped.Load() != 7 {
		t.Fatalf("given up with the queue full: %d", w.dropped.Load())
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	store.fail = errors.New("disk on fire")
	go w.run(ctx)
	w.flush()
	for range 6 {
		w.add(accesslog.Entry{Path: "/y"})
	}
	w.flush()
	if store.count() != 0 || w.dropped.Load() != 7+4 {
		t.Fatalf("while failing: %d written, %d given up", store.count(), w.dropped.Load())
	}
	store.mu.Lock()
	store.fail, store.full = nil, 1
	store.mu.Unlock()
	w.flush()
	if store.count() != 5 || store.sheds != 1 {
		t.Fatalf("after the log recovered: %d written, %d sheds", store.count(), store.sheds)
	}
}

// With the log on, every refusal and sign-in asked for is logged, and
// requests let in that name a person or a token; anonymous ones aren't.
// The console reads the log filtered and in pages, and turns it on and off.
func TestAccessLog(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	var created struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	policies := func(users ...string) {
		f.setPolicies(t,
			control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/ci/"}}, RequireToken: true, Tokens: []string{created.Token.ID}},
			control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/v1/"}}},
			control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}, Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/internal/"}}},
			control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllPaths: true, AllowedUsers: users},
		)
	}
	// Both sign in; then only alice may pass.
	policies("@corp.example.com")
	alice, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	bob, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("bob@corp.example.com")))
	policies("alice@corp.example.com")
	read := func(query string) accessLogView {
		t.Helper()
		var view accessLogView
		w := request(h, "GET", "/api/v1/security/access-log"+query, f.admin, nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
			t.Fatalf("log%s: %d %s", query, w.Code, w.Body.String())
		}
		return view
	}
	// Off until turned on: nothing is logged.
	f.visit("app.example.com", "/workspace")
	if view := read(""); view.Enabled || !view.Available || len(view.Entries) != 0 {
		t.Fatalf("off: %+v", view)
	}
	if w := request(h, "PUT", "/api/v1/security/access-log", f.admin, map[string]any{"enabled": true}); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("turn on: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAudit(t); event.Action != "security.access_log" || string(event.Detail) != `{"enabled":true}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	ci := func(path, value string) {
		r := httptest.NewRequest("POST", "https://app.example.com"+path, nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", "198.51.100.9")
		r.Header.Set(accessTokenHeader, value)
		f.handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	f.visit("app.example.com", "/workspace")
	f.visit("app.example.com", "/v1/models")
	ci("/ci/run", created.Value)
	ci("/workspace", "junk.secret")
	f.visit("app.example.com", "/internal/x", alice)
	f.visit("app.example.com", "/workspace", alice)
	f.visit("app.example.com", "/workspace", bob)
	view := read("")
	var got []string
	for _, e := range view.Entries {
		got = append(got, strings.Join([]string{e.Method, e.Path, e.Decision, e.Reason, e.Email, e.Token}, " "))
	}
	want := []string{
		"GET /workspace denied not_allowed bob@corp.example.com ",
		"GET /workspace passed session alice@corp.example.com ",
		"GET /internal/x denied blocked  ",
		"POST /workspace denied token_invalid  ",
		"POST /ci/run passed bypass  " + created.Token.ID,
		"GET /workspace challenged no_session  ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if e := view.Entries[3]; e.Client != "198.51.100.9" || e.Host != "app.example.com" || e.Policy == "" || e.At.IsZero() {
		t.Fatalf("entry: %+v", e)
	}
	// Filtered, and in pages.
	if view := read("?decision=denied"); len(view.Entries) != 3 {
		t.Fatalf("denied: %+v", view.Entries)
	}
	if view := read("?policy=" + view.Entries[0].Policy); len(view.Entries) != 4 {
		t.Fatalf("one policy: %+v", view.Entries)
	}
	first := read("?limit=4")
	if len(first.Entries) != 4 || first.Next == 0 {
		t.Fatalf("first page: %+v", first)
	}
	if next := read("?limit=4&before=" + strconv.FormatInt(first.Next, 10)); len(next.Entries) != 2 || next.Next != 0 || next.Entries[0].ID >= first.Entries[3].ID {
		t.Fatalf("second page: %+v", next)
	}
	for _, query := range []string{"?decision=maybe", "?before=x", "?limit=0", "?limit=201"} {
		if w := request(h, "GET", "/api/v1/security/access-log"+query, f.admin, nil); w.Code != 400 {
			t.Fatalf("%s: %d", query, w.Code)
		}
	}
	// Off again: nothing more is logged; what was stays.
	if w := request(h, "PUT", "/api/v1/security/access-log", f.admin, map[string]any{"enabled": false}); w.Code != 200 {
		t.Fatalf("turn off: %d", w.Code)
	}
	f.visit("app.example.com", "/internal/y")
	if view := read(""); view.Enabled || len(view.Entries) != 6 {
		t.Fatalf("off again: %d entries", len(view.Entries))
	}
	// The switch survives a restart.
	if err := f.store.reloadAccess(context.Background()); err != nil || f.store.access.logging.Load() {
		t.Fatalf("after reloading: %v %v", f.store.access.logging.Load(), err)
	}
}
