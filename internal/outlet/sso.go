package outlet

import (
	"context"
	"crypto/subtle"
	"errors"
	"html"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/lawnect/meerkat/internal/control"
)

const (
	ssoCookie       = "meerkat_sso"
	ssoPath         = "/api/v1/session/sso"
	ssoCallbackPath = ssoPath + "/callback"
	ssoStateTTL     = 10 * time.Minute
	maxSSOPending   = 1024
	// ssoProviderTTL bounds how long discovery is trusted; keys are refreshed
	// by go-oidc on their own when a token names an unknown one.
	ssoProviderTTL = time.Hour
)

// ssoPending is a sign-in sent to the provider and not yet returned.
type ssoPending struct {
	nonce, verifier, returnTo string
	expires                   time.Time
}

type ssoProvider struct {
	issuer   string
	provider *oidc.Provider
	fetched  time.Time
}

// ssoState holds sign-ins in flight, keyed by the hash of their state, and
// the last discovered provider. Both live in memory: a restart only asks
// people to start signing in again.
type ssoState struct {
	mu       sync.Mutex
	pending  map[string]ssoPending
	provider *ssoProvider
}

func (st *ssoState) put(state string, p ssoPending, now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.pending == nil {
		st.pending = map[string]ssoPending{}
	}
	if len(st.pending) >= maxSSOPending {
		// Expired entries go first; a flood of starts then evicts the
		// sign-in closest to expiring rather than refusing new ones.
		oldest, at := "", time.Time{}
		for key, entry := range st.pending {
			if !now.Before(entry.expires) {
				delete(st.pending, key)
			} else if oldest == "" || entry.expires.Before(at) {
				oldest, at = key, entry.expires
			}
		}
		if len(st.pending) >= maxSSOPending {
			delete(st.pending, oldest)
		}
	}
	st.pending[Hash(state)] = p
}

// take removes the sign-in for state and returns it if it has not expired.
func (st *ssoState) take(state string, now time.Time) (ssoPending, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	key := Hash(state)
	p, ok := st.pending[key]
	delete(st.pending, key)
	return p, ok && now.Before(p.expires)
}

// oidcContext makes go-oidc and oauth2 use the Outlet's client, with its
// timeout, for discovery, keys and the token exchange.
func (s *Store) oidcContext(ctx context.Context) context.Context {
	client := s.ssoHTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return oidc.ClientContext(ctx, client)
}

// discover returns the provider for issuer, fetching its configuration when
// the issuer changed or the cached one is old.
func (s *Store) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.sso.mu.Lock()
	cached := s.sso.provider
	s.sso.mu.Unlock()
	if cached != nil && cached.issuer == issuer && s.now().Sub(cached.fetched) < ssoProviderTTL {
		return cached.provider, nil
	}
	// The provider keeps this context for fetching keys later, so it must
	// not be the request's.
	provider, err := oidc.NewProvider(s.oidcContext(context.Background()), issuer)
	if err != nil {
		return nil, err
	}
	s.sso.mu.Lock()
	s.sso.provider = &ssoProvider{issuer: issuer, provider: provider, fetched: s.now()}
	s.sso.mu.Unlock()
	return provider, nil
}

// ssoRedirectURI is the callback registered at the provider. It is built
// from the management hostname, never from the request's Host header.
func (s *Store) ssoRedirectURI(ctx context.Context) (string, error) {
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return "", err
	}
	if routing.Outlet.AdminHostname == "" {
		return "", errors.New("no management hostname")
	}
	return "https://" + routing.Outlet.AdminHostname + ssoCallbackPath, nil
}

func oauthConfig(settings control.SSO, provider *oidc.Provider, redirectURI string) *oauth2.Config {
	return &oauth2.Config{ClientID: settings.ClientID, ClientSecret: settings.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: redirectURI, Scopes: settings.Scopes}
}

// safeReturn is the in-app path to go back to after signing in. Anything
// that could leave the Outlet becomes "/".
func safeReturn(path string) string {
	if path == "" || len(path) > 256 || path[0] != '/' || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\?#\r\n\t") {
		return "/"
	}
	return path
}

// ssoFailed sends the browser back to the sign-in page with an error code
// the page knows how to show.
func ssoFailed(w http.ResponseWriter, r *http.Request, returnTo, code string) {
	http.Redirect(w, r, safeReturn(returnTo)+"?sso_error="+code, http.StatusSeeOther)
}

