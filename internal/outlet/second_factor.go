package outlet

import (
	"cmp"
	"container/list"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	challengeTTL      = 5 * time.Minute
	challengeAttempts = 5
	maxChallenges     = 64
	loginAttempts     = 10
	loginWindow       = time.Minute
	maxLoginKeys      = 4096
	// Internet logins in total, whatever the address: anyone holding an IPv6
	// prefix has more addresses than a per-address limit can count, and each
	// password check costs about 200 ms of CPU.
	publicFailures      = 30
	publicFailureWindow = 15 * time.Minute
	passwordChecks      = 4
)

var (
	errInvalidSecondFactor = errors.New("invalid second factor")
	errTooManyAttempts     = errors.New("too many second factor attempts")
	errChallengeGone       = errors.New("unknown or expired second factor challenge")
)

// originKey marks requests that arrived through PublicHandler. Only that
// handler sets it, in the request context, so no header can forge it.
type originKey struct{}

type publicOrigin struct{ ip string }

// forwardedClient is the address Caddy saw: the last X-Forwarded-For entry,
// which Caddy itself wrote. Earlier entries are client-supplied.
func forwardedClient(r *http.Request) string {
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
		return ip.String()
	}
	return ""
}

// requestOrigin returns the client address and whether the request came
// from the internet. Local admin port requests report their TCP peer.
func requestOrigin(r *http.Request) (ip string, public bool) {
	if origin, ok := r.Context().Value(originKey{}).(publicOrigin); ok {
		return origin.ip, true
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host, false
}

type loginChallenge struct {
	admin    control.Admin
	codeHash string
	expires  time.Time
	failures int
}

// challenges keeps pending second-factor logins in memory only: a restart
// just asks the administrator to log in again.
type challenges struct {
	mu    sync.Mutex
	items map[string]*loginChallenge
}

func randomCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (c *challenges) create(admin control.Admin, secret string, now time.Time) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]*loginChallenge{}
	}
	c.sweep(now)
	c.items[id] = &loginChallenge{admin: admin, codeHash: Hash(secret), expires: now.Add(challengeTTL)}
	return id, nil
}

// sweep drops expired challenges and, past the cap, the soonest to expire.
func (c *challenges) sweep(now time.Time) {
	for id, item := range c.items {
		if !now.Before(item.expires) {
			delete(c.items, id)
		}
	}
	for len(c.items) >= maxChallenges {
		oldest := ""
		for id, item := range c.items {
			if oldest == "" || item.expires.Before(c.items[oldest].expires) {
				oldest = id
			}
		}
		delete(c.items, oldest)
	}
}

func (c *challenges) drop(id string) {
	c.mu.Lock()
	delete(c.items, id)
	c.mu.Unlock()
}

// verify consumes the challenge on success. The fifth wrong code destroys
// it. Unless the challenge is unknown or expired, the administrator it
// belongs to comes back even on failure, for the audit log; callers must
// answer every failure alike.
func (c *challenges) verify(id, secret string, now time.Time) (control.Admin, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[id]
	if !ok || !now.Before(item.expires) {
		delete(c.items, id)
		return control.Admin{}, errChallengeGone
	}
	if subtle.ConstantTimeCompare([]byte(Hash(secret)), []byte(item.codeHash)) == 1 {
		delete(c.items, id)
		return item.admin, nil
	}
	if item.failures++; item.failures >= challengeAttempts {
		delete(c.items, id)
		return item.admin, errTooManyAttempts
	}
	return item.admin, errInvalidSecondFactor
}

// attemptLimiter allows loginAttempts requests per loginWindow for each
// address. Internet logins also share publicFailures wrong passwords per
// publicFailureWindow and passwordChecks password checks at a time; the local
// admin port is exempt so the operator can always get in over SSH.
type attemptLimiter struct {
	// attempts is what one key may do per loginWindow; zero is
	// loginAttempts.
	attempts int
	mu       sync.Mutex
	// hits holds each key's attempts in the window, and keys orders them
	// by their last try, allowed or not, stalest first: keys out of the
	// window leave from the front, and with maxLoginKeys in it the stalest
	// makes room for a new one, so a key that keeps trying never starts
	// over for others arriving.
	hits     map[string]*list.Element
	keys     list.List
	failures []time.Time
	checking int
}

