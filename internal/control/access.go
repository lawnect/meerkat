package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
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
	settingAccessTokens   = "access_tokens"
	settingAccessTokenUse = "access_token_use"

	MaxAccessPolicies     = 100
	maxAccessPolicyName   = 64
	maxAccessHosts        = 100
	maxAccessConditions   = 50
	defaultAccessSession  = 24
	maxAccessSessionHours = 720
	MaxAccessTokens       = 100
	maxAccessTokenName    = 64
	maxAccessTokenHosts   = 100
	maxAccessTokenRanges  = 50
)

var (
	ErrInvalidAccessProvider = errors.New("invalid access provider")
	ErrInvalidAccessPolicies = errors.New("invalid access policies")
	// ErrStoredAccessPolicies means the saved policies no longer pass the
	// checks; AccessPolicies returns what it could read along with it.
	ErrStoredAccessPolicies = errors.New("stored access policies are invalid")
	ErrInvalidAccessToken   = errors.New("invalid access token")
	ErrAccessTokenNotFound  = errors.New("access token not found")
	// ErrAccessTokenInUse means a policy names the token: take it out of
	// the policies first.
	ErrAccessTokenInUse = errors.New("access token in use")
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

// What a policy does with the requests it covers.
const (
	// AccessAllow lets in, after a sign-in, whom the policy names.
	AccessAllow = "allow"
	// AccessBypass lets anyone in without a sign-in.
	AccessBypass = "bypass"
	// AccessBlock lets nobody in.
	AccessBlock = "block"
)

// AccessPolicy covers requests for some public addresses, all their paths
// or the ones its conditions match, and says what they get.
type AccessPolicy struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Action is AccessAllow, AccessBypass or AccessBlock; empty is
	// AccessAllow.
	Action string `json:"action"`
	// Hosts are hostnames or "*.domain"; a policy names at least one.
	Hosts []string `json:"hosts"`
	// AllPaths covers every path of the hosts; otherwise a path that any
	// condition matches is covered.
	AllPaths   bool             `json:"all_paths"`
	Conditions []BlockCondition `json:"conditions"`
	// Methods limits the policy to requests with these HTTP methods, GET
	// taking HEAD along; none means every method.
	Methods []string `json:"methods"`
	// RequireToken limits the policy to requests that send an access token
	// valid for them: one of Tokens, by ID, or any when Tokens is empty.
	RequireToken bool     `json:"require_token"`
	Tokens       []string `json:"tokens"`
	// Who an AccessAllow policy lets in: everyone the access provider signs
	// in, or else those each list that is set matches, as for the
	// administrators' single sign-on; AllowAdmins also lets in anyone whose
	// email is a Meerkat administrator's.
	AllowEveryone bool     `json:"allow_everyone"`
	AllowedUsers  []string `json:"allowed_users"`
	AllowedGroups []string `json:"allowed_groups"`
	AllowAdmins   bool     `json:"allow_admins"`
}

// AccessMethods are the HTTP methods a policy can be limited to.
var AccessMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}

// CoversMethod reports whether a policy limited to methods covers a request with
// method: every method when none are named, and HEAD with GET.
func CoversMethod(methods []string, method string) bool {
	if method == "HEAD" {
		method = "GET"
	}
	return len(methods) == 0 || slices.Contains(methods, method)
}