func (s *Store) ssoStart(w http.ResponseWriter, r *http.Request) {
	returnTo := safeReturn(r.URL.Query().Get("return"))
	if s.control == nil {
		ssoFailed(w, r, returnTo, "sso_unavailable")
		return
	}
	// The provider can only send people back to the public management host.
	// Before the first administrator exists, setup needs the bootstrap
	// token; single sign-on must not be another way to create one.
	_, public := requestOrigin(r)
	complete, _ := s.setupComplete(r.Context())
	settings, err := s.control.SSO(r.Context())
	if err != nil || !settings.Enabled || !public || !complete {
		ssoFailed(w, r, returnTo, "sso_unavailable")
		return
	}
	if !s.allowLogin(w, r) {
		return
	}
	provider, err := s.discover(r.Context(), settings.Issuer)
	if err != nil {
		ssoFailed(w, r, returnTo, "provider_unreachable")
		return
	}
	redirectURI, err := s.ssoRedirectURI(r.Context())
	if err != nil {
		ssoFailed(w, r, returnTo, "sso_unavailable")
		return
	}
	state, nonce, verifier := Token(), Token(), oauth2.GenerateVerifier()
	now := s.now()
	s.sso.put(state, ssoPending{nonce: nonce, verifier: verifier, returnTo: returnTo, expires: now.Add(ssoStateTTL)}, now)
	// Lax, not Strict: the provider sends the browser back with a top-level
	// navigation from its own site, and the state must come along.
	http.SetCookie(w, &http.Cookie{Name: ssoCookie, Value: state, Path: ssoPath, MaxAge: int(ssoStateTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, oauthConfig(settings, provider, redirectURI).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// ssoClaims are the ID token claims the sign-in uses. Groups stay raw: the
// claim name is configurable and providers send a list or a single string.
type ssoClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
}

func claimStrings(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// ssoAllowed reports whether settings let this person in. Each list that is
// set must match: the email or its "@domain" is in AllowedUsers, and one of
// the groups is in AllowedGroups. With both set, a person needs both; with
// neither, nobody gets in.
func ssoAllowed(settings control.SSO, email string, groups []string) bool {
	if len(settings.AllowedUsers) == 0 && len(settings.AllowedGroups) == 0 {
		return false
	}
	_, domain, _ := strings.Cut(email, "@")
	if len(settings.AllowedUsers) != 0 && !slices.ContainsFunc(settings.AllowedUsers, func(entry string) bool {
		return entry == email || (strings.HasPrefix(entry, "@") && entry[1:] == domain)
	}) {
		return false
	}
	if len(settings.AllowedGroups) != 0 && !slices.ContainsFunc(groups, func(group string) bool { return slices.Contains(settings.AllowedGroups, group) }) {
		return false
	}
	return true
}

func (s *Store) ssoCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	cookie, _ := r.Cookie(ssoCookie)
	http.SetCookie(w, &http.Cookie{Name: ssoCookie, Value: "", Path: ssoPath, MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	state := query.Get("state")
	// The state must come back in the cookie of the browser that started:
	// a callback link sent to someone else cannot sign them in.
	if s.control == nil || cookie == nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		ssoFailed(w, r, "/", "invalid_state")
		return
	}
	pending, ok := s.sso.take(state, s.now())
	if !ok {
		ssoFailed(w, r, "/", "invalid_state")
		return
	}
	fail := func(code, reason, email string) {
		s.audit(r, "session.login_failed", control.AuditEntry{ActorEmail: attemptedEmail(email), ResourceType: "admin", ResourceID: "unknown", Detail: map[string]any{"method": "sso", "reason": reason}})
		ssoFailed(w, r, pending.returnTo, code)
	}
	if query.Get("error") != "" {
		fail("provider_denied", "provider_error", "")
		return
	}
	settings, err := s.control.SSO(r.Context())
	if err != nil || !settings.Enabled {
		ssoFailed(w, r, pending.returnTo, "sso_unavailable")
		return
	}
	provider, err := s.discover(r.Context(), settings.Issuer)
	if err != nil {
		ssoFailed(w, r, pending.returnTo, "provider_unreachable")
		return
	}
	redirectURI, err := s.ssoRedirectURI(r.Context())
	if err != nil {
		ssoFailed(w, r, pending.returnTo, "sso_unavailable")
		return
	}
	ctx := s.oidcContext(r.Context())
	token, err := oauthConfig(settings, provider, redirectURI).Exchange(ctx, query.Get("code"), oauth2.VerifierOption(pending.verifier))
	if err != nil {
		fail("token_failed", "token_exchange", "")
		return
	}
	raw, _ := token.Extra("id_token").(string)
	// Verify checks the signature against the provider's keys, the issuer,
	// the audience and the expiry.
	idToken, err := provider.Verifier(&oidc.Config{ClientID: settings.ClientID}).Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(pending.nonce)) != 1 {
		fail("token_failed", "invalid_token", "")
		return
	}
	var claims ssoClaims
	var all map[string]any
	if idToken.Claims(&claims) != nil || idToken.Claims(&all) != nil {
		fail("token_failed", "invalid_token", "")
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	// A provider that says the address is unverified is not trusted with it.
	if verified, said := claims.EmailVerified.(bool); said && !verified || claims.EmailVerified == "false" || attemptedEmail(email) == "" {
		fail("not_allowed", "unverified_email", email)
		return
	}
	if !ssoAllowed(settings, email, claimStrings(all[settings.GroupsClaim])) {
		fail("not_allowed", "not_allowed", email)
		return
	}
	admin, err := s.control.SSOAdmin(r.Context(), email)
	if err != nil {
		ssoFailed(w, r, pending.returnTo, "sso_unavailable")
		return
	}
	if _, ok := s.startLogin(w, r, admin, map[string]any{"method": "sso"}); !ok {
		return
	}
	// The session cookie is SameSite=Strict, and this response ends a
	// navigation that began on the provider's site, so a redirect from here
	// would arrive without it. A page that moves on by itself starts a fresh,
	// same-site navigation instead.
	target := html.EscapeString(pending.returnTo)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=` + target + `"><title>Meerkat</title><a href="` + target + `">Continue</a>`))
}

// ssoView is the SSO settings as the admin API shows them: never the secret.
type ssoView struct {
	control.SSO
	ClientSecret    string `json:"client_secret,omitempty"`
	SecretSet       bool   `json:"client_secret_set"`
	SecretHint      string `json:"client_secret_hint"`
	RedirectURI     string `json:"redirect_uri"`
	AvailableAtHost bool   `json:"available"`
}

func (s *Store) ssoSettingsView(ctx context.Context, settings control.SSO) ssoView {
	view := ssoView{SSO: settings, SecretSet: settings.ClientSecret != "", SecretHint: settings.SecretHint()}
	view.SSO.ClientSecret = ""
	if uri, err := s.ssoRedirectURI(ctx); err == nil {
		view.RedirectURI, view.AvailableAtHost = uri, true
	}
	return view
}

func (s *Store) getSSO(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	settings, err := s.control.SSO(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, s.ssoSettingsView(r.Context(), settings))
}

type ssoRequest struct {
	Enabled       bool     `json:"enabled"`
	Label         string   `json:"label"`
	Issuer        string   `json:"issuer"`
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret"`
	Scopes        []string `json:"scopes"`
	GroupsClaim   string   `json:"groups_claim"`
	AllowedUsers  []string `json:"allowed_users"`
	AllowedGroups []string `json:"allowed_groups"`
	PasswordLogin bool     `json:"password_login"`
}

func (s *Store) putSSO(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req ssoRequest
	// An empty or missing client_secret keeps the stored one.
	if !decode(w, r, &req, "enabled", "label", "issuer", "client_id", "scopes", "groups_claim", "allowed_users", "allowed_groups", "password_login") {
		return
	}
	saved, err := s.control.SetSSO(r.Context(), control.SSO(req), s.now())
	if errors.Is(err, control.ErrInvalidSSO) {
		fail(w, r, 400, "invalid_sso")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.sso", "security", "sso", map[string]any{
		"enabled": saved.Enabled, "issuer": saved.Issuer, "client_id": saved.ClientID, "secret_changed": req.ClientSecret != "",
		"allowed_users": saved.AllowedUsers, "allowed_groups": saved.AllowedGroups, "password_login": saved.PasswordLogin,
	})
	reply(w, 200, s.ssoSettingsView(r.Context(), saved))
}

// testSSO fetches the provider's discovery document for issuer, so a typo
// shows up before anyone is locked out by it.
func (s *Store) testSSO(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req struct {
		Issuer string `json:"issuer"`
	}
	if !decode(w, r, &req, "issuer") {
		return
	}
	issuer := strings.TrimSuffix(strings.TrimSpace(req.Issuer), "/")
	if !control.ValidIssuer(issuer) {
		fail(w, r, 400, "invalid_issuer")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(s.oidcContext(ctx), issuer)
	if err != nil {
		fail(w, r, 502, "provider_unreachable")
		return
	}
	reply(w, 200, map[string]string{"authorization_endpoint": provider.Endpoint().AuthURL})
}
