package outlet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

func accessLogging(t *testing.T, f proxyFixture, enabled bool) {
	t.Helper()
	if w := request(f.store.Handler(), "PUT", "/api/v1/observe/access-log", f.admin, map[string]any{"enabled": enabled, "max_mib": 64, "retention_days": 7}); w.Code != http.StatusOK {
		t.Fatalf("access log settings: %d %s", w.Code, w.Body.String())
	}
}

func assertRequestID(t *testing.T, id string) {
	t.Helper()
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Fatalf("request ID %q is not a server-generated 128-bit ID", id)
	}
}

func TestRequestEvidenceCorrelatesLogsAndExcludesSecrets(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.route(t, 0, "app.example.com")
	f.requestLogging(t, true)
	f.failureLogging(t, true)
	accessLogging(t, f, true)
	var token struct {
		Token accessTokenView `json:"token"`
		Value string          `json:"value"`
	}
	w := request(f.store.Handler(), "POST", "/api/v1/security/access-tokens", f.admin, map[string]any{"name": "CI", "hosts": []string{"app.example.com"}, "ranges": []string{}, "expires_days": 0})
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &token) != nil {
		t.Fatalf("token: %d %s", w.Code, w.Body.String())
	}
	policies := control.AccessPolicies{Policies: []control.AccessPolicy{{ID: "ci", Name: "CI", Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}, Conditions: conditions(withToken(token.Token.ID))}}}
	if _, err := f.db.SetAccessPolicies(context.Background(), policies, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.reloadAccess(context.Background()); err != nil {
		t.Fatal(err)
	}
	probe := func(ip string) {
		t.Helper()
		r := httptest.NewRequest("GET", "/backup/%2egit/packed-refs?secret=query-secret", nil)
		r.Host = "app.example.com"
		r.Header.Set("X-Forwarded-For", ip)
		r.Header.Set(accessTokenHeader, token.Value)
		r.Header.Set("Authorization", "Bearer auth-secret")
		r.Header.Set("Cookie", "session=cookie-secret")
		r.Header.Set("X-Request-ID", "client-forged-request-id")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("trap: %d", w.Code)
		}
	}
	probe("203.0.113.1")
	requests := f.requestLog(t, "").Entries
	failures := f.failureLog(t, "").Entries
	access := readLog[accessEntry](t, f, "access-log", "").Entries
	if len(requests) != 1 || len(failures) != 1 || len(access) != 1 {
		t.Fatalf("expected one entry per log: %d/%d/%d", len(requests), len(failures), len(access))
	}
	id := requests[0].RequestID
	assertRequestID(t, id)
	for _, e := range []requestEvidence{requests[0].requestEvidence, failures[0].requestEvidence, access[0].requestEvidence} {
		if e.RequestID != id || e.EscapedPath != "/backup/%2egit/packed-refs" || e.PathTruncated || e.EscapedPathTruncated {
			t.Fatalf("correlation/escaped evidence: %+v", e)
		}
	}
	if e := requests[0]; e.AccessDecision != "passed" || e.AccessReason != "bypass" || e.AccessPolicy != "ci" || e.BlockRule != control.HoneypotRuleID || e.Path != "/backup/.git/packed-refs" {
		t.Fatalf("request decisions: %+v", e)
	}
	if e := failures[0]; e.BlockRule != control.HoneypotRuleID || e.Cause != failTrapped {
		t.Fatalf("refusal attribution: %+v", e)
	}
	for _, value := range []any{requests, failures, access} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"query-secret", "auth-secret", "cookie-secret", "client-forged-request-id", token.Value} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("private request data retained: %q", secret)
			}
		}
	}
	q := "?q=" + url.QueryEscape(id)
	if f.requestLog(t, q).Total != 1 || f.failureLog(t, q).Total != 1 || readLog[accessEntry](t, f, "access-log", q).Total != 1 {
		t.Fatal("request ID is not searchable in every log")
	}
	rows, header := download(t, f, "/api/v1/observe/request-log/export"+q)
	if len(rows) != 1 {
		t.Fatalf("CSV query rows: %d", len(rows))
	}
	values := map[string]string{}
	for i, name := range header {
		values[name] = rows[0][i]
	}
	if values["request_id"] != id || values["escaped_path"] != requests[0].EscapedPath || values["block_rule"] != control.HoneypotRuleID || values["access_policy"] != "ci" || values["path_truncated"] != "false" {
		t.Fatalf("CSV lost evidence: %+v", values)
	}
	// A later refusal names the rule that made the ban, even on another path.
	f.visit("GET", "app.example.com", "/", "203.0.113.1", "curl")
	banned := f.requestLog(t, "").Entries[0]
	if banned.BlockRule != control.HoneypotRuleID || banned.Cause != failBanned || banned.RequestID == id || banned.AccessDecision != "" {
		t.Fatalf("ban attribution: %+v", banned)
	}
	// Correlation still works when the broad request log is disabled.
	f.requestLogging(t, false)
	probe("203.0.113.2")
	failure := f.failureLog(t, "").Entries[0]
	decision := readLog[accessEntry](t, f, "access-log", "").Entries[0]
	assertRequestID(t, failure.RequestID)
	if failure.RequestID != decision.RequestID || failure.RequestID == id || f.requestLog(t, "").Total != 2 {
		t.Fatalf("request-log-off correlation: %+v %+v", failure, decision)
	}
	// An access log alone still gets a fresh server ID.
	f.failureLogging(t, false)
	probe("203.0.113.3")
	onlyAccess := readLog[accessEntry](t, f, "access-log", "").Entries[0]
	assertRequestID(t, onlyAccess.RequestID)
	if onlyAccess.RequestID == decision.RequestID || f.failureLog(t, "").Total != 3 {
		t.Fatalf("access-only evidence: %+v", onlyAccess)
	}
}

