package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lawnect/meerkat/internal/control"
)

// banFixture is an Outlet with two routed addresses, the failure log on and
// a clock the test moves.
type banFixture struct {
	proxyFixture
	moved *atomic.Int64 // seconds the clock is ahead
}

func newBanFixture(t *testing.T) banFixture {
	t.Helper()
	f := newProxyFixture(t, echo("app"))
	f.route(t, 0, "app.example.com")
	f.route(t, 0, "other.example.com")
	f.failureLogging(t, true)
	start, moved := time.Now(), new(atomic.Int64)
	f.store.now = func() time.Time { return start.Add(time.Duration(moved.Load()) * time.Second) }
	return banFixture{f, moved}
}

func (f banFixture) advance(d time.Duration) { f.moved.Add(int64(d / time.Second)) }

// as sends a public GET the way Caddy forwards it, for the client ip (none
// when empty), and returns the status.
func (f banFixture) as(ip, host, path string) int {
	r := httptest.NewRequest("GET", "/", nil)
	r.URL.Path, r.Host = path, host
	if ip != "" {
		r.Header.Set("X-Forwarded-For", ip)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w.Code
}

// bans reads the bans, with a query such as "?q=app".
func (f banFixture) bans(t *testing.T, query string) clientBansView {
	t.Helper()
	var view clientBansView
	w := request(f.store.Handler(), "GET", "/api/v1/security/client-bans"+query, f.admin, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatalf("bans%s: %d %s", query, w.Code, w.Body.String())
	}
	return view
}

// clients names the banned clients on a page, newest first.
func clients(view clientBansView) []string {
	names := []string{}
	for _, b := range view.Bans {
		names = append(names, b.Client)
	}
	return names
}

func (f banFixture) setBans(t *testing.T, minutes int, exempt ...string) *httptest.ResponseRecorder {
	t.Helper()
	if exempt == nil {
		exempt = []string{}
	}
	return request(f.store.Handler(), "PUT", "/api/v1/security/client-bans", f.admin, map[string]any{"minutes": minutes, "exempt": exempt})
}

func (f banFixture) release(body map[string]any) *httptest.ResponseRecorder {
	return request(f.store.Handler(), "POST", "/api/v1/security/client-bans/release", f.admin, body)
}

func (f banFixture) lastAuditEvent(t *testing.T) control.AuditEvent {
	t.Helper()
	f.store.journal.flush()
	events, _, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1})
	if err != nil || len(events) == 0 {
		t.Fatalf("audit: %+v %v", events, err)
	}
	return events[0]
}

// The honeypot rule catches what only scanners ask for, at any depth, and
// leaves what applications serve; the rest of the hidden files are blocked
// by the other rule, without a ban.
func TestHoneypotRuleMatchesScannerPaths(t *testing.T) {
	s := &Store{}
	s.applyBlockRules(control.DefaultBlockRules())
	for _, c := range []struct {
		path, want string
	}{
		{"/.env", "honeypot"},
		{"/.ENV", "honeypot"},
		{"/.env.production", "honeypot"},
		{"/.env/", "honeypot"},
		{"/api/.env", "honeypot"},
		{"/app/.env.production", "honeypot"},
		{"/app/.env.my-site_2", "honeypot"},
		{"/.git/config", "honeypot"},
		{"/.git", "honeypot"},
		{"/.git/HEAD", "honeypot"},
		{"/site/.git/config", "honeypot"},
		{"/blog/.git/HEAD", "honeypot"},
		{"/.aws/credentials", "honeypot"},
		{"/.ssh/id_rsa", "honeypot"},
		{"/.svn/entries", "honeypot"},
		{"/.hg/store", "honeypot"},
		{"/.docker/config.json", "honeypot"},
		{"/.kube/config", "honeypot"},
		{"/.htpasswd", "honeypot"},
		{"/.DS_Store", "honeypot"},
		{"/wp-config.php", "honeypot"},
		{"/wp-config.php.bak", "honeypot"},
		{"/blog/wp-config.php~", "honeypot"},
		{"/a/../.env", "honeypot"},
		{"/.htaccess", "hidden-files"},
		{"/.gitignore", "hidden-files"},
		{"/.hidden/x", "hidden-files"},
		{"/.well-known/security.txt", ""},
		{"/.well-known/.env", ""},
		{"/environment", ""},
		{"/env", ""},
		{"/app/environment.js", ""},
		{"/assets/main.env.js", ""},
		{"/wp-login.php", ""},
		{"/gitlab/config", ""},
		{"/", ""},
	} {
		got, ban := "", false
		if rule := s.blockedBy("app.example.com", c.path); rule != nil {
			got, ban = rule.id, rule.ban
		}
		if got != c.want || ban != (c.want == "honeypot") {
			t.Errorf("%s: blocked by %q (ban %v), want %q", c.path, got, ban, c.want)
		}
	}
}

