package outlet

import (
	"cmp"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"math/bits"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/validation"
)

// Access protection: a request for a protected address or path needs a
// session cookie on that address. Without one the browser goes to the
// management host, which signs the person in with the access provider and
// hands the address a one-time ticket; the address swaps it for the cookie.
// The provider then needs one redirect URI, on the management host, however
// many addresses are protected.
const (
	accessCookie = "__Host-meerkat_access"
	// accessStateCookie and accessBindCookie tie a sign-in to the browser
	// that started it, on the management host and on the protected address:
	// the callback and the ticket only work in a browser holding them, so a
	// sign-in someone else started can't sign this browser in. A browser
	// keeps one of each for all its sign-ins, so tabs signing in at once
	// share them and they don't pile up.
	accessStateCookie  = "__Host-meerkat_access_state"
	accessBindCookie   = "__Host-meerkat_access_bind"
	accessPathPrefix   = "/.meerkat/access/"
	accessStartPath    = "/api/v1/access/start"
	accessCallbackPath = "/api/v1/access/callback"
	accessEmailHeader  = "X-Meerkat-Access-Email"
	// accessTokenHeader carries an access token from a client that can't
	// sign in; the app gets the token's name in accessServiceHeader.
	accessTokenHeader   = "X-Meerkat-Access-Token"
	accessServiceHeader = "X-Meerkat-Access-Service"
	accessTicketTTL     = time.Minute
	maxAccessTickets    = 8192
	// maxAccessCookie keeps the session cookie under the 4 KB browsers take.
	maxAccessCookie = 3800
	// maxAccessPerClient and accessStarts are how many sign-ins one client
	// address may have in flight and begin a minute, and maxAccessPending
	// how many all may have in flight: a whole office signs in to protected
	// addresses from one address, far more people than administrators.
	maxAccessPerClient = 512
	accessStarts       = 120
	maxAccessPending   = 32768
)

// leftEscape finds a percent escape still in a decoded path.
var leftEscape = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

// accessRule is one enabled access policy, compiled for matching. A
// bypass policy's conditions are in exact instead: what opens a path is
// compared with it exactly as the app gets it.
type accessRule struct {
	match  blockRule
	exact  exactMatch
	all    bool
	policy control.AccessPolicy
}

// exactMatch is a bypass policy's conditions, compared with a request path
// as is: case and all, and without the trailing slash rules add.
type exactMatch struct {
	equals                     map[string]bool
	prefixes, suffixes, phrase []string
	patterns                   []*regexp.Regexp
}

func compileExact(conditions []control.BlockCondition) exactMatch {
	m := exactMatch{equals: map[string]bool{}}
	for _, c := range conditions {
		switch c.Operator {
		case control.BlockEquals:
			m.equals[c.Value] = true
		case control.BlockStartsWith:
			m.prefixes = append(m.prefixes, c.Value)
		case control.BlockEndsWith:
			m.suffixes = append(m.suffixes, c.Value)
		case control.BlockContains:
			m.phrase = append(m.phrase, c.Value)
		case control.BlockMatches:
			// Saving refused patterns that don't compile; one that doesn't
			// now opens nothing.
			if pattern, err := regexp.Compile(c.Value); err == nil {
				m.patterns = append(m.patterns, pattern)
			}
		}
	}
	return m
}

func (m exactMatch) matches(p string) bool {
	return m.equals[p] ||
		slices.ContainsFunc(m.prefixes, func(prefix string) bool { return strings.HasPrefix(p, prefix) }) ||
		slices.ContainsFunc(m.suffixes, func(suffix string) bool { return strings.HasSuffix(p, suffix) }) ||
		slices.ContainsFunc(m.phrase, func(part string) bool { return strings.Contains(p, part) }) ||
		slices.ContainsFunc(m.patterns, func(pattern *regexp.Regexp) bool { return pattern.MatchString(p) })
}

// plainPath reports whether apps can read u's path only one way, so that a
// bypass condition can be compared with it as is: passed on to the app, as
// EscapedPath, with only the escapes it needs (canonicalEscapes), with no
// escape left after decoding, no backslash, ";", control character or byte
// that isn't UTF-8, and no segment that is empty, ".", "..", or ends in a
// dot or a space.
func plainPath(u *url.URL) bool {
	p := u.Path
	if !canonicalEscapes(u.EscapedPath()) || !strings.HasPrefix(p, "/") || leftEscape.MatchString(p) || !utf8.ValidString(p) || strings.ContainsAny(p, `\;`) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return false
	}
	segments := strings.Split(p[1:], "/")
	for i, segment := range segments {
		if segment == "" && i == len(segments)-1 {
			continue
		}
		if segment == "" || segment == "." || segment == ".." || strings.TrimRight(segment, ". ") != segment {
			return false
		}
	}
	return true
}

type accessConfig struct{ rules []accessRule }

