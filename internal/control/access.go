package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Access protection puts public addresses, or paths under them, behind a
// sign-in with an identity provider of their own: people who aren't Meerkat
// administrators can be let in, by email, domain or group.
const (
	settingAccessProvider = "access_provider"
	settingAccessPolicies = "access_policies"
	settingAccessKey      = "access_key"

	MaxAccessPolicies     = 100
	maxAccessPolicyName   = 64
	maxAccessHosts        = 100
	maxAccessConditions   = 50
	defaultAccessSession  = 24
	maxAccessSessionHours = 720
)

var (
	ErrInvalidAccessProvider = errors.New("invalid access provider")
	ErrInvalidAccessPolicies = errors.New("invalid access policies")
	// ErrStoredAccessPolicies means the saved policies no longer pass the
	// checks; AccessPolicies returns what it could read along with it.
	ErrStoredAccessPolicies = errors.New("stored access policies are invalid")
)

// percentEscape finds an escape left in a path condition: request paths are
// compared decoded, so a condition written encoded would never match.
var percentEscape = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

// AccessProvider is the OpenID Connect client that signs people in to
// protected addresses. It is separate from the administrators' single
// sign-on, so the people it lets in never reach the console.
type AccessProvider struct {
	Enabled bool   `json:"enabled"`
	Issuer  string `json:"issuer"`
	// ClientSecret is write-only: an API response shows SecretHint.
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
	GroupsClaim  string   `json:"groups_claim"`
	// SessionHours is how long a sign-in lasts on a protected address.
	SessionHours int `json:"session_hours"`
}

// DefaultAccessProvider is off, with the scopes every provider accepts.
func DefaultAccessProvider() AccessProvider {
	return AccessProvider{Scopes: []string{"openid", "email", "profile"}, GroupsClaim: "groups", SessionHours: defaultAccessSession}
}

// SecretHint is the last four characters of the client secret.
func (p AccessProvider) SecretHint() string {
	return SSO{ClientSecret: p.ClientSecret}.SecretHint()
}

// Normalize trims and checks the provider. Turning it on needs an issuer
// and a client ID.
func (p AccessProvider) Normalize() (AccessProvider, bool) {
	p.Issuer, p.ClientID, p.GroupsClaim = strings.TrimSuffix(strings.TrimSpace(p.Issuer), "/"), strings.TrimSpace(p.ClientID), strings.TrimSpace(p.GroupsClaim)
	if (p.Issuer != "" && !ValidIssuer(p.Issuer)) || (p.ClientID != "" && !plainSSOValue(p.ClientID, maxSSOEntryBytes)) ||
		(p.ClientSecret != "" && !plainSSOValue(p.ClientSecret, maxSSOSecret)) || (p.GroupsClaim != "" && !plainSSOValue(p.GroupsClaim, 64)) {
		return p, false
	}
	scopes, ok := normalizeSSOList(p.Scopes, false, func(v string) bool { return plainSSOValue(v, 64) })
	if !ok {
		return p, false
	}
	p.Scopes = scopes
	if !containsString(p.Scopes, "openid") {
		p.Scopes = append([]string{"openid"}, p.Scopes...)
	}
	if p.GroupsClaim == "" {
		p.GroupsClaim = "groups"
	}
	if p.SessionHours == 0 {
		p.SessionHours = defaultAccessSession
	}
	if p.SessionHours < 1 || p.SessionHours > maxAccessSessionHours {
		return p, false
	}
	if p.Enabled && (p.Issuer == "" || p.ClientID == "") {
		return p, false
	}
	return p, true
}

// AccessPolicy protects requests for some public addresses, all their
// paths or the ones its conditions match, and says who may pass.
type AccessPolicy struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Hosts are hostnames or "*.domain"; a policy names at least one.
	Hosts []string `json:"hosts"`
	// AllPaths protects every path of the hosts; otherwise a path that
	// any condition matches is protected.
	AllPaths   bool             `json:"all_paths"`
	Conditions []BlockCondition `json:"conditions"`
	// Who may pass: each list that is set must match, as for the
	// administrators' single sign-on; AllowAdmins also lets in anyone whose
	// email is a Meerkat administrator's.
	AllowedUsers  []string `json:"allowed_users"`
	AllowedGroups []string `json:"allowed_groups"`
	AllowAdmins   bool     `json:"allow_admins"`
}

