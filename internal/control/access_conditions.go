package control

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// What an access condition compares.
const (
	// ConditionPath compares the request path with BlockEquals, BlockStartsWith,
	// BlockEndsWith, BlockContains or BlockMatches.
	ConditionPath = "path"
	// ConditionMethod compares the HTTP method, AccessIn the methods named.
	ConditionMethod = "method"
	// ConditionClientIP compares the client address, AccessIn the values: IP
	// address book tags, which stand for every entry carrying them, and
	// addresses or CIDR ranges.
	ConditionClientIP = "client_ip"
	// ConditionToken compares the access token the request sent, valid for it:
	// AccessAnyToken any, AccessIn one of the values, by ID.
	ConditionToken = "token"
)

// Operators of the method, client IP and token conditions; path conditions
// take the block rule operators.
const (
	AccessIn       = "in"
	AccessAnyToken = "any"
)

const (
	maxAccessConditionRows = 20
	maxAccessPathValues    = 50
)

// AccessCondition is one condition of a policy: the request's field compared
// with the values by the operator, matching when any value does, or with
// Not when none does. A policy covers a request that meets all its
// conditions.
type AccessCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Not      bool     `json:"not"`
	Values   []string `json:"values"`
}

// StrictPath reports whether a path condition of a policy with action is
// compared exactly, as the app gets the path: where matching opens what
// it covers to more requests, an app reading a path another way must not
// slip into the opening. A bypass opens what it matches, and a protective
// policy with Not opens what it excludes; the rest compare broadly, so a
// path read another way can't slip past them.
func StrictPath(action string, not bool) bool {
	return (action == AccessBypass) != not
}

// SplitScope sorts client IP values into IP address book tags and
// addresses or ranges, as the console does: a value that looks like an
// address is one (see addressLike).
func SplitScope(values []string) (tags, ranges []string) {
	for _, value := range values {
		if _, isRange := normalizeRange(value); isRange || addressLike.MatchString(value) {
			ranges = append(ranges, value)
		} else {
			tags = append(tags, value)
		}
	}
	return tags, ranges
}

// IPTags are the address book tags the policy's client IP conditions name.
func (p AccessPolicy) IPTags() []string {
	var tags []string
	for _, c := range p.Conditions {
		if c.Field == ConditionClientIP {
			named, _ := SplitScope(c.Values)
			tags = append(tags, named...)
		}
	}
	return tags
}

// TakesToken reports whether the policy lets an access token count: one of
// its conditions asks for one. Such a policy notes the token's use and
// names it to the app.
func (p AccessPolicy) TakesToken() bool {
	return slices.ContainsFunc(p.Conditions, func(c AccessCondition) bool { return c.Field == ConditionToken && !c.Not })
}

// NamesToken reports whether one of the policy's token conditions names the
// access token with ID id.
func (p AccessPolicy) NamesToken(id string) bool {
	return slices.ContainsFunc(p.Conditions, func(c AccessCondition) bool { return c.Field == ConditionToken && slices.Contains(c.Values, id) })
}

// normalizeConditions checks a policy's conditions and puts them in the
// form requests are compared in, each once.
func normalizeConditions(action string, conditions []AccessCondition) ([]AccessCondition, bool) {
	if len(conditions) > maxAccessConditionRows {
		return nil, false
	}
	out := make([]AccessCondition, 0, len(conditions))
	for _, c := range conditions {
		normalized, ok := normalizeAccessCondition(action, c)
		if !ok {
			return nil, false
		}
		if !slices.ContainsFunc(out, func(seen AccessCondition) bool { return sameCondition(seen, normalized) }) {
			out = append(out, normalized)
		}
	}
	return out, true
}

func sameCondition(a, b AccessCondition) bool {
	return a.Field == b.Field && a.Operator == b.Operator && a.Not == b.Not && slices.Equal(a.Values, b.Values)
}

