package outlet

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
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

// attemptLimiter allows loginAttempts requests per loginWindow for each address.
type attemptLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *attemptLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	if len(l.hits) > 1024 {
		for other, times := range l.hits {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= loginWindow {
				delete(l.hits, other)
			}
		}
	}
	recent := l.hits[key][:0]
	for _, at := range l.hits[key] {
		if now.Sub(at) < loginWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) >= loginAttempts {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}

// allowLogin limits password and second-factor guesses per client address.
func (s *Store) allowLogin(w http.ResponseWriter, r *http.Request) bool {
	key, _ := requestOrigin(r)
	if key == "" {
		key, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	if s.loginLimit.allow(key, s.now()) {
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
		reason := map[error]string{errTooManyAttempts: "too_many_attempts", errInvalidSecondFactor: "wrong_code", errChallengeGone: "expired_or_unknown"}[err]
		resource := admin.ID
		if resource == "" {
			resource = "unknown"
		}
		s.auditFailure(r, "session.second_factor_failed", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: resource, Detail: map[string]any{"reason": reason}})
		if errors.Is(err, errTooManyAttempts) {
			fail(w, r, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		fail(w, r, 401, "invalid_second_factor")
		return
	}
	if !s.auditedEntry(w, r, "session.second_factor", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID}) {
		return
	}
	s.finishLogin(w, r, admin, true)
}

// finishLogin starts the session and, without delaying the response, tells
// Telegram about it. A session that cannot be audited is revoked at once.
func (s *Store) finishLogin(w http.ResponseWriter, r *http.Request, admin control.Admin, secondFactor bool) {
	session, err := s.control.StartSession(r.Context(), admin)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if !s.auditedEntry(w, r, "session.login", control.AuditEntry{ActorID: admin.ID, ActorEmail: admin.Email, ResourceType: "admin", ResourceID: admin.ID, Detail: map[string]any{"second_factor": secondFactor}}) {
		_ = s.control.RevokeSession(context.WithoutCancel(r.Context()), session.Token)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "meerkat_session", Value: session.Token, Path: "/", Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	s.announceLogin(r, admin.Email)
	reply(w, 201, map[string]any{"admin": session.Admin, "expires_at": session.ExpiresAt})
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