// AccessPolicies are evaluated together: a request must satisfy every
// enabled policy that covers it.
type AccessPolicies struct {
	Policies []AccessPolicy `json:"policies"`
}

// Normalize checks the policies, puts conditions in the form requests are
// compared in, and gives new policies an ID.
func (a AccessPolicies) Normalize() (AccessPolicies, bool) {
	if len(a.Policies) > MaxAccessPolicies {
		return a, false
	}
	out := make([]AccessPolicy, 0, len(a.Policies))
	seen := map[string]bool{}
	for _, policy := range a.Policies {
		policy.Name = strings.TrimSpace(policy.Name)
		if policy.Name == "" || len([]rune(policy.Name)) > maxAccessPolicyName || strings.IndexFunc(policy.Name, unicode.IsControl) >= 0 {
			return a, false
		}
		if policy.ID == "" {
			id, err := newRuleID()
			if err != nil {
				return a, false
			}
			policy.ID = id
		}
		if !validRuleID(policy.ID) || seen[policy.ID] {
			return a, false
		}
		seen[policy.ID] = true
		if len(policy.Hosts) == 0 || len(policy.Hosts) > maxAccessHosts {
			return a, false
		}
		hosts := make([]string, 0, len(policy.Hosts))
		for _, host := range policy.Hosts {
			host = strings.ToLower(strings.TrimSpace(host))
			if !validBlockHost(host) {
				return a, false
			}
			if !containsString(hosts, host) {
				hosts = append(hosts, host)
			}
		}
		policy.Hosts = hosts
		conditions := make([]BlockCondition, 0, len(policy.Conditions))
		if !policy.AllPaths {
			if len(policy.Conditions) == 0 || len(policy.Conditions) > maxAccessConditions {
				return a, false
			}
			for _, c := range policy.Conditions {
				// A path the policy could never match would leave it open.
				if c.Operator != BlockMatches && (percentEscape.MatchString(c.Value) || strings.Contains(c.Value, ";")) {
					return a, false
				}
				normalized, ok := normalizeCondition(c, true)
				if !ok {
					return a, false
				}
				if !containsCondition(conditions, normalized) {
					conditions = append(conditions, normalized)
				}
			}
		}
		policy.Conditions = conditions
		var ok [2]bool
		policy.AllowedUsers, ok[0] = normalizeSSOList(policy.AllowedUsers, true, allowedUserEntry)
		policy.AllowedGroups, ok[1] = normalizeSSOList(policy.AllowedGroups, false, func(v string) bool {
			return v != "" && len(v) <= maxSSOEntryBytes && strings.IndexFunc(v, unicode.IsControl) < 0
		})
		if ok != [2]bool{true, true} {
			return a, false
		}
		// A policy that names nobody would shut its addresses for everyone.
		if len(policy.AllowedUsers)+len(policy.AllowedGroups) == 0 && !policy.AllowAdmins {
			return a, false
		}
		out = append(out, policy)
	}
	return AccessPolicies{Policies: out}, true
}

// Readable gives saved policies that no longer pass the checks a form the
// console can show and save again: every list there, and a valid ID each,
// none twice.
func (a AccessPolicies) Readable() AccessPolicies {
	seen := map[string]bool{}
	out := make([]AccessPolicy, 0, len(a.Policies))
	for _, policy := range a.Policies {
		for !validRuleID(policy.ID) || seen[policy.ID] {
			policy.ID, _ = newRuleID()
		}
		seen[policy.ID] = true
		for _, list := range []*[]string{&policy.Hosts, &policy.AllowedUsers, &policy.AllowedGroups} {
			if *list == nil {
				*list = []string{}
			}
		}
		if policy.Conditions == nil {
			policy.Conditions = []BlockCondition{}
		}
		out = append(out, policy)
	}
	return AccessPolicies{Policies: out}
}

// AccessIdentity is a person the access provider vouched for.
type AccessIdentity struct {
	Email  string
	Groups []string
}

