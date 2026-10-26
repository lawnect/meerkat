package outlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

type sentMessage struct{ token, chat, text string }

// fakeTelegram stands in for the Bot API. While gate is open (non-nil and not
// closed) every send waits, which lets a test prove a login does not.
type fakeTelegram struct {
	mu   sync.Mutex
	sent []sentMessage
	err  error
	gate chan struct{}
}

func (f *fakeTelegram) send(_ context.Context, token, chat, text string) error {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, sentMessage{token, chat, text})
	return nil
}

func (f *fakeTelegram) fail(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func (f *fakeTelegram) messages() []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentMessage(nil), f.sent...)
}

func (f *fakeTelegram) last(t *testing.T) string {
	t.Helper()
	messages := f.messages()
	if len(messages) == 0 {
		t.Fatal("nothing was sent to Telegram")
	}
	return messages[len(messages)-1].text
}

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type alertFixture struct {
	fixture
	store    *Store
	db       *control.Database
	telegram *fakeTelegram
	clock    *testClock
	host     string
}

var adminLogin = map[string]any{"email": "admin@example.com", "password": "correct horse battery staple"}

func newAlertFixture(t *testing.T) alertFixture {
	t.Helper()
	f := setup(t)
	store, db := controlStore(t, f)
	outlet, err := db.SetDomain(context.Background(), "example.com", "")
	if err != nil {
		t.Fatal(err)
	}
	a := alertFixture{fixture: f, store: store, db: db, telegram: &fakeTelegram{}, clock: &testClock{t: time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)}, host: outlet.AdminHostname}
	store.now, store.sendTelegram = a.clock.now, a.telegram.send
	// Tests never touch the real DNS.
	store.lookupIP = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.1")}}, nil
	}
	// Runs before the database closes: login notifications may still be in flight.
	t.Cleanup(store.background.Wait)
	return a
}