// matching returns the first rule that covers a request for host and u's
// path, with method, that sent the access token with ID token, valid for
// it, or none when token is empty; or nil. Allow and block conditions compare the path broadly, in the
// form rules take: a path an app may read differently matches them all, so
// it can't slip past one, and one with an escape still in it after decoding
// is also compared decoded once more, as an app decoding again reads it. A
// bypass condition, which opens paths, matches only a plain path, exactly.
func (c *accessConfig) matching(host, method string, u *url.URL, token string) *accessRule {
	if c == nil {
		return nil
	}
	// The path is read only once a rule that covers the request needs it.
	var path requestPath
	for i := range c.rules {
		rule := &c.rules[i]
		switch {
		case !rule.match.covers(host) || !control.CoversMethod(rule.policy.Methods, method) || !rule.policy.TakesToken(token):
			continue
		case rule.all:
			return rule
		}
		if path.forms == nil {
			path = readPath(u)
		}
		switch {
		case rule.policy.Action == control.AccessBypass:
			if path.plain && rule.exact.matches(u.Path) {
				return rule
			}
		case path.unclear || slices.ContainsFunc(path.forms, rule.match.matches):
			return rule
		}
	}
	return nil
}

// requestPath is how access rules read a request's path: the forms
// conditions compare, whether an app may read it differently (unclear),
// and whether a bypass condition can compare it as is (plain).
type requestPath struct {
	forms          []rulePath
	unclear, plain bool
}

func readPath(u *url.URL) requestPath {
	path := requestPath{forms: []rulePath{newRulePath(u.Path)}, unclear: ambiguousPath(u.EscapedPath(), true)}
	if !path.unclear && leftEscape.MatchString(u.Path) {
		if twice, err := url.PathUnescape(u.Path); err != nil || ambiguousPath(u.Path, true) {
			path.unclear = true
		} else {
			path.forms = append(path.forms, newRulePath(twice))
		}
	}
	path.plain = !path.unclear && plainPath(u)
	return path
}

// canonicalEscapes reports whether escaped, a path as Go escapes it (which
// leaves only bytes a path carries as is), escapes a byte only when a path
// can't carry it, and in uppercase: "%2F", "%7E", "%2A" or "%c3" would let
// an app read it otherwise than decoded once, but "(", "*" or "!" as
// clients send them don't.
func canonicalEscapes(escaped string) bool {
	for i := strings.IndexByte(escaped, '%'); i >= 0; i = strings.IndexByte(escaped, '%') {
		if i+2 >= len(escaped) || !upperHex(escaped[i+1]) || !upperHex(escaped[i+2]) {
			return false
		}
		if b, _ := hex.DecodeString(escaped[i+1 : i+3]); pathByte(b[0]) {
			return false
		}
		escaped = escaped[i+3:]
	}
	return true
}

// pathByte reports whether a path carries c as is: an unreserved character,
// a sub-delimiter, ":", "@", "/", or a bracket, as Go's EscapedPath keeps.
func pathByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || strings.IndexByte("-._~!$&'()*+,;=:@/[]", c) >= 0
}

func upperHex(c byte) bool { return '0' <= c && c <= '9' || 'A' <= c && c <= 'F' }

// ambiguousPath reports a path, given escaped, whose meaning decoded
// depends on the app: "." or ".." that came escaped (%2e%2e, a%2f..) or
// next to a backslash, which rules compare resolved but many apps keep
// whole or read as a plain character; or a segment of only dots and spaces
// other than "." and "..", which servers resolve or keep in different ways.
// With bytes, so does one decoding to bytes that aren't UTF-8, which some
// read as overlong forms of "/" or ".", or to control characters, a NUL
// ending the path for some.
func ambiguousPath(escaped string, bytes bool) bool {
	p, err := url.PathUnescape(escaped)
	if err != nil || bytes && (!utf8.ValidString(p) || strings.IndexFunc(p, unicode.IsControl) >= 0) {
		return true
	}
	backslash := strings.ContainsRune(p, '\\')
	for raw := range strings.SplitSeq(escaped, "/") {
		segment, err := url.PathUnescape(raw)
		if err != nil {
			return true
		}
		for part := range strings.FieldsFuncSeq(segment, func(r rune) bool { return r == '/' || r == '\\' }) {
			part, _, _ = strings.Cut(part, ";")
			if dots := part == "." || part == ".."; dots && (segment != raw || backslash) || !dots && strings.Trim(part, ". ") == "" {
				return true
			}
		}
	}
	return false
}

// signingIn returns the rules that let people into some path of host
// after a sign-in: the allow rules that some request could reach, not
// those an earlier rule for host shadows.
func (c *accessConfig) signingIn(host string) []*accessRule {
	if c == nil {
		return nil
	}
	var earlier, out []*accessRule
	for i := range c.rules {
		rule := &c.rules[i]
		if !rule.match.covers(host) || slices.ContainsFunc(earlier, func(e *accessRule) bool { return e.shadows(rule) }) {
			continue
		}
		if rule.policy.Action == control.AccessAllow {
			out = append(out, rule)
		}
		earlier = append(earlier, rule)
	}
	return out
}

