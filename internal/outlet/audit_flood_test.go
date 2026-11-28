package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

type suppressedDetail struct {
	Count         int            `json:"count"`
	WindowSeconds int            `json:"window_seconds"`
	Actions       map[string]int `json:"actions"`
}

func suppressedRows(entries []control.AuditEntry) (rows []control.AuditEntry) {
	for _, e := range entries {
		if e.Action == "audit.suppressed" {
			rows = append(rows, e)
		}
	}
	return rows
}

// The reproduction of the finding: 256 IPv6 /56 prefixes, ten wrong access
// tokens each, left 2,560 rows in a minute, since each address only counts
// against its own allowance.
func TestRotatingAddressesCannotFloodTheAuditLog(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{Enabled: true, Hosts: []string{"app.example.com"}, AllowedUsers: []string{"@corp.example.com"}})
	clock := &testClock{t: time.Now()}
	f.store.now = clock.now
	send := func(ip string) int {
		r := httptest.NewRequest("GET", "https://app.example.com/", nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set(accessTokenHeader, "junk.secret")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		return w.Code
	}
	for prefix := range 256 {
		for range 10 {
			if code := send(fmt.Sprintf("2001:db8:%x::1", prefix+1)); code != http.StatusUnauthorized {
				t.Fatalf("token from prefix %d: %d", prefix, code)
			}
		}
	}
	count := func(action string) int {
		t.Helper()
		f.store.journal.flush()
		_, total, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 1, Action: action})
		if err != nil {
			t.Fatal(err)
		}
		return total
	}
	if n := count("access.token_failed"); n != anonymousAuditRows {
		t.Fatalf("%d rows for 2560 refused tokens in a minute, want %d", n, anonymousAuditRows)
	}
	clock.advance(anonymousAuditWindow)
	if code := send("198.51.100.99"); code != http.StatusUnauthorized {
		t.Fatalf("a token a window later: %d", code)
	}
	if n := count("access.token_failed"); n != anonymousAuditRows+1 {
		t.Fatalf("%d rows after the window", n)
	}
	events, _, err := f.db.Audit(context.Background(), control.AuditFilter{Limit: 5, Action: "audit.suppressed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("summaries: %+v %v", events, err)
	}
	var detail suppressedDetail
	if json.Unmarshal(events[0].Detail, &detail) != nil || detail.Count != 2560-anonymousAuditRows || detail.Actions["access.token_failed"] != detail.Count {
		t.Fatalf("summary: %s", events[0].Detail)
	}
}

// The same budget covers a provider's refusals at the callback.
func TestSSOCallbackFailuresShareTheBudget(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	idp := newFakeIdP(t)
	a.enableSSO(t, idp, nil)
	denied := func(i int) {
		t.Helper()
		ip := fmt.Sprintf("198.51.%d.%d", 100+i/200, i%200+1)
		start := a.publicRequest("GET", ssoPath+"/start", ip, chromeOnMac, nil)
		location := start.Header().Get("Location")
		if start.Code != http.StatusFound || location == "" {
			t.Fatalf("start %d: %d", i, start.Code)
		}
		state := cookieNamed(start, ssoCookie)
		r := httptest.NewRequest("GET", ssoCallbackPath+"?error=access_denied&state="+state.Value, nil)
		r.Host = a.host
		r.Header.Set("X-Forwarded-For", ip)
		r.AddCookie(state)
		w := httptest.NewRecorder()
		a.store.PublicHandler().ServeHTTP(w, r)
		if ssoError(w) != "provider_denied" {
			t.Fatalf("callback %d: %d %s", i, w.Code, w.Header().Get("Location"))
		}
	}
	for i := range anonymousAuditRows + 20 {
		denied(i)
	}
	failures := func() (n int) {
		for _, e := range a.auditLog(t) {
			if e.Action == "session.login_failed" {
				n++
			}
		}
		return n
	}
	if n := failures(); n != anonymousAuditRows {
		t.Fatalf("%d sign-in failures stored, want %d", n, anonymousAuditRows)
	}
	a.store.background.Wait()
}