// A request for a trap path is refused and bans its client on every routed
// address, but never on the management host, the Outlet domain's connector
// API or under /.well-known/. The failure log tells the trap from what
// follows.
func TestTrapBansTheClientEverywhereRouted(t *testing.T) {
	f := newBanFixture(t)
	const scanner, visitor = "203.0.113.9", "203.0.113.10"
	if code := f.as(scanner, "app.example.com", "/.env"); code != 403 {
		t.Fatalf("trap: %d", code)
	}
	for _, c := range []struct {
		host, path string
		want       int
	}{
		{"app.example.com", "/", 403},
		{"other.example.com", "/index.html", 403},
		{"app.example.com", "/.well-known/acme-challenge/x", 200},
		{"app.example.com", "/.well-known", 200},
		{"app.example.com", "/.well-known/../index.html", 403},
		{"app.example.com", "/.WELL-KNOWN/x", 200},
		{f.adminHost, "/healthz", 200},
		{f.adminHost, "/.env", 0},
		{"example.com", "/connector/v1/config", 0},
		{"nowhere.example.com", "/", 404},
	} {
		// The management host and the connector API answer for themselves:
		// whatever they say, it is not the ban.
		code := f.as(scanner, c.host, c.path)
		if c.want == 0 && code == 403 || c.want != 0 && code != c.want {
			t.Errorf("banned client: %s%s: %d, want %d", c.host, c.path, code, c.want)
		}
	}
	// Another client, one address over, is not held.
	if code := f.as(visitor, "app.example.com", "/"); code != 200 {
		t.Fatalf("another client: %d", code)
	}

	view := f.bans(t, "")
	if view.Total != 1 || len(view.Bans) != 1 {
		t.Fatalf("bans: %+v", view)
	}
	b := view.Bans[0]
	if b.Client != scanner || b.Rule != "honeypot" || b.Host != "app.example.com" || b.Path != "/.env" || b.Hits != 3 ||
		b.Until.Sub(b.Since) != time.Hour || b.Since.IsZero() {
		t.Fatalf("ban: %+v", b)
	}
	// The rule counts the trap, the ban the requests it refused.
	var rules blockRulesView
	if w := request(f.store.Handler(), "GET", "/api/v1/security/block-rules", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rules) != nil || rules.Hits["honeypot"] != 1 || rules.Hits["hidden-files"] != 0 {
		t.Fatalf("rule hits: %d %s", w.Code, w.Body.String())
	}

	want := []string{
		"trapped 403 [] app.example.com /.env",
		"banned 403 [] app.example.com /",
		"banned 403 [] other.example.com /index.html",
		"banned 403 [] app.example.com /.well-known/../index.html",
	}
	log := f.failureLog(t, "")
	if got := failureLines(log.Entries); !slices.Equal(got, want) {
		t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Both are refusals anyone can cause: routine, and in the group of them.
	for _, e := range log.Entries {
		if !e.Routine || e.Client != scanner || e.Status != 403 {
			t.Fatalf("entry: %+v", e)
		}
	}
	for query, total := range map[string]int{"?cause=trapped": 1, "?cause=banned": 3, "?cause=refused": 4, "?cause=service": 0} {
		if got := f.failureLog(t, query); got.Total != total {
			t.Fatalf("%s: %d, want %d", query, got.Total, total)
		}
	}
}

// Another site can make a visitor's browser ask for a trap path, with an
// image or a link. That request is refused but bans nobody; one typed into
// the address bar, or sent by a scanner, bans.
func TestTrapFromAnotherSitesPageBansNobody(t *testing.T) {
	f := newBanFixture(t)
	trap := func(ip string, headers map[string]string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/", nil)
		r.URL.Path, r.Host = "/.env", "app.example.com"
		r.Header.Set("X-Forwarded-For", ip)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s: trap: %d", ip, w.Code)
		}
	}
	for i, headers := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Dest": "image"},
		{"Sec-Fetch-Site": "same-site"},
		{"Sec-Fetch-Site": "same-origin"},
		{"Referer": "https://elsewhere.example.net/"},
	} {
		ip := fmt.Sprintf("198.51.100.%d", i+1)
		trap(ip, headers)
		if code := f.as(ip, "app.example.com", "/"); code != 200 {
			t.Fatalf("%v: banned: %d", headers, code)
		}
	}
	trap("198.51.100.50", map[string]string{"Sec-Fetch-Site": "none"})
	trap("198.51.100.51", nil)
	if got := clients(f.bans(t, "")); !slices.Equal(got, []string{"198.51.100.51", "198.51.100.50"}) {
		t.Fatalf("bans: %v", got)
	}
	for query, total := range map[string]int{"?cause=blocked": 4, "?cause=trapped": 2} {
		if got := f.failureLog(t, query); got.Total != total {
			t.Fatalf("%s: %d, want %d", query, got.Total, total)
		}
	}
}