func (a alertFixture) configureTelegram(t *testing.T, verified bool) {
	t.Helper()
	ctx := context.Background()
	if err := a.db.SetTelegram(ctx, testBotToken, "123456", true, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	if verified {
		if _, err := a.db.MarkTelegramVerified(ctx, testBotToken, "123456", a.clock.now()); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonRequest(method, path string, body any) *http.Request {
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, path, &b)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func cookieRequest(h http.Handler, method, path string, cookie *http.Cookie, body any) *httptest.ResponseRecorder {
	r := jsonRequest(method, path, body)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// publicRequest arrives the way Caddy forwards internet traffic.
func (a alertFixture) publicRequest(method, path, forwardedFor, agent string, body any) *httptest.ResponseRecorder {
	r := jsonRequest(method, path, body)
	r.Host = a.host
	r.Header.Set("X-Forwarded-For", forwardedFor)
	r.Header.Set("User-Agent", agent)
	w := httptest.NewRecorder()
	a.store.PublicHandler().ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "meerkat_session" && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("no session cookie in %d %s", w.Code, w.Body.String())
	return nil
}

// adminSession creates the administrator and logs in on the local port.
func (a alertFixture) adminSession(t *testing.T) *http.Cookie {
	t.Helper()
	h := a.store.Handler()
	if w := request(h, "POST", "/api/v1/setup/admin", a.admin, adminLogin); w.Code != 201 {
		t.Fatalf("setup status = %d: %s", w.Code, w.Body.String())
	}
	w := request(h, "POST", "/api/v1/session", "", adminLogin)
	if w.Code != 201 {
		t.Fatalf("login status = %d: %s", w.Code, w.Body.String())
	}
	// The login is announced in the background; let it finish before Telegram is set up.
	a.store.background.Wait()
	return sessionCookie(t, w)
}

func decodeAlerts(t *testing.T, w *httptest.ResponseRecorder) alertsResponse {
	t.Helper()
	var view alertsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || w.Code != 200 {
		t.Fatalf("alerts: %d %s (%v)", w.Code, w.Body.String(), err)
	}
	return view
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

func TestTelegramSettingsAPI(t *testing.T) {
	a := newAlertFixture(t)
	h, admin := a.store.Handler(), a.admin
	var bodies []string
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		w := request(h, method, path, admin, body)
		bodies = append(bodies, w.Body.String())
		return w
	}

	w := call("GET", "/api/v1/alerts", nil)
	view := decodeAlerts(t, w)
	if view.Telegram.Configured || view.Telegram.VerifiedAt != nil || view.LoginSecondFactor || view.Rules != control.DefaultAlertRules() ||
		!strings.Contains(w.Body.String(), `"events":[]`) || !strings.Contains(w.Body.String(), `"verified_at":null`) {
		t.Fatalf("initial alerts: %s", w.Body.String())
	}
	if w := request(h, "GET", "/api/v1/alerts", "wrong", nil); w.Code != 401 {
		t.Fatalf("unauthenticated status = %d", w.Code)
	}
	if w := call("POST", "/api/v1/alerts/telegram/test", nil); w.Code != 409 || errorCode(t, w) != "telegram_not_configured" {
		t.Fatalf("test before configuring: %d %s", w.Code, w.Body.String())
	}

	for name, tc := range map[string]struct {
		body map[string]any
		want string
	}{
		"no token yet":  {map[string]any{"chat_id": "123", "enabled": true}, "invalid_telegram_token"},
		"bad token":     {map[string]any{"bot_token": "nope", "chat_id": "123", "enabled": true}, "invalid_telegram_token"},
		"bad chat":      {map[string]any{"bot_token": testBotToken, "chat_id": "not a chat", "enabled": true}, "invalid_telegram_chat"},
		"missing chat":  {map[string]any{"bot_token": testBotToken, "enabled": true}, "invalid_request"},
		"missing flag":  {map[string]any{"bot_token": testBotToken, "chat_id": "123"}, "invalid_request"},
		"unknown field": {map[string]any{"bot_token": testBotToken, "chat_id": "123", "enabled": true, "extra": 1}, "invalid_request"},
	} {
		if w := call("PUT", "/api/v1/alerts/telegram", tc.body); w.Code != 400 || errorCode(t, w) != tc.want {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}

	w = call("PUT", "/api/v1/alerts/telegram", map[string]any{"bot_token": testBotToken, "chat_id": "-100123", "enabled": true})
	view = decodeAlerts(t, w)
	if !view.Telegram.Configured || !view.Telegram.Enabled || view.Telegram.ChatID != "-100123" || view.Telegram.TokenHint != "b-cd" || view.Telegram.VerifiedAt != nil {
		t.Fatalf("saved: %s", w.Body.String())
	}

	for err, want := range map[error]string{errTelegramChatNotFound: "telegram_chat_not_found", errTelegramUnreachable: "telegram_unreachable", errTelegramUnauthorized: "telegram_unauthorized", errors.New("boom"): "telegram_failed"} {
		a.telegram.fail(err)
		if w := call("POST", "/api/v1/alerts/telegram/test", nil); w.Code != 502 || errorCode(t, w) != want {
			t.Errorf("failing test (%v): %d %s", err, w.Code, w.Body.String())
		}
	}
	if decodeAlerts(t, call("GET", "/api/v1/alerts", nil)).Telegram.VerifiedAt != nil {
		t.Fatal("a failed test verified the bot")
	}
	a.telegram.fail(nil)
	w = call("POST", "/api/v1/alerts/telegram/test", nil)
	view = decodeAlerts(t, w)
	if view.Telegram.VerifiedAt == nil || *view.Telegram.VerifiedAt != "2026-10-26T14:00:00Z" {
		t.Fatalf("verified: %s", w.Body.String())
	}
	if sent := a.telegram.messages(); len(sent) != 1 || sent[0].token != testBotToken || sent[0].chat != "-100123" || !strings.HasPrefix(sent[0].text, "✅ <b>Test notification</b>") {
		t.Fatalf("test message = %+v", sent)
	}

	if w := call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": true}); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"login_second_factor":true}` {
		t.Fatalf("second factor: %d %s", w.Code, w.Body.String())
	}
	// Keeping the token and chat keeps the verification.
	view = decodeAlerts(t, call("PUT", "/api/v1/alerts/telegram", map[string]any{"chat_id": "-100123", "enabled": false}))
	if view.Telegram.Enabled || view.Telegram.VerifiedAt == nil || !view.LoginSecondFactor || view.Telegram.TokenHint != "b-cd" {
		t.Fatalf("kept token: %+v", view)
	}
	// A different chat is unverified, which also switches the second factor off.
	view = decodeAlerts(t, call("PUT", "/api/v1/alerts/telegram", map[string]any{"chat_id": "@meerkat_alerts", "enabled": true}))
	if view.Telegram.VerifiedAt != nil || view.LoginSecondFactor || view.Telegram.ChatID != "@meerkat_alerts" {
		t.Fatalf("changed chat: %+v", view)
	}
	if w := call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": true}); w.Code != 409 || errorCode(t, w) != "telegram_not_verified" {
		t.Fatalf("unverified second factor: %d %s", w.Code, w.Body.String())
	}

	call("POST", "/api/v1/alerts/telegram/test", nil)
	call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": true})
	view = decodeAlerts(t, call("DELETE", "/api/v1/alerts/telegram", nil))
	if view.Telegram != (telegramStatus{}) || view.LoginSecondFactor {
		t.Fatalf("deleted: %+v", view)
	}
	if w := call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": true}); w.Code != 409 {
		t.Fatalf("second factor without a bot: %d", w.Code)
	}
	if w := call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": false}); w.Code != 200 {
		t.Fatalf("turning the second factor off: %d", w.Code)
	}
	for _, body := range bodies {
		if strings.Contains(body, "AAExample") || strings.Contains(body, "bot_token") {
			t.Fatalf("response leaks the bot token: %s", body)
		}
	}
}

func TestAlertRulesAPI(t *testing.T) {
	a := newAlertFixture(t)
	h, admin := a.store.Handler(), a.admin
	rules := func() map[string]any {
		return map[string]any{
			"tunnel_down": map[string]any{"enabled": true, "after_minutes": 10},
			"route_error": map[string]any{"enabled": false},
			"dns_failure": map[string]any{"enabled": true},
			"error_spike": map[string]any{"enabled": true, "threshold_percent": 2.5, "min_requests": 100},
			"login":       map[string]any{"enabled": false},
			"rate_limit":  map[string]any{"enabled": true, "min_blocked": 250},
		}
	}
	w := request(h, "PUT", "/api/v1/alerts/rules", admin, rules())
	view := decodeAlerts(t, w)
	want := control.AlertRules{
		TunnelDown: control.TunnelDownRule{Enabled: true, AfterMinutes: 10}, RouteError: control.ToggleRule{}, DNSFailure: control.ToggleRule{Enabled: true},
		ErrorSpike: control.ErrorSpikeRule{Enabled: true, ThresholdPercent: 2.5, MinRequests: 100}, Login: control.ToggleRule{},
		RateLimit: control.RateLimitAlertRule{Enabled: true, MinBlocked: 250},
	}
	if view.Rules != want {
		t.Fatalf("rules = %+v", view.Rules)
	}
	if got := decodeAlerts(t, request(h, "GET", "/api/v1/alerts", admin, nil)).Rules; got != want {
		t.Fatalf("stored rules = %+v", got)
	}
	for name, mutate := range map[string]func(map[string]any){
		"minutes zero":     func(r map[string]any) { r["tunnel_down"] = map[string]any{"enabled": true, "after_minutes": 0} },
		"minutes too many": func(r map[string]any) { r["tunnel_down"] = map[string]any{"enabled": true, "after_minutes": 1441} },
		"percent too small": func(r map[string]any) {
			r["error_spike"] = map[string]any{"enabled": true, "threshold_percent": 0.01, "min_requests": 50}
		},
		"percent too big": func(r map[string]any) {
			r["error_spike"] = map[string]any{"enabled": true, "threshold_percent": 101, "min_requests": 50}
		},
		"requests zero": func(r map[string]any) {
			r["error_spike"] = map[string]any{"enabled": true, "threshold_percent": 5, "min_requests": 0}
		},
		"requests too many": func(r map[string]any) {
			r["error_spike"] = map[string]any{"enabled": true, "threshold_percent": 5, "min_requests": 1000001}
		},
		"blocked zero":     func(r map[string]any) { r["rate_limit"] = map[string]any{"enabled": true, "min_blocked": 0} },
		"blocked too many": func(r map[string]any) { r["rate_limit"] = map[string]any{"enabled": true, "min_blocked": 1000001} },
	} {
		bad := rules()
		mutate(bad)
		if w := request(h, "PUT", "/api/v1/alerts/rules", admin, bad); w.Code != 400 || errorCode(t, w) != "invalid_alert_rules" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for _, key := range []string{"login", "rate_limit"} {
		incomplete := rules()
		delete(incomplete, key)
		if w := request(h, "PUT", "/api/v1/alerts/rules", admin, incomplete); w.Code != 400 || errorCode(t, w) != "invalid_request" {
			t.Errorf("without %s: %d %s", key, w.Code, w.Body.String())
		}
	}
	if got := decodeAlerts(t, request(h, "GET", "/api/v1/alerts", admin, nil)).Rules; got != want {
		t.Fatalf("a rejected update changed the rules: %+v", got)
	}
}

var codePattern = regexp.MustCompile(`<code>(\d{6})</code>`)

// secondFactorFixture has the second factor on, as an administrator would
// have switched it on from the dashboard.
func secondFactorFixture(t *testing.T) (alertFixture, *http.Cookie) {
	t.Helper()
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	if w := cookieRequest(h, "PUT", "/api/v1/alerts/telegram", cookie, map[string]any{"bot_token": testBotToken, "chat_id": "123456", "enabled": true}); w.Code != 200 {
		t.Fatalf("telegram: %d %s", w.Code, w.Body.String())
	}
	if w := cookieRequest(h, "PUT", "/api/v1/security/second-factor", cookie, map[string]any{"enabled": true}); w.Code != 409 {
		t.Fatalf("second factor before the test message: %d", w.Code)
	}
	if w := cookieRequest(h, "POST", "/api/v1/alerts/telegram/test", cookie, nil); w.Code != 200 {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if w := cookieRequest(h, "PUT", "/api/v1/security/second-factor", cookie, map[string]any{"enabled": true}); w.Code != 200 {
		t.Fatalf("second factor: %d %s", w.Code, w.Body.String())
	}
	return a, cookie
}

const (
	clientIP    = "203.0.113.7"
	chromeOnMac = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
)

// startChallenge logs in over the public handler and returns the challenge and the code Telegram got.
func (a alertFixture) startChallenge(t *testing.T) (challenge, secret string) {
	t.Helper()
	w := a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, adminLogin)
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 202 || body["second_factor"] != "telegram" || len(body["challenge"]) != 32 || len(body) != 2 {
		t.Fatalf("challenge: %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a session was created before the second factor")
	}
	match := codePattern.FindStringSubmatch(a.telegram.last(t))
	if match == nil {
		t.Fatalf("no code in %q", a.telegram.last(t))
	}
	return body["challenge"], match[1]
}

func TestPublicLoginNeedsTelegramCode(t *testing.T) {
	a, _ := secondFactorFixture(t)
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	if w := a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, wrong); w.Code != 401 || errorCode(t, w) != "invalid_credentials" {
		t.Fatalf("wrong password: %d %s", w.Code, w.Body.String())
	}
	sentBefore := len(a.telegram.messages())
	challenge, secret := a.startChallenge(t)
	if !strings.HasPrefix(a.telegram.last(t), "🔑 <b>Login code</b>\nCode: <code>"+secret+"</code>\nExpires in: 5 min") || len(a.telegram.messages()) != sentBefore+1 {
		t.Fatalf("code message = %q", a.telegram.last(t))
	}

	other := "000000"
	if secret == other {
		other = "000001"
	}
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": other}); w.Code != 401 || errorCode(t, w) != "invalid_second_factor" {
		t.Fatalf("wrong code: %d %s", w.Code, w.Body.String())
	}
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": "unknown", "code": secret}); w.Code != 401 || errorCode(t, w) != "invalid_second_factor" {
		t.Fatalf("unknown challenge: %d %s", w.Code, w.Body.String())
	}
	w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret})
	if w.Code != 201 {
		t.Fatalf("right code: %d %s", w.Code, w.Body.String())
	}
	cookie := sessionCookie(t, w)
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie flags = %+v", cookie)
	}
	if w := cookieRequest(a.store.Handler(), "GET", "/api/v1/alerts", cookie, nil); w.Code != 200 {
		t.Fatalf("session does not authenticate: %d", w.Code)
	}
	// A code works once.
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret}); w.Code != 401 {
		t.Fatalf("replayed code: %d", w.Code)
	}
}

