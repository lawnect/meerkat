package outlet

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/lawnect/meerkat/internal/control"
)

// accessProviderView is the access provider as the admin API shows it:
// never the secret.
type accessProviderView struct {
	control.AccessProvider
	ClientSecret string `json:"client_secret,omitempty"`
	SecretSet    bool   `json:"client_secret_set"`
	SecretHint   string `json:"client_secret_hint"`
	RedirectURI  string `json:"redirect_uri"`
	Available    bool   `json:"available"`
}

func (s *Store) accessProviderView(ctx context.Context, provider control.AccessProvider) accessProviderView {
	view := accessProviderView{AccessProvider: provider, SecretSet: provider.ClientSecret != "", SecretHint: provider.SecretHint()}
	view.AccessProvider.ClientSecret = ""
	if uri, err := s.accessRedirectURI(ctx); err == nil {
		view.RedirectURI, view.Available = uri, true
	}
	return view
}

func (s *Store) getAccessProvider(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	provider, err := s.control.AccessProvider(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, s.accessProviderView(r.Context(), provider))
}

func (s *Store) putAccessProvider(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.AccessProvider
	// An empty or missing client_secret keeps the stored one.
	if !decode(w, r, &req, "enabled", "issuer", "client_id", "scopes", "groups_claim", "session_hours") {
		return
	}
	secretChanged := req.ClientSecret != ""
	s.saving.Lock()
	saved, err := s.control.SetAccessProvider(r.Context(), req, s.now())
	if err == nil {
		s.access.enabled.Store(saved.Enabled)
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidAccessProvider) {
		fail(w, r, 400, "invalid_access_provider")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.access_provider", "security", "access_provider", map[string]any{
		"enabled": saved.Enabled, "issuer": saved.Issuer, "client_id": saved.ClientID, "secret_changed": secretChanged, "session_hours": saved.SessionHours,
	})
	reply(w, 200, s.accessProviderView(r.Context(), saved))
}

// testAccessProvider fetches an issuer's discovery document, so a typo shows
// before anyone tries to sign in.
func (s *Store) testAccessProvider(w http.ResponseWriter, r *http.Request) {
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

// revokeAccessSessions replaces the session key: everyone signed in to a
// protected address signs in again.
func (s *Store) revokeAccessSessions(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	s.saving.Lock()
	key, err := s.control.RotateAccessKey(r.Context(), s.now())
	if err == nil {
		s.access.key.Store(&key)
	}
	s.saving.Unlock()
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.access_sessions_revoke", "security", "access_sessions", nil)
	w.WriteHeader(http.StatusNoContent)
}

type accessPoliciesView struct {
	Policies []control.AccessPolicy `json:"policies"`
	// Addresses are the public addresses a policy can protect.
	Addresses []string `json:"addresses"`
	// Damaged says the saved policies failed the checks, so what they name
	// is closed (see closedAccess) until they are saved again.
	Damaged bool `json:"damaged,omitempty"`
}

func (s *Store) accessPoliciesView(ctx context.Context, policies control.AccessPolicies) (accessPoliciesView, error) {
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return accessPoliciesView{}, err
	}
	return accessPoliciesView{Policies: policies.Policies, Addresses: routing.Hostnames()}, nil
}

func (s *Store) getAccessPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	// Saved policies that no longer pass the checks show as read, to be
	// fixed and saved again.
	policies, err := s.control.AccessPolicies(r.Context())
	damaged := errors.Is(err, control.ErrStoredAccessPolicies)
	if err != nil && !damaged {
		fail(w, r, 503, "persistence_failed")
		return
	}
	if damaged {
		policies = policies.Readable()
	}
	view, err := s.accessPoliciesView(r.Context(), policies)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	view.Damaged = damaged
	reply(w, 200, view)
}