type keyAttempts struct {
	key   string
	times []time.Time
	last  time.Time
}

func (l *attemptLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string]*list.Element{}
	}
	for first := l.keys.Front(); first != nil && now.Sub(first.Value.(*keyAttempts).last) >= loginWindow; first = l.keys.Front() {
		l.drop(first)
	}
	element, ok := l.hits[key]
	if !ok {
		if l.keys.Len() >= maxLoginKeys {
			l.drop(l.keys.Front())
		}
		element = l.keys.PushBack(&keyAttempts{key: key})
		l.hits[key] = element
	}
	mine := element.Value.(*keyAttempts)
	mine.last = now
	l.keys.MoveToBack(element)
	recent := mine.times[:0]
	for _, at := range mine.times {
		if now.Sub(at) < loginWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) >= cmp.Or(l.attempts, loginAttempts) {
		mine.times = recent
		return false
	}
	mine.times = append(recent, now)
	return true
}

// drop forgets a key's attempts; l.mu is held.
func (l *attemptLimiter) drop(element *list.Element) {
	delete(l.hits, element.Value.(*keyAttempts).key)
	l.keys.Remove(element)
}

const (
	// sessionCookieName and deviceCookie carry the __Host- prefix: browsers then
	// accept them only from this exact host, Secure and for Path=/, so an
	// application on a sibling hostname cannot set or shadow them.
	sessionCookieName = "__Host-meerkat_session"
	deviceCookie      = "__Host-meerkat_device"
	deviceTTL         = 90 * 24 * time.Hour
)

// deviceToken signs a trusted-device cookie valid until expires. The cookie
// only marks a browser that has signed in before; it grants nothing else.
func (s *Store) deviceToken(expires time.Time) string {
	stamp := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, s.deviceKey)
	mac.Write([]byte("meerkat-device|" + stamp))
	return stamp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// trustedDevice reports whether the request carries a valid, unexpired
// trusted-device cookie.
func (s *Store) trustedDevice(r *http.Request) bool {
	cookie, err := r.Cookie(deviceCookie)
	if err != nil || len(s.deviceKey) == 0 {
		return false
	}
	stamp, _, _ := strings.Cut(cookie.Value, ".")
	unix, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || !s.now().Before(time.Unix(unix, 0)) {
		return false
	}
	return hmac.Equal([]byte(cookie.Value), []byte(s.deviceToken(time.Unix(unix, 0))))
}

// beginPasswordCheck admits an internet login to the password check, or says
// how long to wait. endPasswordCheck must follow every admitted check. A
// trusted device is exempt from the shared failure limit, which anyone can
// fill with wrong passwords, but not from the cap on checks in progress.
func (l *attemptLimiter) beginPasswordCheck(now time.Time, trusted bool) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.failures[:0]
	for _, at := range l.failures {
		if now.Sub(at) < publicFailureWindow {
			recent = append(recent, at)
		}
	}
	l.failures = recent
	if len(recent) >= publicFailures && !trusted {
		return recent[0].Add(publicFailureWindow).Sub(now), false
	}
	if l.checking >= passwordChecks {
		return time.Second, false
	}
	l.checking++
	return 0, true
}

func (l *attemptLimiter) endPasswordCheck(failed bool, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checking--
	if failed {
		l.failures = append(l.failures, now)
	}
}

// clientKey is the address a per-client limit counts: IPv6 clients are
// grouped by /56, the smallest prefix networks commonly hand out; a single
// /64 already holds more addresses than any limit counts.
func clientKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(56, 128)).String() + "/56"
}