// A client that can't be told apart is turned away for the trap and never
// banned.
func TestTrapWithoutAClientBansNobody(t *testing.T) {
	f := newBanFixture(t)
	for _, ip := range []string{"", "not-an-address"} {
		for range 2 {
			if code := f.as(ip, "app.example.com", "/.env"); code != 403 {
				t.Fatalf("%q: trap: %d", ip, code)
			}
		}
		if code := f.as(ip, "app.example.com", "/"); code != 200 {
			t.Fatalf("%q after the trap: %d", ip, code)
		}
	}
	if f.store.bans.held.Load() != 0 || f.bans(t, "").Total != 0 {
		t.Fatal("a client without an address was banned")
	}
	b := &f.store.bans
	for _, ip := range []string{"", "x", "1.2.3", "fe80::1%eth0"} {
		b.add(netip.MustParsePrefix("10.0.0.1/32"), "r", "h", "/", time.Now(), time.Hour)
		if b.find(ip, time.Now()) != nil {
			t.Fatalf("found a ban for %q", ip)
		}
	}
}

// A ban lasts the configured time and then is gone, whether or not its
// client came back; a client banned again after that is held from then.
func TestBansExpire(t *testing.T) {
	f := newBanFixture(t)
	if w := f.setBans(t, 30); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	f.as("198.51.100.1", "app.example.com", "/.env")
	f.advance(10 * time.Minute)
	f.as("198.51.100.2", "app.example.com", "/.env")
	if w := f.setBans(t, 120); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	f.as("198.51.100.3", "app.example.com", "/.env")

	f.advance(19 * time.Minute) // 29 minutes after the first
	if code := f.as("198.51.100.1", "app.example.com", "/"); code != 403 {
		t.Fatalf("just before the end: %d", code)
	}
	f.advance(time.Minute) // 30
	if code := f.as("198.51.100.1", "app.example.com", "/"); code != 200 {
		t.Fatalf("at the end: %d", code)
	}
	// Bans made before the settings changed keep their time; the lookup
	// dropped the one that ended, and listing never shows an ended one.
	if got := clients(f.bans(t, "")); !slices.Equal(got, []string{"198.51.100.3", "198.51.100.2"}) || f.store.bans.held.Load() != 2 {
		t.Fatalf("bans: %v, %d held", got, f.store.bans.held.Load())
	}
	f.advance(10 * time.Minute) // the second one, made at 10, ends at 40
	if got := clients(f.bans(t, "")); !slices.Equal(got, []string{"198.51.100.3"}) || f.store.bans.held.Load() != 2 {
		t.Fatalf("bans: %v, %d held", got, f.store.bans.held.Load())
	}
	// Nobody comes back for it: the sweep drops it.
	f.store.bans.sweep(f.store.now())
	if f.store.bans.held.Load() != 1 {
		t.Fatalf("after the sweep: %d held", f.store.bans.held.Load())
	}
	f.advance(2 * time.Hour)
	f.store.bans.sweep(f.store.now())
	if f.store.bans.held.Load() != 0 || f.bans(t, "").Total != 0 {
		t.Fatalf("after two hours: %d held", f.store.bans.held.Load())
	}
	// The same client is banned again, from now.
	f.as("198.51.100.1", "app.example.com", "/.env")
	if code := f.as("198.51.100.1", "app.example.com", "/"); code != 403 {
		t.Fatalf("banned again: %d", code)
	}
	if b := f.bans(t, "").Bans[0]; b.Until.Sub(b.Since) != 2*time.Hour {
		t.Fatalf("ban: %+v", b)
	}
}

