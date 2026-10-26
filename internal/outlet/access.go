package outlet

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/lawnect/meerkat/internal/control"
)

// Access protection: a request for a protected address or path needs a
// session cookie on that address. Without one the browser goes to the
// management host, which signs the person in with the access provider and
// hands the address a one-time ticket; the address swaps it for the cookie.
// The provider then needs one redirect URI, on the management host, however
// many addresses are protected.
const (
	accessCookie       = "__Host-meerkat_access"
	accessStateCookie  = "__Host-meerkat_access_state"
	accessPathPrefix   = "/.meerkat/access/"
	accessStartPath    = "/api/v1/access/start"
	accessCallbackPath = "/api/v1/access/callback"
	accessEmailHeader  = "X-Meerkat-Access-Email"
	accessTicketTTL    = time.Minute
	maxAccessTickets   = 8192
	// maxAccessCookie keeps the session cookie under the 4 KB browsers take.
	maxAccessCookie = 3800
)

// accessRule is one enabled access policy, compiled for matching.
type accessRule struct {
	match  blockRule
	all    bool
	policy control.AccessPolicy
}

type accessConfig struct{ rules []accessRule }

// covering returns the rules that protect a request for host and path.
func (c *accessConfig) covering(host, requestPath string) []*accessRule {
	if c == nil || len(c.rules) == 0 {
		return nil
	}
	p, slashed := rulePath(requestPath)
	var out []*accessRule
	for i := range c.rules {
		if rule := &c.rules[i]; rule.match.covers(host) && (rule.all || rule.match.matches(p, slashed)) {
			out = append(out, rule)
		}
	}
	return out
}

// forHost returns the rules that protect any path of host.
func (c *accessConfig) forHost(host string) []*accessRule {
	if c == nil {
		return nil
	}
	var out []*accessRule
	for i := range c.rules {
		if rule := &c.rules[i]; rule.match.covers(host) {
			out = append(out, rule)
		}
	}
	return out
}

// groups are the group names any rule allows: the only ones a session
// needs to carry.
func (c *accessConfig) groups() map[string]bool {
	out := map[string]bool{}
	if c == nil {
		return out
	}
	for _, rule := range c.rules {
		for _, group := range rule.policy.AllowedGroups {
			out[group] = true
		}
	}
	return out
}

type accessTicket struct {
	identity control.AccessIdentity
	host     string
	returnTo string
	expires  time.Time
}

type accessState struct {
	config atomic.Pointer[accessConfig]
	key    atomic.Pointer[[]byte]
	// login holds sign-ins in flight and the provider's discovery.
	login   ssoState
	mu      sync.Mutex
	tickets map[string]accessTicket
}

type accessIdentityKey struct{}

func compileAccess(policies control.AccessPolicies) *accessConfig {
	config := &accessConfig{}
	for _, policy := range policies.Policies {
		if !policy.Enabled || len(policy.Hosts) == 0 {
			continue
		}
		config.rules = append(config.rules, accessRule{match: compileBlockRule(policy.Hosts, policy.Conditions), all: policy.AllPaths, policy: policy})
	}
	return config
}

func (s *Store) reloadAccess(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	policies, err := s.control.AccessPolicies(ctx)
	if err != nil {
		return err
	}
	s.access.config.Store(compileAccess(policies))
	key, err := s.control.AccessKey(ctx, s.now())
	if err != nil {
		return err
	}
	s.access.key.Store(&key)
	return nil
}

// accessClaims is what a session cookie carries, signed with the access key.
type accessClaims struct {
	Email  string   `json:"e"`
	Groups []string `json:"g,omitempty"`
	Host   string   `json:"h"`
	Expiry int64    `json:"x"`
}

func accessMAC(key []byte, payload string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("meerkat-access\x00" + payload))
	return mac.Sum(nil)
}

func signAccess(key []byte, claims accessClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + base64.RawURLEncoding.EncodeToString(accessMAC(key, payload)), nil
}

func verifyAccess(key []byte, token, host string, now time.Time) (accessClaims, bool) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || len(key) == 0 {
		return accessClaims{}, false
	}
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, accessMAC(key, payload)) {
		return accessClaims{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return accessClaims{}, false
	}
	var claims accessClaims
	if json.Unmarshal(raw, &claims) != nil || claims.Host != host || claims.Email == "" || now.Unix() >= claims.Expiry {
		return accessClaims{}, false
	}
	return claims, true
}