func TestSecondFactorLocksOutAfterFiveWrongCodes(t *testing.T) {
	a, _ := secondFactorFixture(t)
	challenge, secret := a.startChallenge(t)
	wrong := "000000"
	if secret == wrong {
		wrong = "000001"
	}
	for i := 1; i <= 5; i++ {
		w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": wrong})
		wantStatus, wantCode := 401, "invalid_second_factor"
		if i == 5 {
			wantStatus, wantCode = 429, "too_many_attempts"
		}
		if w.Code != wantStatus || errorCode(t, w) != wantCode {
			t.Fatalf("attempt %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret})
	if w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("the destroyed challenge still works: %d %s", w.Code, w.Body.String())
	}
}

func TestSecondFactorCodeExpires(t *testing.T) {
	a, _ := secondFactorFixture(t)
	challenge, secret := a.startChallenge(t)
	a.clock.advance(5 * time.Minute)
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret}); w.Code != 401 || errorCode(t, w) != "invalid_second_factor" {
		t.Fatalf("expired code: %d %s", w.Code, w.Body.String())
	}
	challenge, secret = a.startChallenge(t)
	a.clock.advance(4*time.Minute + 59*time.Second)
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret}); w.Code != 201 {
		t.Fatalf("code just before expiry: %d %s", w.Code, w.Body.String())
	}
}