// Exempt clients get the 403 for a trap path and no ban; the exempt list is
// read from the saved settings when the Outlet starts.
func TestExemptClientsAreNeverBanned(t *testing.T) {
	f := newBanFixture(t)
	if w := f.setBans(t, 60, "203.0.113.0/24", "2001:db8:7::/48", "198.51.100.77"); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	check := func() {
		t.Helper()
		for _, ip := range []string{"203.0.113.9", "2001:db8:7:5::1", "198.51.100.77"} {
			if code := f.as(ip, "app.example.com", "/.env"); code != 403 {
				t.Fatalf("%s: trap: %d", ip, code)
			}
			if code := f.as(ip, "app.example.com", "/"); code != 200 {
				t.Fatalf("%s after the trap: %d", ip, code)
			}
		}
		if f.store.bans.held.Load() != 0 {
			t.Fatal("an exempt client was banned")
		}
		if code := f.as("198.51.100.78", "app.example.com", "/.env"); code != 403 {
			t.Fatalf("trap: %d", code)
		}
		if code := f.as("198.51.100.78", "app.example.com", "/"); code != 403 {
			t.Fatalf("a client next to an exempt one: %d", code)
		}
		f.release(map[string]any{"all": true})
	}
	check()
	// What a restart does: settings come back from the database.
	f.store.bans.config.Store(nil)
	if err := f.store.reloadClientBans(context.Background()); err != nil {
		t.Fatal(err)
	}
	check()
	log := f.failureLog(t, "?cause=trapped")
	if log.Total != 8 {
		t.Fatalf("trapped: %d", log.Total)
	}
}

// Saving exempt ranges ends the bans they hold at once, and a client that is
// exempt is not held by a ban of the /64 it shares with a scanner.
func TestExemptSettingsReleaseBans(t *testing.T) {
	f := newBanFixture(t)
	for _, ip := range []string{"203.0.113.9", "198.51.100.7", "2001:db8:5::1", "2001:db8:6::2", "203.0.114.1"} {
		f.as(ip, "app.example.com", "/.env")
	}
	if view := f.bans(t, ""); view.Total != 5 {
		t.Fatalf("bans: %+v", view)
	}
	// A range releases the bans it holds whole: the /24 and the /64 do. An
	// exempt address does not release the ban of its /64, nor one next to
	// it.
	if w := f.setBans(t, 60, "203.0.113.0/24", "2001:db8:5::/64", "2001:db8:6::1", "203.0.114.0"); w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	view := f.bans(t, "")
	if got := clients(view); !slices.Equal(got, []string{"203.0.114.1", "2001:db8:6::/64", "198.51.100.7"}) {
		t.Fatalf("bans after: %v", got)
	}
	for ip, want := range map[string]int{
		"203.0.113.9":   200,
		"2001:db8:5::1": 200,
		"198.51.100.7":  403,
		"203.0.114.1":   403,
		"2001:db8:6::2": 403,
		"2001:db8:6::1": 200, // exempt, though its /64 is banned
		"2001:db8:6::3": 403,
	} {
		if code := f.as(ip, "app.example.com", "/"); code != want {
			t.Errorf("%s: %d, want %d", ip, code, want)
		}
	}
}

// IPv4 clients are banned one by one, IPv6 clients by /64, however the
// address is written.
func TestIPv6ClientsShareTheirSlash64(t *testing.T) {
	f := newBanFixture(t)
	f.as("2001:db8:1:2::1", "app.example.com", "/.env")
	f.as("::ffff:192.0.2.55", "app.example.com", "/.env")
	for ip, want := range map[string]int{
		"2001:db8:1:2:ffff:ffff:ffff:ffff": 403,
		"2001:DB8:1:2::99":                 403,
		"2001:db8:1:3::1":                  200,
		"2001:db8:1::1":                    200,
		"192.0.2.55":                       403,
		"192.0.2.56":                       200,
		"::ffff:192.0.2.55":                403,
	} {
		if code := f.as(ip, "app.example.com", "/"); code != want {
			t.Errorf("%s: %d, want %d", ip, code, want)
		}
	}
	if got := clients(f.bans(t, "")); !slices.Equal(got, []string{"192.0.2.55", "2001:db8:1:2::/64"}) {
		t.Fatalf("bans: %v", got)
	}
}