// accessReturn is the path on the protected address to go back to after
// signing in, query included. Anything that could leave the address is "/".
func accessReturn(path string) string {
	if path == "" || len(path) > 2048 || path[0] != '/' || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\#\r\n\t") {
		return "/"
	}
	return path
}

// guardAccess passes a request on when no policy covers it, or when its
// session satisfies every policy that does, carrying the email along for
// the app. Otherwise it answers the request itself, as it does the Outlet's
// own paths under /.meerkat/access/, and returns false.
func (s *Store) guardAccess(w http.ResponseWriter, r *http.Request, host, adminHost string) (*http.Request, bool) {
	if strings.HasPrefix(r.URL.Path, accessPathPrefix) {
		switch r.URL.Path {
		case accessPathPrefix + "finish":
			s.accessFinish(w, r, host)
		case accessPathPrefix + "logout":
			http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
			accessPage(w, r, http.StatusOK, "signedOut")
		default:
			http.NotFound(w, r)
		}
		return r, false
	}
	rules := s.access.config.Load().covering(host, r.URL.Path)
	if len(rules) == 0 {
		return r, true
	}
	identity, ok := s.accessSession(r, host)
	if !ok {
		s.accessChallenge(w, r, host, adminHost)
		return r, false
	}
	admin, checked := false, false
	for _, rule := range rules {
		if rule.policy.AllowAdmins && !checked {
			admin, _ = s.control.IsAdminEmail(r.Context(), identity.Email)
			checked = true
		}
		if !rule.policy.Allows(identity, admin) {
			accessPage(w, r, http.StatusForbidden, "denied")
			return r, false
		}
	}
	return r.WithContext(context.WithValue(r.Context(), accessIdentityKey{}, identity.Email)), true
}

func (s *Store) accessSession(r *http.Request, host string) (control.AccessIdentity, bool) {
	cookie, err := r.Cookie(accessCookie)
	key := s.access.key.Load()
	if err != nil || key == nil {
		return control.AccessIdentity{}, false
	}
	claims, ok := verifyAccess(*key, cookie.Value, host, s.now())
	return control.AccessIdentity{Email: claims.Email, Groups: claims.Groups}, ok
}

// accessChallenge sends a browser navigation to sign in; other requests,
// which a redirect to a sign-in page would only confuse, get 401.
func (s *Store) accessChallenge(w http.ResponseWriter, r *http.Request, host, adminHost string) {
	navigation := (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Header.Get("Upgrade") == "" &&
		(r.Header.Get("Sec-Fetch-Mode") == "navigate" || r.Header.Get("Sec-Fetch-Mode") == "" && strings.Contains(r.Header.Get("Accept"), "text/html"))
	if !navigation || adminHost == "" {
		w.Header().Set("Cache-Control", "no-store")
		accessPage(w, r, http.StatusUnauthorized, "required")
		return
	}
	query := url.Values{"host": {host}, "return": {accessReturn(r.URL.RequestURI())}}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "https://"+adminHost+accessStartPath+"?"+query.Encode(), http.StatusFound)
}

func (s *Store) accessRedirectURI(ctx context.Context) (string, error) {
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return "", err
	}
	if routing.Outlet.AdminHostname == "" {
		return "", errors.New("no management host")
	}
	return "https://" + routing.Outlet.AdminHostname + accessCallbackPath, nil
}

func accessOAuth(provider control.AccessProvider, oidcProvider *oidc.Provider, redirectURI string) *oauth2.Config {
	return &oauth2.Config{ClientID: provider.ClientID, ClientSecret: provider.ClientSecret, Endpoint: oidcProvider.Endpoint(), RedirectURL: redirectURI, Scopes: provider.Scopes}
}