// Allows reports whether the identity may pass this policy; admin says
// whether the email is a Meerkat administrator's.
func (p AccessPolicy) Allows(id AccessIdentity, admin bool) bool {
	if p.AllowAdmins && admin {
		return true
	}
	if len(p.AllowedUsers)+len(p.AllowedGroups) == 0 {
		return false
	}
	_, domain, _ := strings.Cut(id.Email, "@")
	if len(p.AllowedUsers) != 0 && !slices.ContainsFunc(p.AllowedUsers, func(entry string) bool {
		return entry == id.Email || (strings.HasPrefix(entry, "@") && entry[1:] == domain)
	}) {
		return false
	}
	if len(p.AllowedGroups) != 0 && !slices.ContainsFunc(id.Groups, func(group string) bool { return slices.Contains(p.AllowedGroups, group) }) {
		return false
	}
	return true
}

// AccessProvider returns the saved provider, or the default.
func (d *Database) AccessProvider(ctx context.Context) (AccessProvider, error) {
	raw, err := getSetting(ctx, d.db, settingAccessProvider)
	if err != nil || raw == "" {
		return DefaultAccessProvider(), err
	}
	var provider AccessProvider
	if json.Unmarshal([]byte(raw), &provider) != nil {
		return DefaultAccessProvider(), nil
	}
	if provider, ok := provider.Normalize(); ok {
		return provider, nil
	}
	return DefaultAccessProvider(), nil
}

// SetAccessProvider saves the provider. An empty client secret keeps the
// stored one.
func (d *Database) SetAccessProvider(ctx context.Context, provider AccessProvider, now time.Time) (AccessProvider, error) {
	if provider.ClientSecret == "" {
		stored, err := d.AccessProvider(ctx)
		if err != nil {
			return provider, err
		}
		provider.ClientSecret = stored.ClientSecret
	}
	provider, ok := provider.Normalize()
	if !ok {
		return provider, ErrInvalidAccessProvider
	}
	raw, err := json.Marshal(provider)
	if err != nil {
		return provider, err
	}
	return provider, putSetting(ctx, d.db, settingAccessProvider, string(raw), now)
}

// AccessPolicies returns the saved policies, normalized. Saved policies
// that no longer pass the checks come back as read, possibly none, with
// ErrStoredAccessPolicies.
func (d *Database) AccessPolicies(ctx context.Context) (AccessPolicies, error) {
	raw, err := getSetting(ctx, d.db, settingAccessPolicies)
	if err != nil || raw == "" {
		return AccessPolicies{Policies: []AccessPolicy{}}, err
	}
	// Saved policies always have a list, perhaps empty: null, {} and the
	// like are damage, not "no policies".
	var stored AccessPolicies
	if json.Unmarshal([]byte(raw), &stored) != nil || stored.Policies == nil {
		return AccessPolicies{Policies: []AccessPolicy{}}, ErrStoredAccessPolicies
	}
	policies, ok := stored.Normalize()
	if !ok {
		return stored, ErrStoredAccessPolicies
	}
	return policies, nil
}

// SetAccessPolicies replaces the policies.
func (d *Database) SetAccessPolicies(ctx context.Context, policies AccessPolicies, now time.Time) (AccessPolicies, error) {
	policies, ok := policies.Normalize()
	if !ok {
		return policies, ErrInvalidAccessPolicies
	}
	raw, err := json.Marshal(policies)
	if err != nil {
		return policies, err
	}
	return policies, putSetting(ctx, d.db, settingAccessPolicies, string(raw), now)
}

// AccessKey signs sessions on protected addresses. It is created on first
// use; RotateAccessKey replaces it, which signs everyone out.
func (d *Database) AccessKey(ctx context.Context, now time.Time) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO NOTHING`,
		settingAccessKey, base64.StdEncoding.EncodeToString(fresh), stamp(now)); err != nil {
		return nil, err
	}
	raw, err := getSetting(ctx, d.db, settingAccessKey)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("stored access key is invalid")
	}
	return key, nil
}

// RotateAccessKey replaces the session key and returns the new one.
func (d *Database) RotateAccessKey(ctx context.Context, now time.Time) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if err := putSetting(ctx, d.db, settingAccessKey, base64.StdEncoding.EncodeToString(fresh), now); err != nil {
		return nil, err
	}
	return fresh, nil
}

// IsAdminEmail reports whether email belongs to a Meerkat administrator.
func (d *Database) IsAdminEmail(ctx context.Context, email string) (bool, error) {
	var exists int
	err := d.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM admins WHERE email = ?)", email).Scan(&exists)
	return exists != 0, err
}