func normalizeAccessCondition(action string, c AccessCondition) (AccessCondition, bool) {
	values := []string{}
	switch c.Field {
	case ConditionPath:
		if len(c.Values) == 0 || len(c.Values) > maxAccessPathValues {
			return c, false
		}
		strict := StrictPath(action, c.Not)
		for _, value := range c.Values {
			condition := BlockCondition{Operator: c.Operator, Value: value}
			// A path the policy could never match would leave it open.
			if c.Operator != BlockMatches && (percentEscape.MatchString(value) || strings.Contains(value, ";")) {
				return c, false
			}
			var ok bool
			if strict {
				condition, ok = bypassCondition(condition)
			} else {
				condition, ok = normalizeCondition(condition)
			}
			if !ok {
				return c, false
			}
			if !slices.Contains(values, condition.Value) {
				values = append(values, condition.Value)
			}
		}
	case ConditionMethod:
		if c.Operator != AccessIn || len(c.Values) == 0 {
			return c, false
		}
		named := map[string]bool{}
		for _, method := range c.Values {
			if method = strings.ToUpper(strings.TrimSpace(method)); !slices.Contains(AccessMethods, method) {
				return c, false
			}
			named[method] = true
		}
		for _, method := range AccessMethods {
			if named[method] {
				values = append(values, method)
			}
		}
	case ConditionClientIP:
		if c.Operator != AccessIn || len(c.Values) == 0 {
			return c, false
		}
		trimmed := make([]string, len(c.Values))
		for i, value := range c.Values {
			trimmed[i] = strings.TrimSpace(value)
		}
		tags, ranges := SplitScope(trimmed)
		tags, okTags := NormalizeTags(tags, MaxScopeTags)
		ranges, okRanges := normalizeRanges(ranges)
		if !okTags || !okRanges || len(ranges) > MaxScopeRanges {
			return c, false
		}
		values = append(tags, ranges...)
	case ConditionToken:
		switch c.Operator {
		case AccessAnyToken:
			if len(c.Values) != 0 {
				return c, false
			}
		case AccessIn:
			if len(c.Values) == 0 || len(c.Values) > MaxAccessTokens {
				return c, false
			}
			for _, id := range c.Values {
				if !validRuleID(id) {
					return c, false
				}
				if !slices.Contains(values, id) {
					values = append(values, id)
				}
			}
		default:
			return c, false
		}
	default:
		return c, false
	}
	return AccessCondition{Field: c.Field, Operator: c.Operator, Not: c.Not, Values: values}, true
}

// accessPolicyFormat marks policies stored with conditions. Policies stored
// before it (0.7 and earlier) are read and turned into conditions.
const accessPolicyFormat = 2

// conditionsMarker is stored among the methods of every policy with
// conditions. Versions before conditions don't accept it as a method, so
// they read the saved policies as damaged and keep closed what they protect
// (closedAccess there) instead of reading what they can of them: their
// conditions would read as paths that aren't, and what they leave out
// would widen the policies.
const conditionsMarker = "CONDITIONS"

// storedPolicy is a policy as saved: in this version's form, marked with
// Format, or in the form before conditions, which had a path list and a
// field for each of methods, tokens and client IPs.
type storedPolicy struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Enabled       bool              `json:"enabled"`
	Action        string            `json:"action"`
	Hosts         []string          `json:"hosts"`
	Format        int               `json:"format,omitempty"`
	Conditions    []storedCondition `json:"conditions"`
	AllowEveryone bool              `json:"allow_everyone"`
	AllowedUsers  []string          `json:"allowed_users"`
	AllowedGroups []string          `json:"allowed_groups"`
	AllowAdmins   bool              `json:"allow_admins"`

	// The form before conditions. A policy saved with conditions keeps
	// all_paths false and the marker in methods, requires a token when it
	// is limited (see limited): closedAccess before conditions lets nobody
	// pass such an allow policy; and names its tokens, tags and ranges, which
	// that version won't let go while a policy names them.
	AllPaths     bool     `json:"all_paths"`
	Methods      []string `json:"methods"`
	RequireToken bool     `json:"require_token"`
	Tokens       []string `json:"tokens,omitempty"`
	IPTags       []string `json:"ip_tags,omitempty"`
	IPRanges     []string `json:"ip_ranges,omitempty"`
}

// storedCondition reads a condition of either form: a path condition
// before conditions had an operator and one value.
type storedCondition struct {
	Field    string   `json:"field,omitempty"`
	Operator string   `json:"operator"`
	Not      bool     `json:"not,omitempty"`
	Values   []string `json:"values,omitempty"`
	Value    string   `json:"value,omitempty"`
}

type storedPolicies struct {
	Policies []storedPolicy `json:"policies"`
}

// storePolicies gives policies their saved form.
func storePolicies(policies AccessPolicies) storedPolicies {
	out := storedPolicies{Policies: make([]storedPolicy, len(policies.Policies))}
	for i, p := range policies.Policies {
		conditions := make([]storedCondition, len(p.Conditions))
		for j, c := range p.Conditions {
			conditions[j] = storedCondition{Field: c.Field, Operator: c.Operator, Not: c.Not, Values: c.Values}
		}
		stored := storedPolicy{
			ID: p.ID, Name: p.Name, Enabled: p.Enabled, Action: p.Action, Hosts: p.Hosts, Format: accessPolicyFormat, Conditions: conditions,
			AllowEveryone: p.AllowEveryone, AllowedUsers: p.AllowedUsers, AllowedGroups: p.AllowedGroups, AllowAdmins: p.AllowAdmins,
			Methods: []string{conditionsMarker}, RequireToken: limited(p),
		}
		// What 0.7 checks before deleting a token or an address book tag.
		for _, c := range p.Conditions {
			switch c.Field {
			case ConditionToken:
				stored.Tokens = append(stored.Tokens, c.Values...)
			case ConditionClientIP:
				tags, ranges := SplitScope(c.Values)
				stored.IPTags, stored.IPRanges = append(stored.IPTags, tags...), append(stored.IPRanges, ranges...)
			}
		}
		out.Policies[i] = stored
	}
	return out
}