func TestLocalLoginSkipsSecondFactor(t *testing.T) {
	a, _ := secondFactorFixture(t)
	// The Outlet's admin port is only reachable over SSH. Headers cannot make
	// a request look public, or the reverse.
	r := jsonRequest("POST", "/api/v1/session", adminLogin)
	r.Host = a.host
	r.Header.Set("X-Forwarded-For", clientIP)
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	a.store.Handler().ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("local login: %d %s", w.Code, w.Body.String())
	}
	sessionCookie(t, w)
	if w := a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, adminLogin); w.Code != 202 {
		t.Fatalf("public login status = %d", w.Code)
	}
}

func TestSecondFactorFailsClosedWhenTelegramFails(t *testing.T) {
	a, _ := secondFactorFixture(t)
	a.telegram.fail(errTelegramUnreachable)
	w := a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, adminLogin)
	if w.Code != 502 || errorCode(t, w) != "telegram_unreachable" || len(w.Result().Cookies()) != 0 {
		t.Fatalf("unreachable: %d %s", w.Code, w.Body.String())
	}
	if len(a.store.challenges.items) != 0 {
		t.Fatal("a challenge outlived its failed delivery")
	}
	// A wrong password never reaches Telegram, so it cannot reveal anything.
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	if w := a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, wrong); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
}