// allowLogin limits password and second-factor guesses per client address.
func (s *Store) allowLogin(w http.ResponseWriter, r *http.Request) bool {
	key, _ := requestOrigin(r)
	if key == "" {
		key, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if s.loginLimit.allow(clientKey(key), s.now()) {
		return true
	}
	w.Header().Set("Retry-After", "60")
	fail(w, r, http.StatusTooManyRequests, "rate_limited")
	return false
}

// startSecondFactor answers a correct password from the internet with a
// challenge and sends the code to Telegram. No session exists yet.
func (s *Store) startSecondFactor(w http.ResponseWriter, r *http.Request, admin control.Admin) {
	telegram, err := s.control.Telegram(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if !telegram.Configured() {
		fail(w, r, 502, "telegram_unreachable")
		return
	}
	secret, err := randomCode()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	id, err := s.challenges.create(admin, secret, s.now())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if err := s.telegramSend(r.Context(), telegram.BotToken, telegram.ChatID, loginCodeNotice(secret, challengeTTL).HTML("")); err != nil {
		s.challenges.drop(id)
		fail(w, r, 502, "telegram_unreachable")
		return
	}
	reply(w, http.StatusAccepted, map[string]string{"second_factor": "telegram", "challenge": id})
}

type secondFactorRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (s *Store) secondFactor(w http.ResponseWriter, r *http.Request) {
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return
	}
	if !s.allowLogin(w, r) {
		return
	}
	var req secondFactorRequest
	if !decode(w, r, &req, "challenge", "code") {
		return
	}
	admin, err := s.challenges.verify(req.Challenge, req.Code, s.now())
	if err != nil {
		// An unknown challenge is anyone's guess and would only let strangers
		// fill the permanent log; a wrong code follows a correct password.
		if admin.ID != "" {
			reason := map[error]string{errTooManyAttempts: "too_many_attempts", errInvalidSecondFactor: "wrong_code"}[err]
			s.audit(r, "session.second_factor_failed", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID, Detail: map[string]any{"reason": reason}})
		}
		if errors.Is(err, errTooManyAttempts) {
			fail(w, r, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		fail(w, r, 401, "invalid_second_factor")
		return
	}
	s.audit(r, "session.second_factor", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID})
	s.finishLogin(w, r, admin, true)
}

// finishLogin starts the session and, without delaying the response, tells
// Telegram about it. The login is audited behind the response, as every
// entry is (see audit).
func (s *Store) finishLogin(w http.ResponseWriter, r *http.Request, admin control.Admin, secondFactor bool) {
	session, ok := s.startLogin(w, r, admin, map[string]any{"method": "password", "second_factor": secondFactor})
	if ok {
		// Logging out of a password session has no provider session to end.
		if _, err := r.Cookie(ssoIDTokenCookie); err == nil {
			http.SetCookie(w, &http.Cookie{Name: ssoIDTokenCookie, Value: "", Path: "/api/v1/session", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
		}
		reply(w, 201, map[string]any{"admin": session.Admin, "expires_at": session.ExpiresAt})
	}
}

// startLogin opens a session for admin, sets its cookies, records the login
// and sends the login notice. On failure it has already answered.
func (s *Store) startLogin(w http.ResponseWriter, r *http.Request, admin control.Admin, detail map[string]any) (control.Session, bool) {
	session, err := s.control.StartSession(r.Context(), admin)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return control.Session{}, false
	}
	s.audit(r, "session.login", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID, Detail: detail})
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: session.Token, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	// The local port is another origin: a cookie set there never reaches the
	// public host, where the shared failure limit applies.
	if _, public := requestOrigin(r); public && len(s.deviceKey) != 0 {
		expires := s.now().Add(deviceTTL)
		http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: s.deviceToken(expires), Path: "/", Expires: expires, MaxAge: int(deviceTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	}
	s.announceLogin(r, admin.Email)
	return session, true
}

// userAgentSummary reduces a User-Agent to "Chrome on macOS" or less.
func userAgentSummary(agent string) string {
	if len(agent) > 512 {
		agent = agent[:512]
	}
	browser, system := "", ""
	for _, candidate := range []struct{ token, name string }{{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"}, {"curl/", "curl"}} {
		if strings.Contains(agent, candidate.token) {
			browser = candidate.name
			break
		}
	}
	for _, candidate := range []struct{ token, name string }{{"Windows", "Windows"}, {"iPhone", "iOS"}, {"iPad", "iOS"}, {"Android", "Android"}, {"Macintosh", "macOS"}, {"CrOS", "ChromeOS"}, {"Linux", "Linux"}} {
		if strings.Contains(agent, candidate.token) {
			system = candidate.name
			break
		}
	}
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	}
	return system
}

// announceLogin records and sends the login notification in the background:
// a slow or broken Telegram must never hold up a login.
func (s *Store) announceLogin(r *http.Request, email string) {
	ip, public := requestOrigin(r)
	agent := userAgentSummary(r.UserAgent())
	s.background.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.sendLoginNotice(ctx, loginNotice(email, ip, !public, agent, s.now()), email)
	})
}