// The table holds 65,536 bans, in the order they were made: past that the
// oldest goes, and a client banned again while held keeps its ban.
func TestBanTableEvictsTheOldest(t *testing.T) {
	var b banState
	start := time.Now()
	now := start.Add(time.Minute)
	key := func(i int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 32)
	}
	held := func(i int) bool { return b.find(key(i).Addr().String(), now) != nil }
	for i := range maxBans {
		b.add(key(i), "honeypot", "app.example.com", "/.env", start, time.Hour)
	}
	if b.held.Load() != maxBans || !held(0) || !held(maxBans-1) {
		t.Fatalf("filled: %d held", b.held.Load())
	}
	// One more makes room by taking the oldest.
	b.add(key(maxBans), "honeypot", "app.example.com", "/.env", start, time.Hour)
	if b.held.Load() != maxBans || held(0) || !held(1) || !held(maxBans) {
		t.Fatalf("after one more: %d held", b.held.Load())
	}
	for i := 1; i < 5; i++ {
		b.add(key(maxBans+i), "honeypot", "app.example.com", "/.env", start, time.Hour)
	}
	if b.held.Load() != maxBans {
		t.Fatalf("held %d over the cap", b.held.Load())
	}
	for _, c := range []struct {
		i    int
		held bool
	}{{0, false}, {1, false}, {4, false}, {5, true}, {100, true}, {maxBans - 1, true}, {maxBans + 4, true}} {
		if got := held(c.i); got != c.held {
			t.Errorf("client %d held: %v, want %v", c.i, got, c.held)
		}
	}
	// A ban in force is not replaced by another for the same client, and
	// does not move up the order.
	b.add(key(5), "other-rule", "x", "/x", now, time.Hour)
	if found := b.find("10.0.0.5", now); found == nil || found.rule != "honeypot" || found.host != "app.example.com" {
		t.Fatalf("replaced: %+v", found)
	}
	b.add(key(maxBans+5), "honeypot", "app.example.com", "/.env", start, time.Hour)
	if held(5) {
		t.Fatal("a repeated ban was kept as the newest")
	}
	if total, page := b.list(now, "", 0, 3); total != maxBans || len(page) != 3 || page[0].Client != "10.1.0.5" || page[1].Client != "10.1.0.4" || page[2].Client != "10.1.0.3" {
		t.Fatalf("newest first: %d %+v", total, page)
	}
	// The trap path is clipped, on a character boundary.
	b.add(key(1), "honeypot", "app.example.com", "/"+strings.Repeat("é", 400), now, time.Hour)
	if found := b.find("10.0.0.1", now); found == nil || len(found.path) > maxBanPath || !strings.HasPrefix(found.path, "/é") || !utf8.ValidString(found.path) {
		t.Fatalf("long path: %+v", found)
	}
}

