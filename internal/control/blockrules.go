package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/lawnect/meerkat/internal/validation"
)

var ErrInvalidBlockRules = errors.New("invalid block rules")

const (
	settingBlockRules = "block_rules"

	MaxBlockRules      = 50
	maxBlockConditions = 50
	maxBlockValueBytes = 512
	maxBlockNameRunes  = 64
)

// Operators of a block condition, compared against the normalized path.
const (
	BlockEquals     = "equals"
	BlockStartsWith = "starts_with"
	BlockEndsWith   = "ends_with"
	BlockContains   = "contains"
	BlockMatches    = "matches" // RE2 regular expression, case-insensitive
)

// BlockCondition tests the request path with one operator.
type BlockCondition struct {
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// BlockRule answers 403 to requests for public addresses whose path matches
// any of its conditions. Hosts limits it to some hostnames; empty means
// every public address. "*.example.com" covers every name under
// example.com, at any depth, but not example.com itself. A rule with Ban also
// bans the client that matched it: for a while every request from that
// client to any public address is refused (see ClientBanSettings).
type BlockRule struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Enabled    bool             `json:"enabled"`
	Ban        bool             `json:"ban"`
	Hosts      []string         `json:"hosts"`
	Conditions []BlockCondition `json:"conditions"`
}

type BlockRules struct {
	Rules []BlockRule `json:"rules"`
}

// HoneypotRuleID is the rule that bans a client for asking for a path only a
// scanner asks for.
const HoneypotRuleID = "honeypot"

// honeypotRule is the rule DefaultBlockRules starts with and migration 15
// adds to saved rules: secrets, repositories and configuration no
// application serves, which only scanners look for, wherever they look
// (/.env, /api/.env.production, /.git/config, a copy of wp-config.php).
func honeypotRule() BlockRule {
	return BlockRule{ID: HoneypotRuleID, Name: "Honeypot", Enabled: true, Ban: true, Hosts: []string{}, Conditions: []BlockCondition{
		{BlockStartsWith, "/.env"},
		{BlockStartsWith, "/.git/"},
		{BlockStartsWith, "/.aws/"},
		{BlockStartsWith, "/.ssh/"},
		{BlockStartsWith, "/.svn/"},
		{BlockStartsWith, "/.hg/"},
		{BlockStartsWith, "/.docker/"},
		{BlockStartsWith, "/.kube/"},
		{BlockStartsWith, "/.htpasswd"},
		{BlockStartsWith, "/.ds_store"},
		{BlockMatches, `/\.env(\.[a-z0-9_-]+)?$`},
		{BlockMatches, `/\.git/(config|head)$`},
		{BlockContains, "/wp-config.php"},
	}}
}

// DefaultBlockRules starts with the honeypot, which bans the clients that ask
// for what no application serves, and turns on only that and the hidden
// files at the root. The scanner list blocks paths real applications use
// (.php for Nextcloud or WordPress, .zip downloads), and the last rule is an
// example of paths a particular site does not have, so both start off, to be
// turned on for the addresses that do not need them.
func DefaultBlockRules() BlockRules {
	return BlockRules{Rules: []BlockRule{
		honeypotRule(),
		{ID: "hidden-files", Name: "Hidden files", Enabled: true, Hosts: []string{}, Conditions: []BlockCondition{
			{BlockStartsWith, "/."},
		}},
		{ID: "scanners", Name: "Vulnerability scanners", Enabled: false, Hosts: []string{}, Conditions: []BlockCondition{
			{BlockMatches, `\.(php|phtml|asp|aspx|jsp|cgi|sql|bak|key|log|ya?ml|zip|env)$`},
			{BlockStartsWith, "/cgi-bin/"},
			{BlockContains, "/wp-"},
			{BlockContains, "/.env"},
			{BlockMatches, `^/(env|pma|phpmyadmin|cpanel|kpanel|plesk-stat|plesk/login|setup|config\.json|config\.js|settings\.js|secrets\.json|js/env\.js|js/config\.js|api/env|api/config|actuator/heapdump|_ignition/health-check)$`},
		}},
		{ID: "unused-paths", Name: "Paths this site does not use", Enabled: false, Hosts: []string{}, Conditions: []BlockCondition{
			{BlockStartsWith, "/assets/"},
			{BlockStartsWith, "/media/"},
			{BlockStartsWith, "/static/"},
			{BlockStartsWith, "/uploads/"},
			{BlockMatches, `^/(admin|administrator|components|feed|files|login|register|backup\.tar\.gz|config\.xml|config/production\.json)$`},
		}},
	}}
}

// NormalizeBlockPath is the form of a path that rules compare: decoded by
// the caller, backslashes read as slashes and ";parameters" and anything
// after a NUL dropped from each segment, as some servers do, cleaned and
// lowercased, then trailing dots and spaces dropped from every segment, as
// some servers also do ("/admin./x" is "/admin/x" to them), save from a
// segment of only those.
func NormalizeBlockPath(p string) string {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	if i := strings.IndexByte(p, 0); i >= 0 {
		p = p[:i]
	}
	segments := strings.Split(path.Clean("/"+p), "/")
	for i, segment := range segments {
		segment, _, _ = strings.Cut(segment, ";")
		segments[i] = trimSegment(segment)
	}
	return path.Clean("/" + strings.Join(segments, "/"))
}

// trimSegment drops trailing dots and spaces from a segment, unless that
// would leave nothing: "..." and ". " stay as they are.
func trimSegment(segment string) string {
	if trimmed := strings.TrimRight(segment, ". "); trimmed != "" {
		return trimmed
	}
	return segment
}