// shadows reports whether rule, applying first on an address, takes every
// request later's conditions would: it covers every method later does and
// every token later requires, if it requires one, and it takes every path,
// or it isn't a bypass (which matches narrowly) and starts with a path that
// each of later's equals and starts-with conditions does, compared as
// requests are. The console marks the same as never applying.
func (rule *accessRule) shadows(later *accessRule) bool {
	if len(rule.policy.Methods) != 0 && (len(later.policy.Methods) == 0 || slices.ContainsFunc(later.policy.Methods, func(method string) bool { return !control.CoversMethod(rule.policy.Methods, method) })) {
		return false
	}
	if tokens := rule.policy.Tokens; rule.policy.RequireToken && (!later.policy.RequireToken || len(tokens) != 0 && (len(later.policy.Tokens) == 0 || slices.ContainsFunc(later.policy.Tokens, func(id string) bool { return !slices.Contains(tokens, id) }))) {
		return false
	}
	if rule.all || slices.Contains(rule.match.prefixes, "/") {
		return true
	}
	if rule.policy.Action == control.AccessBypass || later.all || len(later.policy.Conditions) == 0 {
		return false
	}
	return !slices.ContainsFunc(later.policy.Conditions, func(c control.BlockCondition) bool {
		var path string
		switch c.Operator {
		case control.BlockStartsWith:
			path = strings.ToLower(c.Value)
		case control.BlockEquals:
			path = newRulePath(c.Value).slashed
		default:
			return true
		}
		return !slices.ContainsFunc(rule.match.prefixes, func(prefix string) bool { return strings.HasPrefix(path, prefix) })
	})
}

// policyGroups are the group names rules allow: the only ones a session for
// their address needs to carry.
func policyGroups(rules []*accessRule) map[string]bool {
	out := map[string]bool{}
	for _, rule := range rules {
		for _, group := range rule.policy.AllowedGroups {
			out[group] = true
		}
	}
	return out
}

// fewestGroups keeps few of groups that still satisfy every rule naming
// groups that groups satisfied: it takes the group that satisfies the most
// rules left, the shorter on a tie and then the earlier, until none is
// left, then drops any it took that the others make unneeded. Each group
// carries the rules it satisfies as bits, and of groups satisfying the
// same rules only the one the greedy choice would take is weighed.
func fewestGroups(rules []*accessRule, groups []string) []string {
	position := make(map[string]int, len(groups))
	for i, group := range groups {
		if _, ok := position[group]; !ok {
			position[group] = i
		}
	}
	words := (len(rules) + 63) / 64
	meets := map[int]ruleBits{}
	all := make(ruleBits, words)
	named := 0
	for _, rule := range rules {
		hit := false
		for _, group := range rule.policy.AllowedGroups {
			if i, ok := position[group]; ok {
				if meets[i] == nil {
					meets[i] = make(ruleBits, words)
				}
				meets[i].set(named)
				hit = true
			}
		}
		if hit {
			all.set(named)
			named++
		}
	}
	type candidate struct {
		at    int
		group string
		meets ruleBits
	}
	var candidates []candidate
	same := map[string]int{}
	for i, group := range groups {
		set := meets[i]
		if set == nil || position[group] != i {
			continue
		}
		key := set.key()
		if j, ok := same[key]; !ok {
			same[key] = len(candidates)
			candidates = append(candidates, candidate{i, group, set})
		} else if len(group) < len(candidates[j].group) {
			candidates[j] = candidate{i, group, set}
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return a.at - b.at })
	var kept []candidate
	for open := slices.Clone(all); !open.empty(); {
		best, most := 0, 0
		for i, c := range candidates {
			if n := c.meets.common(open); n > most || n == most && n > 0 && len(c.group) < len(candidates[best].group) {
				best, most = i, n
			}
		}
		kept = append(kept, candidates[best])
		open.clear(candidates[best].meets)
	}
	for i := len(kept) - 1; i >= 0; i-- {
		without := slices.Delete(slices.Clone(kept), i, i+1)
		covered := make(ruleBits, words)
		for _, c := range without {
			covered.add(c.meets)
		}
		if slices.Equal(covered, all) {
			kept = without
		}
	}
	out := make([]string, len(kept))
	for i, c := range kept {
		out[i] = c.group
	}
	return out
}

// ruleBits is a set of rules, by their place in a list.
type ruleBits []uint64

func (b ruleBits) set(i int) { b[i/64] |= 1 << (i % 64) }
func (b ruleBits) add(other ruleBits) {
	for i := range b {
		b[i] |= other[i]
	}
}
func (b ruleBits) clear(other ruleBits) {
	for i := range b {
		b[i] &^= other[i]
	}
}
func (b ruleBits) common(other ruleBits) int {
	n := 0
	for i := range b {
		n += bits.OnesCount64(b[i] & other[i])
	}
	return n
}
func (b ruleBits) empty() bool {
	return !slices.ContainsFunc(b, func(word uint64) bool { return word != 0 })
}
func (b ruleBits) key() string {
	out := make([]byte, 0, 8*len(b))
	for _, word := range b {
		out = binary.LittleEndian.AppendUint64(out, word)
	}
	return string(out)
}