// One ban, or all of them, can be released; a released client is let in.
func TestReleaseBans(t *testing.T) {
	f := newBanFixture(t)
	for _, ip := range []string{"203.0.113.9", "198.51.100.7", "2001:db8:1:2::1", "192.0.2.5"} {
		f.as(ip, "app.example.com", "/.env")
	}
	w := f.release(map[string]any{"client": "198.51.100.7"})
	var view clientBansView
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Total != 3 {
		t.Fatalf("release one: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAuditEvent(t); event.Action != "security.client_ban_release" || string(event.Detail) != `{"client":"198.51.100.7","count":1}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	if code := f.as("198.51.100.7", "app.example.com", "/"); code != 200 {
		t.Fatalf("released: %d", code)
	}
	// The /64 is released by the name it is listed with, or by an address in it.
	if w := f.release(map[string]any{"client": "2001:db8:1:2::/64"}); w.Code != 200 {
		t.Fatalf("release a /64: %d %s", w.Code, w.Body.String())
	}
	if code := f.as("2001:db8:1:2::5", "app.example.com", "/"); code != 200 {
		t.Fatalf("released /64: %d", code)
	}
	f.as("2001:db8:1:2::1", "app.example.com", "/.env")
	if w := f.release(map[string]any{"client": "2001:db8:1:2::77"}); w.Code != 200 || f.bans(t, "").Total != 2 {
		t.Fatalf("release by an address: %d %s", w.Code, w.Body.String())
	}

	for _, body := range []map[string]any{{"client": "198.51.100.7"}, {"client": "203.0.113.1"}, {"client": "nonsense"}, {"client": "2001:db8:1:2::/48"}, {"client": "203.0.113.9/32"}} {
		if w := f.release(body); w.Code != 404 || errorCode(t, w) != "ban_not_found" {
			t.Fatalf("release %v: %d %s", body, w.Code, w.Body.String())
		}
	}
	for _, body := range []map[string]any{{}, {"client": ""}, {"all": false}, {"all": true, "client": "203.0.113.9"}, {"all": "yes"}, {"client": 7}, {"everyone": true}} {
		if w := f.release(body); w.Code != 400 || errorCode(t, w) != "invalid_request" {
			t.Fatalf("release %v: %d %s", body, w.Code, w.Body.String())
		}
	}
	if view := f.bans(t, ""); view.Total != 2 {
		t.Fatalf("a refused release changed the bans: %+v", view)
	}

	w = f.release(map[string]any{"all": true})
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Total != 0 || len(view.Bans) != 0 || f.store.bans.held.Load() != 0 {
		t.Fatalf("release all: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAuditEvent(t); event.Action != "security.client_ban_release" || string(event.Detail) != `{"all":true,"count":2}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	for _, ip := range []string{"203.0.113.9", "192.0.2.5"} {
		if code := f.as(ip, "app.example.com", "/"); code != 200 {
			t.Fatalf("%s after releasing all: %d", ip, code)
		}
	}
	if w := f.release(map[string]any{"all": true}); w.Code != 200 {
		t.Fatalf("release none: %d", w.Code)
	}
	if event := f.lastAuditEvent(t); string(event.Detail) != `{"all":true,"count":0}` {
		t.Fatalf("audit: %s", event.Detail)
	}
}

// The settings and the bans are read and changed by an administrator only.
func TestClientBansAPI(t *testing.T) {
	f := newBanFixture(t)
	h := f.store.Handler()
	w := request(h, "GET", "/api/v1/security/client-bans", f.admin, nil)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"settings":{"minutes":60,"exempt":[]},"total":0,"bans":[]}` {
		t.Fatalf("defaults: %d %s", w.Code, w.Body.String())
	}

	// Newest first, paged, searched by client, host or path.
	for i := range 5 {
		ip := fmt.Sprintf("203.0.113.%d", i+1)
		host := "app.example.com"
		if i%2 == 1 {
			host = "other.example.com"
		}
		f.as(ip, host, fmt.Sprintf("/.Env.v%d", i))
		f.advance(time.Second)
	}
	view := f.bans(t, "")
	if view.Total != 5 || !slices.Equal(clients(view), []string{"203.0.113.5", "203.0.113.4", "203.0.113.3", "203.0.113.2", "203.0.113.1"}) || view.Bans[0].Path != "/.Env.v4" || view.Bans[0].Host != "app.example.com" {
		t.Fatalf("bans: %+v", view)
	}
	for _, c := range []struct {
		query string
		want  []string
		total int
	}{
		{"?per_page=2", []string{"203.0.113.5", "203.0.113.4"}, 5},
		{"?per_page=2&page=2", []string{"203.0.113.3", "203.0.113.2"}, 5},
		{"?per_page=2&page=3", []string{"203.0.113.1"}, 5},
		{"?per_page=2&page=4", []string{}, 5},
		{"?per_page=200", []string{"203.0.113.5", "203.0.113.4", "203.0.113.3", "203.0.113.2", "203.0.113.1"}, 5},
		{"?q=OTHER", []string{"203.0.113.4", "203.0.113.2"}, 2},
		{"?q=.v3", []string{"203.0.113.4"}, 1},
		{"?q=113.1", []string{"203.0.113.1"}, 1},
		{"?q=nothing", []string{}, 0},
		{"?q=app&per_page=2&page=2", []string{"203.0.113.1"}, 3},
	} {
		if got := f.bans(t, c.query); !slices.Equal(clients(got), c.want) || got.Total != c.total {
			t.Errorf("%s: %v (total %d), want %v (%d)", c.query, clients(got), got.Total, c.want, c.total)
		}
	}
	for _, query := range []string{"?page=0", "?page=x", "?page=1048577", "?per_page=0", "?per_page=201", "?per_page=-1", "?q=" + strings.Repeat("a", 129)} {
		if w := request(h, "GET", "/api/v1/security/client-bans"+query, f.admin, nil); w.Code != 400 || errorCode(t, w) != "invalid_ban_query" {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
	}
	if w := request(h, "GET", "/api/v1/security/client-bans?q="+strings.Repeat("a", 128), f.admin, nil); w.Code != 200 {
		t.Fatalf("longest search: %d", w.Code)
	}

	// Settings are checked, normalized, saved, audited and answered like a
	// first page.
	w = f.setBans(t, 5, "203.0.113.50/32", "2001:DB8::1", "2001:db8:aa::7/32", "::ffff:192.0.2.9", "203.0.113.50")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Total != 5 || len(view.Bans) != 5 ||
		view.Settings.Minutes != 5 || !slices.Equal(view.Settings.Exempt, []string{"203.0.113.50", "2001:db8::1", "2001:db8::/32", "192.0.2.9"}) {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if event := f.lastAuditEvent(t); event.Action != "security.client_bans" || event.ResourceType != "security" || string(event.Detail) != `{"exempt":["203.0.113.50","2001:db8::1","2001:db8::/32","192.0.2.9"],"minutes":5}` {
		t.Fatalf("audit: %s %s", event.Action, event.Detail)
	}
	if got := f.bans(t, "").Settings; got.Minutes != 5 || len(got.Exempt) != 4 {
		t.Fatalf("saved: %+v", got)
	}
	f.as("198.51.100.1", "app.example.com", "/.env")
	if b := f.bans(t, "").Bans[0]; b.Client != "198.51.100.1" || b.Until.Sub(b.Since) != 5*time.Minute {
		t.Fatalf("new ban: %+v", b)
	}
	// What the Outlet starts with is what was saved.
	f.store.bans.config.Store(nil)
	if err := f.store.reloadClientBans(context.Background()); err != nil {
		t.Fatal(err)
	}
	if config := f.store.bans.settings(); config.duration != 5*time.Minute || len(config.exempt) != 4 {
		t.Fatalf("reloaded: %+v", config)
	}

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("10.0.0.%d", i)
	}
	for _, c := range []struct {
		minutes int
		exempt  []string
	}{
		{0, nil}, {-5, nil}, {10081, nil}, {60, tooMany}, {60, []string{"10.0.0"}}, {60, []string{"example.com"}}, {60, []string{""}}, {60, []string{"10.0.0.1/33"}},
		{60, []string{"fe80::1%eth0"}}, {60, []string{"10.0.0.1/24/8"}}, {60, []string{strings.Repeat("1", 65)}},
	} {
		if w := f.setBans(t, c.minutes, c.exempt...); w.Code != 400 || errorCode(t, w) != "invalid_client_bans" {
			t.Fatalf("%d %v: %d %s", c.minutes, c.exempt, w.Code, w.Body.String())
		}
	}
	for _, body := range []map[string]any{{"minutes": 60}, {"exempt": []string{}}, {"minutes": 60, "exempt": nil}, {"minutes": "60", "exempt": []string{}}, {"minutes": 60, "exempt": []string{}, "extra": 1}} {
		if w := request(h, "PUT", "/api/v1/security/client-bans", f.admin, body); w.Code != 400 || errorCode(t, w) != "invalid_request" {
			t.Fatalf("%v: %d %s", body, w.Code, w.Body.String())
		}
	}
	if w := f.setBans(t, 10080, tooMany[:50]...); w.Code != 200 {
		t.Fatalf("the most: %d %s", w.Code, w.Body.String())
	}
	if w := f.setBans(t, 1); w.Code != 200 {
		t.Fatalf("the least: %d %s", w.Code, w.Body.String())
	}
	if got := f.bans(t, "").Settings; got.Minutes != 1 || got.Exempt == nil || len(got.Exempt) != 0 {
		t.Fatalf("saved: %+v", got)
	}

	// Without an administrator nothing is read or changed.
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/security/client-bans", nil},
		{"PUT", "/api/v1/security/client-bans", map[string]any{"minutes": 1, "exempt": []string{}}},
		{"POST", "/api/v1/security/client-bans/release", map[string]any{"all": true}},
	} {
		for _, token := range []string{"", "wrong"} {
			if w := request(h, c.method, c.path, token, c.body); w.Code != 401 {
				t.Fatalf("%s %s with %q: %d", c.method, c.path, token, w.Code)
			}
		}
	}
	if f.bans(t, "").Total == 0 {
		t.Fatal("an unauthorized release changed the bans")
	}
}

// A rule that bans is tried before the others, so the honeypot wins over a
// broad rule that comes first; rules without ban only refuse; and a
// honeypot the administrator deleted stays deleted.
func TestBanRulesComeFirst(t *testing.T) {
	f := newBanFixture(t)
	h := f.store.Handler()
	view := func() blockRulesView {
		t.Helper()
		var v blockRulesView
		if w := request(h, "GET", "/api/v1/security/block-rules", f.admin, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatalf("rules: %d %s", w.Code, w.Body.String())
		}
		return v
	}
	rules := view().Rules
	// Broad rule first, the honeypot after it, and one of the administrator's
	// own that bans, for one address only.
	rules = []control.BlockRule{rules[1], rules[0], {ID: "mine", Name: "Mine", Enabled: true, Ban: true, Hosts: []string{"other.example.com"},
		Conditions: []control.BlockCondition{{Operator: control.BlockStartsWith, Value: "/secret-admin"}}}}
	if w := request(h, "PUT", "/api/v1/security/block-rules", f.admin, map[string]any{"rules": rules}); w.Code != 200 {
		t.Fatalf("rules: %d %s", w.Code, w.Body.String())
	}
	if got := view().Rules; !got[1].Ban || !got[2].Ban || got[0].Ban {
		t.Fatalf("ban lost: %+v", got)
	}

	f.as("203.0.113.1", "app.example.com", "/.env")
	f.as("203.0.113.2", "app.example.com", "/.hidden")
	f.as("203.0.113.3", "app.example.com", "/secret-admin")
	f.as("203.0.113.4", "other.example.com", "/secret-admin/login")
	if got := clients(f.bans(t, "")); !slices.Equal(got, []string{"203.0.113.4", "203.0.113.1"}) {
		t.Fatalf("bans: %v", got)
	}
	if b := f.bans(t, "").Bans[0]; b.Rule != "mine" || b.Host != "other.example.com" || b.Path != "/secret-admin/login" {
		t.Fatalf("ban: %+v", b)
	}
	want := []string{
		"trapped 403 [] app.example.com /.env",
		"blocked 403 [] app.example.com /.hidden",
		"trapped 403 [] other.example.com /secret-admin/login",
	}
	if got := failureLines(f.failureLog(t, "").Entries); !slices.Equal(got, want) {
		t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	hits := view().Hits
	if hits["honeypot"] != 1 || hits["hidden-files"] != 1 || hits["mine"] != 1 {
		t.Fatalf("hits: %v", hits)
	}

	// Deleting the honeypot sticks: the path is only blocked, nobody banned.
	f.release(map[string]any{"all": true})
	if w := request(h, "PUT", "/api/v1/security/block-rules", f.admin, map[string]any{"rules": rules[:1]}); w.Code != 200 {
		t.Fatalf("rules: %d %s", w.Code, w.Body.String())
	}
	if code := f.as("203.0.113.1", "app.example.com", "/.env"); code != 403 {
		t.Fatalf("blocked: %d", code)
	}
	if code := f.as("203.0.113.1", "app.example.com", "/"); code != 200 || f.bans(t, "").Total != 0 {
		t.Fatalf("after: %d", code)
	}
	// A disabled ban rule bans nobody either.
	rules[1].Enabled = false
	if w := request(h, "PUT", "/api/v1/security/block-rules", f.admin, map[string]any{"rules": rules[:2]}); w.Code != 200 {
		t.Fatalf("rules: %d %s", w.Code, w.Body.String())
	}
	f.as("203.0.113.1", "app.example.com", "/.env")
	if f.bans(t, "").Total != 0 {
		t.Fatal("a disabled rule banned a client")
	}
}

// Requests look clients up while traps add bans and an administrator
// releases them and lists them; run with -race.
func TestBanTableConcurrentUse(t *testing.T) {
	var b banState
	now := time.Now()
	key := func(i int) netip.Prefix {
		return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}), 32)
	}
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			for i := range 2000 {
				k := key(i)
				b.add(k, "honeypot", "app.example.com", "/.env", now, time.Hour)
				if found := b.find(k.Addr().String(), now); found != nil {
					found.hits.Add(1)
				}
				b.find(key(i+worker).Addr().String(), now.Add(2*time.Hour))
				switch i % 50 {
				case 0:
					b.release(k, now)
				case 1:
					b.sweep(now.Add(time.Duration(i) * time.Second))
				case 2:
					b.list(now, "10.0", 0, 20)
				case 3:
					b.releaseWhere(now, func(e *ban) bool { return e.key == key(i-1) })
				}
			}
		})
	}
	wg.Wait()
	total, page := b.list(now, "", 0, maxBanPage)
	if int64(total) != b.held.Load() || total > 2000 || len(page) > maxBanPage {
		t.Fatalf("%d listed, %d held", total, b.held.Load())
	}
}