func TestRequestLogIncludesAnonymousBypassDecision(t *testing.T) {
	f := newAccessFixture(t, control.AccessPolicy{ID: "public", Name: "Public API", Enabled: true, Action: control.AccessBypass, Hosts: []string{"app.example.com"}})
	f.requestLogging(t, true)
	accessLogging(t, f.proxyFixture, true)
	if w := f.visit("app.example.com", "/api"); w.Code != http.StatusOK {
		t.Fatalf("anonymous bypass: %d", w.Code)
	}
	e := f.requestLog(t, "").Entries[0]
	if e.AccessDecision != "passed" || e.AccessReason != "bypass" || e.AccessPolicy != "public" {
		t.Fatalf("anonymous bypass evidence: %+v", e)
	}
	if view := readLog[accessEntry](t, f.proxyFixture, "access-log", ""); view.Total != 0 {
		t.Fatalf("anonymous bypass changed access log budgets: %+v", view)
	}
}

func TestEscapedEvidenceRetainsInvalidUTF8Encoding(t *testing.T) {
	s := &Store{}
	s.logs.requests = &outletLog[requestEntry]{}
	s.logs.requests.enabled.Store(true)
	r := httptest.NewRequest("GET", "/%FF?secret=private", nil)
	r = s.evidenceRequest(r)
	e := evidenceOf(r)
	if e.EscapedPath != "/%FF" || e.PathTruncated || e.EscapedPathTruncated {
		t.Fatalf("escaped original bytes lost: %+v", e)
	}
}

func TestLoggingEnabledMidRequestAppliesToNextRequest(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	f := newProxyFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		w.WriteHeader(http.StatusInternalServerError)
	}))
	f.route(t, 0, "app.example.com")
	finished := make(chan int, 1)
	go func() { finished <- f.visit("GET", "app.example.com", "/first", "203.0.113.9", "curl").Code }()
	<-arrived
	f.requestLogging(t, true)
	f.failureLogging(t, true)
	accessLogging(t, f, true)
	close(release)
	if status := <-finished; status != http.StatusInternalServerError {
		t.Fatalf("first request: %d", status)
	}
	if f.requestLog(t, "").Total != 0 || f.failureLog(t, "").Total != 0 || readLog[accessEntry](t, f, "access-log", "").Total != 0 {
		t.Fatal("enabling logs midway emitted an entry without shared evidence")
	}
	if w := f.visit("GET", "app.example.com", "/next", "203.0.113.9", "curl"); w.Code != http.StatusInternalServerError {
		t.Fatalf("next request: %d", w.Code)
	}
	requests := f.requestLog(t, "").Entries
	failures := f.failureLog(t, "").Entries
	if len(requests) != 1 || len(failures) != 1 || requests[0].Path != "/next" || requests[0].RequestID != failures[0].RequestID {
		t.Fatalf("next request correlation: %+v %+v", requests, failures)
	}
	assertRequestID(t, requests[0].RequestID)
}