// limited reports whether an allow policy's conditions decide whom it
// covers. Saved policies that fail the checks are kept closed by letting
// whom an allow policy names in to all of its addresses (closedAccess):
// for one limited to some paths, methods, clients or tokens, that would let
// them where they weren't let in, so nobody passes it instead.
func limited(p AccessPolicy) bool {
	return (p.Action == AccessAllow || p.Action == "") && len(p.Conditions) != 0
}

// Limited reports whether closedAccess must let nobody past the allow
// policy p (see limited).
func (p AccessPolicy) Limited() bool { return limited(p) }

// readPolicies turns saved policies into this version's form, as saved:
// not yet checked. A policy of the form before conditions whose paths use
// more than one operator becomes one policy per operator, in a row, which
// cover between them what it did: conditions of one field all hold, so a
// path can match only one operator's values in a policy. The first keeps
// its ID; the others get one made from it, the same on every read.
func readPolicies(stored storedPolicies) AccessPolicies {
	out := AccessPolicies{Policies: make([]AccessPolicy, 0, len(stored.Policies))}
	taken := map[string]bool{}
	for _, s := range stored.Policies {
		taken[s.ID] = true
	}
	for _, s := range stored.Policies {
		policy := AccessPolicy{
			ID: s.ID, Name: s.Name, Enabled: s.Enabled, Action: s.Action, Hosts: s.Hosts,
			AllowEveryone: s.AllowEveryone, AllowedUsers: s.AllowedUsers, AllowedGroups: s.AllowedGroups, AllowAdmins: s.AllowAdmins,
		}
		if s.Format == accessPolicyFormat {
			policy.Conditions = make([]AccessCondition, len(s.Conditions))
			for i, c := range s.Conditions {
				policy.Conditions[i] = AccessCondition{Field: c.Field, Operator: c.Operator, Not: c.Not, Values: c.Values}
			}
			out.Policies = append(out.Policies, policy)
			continue
		}
		out.Policies = append(out.Policies, legacyPolicies(policy, s, taken)...)
	}
	return out
}

// splitID is the ID of a policy split off the one with ID id: id with a
// number after it, cut to fit the 32 characters an ID may have, and the
// first such number no saved policy has, so it is the same on every read.
func splitID(id string, n int, taken map[string]bool) string {
	for ; ; n++ {
		suffix := fmt.Sprintf("-%d", n)
		base := id
		if len(base)+len(suffix) > 32 {
			base = base[:max(32-len(suffix), 0)]
		}
		if candidate := base + suffix; !taken[candidate] {
			taken[candidate] = true
			return candidate
		}
	}
}

func legacyPolicies(policy AccessPolicy, s storedPolicy, taken map[string]bool) []AccessPolicy {
	var others []AccessCondition
	if methods := slices.DeleteFunc(slices.Clone(s.Methods), func(m string) bool { return m == clientIPMarker }); len(methods) != 0 {
		others = append(others, AccessCondition{Field: ConditionMethod, Operator: AccessIn, Values: methods})
	}
	if s.RequireToken {
		if len(s.Tokens) == 0 {
			others = append(others, AccessCondition{Field: ConditionToken, Operator: AccessAnyToken, Values: []string{}})
		} else {
			others = append(others, AccessCondition{Field: ConditionToken, Operator: AccessIn, Values: s.Tokens})
		}
	}
	if ips := append(slices.Clone(s.IPTags), s.IPRanges...); len(ips) != 0 {
		others = append(others, AccessCondition{Field: ConditionClientIP, Operator: AccessIn, Values: ips})
	}
	if s.AllPaths {
		policy.Conditions = others
		return []AccessPolicy{policy}
	}
	// Paths by operator, in the order they first appear.
	var paths []AccessCondition
	for _, c := range s.Conditions {
		i := slices.IndexFunc(paths, func(p AccessCondition) bool { return p.Operator == c.Operator })
		if i < 0 {
			paths = append(paths, AccessCondition{Field: ConditionPath, Operator: c.Operator, Values: []string{}})
			i = len(paths) - 1
		}
		paths[i].Values = append(paths[i].Values, c.Value)
	}
	if len(paths) == 0 {
		// Neither all paths nor any: damaged, as it was.
		policy.Conditions = append([]AccessCondition{{Field: ConditionPath, Operator: BlockStartsWith, Values: []string{}}}, others...)
		return []AccessPolicy{policy}
	}
	out := make([]AccessPolicy, len(paths))
	for i, path := range paths {
		split := policy
		split.Conditions = append([]AccessCondition{path}, others...)
		if i > 0 {
			split.ID = splitID(policy.ID, i+1, taken)
			suffix := fmt.Sprintf(" (%d)", i+1)
			name := []rune(policy.Name)
			if len(name)+len([]rune(suffix)) > maxAccessPolicyName {
				name = name[:max(maxAccessPolicyName-len([]rune(suffix)), 0)]
			}
			split.Name = string(name) + suffix
		}
		out[i] = split
	}
	return out
}

// marshalPolicies is the saved form of policies, as JSON.
func marshalPolicies(policies AccessPolicies) (string, error) {
	raw, err := json.Marshal(storePolicies(policies))
	return string(raw), err
}