type accessTicket struct {
	identity control.AccessIdentity
	host     string
	returnTo string
	bind     string // Hash of the browser's accessBindCookie
	expires  time.Time
}

type accessState struct {
	config atomic.Pointer[accessConfig]
	key    atomic.Pointer[[]byte]
	// tokens maps an access token's ID to it; used maps an ID to when the
	// token last let a request in, until saved (see flushAccessTokenUse).
	tokens atomic.Pointer[map[string]accessTokenRule]
	used   sync.Map
	// enabled is the provider's switch: while it is off nobody can sign in,
	// so protected addresses let nobody in.
	enabled atomic.Bool
	// login holds sign-ins in flight and the provider's discovery; starts
	// limits how often one client address begins one, and failures how
	// many failed sign-ins of one the audit log records a minute.
	login    ssoState
	starts   attemptLimiter
	failures attemptLimiter
	mu       sync.Mutex
	tickets  map[string]accessTicket
	// admins remembers which emails are administrators' (see isAdminEmail).
	admins adminEmails
}

// adminEmails remembers, for adminEmailTTL, whether an email is an
// administrator's, so that an allow policy letting administrators in
// doesn't ask the database, whose one connection writes also take, on
// every request.
type adminEmails struct {
	mu    sync.Mutex
	known map[string]adminEmail
}

type adminEmail struct {
	admin bool
	at    time.Time
}

const (
	adminEmailTTL  = 30 * time.Second
	maxAdminEmails = 4096
)

func (s *Store) isAdminEmail(ctx context.Context, email string) (bool, error) {
	now, admins := s.now(), &s.access.admins
	admins.mu.Lock()
	known, ok := admins.known[email]
	admins.mu.Unlock()
	if ok && now.Sub(known.at) < adminEmailTTL {
		return known.admin, nil
	}
	admin, err := s.control.IsAdminEmail(ctx, email)
	if err != nil {
		return false, err
	}
	admins.mu.Lock()
	if admins.known == nil || len(admins.known) >= maxAdminEmails {
		admins.known = map[string]adminEmail{}
	}
	admins.known[email] = adminEmail{admin, now}
	admins.mu.Unlock()
	return admin, nil
}

// accessIdentityKey and accessServiceKey carry, in a request's context,
// the email of the person or the name of the access token let in.
type accessIdentityKey struct{}
type accessServiceKey struct{}

func compileAccess(policies control.AccessPolicies) *accessConfig {
	config := &accessConfig{}
	for _, policy := range policies.Policies {
		if !policy.Enabled || len(policy.Hosts) == 0 {
			continue
		}
		rule := accessRule{match: compileBlockRule(policy.Hosts, policy.Conditions), all: policy.AllPaths, policy: policy}
		if policy.Action == control.AccessBypass {
			rule.match, rule.exact = compileBlockRule(policy.Hosts, nil), compileExact(policy.Conditions)
		}
		config.rules = append(config.rules, rule)
	}
	return config
}

// closedAccess compiles saved policies that no longer pass the checks so
// that they keep closed what they may have protected, in their saved
// order. Each enabled allow or block policy covers every path and method of
// the addresses it names, or of every address when it names no valid one: an
// allow policy lets in, after a sign-in, only whom it names as saved, and a
// block policy, or an allow policy that also required an access token,
// nobody. A bypass policy, which only opened paths, is left out, so what it
// opened falls to the policies below. With nothing readable, or a policy
// that isn't, every address is closed.
func closedAccess(policies control.AccessPolicies) *accessConfig {
	nobody := accessRule{match: compileBlockRule(nil, nil), all: true, policy: control.AccessPolicy{Action: control.AccessAllow}}
	// Every saved policy has an ID: one without was never readable.
	if len(policies.Policies) == 0 || slices.ContainsFunc(policies.Policies, func(policy control.AccessPolicy) bool { return policy.ID == "" }) {
		return &accessConfig{rules: []accessRule{nobody}}
	}
	config := &accessConfig{}
	for _, policy := range policies.Policies {
		if !policy.Enabled || policy.Action == control.AccessBypass {
			continue
		}
		var hosts []string
		for _, host := range policy.Hosts {
			if host = strings.ToLower(strings.TrimSpace(host)); validation.Hostname(strings.TrimPrefix(host, "*.")) {
				hosts = append(hosts, host)
			}
		}
		rule := nobody
		rule.match = compileBlockRule(hosts, nil)
		if (policy.Action == control.AccessAllow || policy.Action == "") && !policy.RequireToken {
			// Whom it names, in the form sessions carry, as saving would put it.
			rule.policy.AllowEveryone, rule.policy.AllowAdmins = policy.AllowEveryone, policy.AllowAdmins
			for _, user := range policy.AllowedUsers {
				rule.policy.AllowedUsers = append(rule.policy.AllowedUsers, strings.ToLower(strings.TrimSpace(user)))
			}
			for _, group := range policy.AllowedGroups {
				rule.policy.AllowedGroups = append(rule.policy.AllowedGroups, strings.TrimSpace(group))
			}
		}
		config.rules = append(config.rules, rule)
	}
	return config
}