func TestEarlyRefusalEvidenceIsBoundedAndCorrelated(t *testing.T) {
	f := newProxyFixture(t, echo("app"))
	f.route(t, 0, "app.example.com")
	f.route(t, 0, "limited.example.com")
	f.requestLogging(t, true)
	f.failureLogging(t, true)
	f.limitHost(t, "limited.example.com")
	for _, tc := range []struct {
		path          string
		pathTruncated bool
	}{
		{"/" + strings.Repeat("a", maxPublicPath), true},
		{"/" + strings.Repeat("%61", maxPublicPath/2), true},
		{"/" + strings.Repeat("%61", maxEvidencePath/2), false},
	} {
		w := f.visit("GET", "app.example.com", tc.path+"?secret=must-not-log", "203.0.113.5", "curl")
		want := http.StatusRequestURITooLong
		if !tc.pathTruncated {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Fatalf("path request: %d, want %d", w.Code, want)
		}
		e := f.requestLog(t, "").Entries[0]
		assertRequestID(t, e.RequestID)
		if len(e.EscapedPath) > maxEvidencePath || len(e.Path) > maxEvidencePath || !e.EscapedPathTruncated || e.PathTruncated != tc.pathTruncated || strings.Contains(e.EscapedPath, "must-not-log") {
			t.Fatalf("bounded path evidence: %+v", e)
		}
		if want == http.StatusRequestURITooLong {
			if failure := f.failureLog(t, "").Entries[0]; failure.RequestID != e.RequestID || failure.EscapedPath != e.EscapedPath || e.AccessDecision != "" {
				t.Fatalf("early refusal correlation: %+v %+v", e, failure)
			}
		}
	}
	f.visit("GET", "limited.example.com", "/", "203.0.113.5", "curl")
	if w := f.visit("GET", "limited.example.com", "/", "203.0.113.5", "curl"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate refusal: %d", w.Code)
	}
	e := f.requestLog(t, "").Entries[0]
	if failure := f.failureLog(t, "").Entries[0]; e.RequestID != failure.RequestID || e.Cause != failRateLimited {
		t.Fatalf("rate refusal correlation: %+v %+v", e, failure)
	}
	// Disabled logs do not allocate a context or generate a useless ID.
	s := &Store{}
	r := httptest.NewRequest("GET", "/", nil)
	if next := s.evidenceRequest(r); next != r || recordOf(next) != nil {
		t.Fatal("disabled logs created evidence")
	}
}

func TestUnavailableRouteIsTemporaryAndAccessRunsFirst(t *testing.T) {
	f := newAccessFixture(t)
	f.requestLogging(t, true)
	f.failureLogging(t, true)
	f.setStatus(0, "down")
	w := f.visit("app.example.com", "/")
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "5" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("registered unavailable route: %d %v", w.Code, w.Header())
	}
	if e := f.requestLog(t, "").Entries[0]; e.Cause != failNoService || e.Status != http.StatusServiceUnavailable || e.AccessDecision != "passed" || e.AccessReason != "no_policy" {
		t.Fatalf("temporary failure and no-policy evidence: %+v", e)
	}
	f.setPolicies(t, control.AccessPolicy{Enabled: true, Action: control.AccessBlock, Hosts: []string{"app.example.com"}})
	if w := f.visit("app.example.com", "/"); w.Code != http.StatusForbidden || w.Header().Get("Retry-After") != "" {
		t.Fatalf("service availability exposed before access: %d %v", w.Code, w.Header())
	}
	if e := f.requestLog(t, "").Entries[0]; e.AccessDecision != "denied" || e.AccessReason != "blocked" || e.AccessPolicy == "" || e.Cause == failNoService {
		t.Fatalf("access denial evidence: %+v", e)
	}
	if w := f.visit("unknown.example.com", "/"); w.Code != http.StatusNotFound || w.Header().Get("Retry-After") != "" {
		t.Fatalf("unregistered route: %d %v", w.Code, w.Header())
	}
	if f.requestLog(t, "").Total != 2 || f.failureLog(t, "").Total != 1 {
		t.Fatal("unregistered host expanded the logs")
	}
}