func TestLoginAttemptsAreRateLimited(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	nobody := map[string]any{"email": "nobody@example.com", "password": "wrong password entirely"}
	for i := 0; i < loginAttempts; i++ {
		if w := request(h, "POST", "/api/v1/session", "", nobody); w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w := request(h, "POST", "/api/v1/session", "", nobody)
	if w.Code != 429 || errorCode(t, w) != "rate_limited" || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("limit: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/session/second-factor", "", map[string]any{"challenge": "x", "code": "000000"}); w.Code != 429 {
		t.Fatalf("second factor shares the limit: %d", w.Code)
	}
	// Another client address is unaffected, and the window slides.
	if w := a.publicRequest("POST", "/api/v1/session", "198.51.100.1", "", nobody); w.Code != 401 {
		t.Fatalf("other address: %d", w.Code)
	}
	a.clock.advance(time.Minute)
	if w := request(h, "POST", "/api/v1/session", "", nobody); w.Code != 401 {
		t.Fatalf("after the window: %d", w.Code)
	}
}

func loginEvents(t *testing.T, a alertFixture) []control.AlertEvent {
	t.Helper()
	events, err := a.db.RecentAlerts(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	var logins []control.AlertEvent
	for _, e := range events {
		if e.Kind == "login" {
			logins = append(logins, e)
		}
	}
	return logins
}

func TestLoginIsAnnouncedWithTrustedAddressOnly(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	a.telegram.fail(nil)
	a.configureTelegram(t, false)
	before := len(a.telegram.messages())

	// The client sent its own X-Forwarded-For; only Caddy's last entry counts.
	w := a.publicRequest("POST", "/api/v1/session", "198.51.100.9, "+clientIP, chromeOnMac, adminLogin)
	if w.Code != 201 {
		t.Fatalf("public login: %d %s", w.Code, w.Body.String())
	}
	a.store.background.Wait()
	want := "🔐 <b>Admin login</b>\nAccount: <code>admin@example.com</code>\nFrom: <code>203.0.113.7</code>\nBrowser: Chrome on macOS\nTime: 2026-10-26 14:00 UTC\n<a href=\"https://" + a.host + "/\">Open in Meerkat</a>"
	if messages := a.telegram.messages(); len(messages) != before+1 || messages[before].text != want {
		t.Fatalf("messages = %+v", messages)
	}
	logins := loginEvents(t, a)
	if len(logins) != 1 || logins[0].Subject != "admin@example.com" || !logins[0].Sent || logins[0].ResolvedAt == nil || logins[0].Error != nil ||
		logins[0].Message != "Admin login: admin@example.com from 203.0.113.7 (Chrome on macOS)" {
		t.Fatalf("login events = %+v", logins)
	}

	// On the local port a forwarded header is not trusted at all.
	r := jsonRequest("POST", "/api/v1/session", adminLogin)
	r.Header.Set("X-Forwarded-For", "198.51.100.9")
	rec := httptest.NewRecorder()
	a.store.Handler().ServeHTTP(rec, r)
	a.store.background.Wait()
	if rec.Code != 201 {
		t.Fatalf("local login: %d", rec.Code)
	}
	local := a.telegram.last(t)
	if !strings.Contains(local, "Via: local admin port") || strings.Contains(local, "198.51.100.9") || strings.Contains(local, "From:") {
		t.Fatalf("local message = %q", local)
	}

	// A failed login is not a login.
	count := len(a.telegram.messages())
	request(a.store.Handler(), "POST", "/api/v1/session", "", map[string]any{"email": "admin@example.com", "password": "wrong password entirely"})
	a.store.background.Wait()
	if len(a.telegram.messages()) != count {
		t.Fatal("a failed login was announced")
	}
}

func TestLoginNotificationCanBeSwitchedOff(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	a.configureTelegram(t, false)
	rules := control.DefaultAlertRules()
	rules.Login.Enabled = false
	if err := a.db.SetAlertRules(context.Background(), rules, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin)
	a.store.background.Wait()
	if len(a.telegram.messages()) != 0 || len(loginEvents(t, a)) != 0 {
		t.Fatal("login announced although the rule is off")
	}
	// Alerts off at the bot level silence everything, including logins.
	rules.Login.Enabled = true
	_ = a.db.SetAlertRules(context.Background(), rules, a.clock.now())
	if err := a.db.SetTelegram(context.Background(), "", "123456", false, a.clock.now()); err != nil {
		t.Fatal(err)
	}
	a.clock.advance(time.Minute)
	request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin)
	a.store.background.Wait()
	if len(a.telegram.messages()) != 0 || len(loginEvents(t, a)) != 0 {
		t.Fatal("login announced although Telegram alerts are disabled")
	}
}

func TestSlowOrBrokenTelegramNeverBlocksLogin(t *testing.T) {
	a := newAlertFixture(t)
	a.adminSession(t)
	a.configureTelegram(t, false)
	a.telegram.gate = make(chan struct{})
	a.telegram.fail(errTelegramUnreachable)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(a.store.Handler(), "POST", "/api/v1/session", "", adminLogin) }()
	select {
	case w := <-done:
		if w.Code != 201 {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("login waited for Telegram")
	}
	close(a.telegram.gate)
	a.store.background.Wait()
	logins := loginEvents(t, a)
	if len(logins) != 1 || logins[0].Sent || logins[0].Error == nil || *logins[0].Error != "telegram_unreachable" {
		t.Fatalf("login events = %+v", logins)
	}
}
