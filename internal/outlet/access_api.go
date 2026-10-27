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
	// Who may get in, per policy; the path conditions can be long and say
	// only where.
	type logged struct {
		Name    string   `json:"name"`
		Enabled bool     `json:"enabled"`
		Hosts   []string `json:"hosts"`
		Users   []string `json:"allowed_users,omitempty"`
		Groups  []string `json:"allowed_groups,omitempty"`
		Admins  bool     `json:"allow_admins,omitempty"`
	}
	policies := make([]logged, 0, len(saved.Policies))
	for _, p := range saved.Policies {
		policies = append(policies, logged{p.Name, p.Enabled, p.Hosts, p.AllowedUsers, p.AllowedGroups, p.AllowAdmins})
	}
	s.audited(r, "security.access_policies", "security", "access_policies", map[string]any{"policies": policies})
	view, err := s.accessPoliciesView(r.Context(), saved)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}