func (s *Store) reloadAccess(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	provider, err := s.control.AccessProvider(ctx)
	if err != nil {
		return err
	}
	s.access.enabled.Store(provider.Enabled)
	policies, err := s.control.AccessPolicies(ctx)
	switch {
	case errors.Is(err, control.ErrStoredAccessPolicies):
		// Not a reason to keep every other address down: the ones the
		// policies may protect stay closed until they are saved again.
		slog.Error("saved access policies are invalid; the addresses they name stay closed until they are saved again", "error", err)
		s.access.config.Store(closedAccess(policies))
	case err != nil:
		return err
	default:
		s.access.config.Store(compileAccess(policies))
	}
	if err := s.reloadAccessTokens(ctx); err != nil {
		return err
	}
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
	if path == "" || len(path) > 2048 || path[0] != '/' || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "\\#") || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return "/"
	}
	return path
}

// guardAccess passes a request on when no policy covers it, when the first
// that does opens it, or when its session satisfies that policy, carrying
// along for the app the email and the name of the access token the policy
// required. Otherwise it answers the request itself, as it does the
// Outlet's own paths under /.meerkat/access/, and returns false.
func (s *Store) guardAccess(w http.ResponseWriter, r *http.Request, host, adminHost string) (*http.Request, bool) {
	if strings.HasPrefix(r.URL.Path, accessPathPrefix) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case accessPathPrefix + "finish":
			s.accessFinish(w, r, host)
		case accessPathPrefix + "logout":
			accessLogout(w, r)
		default:
			http.NotFound(w, r)
		}
		return r, false
	}
	// A client that can't sign in sends an access token, which policies can
	// require: it counts where it is valid, on this address from this client.
	value := strings.TrimSpace(r.Header.Get(accessTokenHeader))
	var token accessTokenRule
	var refused, valid string
	if value != "" {
		token, refused = s.accessToken(value)
		if ip, _ := requestOrigin(r); refused == "" && !token.usableFrom(host, ip) {
			refused = "out_of_scope"
		}
		if refused == "" {
			valid = token.ID
		}
	}
	rule := s.access.config.Load().matching(host, r.Method, r.URL, valid)
	switch {
	case rule == nil:
		return r, true
	case rule.policy.Action == control.AccessBypass:
		return s.tokenTaken(r, rule, token), true
	}
	// The request doesn't simply pass, so a token sent with it that the
	// policy doesn't require is audited, saying why it didn't count.
	if value != "" && !rule.policy.RequireToken {
		refused = cmp.Or(refused, "not_allowed")
		s.accessTokenFailed(r, host, token.ID, refused)
	}
	if rule.policy.Action == control.AccessBlock {
		accessPage(w, r, http.StatusForbidden, "denied")
		return r, false
	}
	// Such a token leaves the request to a session it may also carry;
	// without one, the refusal stands, rather than a sign-in.
	if _, session := s.accessSession(r, host); refused != "" && !session {
		if refused == "invalid" || refused == "expired" {
			accessPage(w, r, http.StatusUnauthorized, "token")
		} else {
			accessPage(w, r, http.StatusForbidden, "denied")
		}
		return r, false
	}
	if !s.access.enabled.Load() {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return r, false
	}
	identity, ok := s.accessSession(r, host)
	if !ok {
		s.accessChallenge(w, r, host, adminHost)
		return r, false
	}
	// The database is asked only when the session alone doesn't settle it.
	allowed := rule.policy.Allows(identity, false)
	if !allowed && rule.policy.AllowAdmins {
		admin, err := s.isAdminEmail(r.Context(), identity.Email)
		if err != nil {
			accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
			return r, false
		}
		allowed = admin
	}
	if !allowed {
		// The person can sign in again, with another account or with groups
		// a policy named after they signed in.
		accessPageNext(w, r, http.StatusForbidden, "denied", accessNext{action: accessPathPrefix + "logout?" + url.Values{"return": {accessReturn(r.URL.RequestURI())}}.Encode()})
		return r, false
	}
	return s.tokenTaken(r.WithContext(context.WithValue(r.Context(), accessIdentityKey{}, identity.Email)), rule, token), true
}

// tokenTaken notes, for a request rule lets through, the access token rule
// required: when it was last used, and its name for the app.
func (s *Store) tokenTaken(r *http.Request, rule *accessRule, token accessTokenRule) *http.Request {
	if !rule.policy.RequireToken {
		return r
	}
	s.noteTokenUse(token.ID)
	return r.WithContext(context.WithValue(r.Context(), accessServiceKey{}, token.Name))
}