func (s *Store) putAccessPolicies(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.AccessPolicies
	if !decodeLimited(w, r, &req, maxBlockRulesBody, "policies") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetAccessPolicies(r.Context(), req, s.now())
	if err == nil {
		s.access.config.Store(compileAccess(saved))
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidAccessPolicies) {
		fail(w, r, 400, "invalid_access_policies")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// What each policy does and for whom, in order; the path conditions
	// can be long and say only where.
	type logged struct {
		Name     string   `json:"name"`
		Enabled  bool     `json:"enabled"`
		Action   string   `json:"action"`
		Hosts    []string `json:"hosts"`
		Everyone bool     `json:"allow_everyone,omitempty"`
		Users    []string `json:"allowed_users,omitempty"`
		Groups   []string `json:"allowed_groups,omitempty"`
		Admins   bool     `json:"allow_admins,omitempty"`
		Tokens   []string `json:"allowed_tokens,omitempty"`
	}
	policies := make([]logged, 0, len(saved.Policies))
	for _, p := range saved.Policies {
		policies = append(policies, logged{p.Name, p.Enabled, p.Action, p.Hosts, p.AllowEveryone, p.AllowedUsers, p.AllowedGroups, p.AllowAdmins, p.AllowedTokens})
	}
	s.audited(r, "security.access_policies", "security", "access_policies", map[string]any{"policies": policies})
	view, err := s.accessPoliciesView(r.Context(), saved)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

// accessTokenView is an access token as the admin API shows it: never its
// secret, and with where it is used: the policies that allow it and when it
// last let a request in ("" never).
type accessTokenView struct {
	ID string `json:"id"`
	control.AccessTokenScope
	CreatedAt  string            `json:"created_at"`
	ExpiresAt  string            `json:"expires_at"`
	LastUsedAt string            `json:"last_used_at"`
	Policies   []accessPolicyRef `json:"policies"`
}

type accessPolicyRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type accessTokensView struct {
	Tokens []accessTokenView `json:"tokens"`
	// Addresses are the public addresses a token can be limited to.
	Addresses []string `json:"addresses"`
}

// accessTokenViews shows tokens with the policies that allow them and their
// last use, saved or since the last save.
func (s *Store) accessTokenViews(ctx context.Context, tokens []control.AccessToken) ([]accessTokenView, error) {
	policies, err := s.control.AccessPolicies(ctx)
	if err != nil && !errors.Is(err, control.ErrStoredAccessPolicies) {
		return nil, err
	}
	used, err := s.control.AccessTokenUse(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]accessTokenView, 0, len(tokens))
	for _, token := range tokens {
		view := accessTokenView{ID: token.ID, AccessTokenScope: token.AccessTokenScope, CreatedAt: token.CreatedAt, ExpiresAt: token.ExpiresAt, Policies: []accessPolicyRef{}}
		last := used[token.ID]
		if recent, ok := s.access.used.Load(token.ID); ok && recent.(time.Time).After(last) {
			last = recent.(time.Time)
		}
		if !last.IsZero() {
			view.LastUsedAt = last.UTC().Format(time.RFC3339)
		}
		for _, policy := range policies.Policies {
			if policy.AllowsToken(token.ID) {
				view.Policies = append(view.Policies, accessPolicyRef{policy.ID, policy.Name})
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Store) getAccessTokens(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	tokens, err := s.control.AccessTokens(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	views, err := s.accessTokenViews(r.Context(), tokens)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	routing, err := s.control.Routing(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, accessTokensView{Tokens: views, Addresses: routing.Hostnames()})
}

// createAccessToken makes an access token and answers with its value, the
// only time it is shown.
func (s *Store) createAccessToken(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req struct {
		control.AccessTokenScope
		// ExpiresDays is how many days the token works; 0 is for good.
		ExpiresDays int `json:"expires_days"`
	}
	if !decode(w, r, &req, "name", "hosts", "ranges", "expires_days") {
		return
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		fail(w, r, 400, "invalid_access_token")
		return
	}
	var expires time.Time
	if req.ExpiresDays > 0 {
		expires = s.now().AddDate(0, 0, req.ExpiresDays)
	}
	s.saving.Lock()
	token, value, err := s.control.CreateAccessToken(r.Context(), req.AccessTokenScope, expires, s.now())
	if err == nil {
		err = s.reloadAccessTokens(r.Context())
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidAccessToken) {
		fail(w, r, 400, "invalid_access_token")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.access_token_create", "access_token", token.ID, map[string]any{"name": token.Name, "hosts": token.Hosts, "ranges": token.Ranges, "expires_at": token.ExpiresAt})
	views, err := s.accessTokenViews(r.Context(), []control.AccessToken{token})
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 201, map[string]any{"token": views[0], "value": value})
}

// updateAccessToken changes a token's name and where it can be used; its
// value stays.
func (s *Store) updateAccessToken(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.AccessTokenScope
	if !decode(w, r, &req, "name", "hosts", "ranges") {
		return
	}
	s.saving.Lock()
	token, err := s.control.UpdateAccessToken(r.Context(), r.PathValue("id"), req, s.now())
	if err == nil {
		err = s.reloadAccessTokens(r.Context())
	}
	s.saving.Unlock()
	switch {
	case errors.Is(err, control.ErrInvalidAccessToken):
		fail(w, r, 400, "invalid_access_token")
		return
	case errors.Is(err, control.ErrAccessTokenNotFound):
		fail(w, r, 404, "access_token_not_found")
		return
	case err != nil:
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "security.access_token_update", "access_token", token.ID, map[string]any{"name": token.Name, "hosts": token.Hosts, "ranges": token.Ranges})
	views, err := s.accessTokenViews(r.Context(), []control.AccessToken{token})
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, views[0])
}

// deleteAccessToken deletes an access token no policy names.
func (s *Store) deleteAccessToken(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	s.saving.Lock()
	token, err := s.control.DeleteAccessToken(r.Context(), r.PathValue("id"), s.now())
	if err == nil {
		err = s.reloadAccessTokens(r.Context())
		s.access.used.Delete(token.ID)
	}
	s.saving.Unlock()
	switch {
	case errors.Is(err, control.ErrAccessTokenNotFound):
		fail(w, r, 404, "access_token_not_found")
	case errors.Is(err, control.ErrAccessTokenInUse):
		fail(w, r, 409, "access_token_in_use")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		s.audited(r, "security.access_token_delete", "access_token", token.ID, map[string]any{"name": token.Name})
		w.WriteHeader(http.StatusNoContent)
	}
}
