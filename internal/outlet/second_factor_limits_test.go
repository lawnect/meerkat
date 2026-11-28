package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

// challengeFrom logs in over the public handler from ip and returns the
// challenge and the code Telegram got.
func (a alertFixture) challengeFrom(t *testing.T, ip string) (challenge, secret string) {
	t.Helper()
	w := a.publicRequest("POST", "/api/v1/session", ip, chromeOnMac, adminLogin)
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 202 || len(body["challenge"]) != 32 {
		t.Fatalf("challenge from %s: %d %s", ip, w.Code, w.Body.String())
	}
	match := codePattern.FindStringSubmatch(a.telegram.last(t))
	if match == nil {
		t.Fatalf("no code in %q", a.telegram.last(t))
	}
	return body["challenge"], match[1]
}

func (a alertFixture) answer(ip, challenge, code string) *httptest.ResponseRecorder {
	return a.publicRequest("POST", "/api/v1/session/second-factor", ip, chromeOnMac, map[string]any{"challenge": challenge, "code": code})
}

func differentCode(secret string) string {
	if secret == "000000" {
		return "000001"
	}
	return "000000"
}

// Five wrong codes destroy one challenge, and a login makes another, from
// any address: wrong codes are therefore counted for the administrator, over
// all of their challenges. The twentieth in a quarter of an hour locks them
// out of the internet until the window passes; the local port stays open.
func TestSecondFactorWrongCodesAreCountedPerAdministrator(t *testing.T) {
	a, _ := secondFactorFixture(t)
	var live, liveSecret string
	for i := range 5 {
		ip := fmt.Sprintf("198.51.100.%d", 10+i)
		challenge, secret := a.challengeFrom(t, ip)
		live, liveSecret = challenge, secret
		for k := range 4 {
			w := a.answer(ip, challenge, differentCode(secret))
			wantStatus, wantCode := 401, "invalid_second_factor"
			if i == 4 && k == 3 {
				wantStatus, wantCode = 429, "too_many_attempts"
			}
			if w.Code != wantStatus || errorCode(t, w) != wantCode {
				t.Fatalf("challenge %d, wrong code %d: %d %s", i, k, w.Code, w.Body.String())
			}
		}
	}
	// What was left of the last challenge went with the lockout, right code or not.
	if w := a.answer("198.51.100.20", live, liveSecret); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("a challenge of a locked administrator: %d %s", w.Code, w.Body.String())
	}
	sent := len(a.telegram.messages())
	w := a.publicRequest("POST", "/api/v1/session", "198.51.100.40", chromeOnMac, adminLogin)
	if w.Code != 429 || errorCode(t, w) != "too_many_attempts" {
		t.Fatalf("login of a locked administrator: %d %s", w.Code, w.Body.String())
	}
	if wait, err := strconv.Atoi(w.Header().Get("Retry-After")); err != nil || wait < 1 || wait > int(secondFactorWindow.Seconds()) {
		t.Fatalf("Retry-After = %q", w.Header().Get("Retry-After"))
	}
	if len(a.telegram.messages()) != sent {
		t.Fatal("a code was sent to a locked administrator")
	}
	// A wrong password still reads as a wrong password: the lockout is not
	// something to learn without it.
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	if w := a.publicRequest("POST", "/api/v1/session", "198.51.100.41", chromeOnMac, wrong); w.Code != 401 {
		t.Fatalf("wrong password of a locked administrator: %d", w.Code)
	}
	if w := request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin); w.Code != 201 {
		t.Fatalf("the local port: %d %s", w.Code, w.Body.String())
	}
	a.clock.advance(secondFactorWindow)
	if _, secret := a.challengeFrom(t, "198.51.100.42"); secret == "" {
		t.Fatal("no code after the window")
	}
	a.store.background.Wait()
}

// Each login keeps its own code, but only three at a time: the fourth takes
// the place of the oldest.
func TestSecondFactorChallengesPerAdministratorAreCapped(t *testing.T) {
	a, _ := secondFactorFixture(t)
	type pair struct{ challenge, secret string }
	var issued []pair
	for range 4 {
		challenge, secret := a.challengeFrom(t, clientIP)
		issued = append(issued, pair{challenge, secret})
	}
	if held := len(a.store.challenges.items); held != maxChallengesPerAdmin {
		t.Fatalf("%d challenges held after four logins", held)
	}
	if w := a.answer(clientIP, issued[0].challenge, issued[0].secret); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("the oldest of four challenges: %d %s", w.Code, w.Body.String())
	}
	for _, p := range issued[1:] {
		if w := a.answer(clientIP, p.challenge, p.secret); w.Code != 201 {
			t.Fatalf("a challenge within the cap: %d %s", w.Code, w.Body.String())
		}
	}
	a.store.background.Wait()
}

// Someone with a password can start as many logins as they like; they must
// not be able to push another administrator's pending code out.
func TestChallengesOfOneAdministratorNeverEvictAnothers(t *testing.T) {
	var c challenges
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	other := control.Admin{ID: "m_other", Email: "other@example.com"}
	mine := control.Admin{ID: "m_mine", Email: "mine@example.com"}
	waiting, err := c.create(other, "111111", now)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 * maxChallenges {
		now = now.Add(time.Second)
		if _, err := c.create(mine, fmt.Sprintf("%06d", i), now); err != nil {
			t.Fatal(err)
		}
	}
	if admin, err := c.verify(waiting, "111111", now); err != nil || admin.ID != other.ID {
		t.Fatalf("another administrator's challenge: %+v %v", admin, err)
	}
	held := 0
	for _, item := range c.items {
		if item.admin.ID == mine.ID {
			held++
		}
	}
	if held != maxChallengesPerAdmin {
		t.Fatalf("%d challenges held for one administrator", held)
	}
}