// conditionFragment puts part of a path in the form NormalizeBlockPath
// gives whole ones: lowercased, backslashes read as slashes, and segments
// trimmed where they surely end, before a slash, and at the end of the
// value when whole says it ends a segment there too.
func conditionFragment(value string, whole bool) string {
	parts := strings.Split(strings.ToLower(strings.ReplaceAll(value, `\`, "/")), "/")
	for i, part := range parts {
		if i < len(parts)-1 || whole {
			parts[i] = trimSegment(part)
		}
	}
	return strings.Join(parts, "/")
}

// CompileBlockPattern compiles a "matches" value. Patterns are RE2, so
// matching time is linear in the path whatever the pattern.
func CompileBlockPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile("(?i)" + pattern)
}

// normalizeCondition puts a condition in the form requests are compared in.
func normalizeCondition(c BlockCondition) (BlockCondition, bool) {
	value := strings.TrimSpace(c.Value)
	if value == "" || len(value) > maxBlockValueBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return c, false
	}
	switch c.Operator {
	case BlockEquals:
		if !strings.HasPrefix(value, "/") {
			return c, false
		}
		value = NormalizeBlockPath(value)
	case BlockStartsWith:
		if !strings.HasPrefix(value, "/") {
			return c, false
		}
		value = conditionFragment(value, false)
	case BlockEndsWith, BlockContains:
		value = conditionFragment(value, c.Operator == BlockEndsWith)
	case BlockMatches:
		if _, err := CompileBlockPattern(value); err != nil {
			return c, false
		}
	default:
		return c, false
	}
	return BlockCondition{Operator: c.Operator, Value: value}, true
}

// validBlockHost accepts a hostname or "*." followed by one.
func validBlockHost(host string) bool {
	if suffix, ok := strings.CutPrefix(host, "*."); ok {
		return validation.Hostname(suffix)
	}
	return validation.Hostname(host)
}

func validRuleID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func newRuleID() (string, error) {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Normalize checks the rules, puts conditions in the form requests are
// compared in, and gives new rules an ID.
func (b BlockRules) Normalize() (BlockRules, bool) {
	if len(b.Rules) > MaxBlockRules {
		return b, false
	}
	rules := make([]BlockRule, 0, len(b.Rules))
	ids := map[string]bool{}
	for _, rule := range b.Rules {
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Name == "" || len([]rune(rule.Name)) > maxBlockNameRunes || strings.IndexFunc(rule.Name, unicode.IsControl) >= 0 {
			return b, false
		}
		if rule.ID == "" {
			id, err := newRuleID()
			if err != nil {
				return b, false
			}
			rule.ID = id
		}
		if !validRuleID(rule.ID) || ids[rule.ID] {
			return b, false
		}
		ids[rule.ID] = true
		hosts := make([]string, 0, len(rule.Hosts))
		for _, host := range rule.Hosts {
			host = strings.ToLower(strings.TrimSpace(host))
			if !validBlockHost(host) || len(hosts) == MaxBlockRules*4 {
				return b, false
			}
			if !containsString(hosts, host) {
				hosts = append(hosts, host)
			}
		}
		rule.Hosts = hosts
		if len(rule.Conditions) == 0 || len(rule.Conditions) > maxBlockConditions {
			return b, false
		}
		conditions := make([]BlockCondition, 0, len(rule.Conditions))
		for _, c := range rule.Conditions {
			c, ok := normalizeCondition(c)
			if !ok {
				return b, false
			}
			if !containsCondition(conditions, c) {
				conditions = append(conditions, c)
			}
		}
		rule.Conditions = conditions
		rules = append(rules, rule)
	}
	b.Rules = rules
	return b, true
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func containsCondition(list []BlockCondition, c BlockCondition) bool {
	for _, item := range list {
		if item == c {
			return true
		}
	}
	return false
}

// addHoneypotRule is migration 15. Saved rules get the honeypot in front,
// unless they hold a rule of that ID already (the administrator has one, or
// deleted it and added it back) or are as many as can be saved. With no
// saved rules the defaults apply, which start with it. Once applied it is
// never added again, so deleting the rule sticks.
func addHoneypotRule(ctx context.Context, q conn) error {
	raw, err := getSetting(ctx, q, settingBlockRules)
	if err != nil || raw == "" {
		return err
	}
	var saved BlockRules
	if json.Unmarshal([]byte(raw), &saved) != nil || len(saved.Rules) >= MaxBlockRules {
		return nil
	}
	for _, rule := range saved.Rules {
		if rule.ID == HoneypotRuleID {
			return nil
		}
	}
	saved.Rules = append([]BlockRule{honeypotRule()}, saved.Rules...)
	updated, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	return putSetting(ctx, q, settingBlockRules, string(updated), time.Now())
}

// BlockRules returns the stored rules, or the defaults.
func (d *Database) BlockRules(ctx context.Context) (BlockRules, error) {
	raw, err := getSetting(ctx, d.db, settingBlockRules)
	if err != nil || raw == "" {
		return DefaultBlockRules(), err
	}
	var rules BlockRules
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return DefaultBlockRules(), nil
	}
	if rules, ok := rules.Normalize(); ok {
		return rules, nil
	}
	return DefaultBlockRules(), nil
}

func (d *Database) SetBlockRules(ctx context.Context, rules BlockRules, now time.Time) (BlockRules, error) {
	rules, ok := rules.Normalize()
	if !ok {
		return rules, ErrInvalidBlockRules
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return rules, err
	}
	return rules, putSetting(ctx, d.db, settingBlockRules, string(raw), now)
}
