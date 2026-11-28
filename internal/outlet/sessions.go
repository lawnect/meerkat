package outlet

import (
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	// A cached session is checked against the database again after this, so
	// a change made outside the Outlet still takes effect.
	sessionRecheck    = time.Minute
	maxCachedSessions = 1024
)

type cachedSession struct {
	admin   control.Admin
	expires time.Time
	checked time.Time
}

// sessionCache remembers sessions the database confirmed, keyed by token
// hash, so admin requests skip the lookup. Unknown tokens are never cached.
//
// A request can read a session from the database just before logout revokes
// it and cache it just after. revoke therefore advances a generation, and a
// lookup is only cached if no revocation happened while it ran.
type sessionCache struct {
	mu         sync.RWMutex
	items      map[string]cachedSession
	generation uint64
}

// begin is the generation a lookup starts in; pass it to put.
func (c *sessionCache) begin() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.generation
}

func (c *sessionCache) get(token string, now time.Time) (control.Admin, bool) {
	c.mu.RLock()
	item, ok := c.items[Hash(token)]
	c.mu.RUnlock()
	if !ok || !now.Before(item.expires) || now.Sub(item.checked) >= sessionRecheck {
		return control.Admin{}, false
	}
	return item.admin, true
}

func (c *sessionCache) put(token string, admin control.Admin, expires, now time.Time, generation uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	if c.items == nil || len(c.items) >= maxCachedSessions {
		c.items = map[string]cachedSession{}
	}
	c.items[Hash(token)] = cachedSession{admin: admin, expires: expires, checked: now}
}

func (c *sessionCache) drop(token string) {
	c.mu.Lock()
	delete(c.items, Hash(token))
	c.mu.Unlock()
}

// revoke drops the token after the database has revoked it, and keeps
// lookups already running from caching it again.
func (c *sessionCache) revoke(token string) {
	c.mu.Lock()
	delete(c.items, Hash(token))
	c.generation++
	c.mu.Unlock()
}

// revokeAdmins is revoke for every session of these administrators. The cache
// doesn't know whose token is whose, so it drops by the administrator each
// entry holds.
func (c *sessionCache) revokeAdmins(ids []string) {
	c.mu.Lock()
	for key, item := range c.items {
		if slices.Contains(ids, item.admin.ID) {
			delete(c.items, key)
		}
	}
	c.generation++
	c.mu.Unlock()
}

// revokeAll is revoke for every session.
func (c *sessionCache) revokeAll() {
	c.mu.Lock()
	clear(c.items)
	c.generation++
	c.mu.Unlock()
}

// sessionAdmin is the administrator behind the request's session cookie.
func (s *Store) sessionAdmin(r *http.Request) (control.Admin, bool) {
	if s.control == nil {
		return control.Admin{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return control.Admin{}, false
	}
	now := s.now()
	if admin, ok := s.sessions.get(cookie.Value, now); ok {
		return admin, true
	}
	generation := s.sessions.begin()
	admin, expires, err := s.control.Session(r.Context(), cookie.Value)
	if err != nil {
		s.sessions.drop(cookie.Value)
		return control.Admin{}, false
	}
	// The cache lapsed, so this is when a change made behind the Outlet's
	// back (single sign-on from the environment, a restored database) is
	// noticed. An administrator whose sessions all come from the provider is
	// held to the email and domain lists again; one who also has a password
	// is not, as this session may be that one's.
	if admin.SSOOnly && !s.ssoStillListed(r.Context(), admin.Email) {
		_ = s.control.RevokeSession(r.Context(), cookie.Value)
		s.sessions.revoke(cookie.Value)
		return control.Admin{}, false
	}
	s.sessions.put(cookie.Value, admin, expires, now, generation)
	return admin, true
}

// revokeAdminSessions ends every administrator session, the caller's too,
// as ending every access session does for those who hold one.
func (s *Store) revokeAdminSessions(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	// Who asked is read first: afterwards no session vouches for them.
	caller, _ := s.sessionAdmin(r)
	revoked, err := s.control.RevokeAllSessions(r.Context())
	// Even when revoking failed, the next request asks the database.
	s.sessions.revokeAll()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audit(r, "security.admin_sessions_revoke", control.AuditEntry{ActorID: caller.ID, ActorEmail: caller.Email, ResourceType: "security", ResourceID: "admin_sessions", Detail: map[string]any{"sessions": revoked}})
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

// setupComplete stays true once an administrator exists; none can be deleted.
func (s *Store) setupComplete(r *http.Request) (bool, error) {
	if s.setupDone.Load() {
		return true, nil
	}
	_, public := requestOrigin(r)
	complete, err := s.control.SetupComplete(r.Context(), public)
	if err != nil || !complete {
		return false, err
	}
	// Only an administrator ends setup for every origin.
	if admins, err := s.control.SetupComplete(r.Context(), false); err == nil && admins {
		s.setupDone.Store(true)
	}
	return true, nil
}
