package outlet

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// trap sends the requests that ban a client: two for a trap path.
func (f banFixture) trap(t *testing.T, ip, host string) {
	t.Helper()
	for range 2 {
		if code := f.as(ip, host, "/.env"); code != 403 {
			t.Fatalf("%s: trap: %d", ip, code)
		}
	}
}

// visitAs requests host and path from client ip as scanners do, without a
// browser's headers, with header and cookies.
func (f accessFixture) visitAs(ip, host, path string, header http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://"+host+path, nil)
	r.Host = host
	r.Header.Set("X-Forwarded-For", ip)
	maps.Copy(r.Header, header)
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

// One request for a trap path is refused and remembered, but bans nobody: it
// could come from anyone behind a shared address. A second from the same
// client within the window bans, on every routed address; one after the
// window starts over.
func TestTrapNeedsASecondHitToBan(t *testing.T) {
	f := newBanFixture(t)
	const scanner, neighbour = "203.0.113.9", "203.0.113.10"
	if code := f.as(scanner, "app.example.com", "/.env"); code != 403 {
		t.Fatalf("first hit: %d", code)
	}
	// Refused as before, and logged as a trap; the client is not held.
	if code := f.as(scanner, "app.example.com", "/"); code != 200 {
		t.Fatalf("after one hit: %d", code)
	}
	if view := f.bans(t, ""); view.Total != 0 || f.store.bans.held.Load() != 0 {
		t.Fatalf("banned by one hit: %+v", view)
	}
	if log := f.failureLog(t, "?cause=trapped"); log.Total != 1 {
		t.Fatalf("trapped: %d", log.Total)
	}
	// Another client's hit doesn't count for this one.
	f.as(neighbour, "other.example.com", "/.env")
	if code := f.as(scanner, "app.example.com", "/"); code != 200 {
		t.Fatalf("after a neighbour's hit: %d", code)
	}

	// The second, on another address, inside the window.
	f.advance(trapWindow - time.Minute)
	if code := f.as(scanner, "other.example.com", "/.env"); code != 403 {
		t.Fatalf("second hit: %d", code)
	}
	for _, host := range []string{"app.example.com", "other.example.com"} {
		if code := f.as(scanner, host, "/"); code != 403 {
			t.Fatalf("%s after two hits: %d", host, code)
		}
	}
	view := f.bans(t, "")
	if got := clients(view); !slices.Equal(got, []string{scanner}) || view.Bans[0].Host != "other.example.com" || view.Bans[0].Path != "/.env" {
		t.Fatalf("bans: %+v", view)
	}
	// The neighbour, who hit once, is let in.
	if code := f.as(neighbour, "app.example.com", "/"); code != 200 {
		t.Fatalf("neighbour: %d", code)
	}

	// A hit that was alone for the whole window is forgotten: the next
	// starts over, and the one after it bans.
	const slow = "198.51.100.77"
	f.as(slow, "app.example.com", "/.env")
	f.advance(trapWindow)
	f.as(slow, "app.example.com", "/.env")
	if code := f.as(slow, "app.example.com", "/"); code != 200 {
		t.Fatalf("a hit after the window: %d", code)
	}
	f.as(slow, "app.example.com", "/.env")
	if code := f.as(slow, "app.example.com", "/"); code != 403 {
		t.Fatalf("two hits inside a window: %d", code)
	}
}

// IPv6 clients are held by their /64, however many addresses of it hit the
// trap.
func TestTrapHitsOfOneSlash64AddUp(t *testing.T) {
	f := newBanFixture(t)
	f.as("2001:db8:1:2::1", "app.example.com", "/.env")
	if code := f.as("2001:db8:1:2::1", "app.example.com", "/"); code != 200 {
		t.Fatalf("after one hit: %d", code)
	}
	f.as("2001:db8:1:2:aaaa::2", "app.example.com", "/.env")
	if code := f.as("2001:db8:1:2::3", "app.example.com", "/"); code != 403 {
		t.Fatalf("two hits from one /64: %d", code)
	}
	if code := f.as("2001:db8:1:3::1", "app.example.com", "/"); code != 200 {
		t.Fatalf("another /64: %d", code)
	}
}

// Clients that hit the trap once each fill no more than the table's room, the
// oldest hits go first, and hits older than the window are forgotten.
func TestPendingTrapHitsAreBounded(t *testing.T) {
	var b banState
	start := time.Now()
	key := func(i int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
	}
	size := func() int {
		b.trapMu.Lock()
		defer b.trapMu.Unlock()
		if len(b.traps) != b.trapOrder.Len() {
			t.Fatalf("%d hits in the table, %d in the order", len(b.traps), b.trapOrder.Len())
		}
		return len(b.traps)
	}
	for i := range maxTraps + 100 {
		if b.trip(key(i), start) {
			t.Fatalf("client %d banned by its first hit", i)
		}
	}
	if got := size(); got != maxTraps {
		t.Fatalf("%d hits held, want %d", got, maxTraps)
	}
	// The oldest hundred were pushed out: they start over. The rest are
	// still on record, and spring the second.
	if b.trip(key(0), start.Add(time.Second)) {
		t.Fatal("a hit pushed out of the table still counted")
	}
	if !b.trip(key(maxTraps+50), start.Add(time.Second)) {
		t.Fatal("a recent hit was forgotten")
	}
	// Once the window has passed, the next new hit clears the old ones.
	if b.trip(key(maxTraps+500), start.Add(trapWindow+time.Minute)) {
		t.Fatal("banned by a first hit")
	}
	if got := size(); got != 1 {
		t.Fatalf("%d hits held after the window, want 1", got)
	}
	// A hit just inside the window counts, one at its end doesn't.
	b.trip(key(1), start)
	if !b.trip(key(1), start.Add(trapWindow-time.Nanosecond)) {
		t.Fatal("a hit inside the window did not count")
	}
	b.trip(key(2), start)
	if b.trip(key(2), start.Add(trapWindow)) {
		t.Fatal("a hit at the end of the window counted")
	}
}

// Someone the Outlet let in, with a session or an access token, is never
// banned for a trap path: the hit isn't even held against them.
func TestTrapHitsOfAccessHoldersBanNobody(t *testing.T) {
	f := newAccessFixture(t)
	h := f.store.Handler()
	var created struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(h, "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("create token: %d %s", w.Code, w.Body.String())
	}
	f.setPolicies(t,
		control.AccessPolicy{Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: conditions(withToken(created.Token.ID))},
		control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}},
	)
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	hit := func(ip string, header http.Header, cookies ...*http.Cookie) int {
		t.Helper()
		return f.visitAs(ip, "app.example.com", "/.env", header, cookies...).Code
	}
	for name, c := range map[string]struct {
		ip      string
		header  http.Header
		cookies []*http.Cookie
	}{
		"session": {"198.51.100.1", nil, []*http.Cookie{session}},
		"token":   {"198.51.100.2", http.Header{accessTokenHeader: {created.Value}}, nil},
	} {
		for range 4 {
			if code := hit(c.ip, c.header, c.cookies...); code != 403 {
				t.Fatalf("%s: trap: %d", name, code)
			}
		}
		if code := f.visitAs(c.ip, "app.example.com", "/", c.header, c.cookies...).Code; code != 200 {
			t.Fatalf("%s: banned by trap hits: %d", name, code)
		}
	}
	// A session or a token that doesn't hold for the address asked, a forged
	// session and a token with the wrong secret count for nothing: two hits
	// ban, on an address no policy covers.
	forged := *session
	forged.Value = session.Value[:len(session.Value)-2] + "xx"
	for i, c := range []struct {
		header  http.Header
		cookies []*http.Cookie
	}{
		{nil, nil},
		{nil, []*http.Cookie{session}},
		{http.Header{accessTokenHeader: {created.Value}}, nil},
		{http.Header{accessTokenHeader: {created.Token.ID + ".wrong-secret"}}, nil},
		{nil, []*http.Cookie{&forged}},
	} {
		ip := fmt.Sprintf("198.51.100.%d", 10+i)
		for range 2 {
			if code := f.visitAs(ip, "other.example.com", "/.env", c.header, c.cookies...).Code; code != 403 {
				t.Fatalf("case %d: trap: %d", i, code)
			}
		}
		if code := f.visitAs(ip, "other.example.com", "/", nil).Code; code != 403 {
			t.Fatalf("case %d: not banned by two hits: %d", i, code)
		}
	}
	if held := f.store.bans.held.Load(); held != 5 {
		t.Fatalf("%d bans", held)
	}
}