// AccessPolicies apply in order: the first enabled policy that covers a
// request decides it.
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
		if policy.Action == "" {
			policy.Action = AccessAllow
		}
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
				normalized, ok := normalizeCondition(c)
				if policy.Action == AccessBypass {
					normalized, ok = bypassCondition(c)
				}
				if !ok {
					return a, false
				}
				if !containsCondition(conditions, normalized) {
					conditions = append(conditions, normalized)
				}
			}
		}
		policy.Conditions = conditions
		named := map[string]bool{}
		for _, method := range policy.Methods {
			if method = strings.ToUpper(strings.TrimSpace(method)); !slices.Contains(AccessMethods, method) {
				return a, false
			}
			named[method] = true
		}
		policy.Methods = []string{}
		for _, method := range AccessMethods {
			if named[method] {
				policy.Methods = append(policy.Methods, method)
			}
		}
		var ok [2]bool
		policy.AllowedUsers, ok[0] = normalizeSSOList(policy.AllowedUsers, true, allowedUserEntry)
		policy.AllowedGroups, ok[1] = normalizeSSOList(policy.AllowedGroups, false, func(v string) bool {
			return v != "" && len(v) <= maxSSOEntryBytes && strings.IndexFunc(v, unicode.IsControl) < 0
		})
		if ok != [2]bool{true, true} {
			return a, false
		}
		tokens := make([]string, 0, len(policy.Tokens))
		for _, id := range policy.Tokens {
			if !validRuleID(id) || len(tokens) == MaxAccessTokens {
				return a, false
			}
			if !slices.Contains(tokens, id) {
				tokens = append(tokens, id)
			}
		}
		if !policy.RequireToken {
			tokens = []string{}
		}
		policy.Tokens = tokens
		switch policy.Action {
		case AccessAllow:
		case AccessBypass, AccessBlock:
			// Who may pass means nothing to these.
			policy.AllowEveryone = false
		default:
			return a, false
		}
		if policy.Action != AccessAllow || policy.AllowEveryone {
			policy.AllowedUsers, policy.AllowedGroups, policy.AllowAdmins = []string{}, []string{}, false
		}
		// An allow policy that names nobody would be a block policy.
		if policy.Action == AccessAllow && !policy.AllowEveryone && len(policy.AllowedUsers)+len(policy.AllowedGroups) == 0 && !policy.AllowAdmins {
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
		if policy.Action != AccessBypass && policy.Action != AccessBlock {
			policy.Action = AccessAllow
		}
		seen[policy.ID] = true
		for _, list := range []*[]string{&policy.Hosts, &policy.Methods, &policy.Tokens, &policy.AllowedUsers, &policy.AllowedGroups} {
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

// bypassCondition checks a condition of a bypass policy. Bypass opens what
// it matches, so requests are compared with it exactly as the app gets
// them, case and all, and a literal value must already be a path an app
// reads only one way: no backslash, no empty, "." or ".." segment, and none
// that ends in a dot or a space where it surely ends. Patterns are compared
// with case too.
func bypassCondition(c BlockCondition) (BlockCondition, bool) {
	value := strings.TrimSpace(c.Value)
	if value == "" || len(value) > maxBlockValueBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return c, false
	}
	switch c.Operator {
	case BlockMatches:
		_, err := regexp.Compile(value)
		return BlockCondition{Operator: c.Operator, Value: value}, err == nil
	case BlockEquals, BlockStartsWith:
		if !strings.HasPrefix(value, "/") {
			return c, false
		}
	case BlockEndsWith, BlockContains:
	default:
		return c, false
	}
	if strings.Contains(value, `\`) {
		return c, false
	}
	parts := strings.Split(value, "/")
	for i, part := range parts {
		// The first part of a value starting with "/" is empty, and the last
		// of starts_with and contains may go on in the request.
		whole := i < len(parts)-1 || c.Operator == BlockEquals || c.Operator == BlockEndsWith
		if i == 0 && part == "" || i == len(parts)-1 && part == "" {
			continue
		}
		if part == "" || part == "." || part == ".." || whole && strings.TrimRight(part, ". ") != part {
			return c, false
		}
	}
	return BlockCondition{Operator: c.Operator, Value: value}, true
}

// AccessIdentity is a person the access provider vouched for.
type AccessIdentity struct {
	Email  string
	Groups []string
}

// TakesToken reports whether the policy covers a request that sent the
// access token with ID id, valid for it, or none when id is empty.
func (p AccessPolicy) TakesToken(id string) bool {
	return !p.RequireToken || id != "" && (len(p.Tokens) == 0 || slices.Contains(p.Tokens, id))
}

// Allows reports whether the identity may pass this policy; admin says
// whether the email is a Meerkat administrator's.
func (p AccessPolicy) Allows(id AccessIdentity, admin bool) bool {
	if p.AllowEveryone || p.AllowAdmins && admin {
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
	// Normalize gives a new policy an ID; a saved one without was damaged.
	if slices.ContainsFunc(stored.Policies, func(policy AccessPolicy) bool { return policy.ID == "" }) {
		return stored, ErrStoredAccessPolicies
	}
	policies, ok := stored.Normalize()
	if !ok {
		return stored, ErrStoredAccessPolicies
	}
	return policies, nil
}

// SetAccessPolicies replaces the policies. The access tokens they name
// must exist.
func (d *Database) SetAccessPolicies(ctx context.Context, policies AccessPolicies, now time.Time) (AccessPolicies, error) {
	policies, ok := policies.Normalize()
	if !ok {
		return policies, ErrInvalidAccessPolicies
	}
	raw, err := json.Marshal(policies)
	if err != nil {
		return policies, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return policies, err
	}
	defer tx.Rollback()
	tokens, err := accessTokens(ctx, tx)
	if err != nil {
		return policies, err
	}
	for _, policy := range policies.Policies {
		for _, id := range policy.Tokens {
			if !slices.ContainsFunc(tokens, func(token AccessToken) bool { return token.ID == id }) {
				return policies, ErrInvalidAccessPolicies
			}
		}
	}
	if err := putSetting(ctx, tx, settingAccessPolicies, string(raw), now); err != nil {
		return policies, err
	}
	return policies, tx.Commit()
}

// AccessToken lets a client that can't sign in, such as a script or another
// service, meet the policies that require it, on the addresses and from the
// client addresses it is limited to: it sends its value, "<ID>.<secret>", in
// the X-Meerkat-Access-Token header.
type AccessToken struct {
	ID string `json:"id"`
	AccessTokenScope
	// SecretHash is the secret's SHA-256, in hex: the secret itself is
	// shown once, when the token is made.
	SecretHash string `json:"secret_hash"`
	CreatedAt  string `json:"created_at"`
	// ExpiresAt is when the token stops working, RFC 3339; empty is never.
	ExpiresAt string `json:"expires_at"`
}

// AccessTokenScope is what of an access token can be changed: its name and
// where it can be used. A token works only on Hosts, hostnames or
// "*.domain", at least one; and, when Ranges are set, only from client
// addresses in them, as CIDR prefixes or single addresses.
type AccessTokenScope struct {
	Name   string   `json:"name"`
	Hosts  []string `json:"hosts"`
	Ranges []string `json:"ranges"`
}

// Normalize checks the scope and puts it in its saved form: hosts in lower
// case, ranges as masked prefixes, none twice.
func (a AccessTokenScope) Normalize() (AccessTokenScope, bool) {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || len([]rune(a.Name)) > maxAccessTokenName || strings.IndexFunc(a.Name, unicode.IsControl) >= 0 || len(a.Hosts) == 0 || len(a.Hosts) > maxAccessTokenHosts || len(a.Ranges) > maxAccessTokenRanges {
		return a, false
	}
	hosts := make([]string, 0, len(a.Hosts))
	for _, host := range a.Hosts {
		if host = strings.ToLower(strings.TrimSpace(host)); !validBlockHost(host) {
			return a, false
		}
		if !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	ranges := make([]string, 0, len(a.Ranges))
	for _, value := range a.Ranges {
		value = strings.TrimSpace(value)
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, err := netip.ParseAddr(value)
			if err != nil || addr.Zone() != "" {
				return a, false
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		// Clients compare as IPv4 when they are; so do IPv4-mapped ranges.
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return a, false
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		if value = prefix.Masked().String(); !slices.Contains(ranges, value) {
			ranges = append(ranges, value)
		}
	}
	a.Hosts, a.Ranges = hosts, ranges
	return a, true
}

// AccessTokens returns the access tokens, oldest first. Saved tokens that
// can't be read count as none: a token only ever lets someone in.
func (d *Database) AccessTokens(ctx context.Context) ([]AccessToken, error) {
	return accessTokens(ctx, d.db)
}

func accessTokens(ctx context.Context, q conn) ([]AccessToken, error) {
	raw, err := getSetting(ctx, q, settingAccessTokens)
	if err != nil || raw == "" {
		return []AccessToken{}, err
	}
	var tokens []AccessToken
	if json.Unmarshal([]byte(raw), &tokens) != nil || tokens == nil {
		return []AccessToken{}, nil
	}
	return tokens, nil
}

func putAccessTokens(ctx context.Context, q conn, tokens []AccessToken, now time.Time) error {
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return putSetting(ctx, q, settingAccessTokens, string(raw), now)
}

// CreateAccessToken makes an access token with scope that stops working at
// expires, or never when that is zero, and returns it with its value.
func (d *Database) CreateAccessToken(ctx context.Context, scope AccessTokenScope, expires, now time.Time) (AccessToken, string, error) {
	scope, ok := scope.Normalize()
	if !ok || !expires.IsZero() && !expires.After(now) {
		return AccessToken{}, "", ErrInvalidAccessToken
	}
	id, err := newRuleID()
	if err != nil {
		return AccessToken{}, "", err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return AccessToken{}, "", err
	}
	value := id + "." + base64.RawURLEncoding.EncodeToString(secret)
	token := AccessToken{ID: id, AccessTokenScope: scope, SecretHash: AccessTokenHash(value), CreatedAt: stamp(now)}
	if !expires.IsZero() {
		token.ExpiresAt = stamp(expires)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return AccessToken{}, "", err
	}
	defer tx.Rollback()
	tokens, err := accessTokens(ctx, tx)
	if err != nil {
		return AccessToken{}, "", err
	}
	if len(tokens) >= MaxAccessTokens {
		return AccessToken{}, "", ErrInvalidAccessToken
	}
	if err := putAccessTokens(ctx, tx, append(tokens, token), now); err != nil {
		return AccessToken{}, "", err
	}
	return token, value, tx.Commit()
}

// UpdateAccessToken changes the name and scope of the access token with ID
// id; its value stays.
func (d *Database) UpdateAccessToken(ctx context.Context, id string, scope AccessTokenScope, now time.Time) (AccessToken, error) {
	scope, ok := scope.Normalize()
	if !ok {
		return AccessToken{}, ErrInvalidAccessToken
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return AccessToken{}, err
	}
	defer tx.Rollback()
	tokens, err := accessTokens(ctx, tx)
	if err != nil {
		return AccessToken{}, err
	}
	i := slices.IndexFunc(tokens, func(token AccessToken) bool { return token.ID == id })
	if i < 0 {
		return AccessToken{}, ErrAccessTokenNotFound
	}
	tokens[i].AccessTokenScope = scope
	if err := putAccessTokens(ctx, tx, tokens, now); err != nil {
		return AccessToken{}, err
	}
	return tokens[i], tx.Commit()
}

// AccessTokenUse returns when each access token last let a request in, by
// ID, as of the last RecordAccessTokenUse.
func (d *Database) AccessTokenUse(ctx context.Context) (map[string]time.Time, error) {
	return accessTokenUse(ctx, d.db)
}

func accessTokenUse(ctx context.Context, q conn) (map[string]time.Time, error) {
	used := map[string]time.Time{}
	raw, err := getSetting(ctx, q, settingAccessTokenUse)
	if err != nil || raw == "" {
		return used, err
	}
	var saved map[string]string
	if json.Unmarshal([]byte(raw), &saved) != nil {
		return used, nil
	}
	for id, at := range saved {
		if t, err := time.Parse(time.RFC3339, at); err == nil {
			used[id] = t
		}
	}
	return used, nil
}

// RecordAccessTokenUse keeps the later of used and what is saved for each
// token that still exists.
func (d *Database) RecordAccessTokenUse(ctx context.Context, used map[string]time.Time, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tokens, err := accessTokens(ctx, tx)
	if err != nil {
		return err
	}
	saved, err := accessTokenUse(ctx, tx)
	if err != nil {
		return err
	}
	out := map[string]string{}
	for _, token := range tokens {
		at := saved[token.ID]
		if used[token.ID].After(at) {
			at = used[token.ID]
		}
		if !at.IsZero() {
			out[token.ID] = stamp(at)
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	if err := putSetting(ctx, tx, settingAccessTokenUse, string(raw), now); err != nil {
		return err
	}
	return tx.Commit()
}

// AccessTokenHash is what a token's value is kept as: the SHA-256 of the
// whole value, in hex.
func AccessTokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// DeleteAccessToken deletes the access token with ID id and returns it. A
// token a policy names stays: ErrAccessTokenInUse. Policies that take any
// token don't hold one back.
func (d *Database) DeleteAccessToken(ctx context.Context, id string, now time.Time) (AccessToken, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return AccessToken{}, err
	}
	defer tx.Rollback()
	tokens, err := accessTokens(ctx, tx)
	if err != nil {
		return AccessToken{}, err
	}
	i := slices.IndexFunc(tokens, func(token AccessToken) bool { return token.ID == id })
	if i < 0 {
		return AccessToken{}, ErrAccessTokenNotFound
	}
	raw, err := getSetting(ctx, tx, settingAccessPolicies)
	if err != nil {
		return AccessToken{}, err
	}
	var policies AccessPolicies
	if raw != "" && json.Unmarshal([]byte(raw), &policies) == nil && slices.ContainsFunc(policies.Policies, func(policy AccessPolicy) bool { return slices.Contains(policy.Tokens, id) }) {
		return AccessToken{}, ErrAccessTokenInUse
	}
	token := tokens[i]
	if err := putAccessTokens(ctx, tx, slices.Delete(tokens, i, i+1), now); err != nil {
		return AccessToken{}, err
	}
	return token, tx.Commit()
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