// accessSameOrigin refuses a POST that comes from another site, sibling
// addresses under the same domain included.
var accessSameOrigin = http.NewCrossOriginProtection()

// accessLogout ends the session on this address, then goes to return if
// given. Only a POST from the address itself does: anything else gets the
// button that sends one, so no other page can sign people out.
func accessLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || accessSameOrigin.Check(r) != nil {
		accessPageNext(w, r, http.StatusOK, "signOut", accessNext{action: r.URL.RequestURI()})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	if back := r.URL.Query().Get("return"); back != "" {
		http.Redirect(w, r, accessReturn(back), http.StatusSeeOther)
		return
	}
	accessPage(w, r, http.StatusOK, "signedOut")
}

// accessTokenRule is an access token compiled for checking where it is
// used.
type accessTokenRule struct {
	control.AccessToken
	hosts  blockRule
	ranges []netip.Prefix
	// unusable marks a token whose saved scope can't be read; expires is
	// when it stops working, if it has an ExpiresAt, and the zero time,
	// long past, for one that can't be read.
	unusable bool
	expires  time.Time
}

// usableFrom reports whether the token may be used on host by a client at
// ip. A token with no hosts, which saving refuses, is usable nowhere.
func (t accessTokenRule) usableFrom(host, ip string) bool {
	if t.unusable || len(t.Hosts) == 0 || !t.hosts.covers(host) {
		return false
	}
	if len(t.ranges) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(ip)
	return err == nil && slices.ContainsFunc(t.ranges, func(prefix netip.Prefix) bool { return prefix.Contains(addr.Unmap()) })
}

// accessTokenFailed records a refused access token in the audit log, as
// many a minute as one client's failed sign-ins (see accessState.failures).
// id is the token's, when the value named one.
func (s *Store) accessTokenFailed(r *http.Request, host, id, reason string) {
	if ip, _ := requestOrigin(r); !s.access.failures.allow(clientKey(ip), s.now()) {
		return
	}
	detail := map[string]any{"reason": reason}
	if id != "" {
		detail["token"] = id
	}
	s.audit(r, "access.token_failed", control.AuditEntry{ResourceType: "address", ResourceID: host, Detail: detail})
}

// accessToken returns the access token whose value a request sent, and
// why it can't be used: "" when it can, "invalid" for a value that isn't a
// token's, "expired". The token comes back whenever the value names one,
// so a refusal can say which.
func (s *Store) accessToken(value string) (accessTokenRule, string) {
	id, _, _ := strings.Cut(value, ".")
	tokens := s.access.tokens.Load()
	if tokens == nil {
		return accessTokenRule{}, "invalid"
	}
	token, ok := (*tokens)[id]
	if !ok {
		return accessTokenRule{}, "invalid"
	}
	if subtle.ConstantTimeCompare([]byte(control.AccessTokenHash(value)), []byte(token.SecretHash)) != 1 {
		return token, "invalid"
	}
	if token.ExpiresAt != "" && !s.now().Before(token.expires) {
		return token, "expired"
	}
	return token, ""
}

func (s *Store) reloadAccessTokens(ctx context.Context) error {
	tokens, err := s.control.AccessTokens(ctx)
	if err != nil {
		return err
	}
	byID := make(map[string]accessTokenRule, len(tokens))
	for _, token := range tokens {
		rule := accessTokenRule{AccessToken: token, hosts: compileBlockRule(token.Hosts, nil)}
		for _, value := range token.Ranges {
			prefix, err := netip.ParsePrefix(value)
			rule.ranges, rule.unusable = append(rule.ranges, prefix), rule.unusable || err != nil
		}
		if token.ExpiresAt != "" {
			rule.expires, _ = time.Parse(time.RFC3339, token.ExpiresAt)
		}
		// Two saved tokens with one ID can't be told apart: neither counts.
		if _, twice := byID[token.ID]; twice {
			rule.unusable = true
		}
		byID[token.ID] = rule
	}
	s.access.tokens.Store(&byID)
	return nil
}

// noteTokenUse records that a token let a request in, at most once a second
// a token: requests using one token then don't all write the same entry.
func (s *Store) noteTokenUse(id string) {
	now := s.now()
	if at, ok := s.access.used.Load(id); !ok || now.Sub(at.(time.Time)) >= time.Second {
		s.access.used.Store(id, now)
	}
}