// accessStart begins a sign-in for a protected address. It runs on the
// management host, and only for an address a policy protects, so it can't
// send anyone elsewhere.
func (s *Store) accessStart(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	host, returnTo := strings.ToLower(query.Get("host")), accessReturn(query.Get("return"))
	_, public := requestOrigin(r)
	if s.control == nil || !public || len(s.access.config.Load().forHost(host)) == 0 {
		accessPage(w, r, http.StatusNotFound, "unknown")
		return
	}
	if routing, err := s.control.Routing(r.Context()); err != nil {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	} else if plans, _ := routing.Routes(host); len(plans) == 0 {
		accessPage(w, r, http.StatusNotFound, "unknown")
		return
	}
	provider, err := s.control.AccessProvider(r.Context())
	if err != nil || !provider.Enabled {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	oidcProvider, err := s.discoverWith(&s.access.login, provider.Issuer)
	if err != nil {
		accessPage(w, r, http.StatusBadGateway, "provider")
		return
	}
	redirectURI, err := s.accessRedirectURI(r.Context())
	if err != nil {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	state, nonce, verifier := Token(), Token(), oauth2.GenerateVerifier()
	now := s.now()
	ip, _ := requestOrigin(r)
	if !s.access.login.put(state, ssoPending{nonce: nonce, verifier: verifier, returnTo: returnTo, host: host, client: clientKey(ip), expires: now.Add(ssoStateTTL)}, now) {
		accessPage(w, r, http.StatusServiceUnavailable, "busy")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accessStateCookie, Value: state, Path: "/", MaxAge: int(ssoStateTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, accessOAuth(provider, oidcProvider, redirectURI).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// accessCallback finishes the provider's part and sends the browser to the
// protected address with a one-time ticket for its session.
func (s *Store) accessCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	cookie, _ := r.Cookie(accessStateCookie)
	http.SetCookie(w, &http.Cookie{Name: accessStateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	state := query.Get("state")
	if s.control == nil || cookie == nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		accessPage(w, r, http.StatusBadRequest, "expired")
		return
	}
	pending, ok := s.access.login.take(state, s.now())
	if !ok {
		accessPage(w, r, http.StatusBadRequest, "expired")
		return
	}
	failed := func(status int, page, reason, email string) {
		s.audit(r, "access.login_failed", control.AuditEntry{ActorEmail: attemptedEmail(email), ResourceType: "address", ResourceID: pending.host, Detail: map[string]any{"reason": reason}})
		accessPage(w, r, status, page)
	}
	if query.Get("error") != "" {
		failed(http.StatusForbidden, "denied", "provider_error", "")
		return
	}
	provider, err := s.control.AccessProvider(r.Context())
	if err != nil || !provider.Enabled {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	oidcProvider, err := s.discoverWith(&s.access.login, provider.Issuer)
	if err != nil {
		accessPage(w, r, http.StatusBadGateway, "provider")
		return
	}
	redirectURI, err := s.accessRedirectURI(r.Context())
	if err != nil {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	ctx := s.oidcContext(r.Context())
	token, err := accessOAuth(provider, oidcProvider, redirectURI).Exchange(ctx, query.Get("code"), oauth2.VerifierOption(pending.verifier))
	if err != nil {
		failed(http.StatusBadGateway, "provider", "token_exchange", "")
		return
	}
	raw, _ := token.Extra("id_token").(string)
	idToken, err := oidcProvider.Verifier(&oidc.Config{ClientID: provider.ClientID}).Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(pending.nonce)) != 1 {
		failed(http.StatusBadGateway, "provider", "invalid_token", "")
		return
	}
	var claims ssoClaims
	var all map[string]any
	if idToken.Claims(&claims) != nil || idToken.Claims(&all) != nil {
		failed(http.StatusBadGateway, "provider", "invalid_token", "")
		return
	}
	email := strings.TrimSpace(claims.Email)
	if claims.EmailVerified != true && claims.EmailVerified != "true" || !asciiOnly(email) || attemptedEmail(strings.ToLower(email)) == "" {
		failed(http.StatusForbidden, "denied", "unverified_email", attemptedEmail(strings.ToLower(email)))
		return
	}
	email = strings.ToLower(email)
	config := s.access.config.Load()
	// A session carries only the groups some policy names, which keeps the
	// cookie small: a group added to a policy later counts from the next
	// sign-in.
	wanted := config.groups()
	var groups []string
	for _, group := range claimStrings(all[provider.GroupsClaim]) {
		if wanted[group] && !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	identity := control.AccessIdentity{Email: email, Groups: groups}
	admin, _ := s.control.IsAdminEmail(r.Context(), email)
	if !slices.ContainsFunc(config.forHost(pending.host), func(rule *accessRule) bool { return rule.policy.Allows(identity, admin) }) {
		failed(http.StatusForbidden, "denied", "not_allowed", email)
		return
	}
	ticket := Token()
	if !s.access.putTicket(ticket, accessTicket{identity: identity, host: pending.host, returnTo: pending.returnTo, expires: s.now().Add(accessTicketTTL)}, s.now()) {
		accessPage(w, r, http.StatusServiceUnavailable, "busy")
		return
	}
	s.audit(r, "access.login", control.AuditEntry{ActorEmail: email, ResourceType: "address", ResourceID: pending.host})
	http.Redirect(w, r, "https://"+pending.host+accessPathPrefix+"finish?"+url.Values{"ticket": {ticket}}.Encode(), http.StatusSeeOther)
}

func (st *accessState) putTicket(ticket string, t accessTicket, now time.Time) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.tickets == nil {
		st.tickets = map[string]accessTicket{}
	}
	if len(st.tickets) >= maxAccessTickets {
		for key, entry := range st.tickets {
			if !now.Before(entry.expires) {
				delete(st.tickets, key)
			}
		}
		if len(st.tickets) >= maxAccessTickets {
			return false
		}
	}
	st.tickets[Hash(ticket)] = t
	return true
}

func (st *accessState) takeTicket(ticket, host string, now time.Time) (accessTicket, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := Hash(ticket)
	t, ok := st.tickets[key]
	delete(st.tickets, key)
	return t, ok && t.host == host && now.Before(t.expires)
}

// accessFinish swaps a ticket for the session cookie on the address the
// ticket was issued for.
func (s *Store) accessFinish(w http.ResponseWriter, r *http.Request, host string) {
	w.Header().Set("Cache-Control", "no-store")
	ticket, ok := s.access.takeTicket(r.URL.Query().Get("ticket"), host, s.now())
	key := s.access.key.Load()
	if !ok || key == nil || s.control == nil {
		accessPage(w, r, http.StatusBadRequest, "expired")
		return
	}
	provider, err := s.control.AccessProvider(r.Context())
	if err != nil {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	expires := s.now().Add(time.Duration(provider.SessionHours) * time.Hour)
	claims := accessClaims{Email: ticket.identity.Email, Groups: ticket.identity.Groups, Host: host, Expiry: expires.Unix()}
	value, err := signAccess(*key, claims)
	for err == nil && len(value) > maxAccessCookie && len(claims.Groups) > 0 {
		claims.Groups = claims.Groups[:len(claims.Groups)-1]
		value, err = signAccess(*key, claims)
	}
	if err != nil || len(value) > maxAccessCookie {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// Lax: the sign-in arrives as a navigation from the provider's site.
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: value, Path: "/", Expires: expires, MaxAge: provider.SessionHours * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, accessReturn(ticket.returnTo), http.StatusSeeOther)
}

// stripAccessCookie keeps the session cookie from the app: it is the
// Outlet's, and the app gets the email in a header instead.
func stripAccessCookie(header http.Header) {
	cookies := header.Values("Cookie")
	if len(cookies) == 0 {
		return
	}
	var kept []string
	for _, line := range cookies {
		var parts []string
		for _, part := range strings.Split(line, ";") {
			if name, _, _ := strings.Cut(strings.TrimSpace(part), "="); name != accessCookie {
				parts = append(parts, strings.TrimSpace(part))
			}
		}
		if len(parts) != 0 {
			kept = append(kept, strings.Join(parts, "; "))
		}
	}
	header.Del("Cookie")
	for _, line := range kept {
		header.Add("Cookie", line)
	}
}

// accessPages are the few pages a protected address shows by itself.
var accessPages = map[string][2]string{
	"required":    {"Sign-in required", "로그인이 필요합니다"},
	"denied":      {"You don't have access to this address.", "이 주소에 대한 접근 권한이 없습니다."},
	"expired":     {"The sign-in expired. Open the address again.", "로그인 요청이 만료되었습니다. 주소에 다시 접속하십시오."},
	"unavailable": {"Sign-in is unavailable.", "로그인을 사용할 수 없습니다."},
	"provider":    {"Couldn't sign in with the identity provider.", "인증 공급자 로그인 중 오류가 발생했습니다."},
	"busy":        {"Too many sign-ins in progress. Try again shortly.", "잠시 후 다시 시도하십시오."},
	"unknown":     {"This address isn't protected.", "보호되지 않은 주소입니다."},
	"signedOut":   {"Signed out.", "로그아웃되었습니다."},
}

func accessPage(w http.ResponseWriter, r *http.Request, status int, page string) {
	text := accessPages[page]
	message, lang := text[0], "en"
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ko") {
		message, lang = text[1], "ko"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="` + lang + `"><meta charset="utf-8"><title>Meerkat</title><p>` + html.EscapeString(message) + `</p></html>`))
}