// The code message says where the login came from, so an administrator who
// gets a code they didn't ask for can see whose it is.
func TestLoginCodeMessageNamesTheRequestingAddress(t *testing.T) {
	a, _ := secondFactorFixture(t)
	for ip, want := range map[string]string{clientIP: "203.0.113.7", "2001:DB8:0:0::0:1": "2001:db8::1"} {
		a.challengeFrom(t, ip)
		message := a.telegram.last(t)
		if !strings.Contains(message, "\nFrom: <code>"+want+"</code>") || !strings.HasSuffix(message, "If this wasn&#39;t you, change your password.") {
			t.Fatalf("message for %s: %q", ip, message)
		}
	}
}

// Behind Docker's bridge, internet clients and the SSH-forwarded admin port
// can show the same address. Their limits are separate: bad internet logins
// don't lock the operator out of the local port, and the reverse.
func TestPublicLoginFailuresDoNotLockTheLocalPort(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	h := a.store.Handler()
	a.clock.advance(loginWindow)
	// The address httptest gives local requests.
	const shared = "192.0.2.1"
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	for i := range loginAttempts {
		if w := a.publicRequest("POST", "/api/v1/session", shared, chromeOnMac, wrong); w.Code != 401 {
			t.Fatalf("internet login %d: %d", i, w.Code)
		}
	}
	if w := a.publicRequest("POST", "/api/v1/session", shared, chromeOnMac, wrong); w.Code != 429 {
		t.Fatalf("internet limit: %d", w.Code)
	}
	if w := request(h, "POST", "/api/v1/session", "", adminLogin); w.Code != 201 {
		t.Fatalf("local login behind the same address: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/setup/recover", a.admin, adminLogin); w.Code != 200 {
		t.Fatalf("recovery behind the same address: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
}

func TestLocalLoginFailuresDoNotLockTheInternet(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	h := a.store.Handler()
	a.clock.advance(loginWindow)
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	for i := range loginAttempts {
		if w := request(h, "POST", "/api/v1/session", "", wrong); w.Code != 401 {
			t.Fatalf("local login %d: %d", i, w.Code)
		}
	}
	if w := request(h, "POST", "/api/v1/session", "", wrong); w.Code != 429 {
		t.Fatalf("local limit: %d", w.Code)
	}
	if w := a.publicRequest("POST", "/api/v1/session", "192.0.2.1", chromeOnMac, adminLogin); w.Code != 201 {
		t.Fatalf("internet login behind the same address: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
}

// A limiter that is full refuses a key it hasn't seen rather than forget one
// it has: forgetting is how rotating through enough addresses went unlimited.
func TestAttemptLimiterRefusesNewKeysWhenFull(t *testing.T) {
	var l attemptLimiter
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for i := range maxLoginKeys {
		if !l.allow(fmt.Sprint("client ", i), now) {
			t.Fatalf("client %d refused before the table was full", i)
		}
	}
	if l.allow("one more", now) || len(l.hits) != maxLoginKeys || l.keys.Len() != maxLoginKeys {
		t.Fatalf("a full table admitted a new key (%d held)", len(l.hits))
	}
	if _, ok := l.hits["client 0"]; !ok {
		t.Fatal("a held key was forgotten")
	}
	// Held keys go on being counted, up to their limit.
	n := 0
	for l.allow("client 0", now.Add(time.Second)) {
		if n++; n > loginAttempts {
			t.Fatal("a held key was never limited")
		}
	}
	if n != loginAttempts-1 {
		t.Fatalf("a held key allowed %d more tries", n)
	}
	// Keys out of the window are reclaimed, making room.
	if !l.allow("one more", now.Add(loginWindow+time.Second)) || len(l.hits) != 1 {
		t.Fatalf("after the window: %d keys held", len(l.hits))
	}
}

// The local admin port keeps room of its own in a table the internet filled.
func TestLocalLoginKeysKeepTheirRoom(t *testing.T) {
	var l attemptLimiter
	now := time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for i := range maxLoginKeys {
		l.allow(fmt.Sprint("client ", i), now)
	}
	if !l.allow(localKey("192.0.2.1"), now) {
		t.Fatal("the local port was refused in a full table")
	}
	if l.allow("another client", now) {
		t.Fatal("an internet client was admitted past the limit")
	}
	if localKey("192.0.2.1") == clientKey("192.0.2.1") {
		t.Fatal("local and internet clients share a key")
	}
	// httptest gives a local request this address.
	if loginKey(httptest.NewRequest("GET", "/", nil)) != localKey("192.0.2.1") {
		t.Fatal("a local request is not counted as local")
	}
	public := httptest.NewRequest("GET", "/", nil)
	public = public.WithContext(context.WithValue(public.Context(), originKey{}, publicOrigin{ip: "192.0.2.1"}))
	if loginKey(public) != clientKey("192.0.2.1") {
		t.Fatal("an internet request is not counted as one")
	}
}