// flushAccessTokenUse saves when tokens last let a request in, so the
// console can show it across restarts.
func (s *Store) flushAccessTokenUse(ctx context.Context) {
	used := map[string]time.Time{}
	s.access.used.Range(func(id, at any) bool {
		used[id.(string)] = at.(time.Time)
		return true
	})
	if len(used) == 0 || s.control == nil {
		return
	}
	if err := s.control.RecordAccessTokenUse(ctx, used, s.now()); err != nil {
		slog.Warn("access token use not saved", "error", err)
		return
	}
	for id, at := range used {
		s.access.used.CompareAndDelete(id, at)
	}
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
		accessPage(w, r, http.StatusUnauthorized, "required")
		return
	}
	bind := browserCookie(r, accessBindCookie)
	http.SetCookie(w, &http.Cookie{Name: accessBindCookie, Value: bind, Path: "/", MaxAge: int(ssoStateTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	query := url.Values{"host": {host}, "return": {accessReturn(r.URL.RequestURI())}, "bind": {Hash(bind)}}
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
	host, returnTo, bind := strings.ToLower(query.Get("host")), accessReturn(query.Get("return")), query.Get("bind")
	ip, public := requestOrigin(r)
	// bind is the hash of the browser's binding cookie.
	if s.control == nil || !public || !validation.Hostname(host) || len(s.access.config.Load().signingIn(host)) == 0 || len(bind) != 64 || strings.Trim(bind, "0123456789abcdef") != "" {
		accessPage(w, r, http.StatusNotFound, "unknown")
		return
	}
	// Each start costs a pending sign-in and, at the callback, maybe a
	// request to the provider and an audit entry.
	back := accessNext{href: "https://" + host + returnTo}
	if !s.access.starts.allow(clientKey(ip), s.now()) {
		w.Header().Set("Retry-After", "60")
		accessPageNext(w, r, http.StatusTooManyRequests, "busy", back)
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
	state, nonce, verifier, browser := Token(), Token(), oauth2.GenerateVerifier(), browserCookie(r, accessStateCookie)
	now := s.now()
	if !s.access.login.put(state, ssoPending{nonce: nonce, verifier: verifier, returnTo: returnTo, host: host, bind: bind, browser: Hash(browser), client: clientKey(ip), expires: now.Add(ssoStateTTL)}, now) {
		w.Header().Set("Retry-After", "60")
		accessPageNext(w, r, http.StatusServiceUnavailable, "busy", back)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: accessStateCookie, Value: browser, Path: "/", MaxAge: int(ssoStateTTL.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, accessOAuth(provider, oidcProvider, redirectURI).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// accessCallback finishes the provider's part and sends the browser to the
// protected address with a one-time ticket for its session.
func (s *Store) accessCallback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	state := query.Get("state")
	cookie, _ := r.Cookie(accessStateCookie)
	w.Header().Set("Cache-Control", "no-store")
	if s.control == nil || cookie == nil || state == "" {
		accessPage(w, r, http.StatusBadRequest, "expired")
		return
	}
	pending, ok := s.access.login.take(state, s.now())
	if !ok {
		accessPage(w, r, http.StatusBadRequest, "expired")
		return
	}
	if subtle.ConstantTimeCompare([]byte(Hash(cookie.Value)), []byte(pending.browser)) != 1 {
		accessPageNext(w, r, http.StatusBadRequest, "expired", accessNext{href: "https://" + pending.host + pending.returnTo})
		return
	}
	failed := func(status int, page, reason, email string) {
		// One client address gets a few failures a minute into the audit
		// log, so failing on purpose can't flood it.
		if ip, _ := requestOrigin(r); s.access.failures.allow(clientKey(ip), s.now()) {
			s.audit(r, "access.login_failed", control.AuditEntry{ActorEmail: attemptedEmail(email), ResourceType: "address", ResourceID: pending.host, Detail: map[string]any{"reason": reason}})
		}
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
	rules := s.access.config.Load().signingIn(pending.host)
	// A session carries only the groups the address's policies name, which
	// keeps the cookie small: a group added to a policy later counts from
	// the next sign-in.
	wanted := policyGroups(rules)
	var groups []string
	for _, group := range claimStrings(all[provider.GroupsClaim]) {
		if wanted[group] && !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	identity := control.AccessIdentity{Email: email, Groups: groups}
	// One policy of the address must let the person in; the database is
	// asked only when none does by the session alone.
	allowed := slices.ContainsFunc(rules, func(rule *accessRule) bool { return rule.policy.Allows(identity, false) })
	if !allowed && slices.ContainsFunc(rules, func(rule *accessRule) bool { return rule.policy.AllowAdmins }) {
		admin, err := s.isAdminEmail(r.Context(), email)
		if err != nil {
			accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
			return
		}
		allowed = admin
	}
	if !allowed {
		failed(http.StatusForbidden, "denied", "not_allowed", email)
		return
	}
	ticket := Token()
	if !s.access.putTicket(ticket, accessTicket{identity: identity, host: pending.host, returnTo: pending.returnTo, bind: pending.bind, expires: s.now().Add(accessTicketTTL)}, s.now()) {
		accessPage(w, r, http.StatusServiceUnavailable, "busy")
		return
	}
	s.audit(r, "access.login", control.AuditEntry{ActorEmail: email, ResourceType: "address", ResourceID: pending.host, Detail: map[string]any{"issuer": idToken.Issuer, "subject": idToken.Subject}})
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
	bind, _ := r.Cookie(accessBindCookie)
	if !ok || key == nil || s.control == nil || bind == nil || subtle.ConstantTimeCompare([]byte(Hash(bind.Value)), []byte(ticket.bind)) != 1 {
		accessPageNext(w, r, http.StatusBadRequest, "expired", accessNext{href: "https://" + host + "/"})
		return
	}
	provider, err := s.control.AccessProvider(r.Context())
	if err != nil || !provider.Enabled {
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	expires := s.now().Add(time.Duration(provider.SessionHours) * time.Hour)
	claims := accessClaims{Email: ticket.identity.Email, Groups: ticket.identity.Groups, Host: host, Expiry: expires.Unix()}
	value, err := signAccess(*key, claims)
	if err == nil && len(value) > maxAccessCookie {
		// Too many groups for a cookie: fewer that satisfy the same policies.
		claims.Groups = fewestGroups(s.access.config.Load().signingIn(host), claims.Groups)
		value, err = signAccess(*key, claims)
	}
	if err != nil || len(value) > maxAccessCookie {
		slog.Error("access session does not fit in a cookie", "host", host, "groups", len(claims.Groups), "bytes", len(value))
		accessPage(w, r, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// Lax: the sign-in arrives as a navigation from the provider's site.
	http.SetCookie(w, &http.Cookie{Name: accessCookie, Value: value, Path: "/", Expires: expires, MaxAge: provider.SessionHours * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, accessReturn(ticket.returnTo), http.StatusSeeOther)
}

// browserCookie is the value of the browser's cookie name when the Outlet
// made it, or a new one.
func browserCookie(r *http.Request, name string) string {
	if cookie, err := r.Cookie(name); err == nil && len(cookie.Value) == 43 && strings.Trim(cookie.Value, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_") == "" {
		return cookie.Value
	}
	return Token()
}

// dropAccessSetCookie keeps an app from setting the Outlet's cookies, which
// would sign its visitors out or plant a binding it knows.
func dropAccessSetCookie(header http.Header) {
	lines := header.Values("Set-Cookie")
	kept := slices.DeleteFunc(slices.Clone(lines), func(line string) bool {
		name, _, _ := strings.Cut(line, "=")
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), strings.ToLower(accessCookie))
	})
	if len(kept) == len(lines) {
		return
	}
	header.Del("Set-Cookie")
	for _, line := range kept {
		header.Add("Set-Cookie", line)
	}
}

// dropAccessHeaders removes from what goes to the app every header named
// like the Outlet's own, X-Meerkat-Access…, in any case and with "_" for
// "-": some app servers read X_Meerkat_Access_Email as the same header.
func dropAccessHeaders(header http.Header) {
	const prefix = "X-Meerkat-Access"
	for name := range header {
		if len(name) >= len(prefix) && strings.EqualFold(strings.ReplaceAll(name[:len(prefix)], "_", "-"), prefix) {
			delete(header, name)
		}
	}
}

// stripAccessCookie keeps the Outlet's cookies from the app: the session
// and the bindings of sign-ins in progress. The app gets the email in a
// header instead. Names compare with the spaces around them trimmed, as Go
// reads them, and in any case, as some apps read them.
func stripAccessCookie(header http.Header) {
	cookies := header.Values("Cookie")
	if len(cookies) == 0 {
		return
	}
	var kept []string
	for _, line := range cookies {
		var parts []string
		for _, part := range strings.Split(line, ";") {
			if name, _, _ := strings.Cut(part, "="); !strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), strings.ToLower(accessCookie)) {
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
	"token":       {"The access token isn't valid.", "접근 토큰이 올바르지 않습니다."},
	"signOut":     {"Sign out of this address?", "이 주소에서 로그아웃하시겠습니까?"},
	"signedOut":   {"Signed out.", "로그아웃되었습니다."},
}

// accessButtons are the buttons of the pages that have one.
var accessButtons = map[string][2]string{
	"signOut": {"Sign out", "로그아웃"},
	"denied":  {"Sign in again", "다시 로그인"},
}

// accessNext is what a page offers next: its button, posting to action, or
// a link to href.
type accessNext struct{ action, href string }

// accessPage answers with one of accessPages. Each depends on who asks, so
// none is stored.
func accessPage(w http.ResponseWriter, r *http.Request, status int, page string) {
	accessPageNext(w, r, status, page, accessNext{})
}

func accessPageNext(w http.ResponseWriter, r *http.Request, status int, page string, next accessNext) {
	i, lang := 0, "en"
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "ko") {
		i, lang = 1, "ko"
	}
	body := `<p>` + html.EscapeString(accessPages[page][i]) + `</p>`
	if next.action != "" {
		body += `<form method="post" action="` + html.EscapeString(next.action) + `"><button>` + html.EscapeString(accessButtons[page][i]) + `</button></form>`
	}
	if next.href != "" {
		body += `<p><a href="` + html.EscapeString(next.href) + `">` + html.EscapeString(next.href) + `</a></p>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`<!doctype html><html lang="` + lang + `"><meta charset="utf-8"><title>Meerkat</title>` + body + `</html>`))
}
