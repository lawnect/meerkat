package outlet

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// completed takes the sign-in that a start began out of the table, as its
// callback does, so that the next one finds room.
func (f accessFixture) completed(start *httptest.ResponseRecorder) {
	if location, err := url.Parse(start.Header().Get("Location")); err == nil && start.Code == http.StatusFound {
		f.store.access.login.take(location.Query().Get("state"), time.Now())
	}
}

// A few hundred addresses, each starting sign-ins as fast as it is let,
// must not leave everyone else without one: 274 IPv6 /56s at 120 starts a
// minute once filled the table of sign-ins in flight, which refused every
// new one until they expired ten minutes later.
func TestAccessStartsFromManyAddressesLeaveRoomToSignIn(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	for prefix := range 274 {
		// Each is a /56 of its own.
		ip := fmt.Sprintf("2001:db8:%x:%x00::1", prefix/256+1, prefix%256)
		for i := range accessStarts {
			w := f.visitFrom(ip, f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash(fmt.Sprint(prefix, "/", i)))
			if w.Code != http.StatusFound && w.Code != http.StatusServiceUnavailable {
				t.Fatalf("start %d of %s: %d", i, ip, w.Code)
			}
		}
	}
	// Someone else, from an address that never started one, signs in.
	session, back := f.finish(t, f.signIn(t, "app.example.com", "/reports", verified("alice@corp.example.com")))
	if back != "/reports" || session == nil {
		t.Fatalf("sign-in after the flood: %q", back)
	}
}

// Even with the table full of sign-ins, from as many addresses as it takes
// to hold it at the cap of each, a visitor holding none signs in.
func TestAccessSignInSurvivesAFullTable(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	now := time.Now()
	for client := range maxAccessPending / maxAccessPerClient {
		for i := range maxAccessPerClient {
			if !f.store.access.login.put(fmt.Sprint("flood ", client, "/", i), ssoPending{client: fmt.Sprint("client ", client), expires: now.Add(ssoStateTTL)}, now) {
				t.Fatalf("filling: client %d sign-in %d refused", client, i)
			}
		}
	}
	if f.store.access.login.put("one more", ssoPending{client: "another", expires: now.Add(ssoStateTTL)}, now) {
		t.Fatal("the table was not full")
	}
	session, _ := f.finish(t, f.signIn(t, "app.example.com", "/", verified("alice@corp.example.com")))
	if session == nil {
		t.Fatal("no session")
	}
	if n := len(f.store.access.login.pending); n > maxAccessPending {
		t.Fatalf("%d sign-ins held over the capacity %d", n, maxAccessPending)
	}
}

// One client address, which may be an office, keeps so many sign-ins in
// flight and no more; past that it is refused, and the sign-ins it has are
// not replaced by newer ones. Filling the table takes hundreds of addresses.
func TestAccessSignInsInFlightPerClientAreFew(t *testing.T) {
	if maxAccessPerClient*256 > maxAccessPending {
		t.Fatalf("%d addresses holding %d sign-ins each fill the table", maxAccessPending/maxAccessPerClient, maxAccessPerClient)
	}
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	codes := map[int]int{}
	for i := range maxAccessPerClient + 5 {
		codes[f.visitFrom("198.51.100.8", f.adminHost, accessStartPath+"?host=app.example.com&return=/&bind="+Hash(fmt.Sprint("browser ", i))).Code]++
	}
	if codes[http.StatusFound] != maxAccessPerClient || codes[http.StatusServiceUnavailable] != 5 {
		t.Fatalf("starts of one client: %v", codes)
	}
}

// When the table is full, a new sign-in makes room by dropping the oldest of
// the client holding the most; a client at its own cap is refused, whatever
// the others hold, and a client holding few loses nothing.
func TestAccessTableEvictsFromTheHeaviestClient(t *testing.T) {
	st := ssoState{perClient: 4, capacity: 10, keep: true}
	now := time.Now()
	put := func(client string, i int) bool {
		return st.putEvicting(fmt.Sprint(client, i), ssoPending{client: client, expires: now.Add(time.Minute + time.Duration(len(st.pending))*time.Second)}, now)
	}
	held := func(client string) int {
		if mine := st.clients[client]; mine != nil {
			return mine.Len()
		}
		return 0
	}
	for _, c := range []struct {
		client string
		n      int
	}{{"a", 4}, {"b", 4}, {"c", 2}} {
		for i := range c.n {
			if !put(c.client, i) {
				t.Fatalf("%s %d refused with room left", c.client, i)
			}
		}
	}
	if len(st.pending) != 10 {
		t.Fatalf("%d held", len(st.pending))
	}
	// A client at its cap is refused, and nobody loses anything for it.
	if put("b", 9) || len(st.pending) != 10 || held("b") != 4 {
		t.Fatalf("a client at its cap: %d held, b %d", len(st.pending), held("b"))
	}
	// A new one finds room, dropping the oldest sign-in of a client holding
	// four; the one holding two keeps both.
	if !put("d", 0) || held("d") != 1 || held("c") != 2 || held("a")+held("b") != 7 || len(st.pending) != 10 {
		t.Fatalf("new client: a %d b %d c %d d %d, %d held", held("a"), held("b"), held("c"), held("d"), len(st.pending))
	}
	if _, ok := st.take("a0", now); ok {
		t.Fatal("the oldest sign-in of the heaviest client was kept")
	}
	// More of them: the heaviest give way first, and those holding two or
	// fewer keep theirs while one holds more.
	for i := range 3 {
		if !put(fmt.Sprint("new", i), 0) {
			t.Fatalf("new client %d refused", i)
		}
		if held("c") != 2 || held("d") != 1 || len(st.pending) != 10 {
			t.Fatalf("after new client %d: a %d b %d c %d d %d, %d held", i, held("a"), held("b"), held("c"), held("d"), len(st.pending))
		}
	}
}

// With every client holding one, a new sign-in still takes the oldest slot
// rather than being refused.
func TestAccessTableFullOfSingleSignIns(t *testing.T) {
	st := ssoState{perClient: 4, capacity: 5, keep: true}
	now := time.Now()
	for i := range 5 {
		if !st.putEvicting(fmt.Sprint("s", i), ssoPending{client: fmt.Sprint("c", i), expires: now.Add(time.Minute + time.Duration(i)*time.Second)}, now) {
			t.Fatalf("client %d refused", i)
		}
	}
	if !st.putEvicting("late", ssoPending{client: "late", expires: now.Add(2 * time.Minute)}, now) || len(st.pending) != 5 {
		t.Fatalf("late client: %d held", len(st.pending))
	}
	if _, ok := st.take("s0", now); ok {
		t.Fatal("the oldest sign-in was kept")
	}
	if _, ok := st.take("late", now); !ok {
		t.Fatal("the new sign-in was not kept")
	}
}
