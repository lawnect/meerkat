package outlet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lawnect/meerkat/internal/control"
)

// maxBlockRulesBody bounds a PUT of the block rules, which can be longer
// than the 16 KiB other settings fit in.
const maxBlockRulesBody = 256 << 10

// blockRule is one enabled control.BlockRule, compiled for matching.
type blockRule struct {
	id       string
	ban      bool            // a match bans the client
	hosts    map[string]bool // nil with no wildcards: every public address
	wildcard []string        // ".example.com" for "*.example.com"
	equals   map[string]bool
	prefixes []string
	suffixes []string
	contains []string
	patterns []*regexp.Regexp
	hits     *atomic.Int64
}

// blockRules holds the enabled rules in order, immutable once stored.
type blockRules struct{ rules []blockRule }

type blockState struct {
	config atomic.Pointer[blockRules]
	// hits maps a rule ID to an *atomic.Int64 of requests it blocked since
	// the Outlet started. It survives edits; deleted rules leave it.
	hits sync.Map
}

// applyBlockRules compiles the enabled rules. Rules that ban the client are
// tried first, in their order, then the others: a request for a trap path
// bans its client whatever broader rule comes before.
func (s *Store) applyBlockRules(rules control.BlockRules) {
	compiled := &blockRules{}
	var others []blockRule
	known := map[string]bool{}
	for _, rule := range rules.Rules {
		known[rule.ID] = true
		if !rule.Enabled {
			continue
		}
		hits, _ := s.blocks.hits.LoadOrStore(rule.ID, new(atomic.Int64))
		b := compileBlockRule(rule.Hosts, rule.Conditions)
		b.id, b.ban, b.hits = rule.ID, rule.Ban, hits.(*atomic.Int64)
		if b.ban {
			compiled.rules = append(compiled.rules, b)
		} else {
			others = append(others, b)
		}
	}
	compiled.rules = append(compiled.rules, others...)
	s.blocks.hits.Range(func(id, _ any) bool {
		if !known[id.(string)] {
			s.blocks.hits.Delete(id)
		}
		return true
	})
	s.blocks.config.Store(compiled)
}

// compileBlockRule compiles hosts and path conditions for matching. No hosts
// means every public address.
func compileBlockRule(hosts []string, conditions []control.BlockCondition) blockRule {
	b := blockRule{equals: map[string]bool{}}
	if len(hosts) != 0 {
		b.hosts = map[string]bool{}
		for _, host := range hosts {
			if suffix, ok := strings.CutPrefix(host, "*"); ok {
				b.wildcard = append(b.wildcard, suffix)
			} else {
				b.hosts[host] = true
			}
		}
	}
	for _, c := range conditions {
		switch c.Operator {
		case control.BlockEquals:
			b.equals[c.Value] = true
		case control.BlockStartsWith:
			b.prefixes = append(b.prefixes, c.Value)
		case control.BlockEndsWith:
			b.suffixes = append(b.suffixes, c.Value)
		case control.BlockContains:
			b.contains = append(b.contains, c.Value)
		case control.BlockMatches:
			// Stored patterns were compiled when they were saved.
			if pattern, err := control.CompileBlockPattern(c.Value); err == nil {
				b.patterns = append(b.patterns, pattern)
			}
		}
	}
	return b
}

// rulePath is a request path in the form rules compare (see
// control.NormalizeBlockPath), and with a trailing slash so that
// "/cgi-bin/" matches "/cgi-bin" as well.
type rulePath struct{ p, slashed string }

func newRulePath(requestPath string) rulePath {
	return slashedRulePath(control.NormalizeBlockPath(requestPath))
}

func slashedRulePath(p string) rulePath {
	if p == "/" {
		return rulePath{p, p}
	}
	return rulePath{p, p + "/"}
}

// rulePaths are the forms a request path takes for rules (see
// control.NormalizeBlockPaths): one, or two where servers read a segment
// with parameters differently. A rule holds when it holds for any.
func rulePaths(requestPath string) []rulePath {
	forms := control.NormalizeBlockPaths(requestPath)
	paths := make([]rulePath, len(forms))
	for i, p := range forms {
		paths[i] = slashedRulePath(p)
	}
	return paths
}

// wellKnown says whether every form of a path is under /.well-known/: one
// that some server reads elsewhere is not exempt.
func wellKnown(paths []rulePath) bool {
	for _, path := range paths {
		if !wellKnownPath(path.p) {
			return false
		}
	}
	return true
}

func (s *Store) reloadBlockRules(ctx context.Context) error {
	if s.control == nil {
		return nil
	}
	rules, err := s.control.BlockRules(ctx)
	if err != nil {
		return err
	}
	s.applyBlockRules(rules)
	return nil
}

// wellKnownPath says whether a path in the form rules compare is under
// /.well-known/, where certificate checks and service discovery live: block
// rules and bans never apply there.
func wellKnownPath(p string) bool {
	return p == "/.well-known" || strings.HasPrefix(p, "/.well-known/")
}

// blockedBy returns the first enabled rule that blocks a request for host
// and path, or nil. The path is compared in the form rules are stored in,
// and paths under /.well-known/ are never blocked.
func (s *Store) blockedBy(host, requestPath string) *blockRule {
	config := s.blocks.config.Load()
	if config == nil || len(config.rules) == 0 {
		return nil
	}
	// A path without parameters has one reading; most paths have none.
	if !strings.Contains(requestPath, ";") {
		path := newRulePath(requestPath)
		if wellKnownPath(path.p) {
			return nil
		}
		for i := range config.rules {
			if rule := &config.rules[i]; rule.covers(host) && rule.matches(path) {
				return rule
			}
		}
		return nil
	}
	paths := rulePaths(requestPath)
	if wellKnown(paths) {
		return nil
	}
	for i := range config.rules {
		if rule := &config.rules[i]; rule.covers(host) && slices.ContainsFunc(paths, rule.matches) {
			return rule
		}
	}
	return nil
}

// covers reports whether the rule applies to host.
func (rule *blockRule) covers(host string) bool {
	if rule.hosts == nil || rule.hosts[host] {
		return true
	}
	for _, suffix := range rule.wildcard {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// matches reports whether any condition holds for the path. Patterns are
// tried with the trailing slash too, so "^/admin/" holds for "/admin" as
// starts_with "/admin/" does.
func (rule *blockRule) matches(path rulePath) bool {
	p, slashed := path.p, path.slashed
	if rule.equals[p] {
		return true
	}
	for _, prefix := range rule.prefixes {
		if strings.HasPrefix(slashed, prefix) {
			return true
		}
	}
	for _, suffix := range rule.suffixes {
		if strings.HasSuffix(p, suffix) || strings.HasSuffix(slashed, suffix) {
			return true
		}
	}
	for _, part := range rule.contains {
		if strings.Contains(slashed, part) {
			return true
		}
	}
	for _, pattern := range rule.patterns {
		if pattern.MatchString(p) || pattern.MatchString(slashed) {
			return true
		}
	}
	return false
}

func blocked(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, "Forbidden\n")
}

type blockRulesView struct {
	Rules []control.BlockRule `json:"rules"`
	Hits  map[string]int64    `json:"hits"`
	// Addresses are the public addresses a rule can be limited to.
	Addresses []string `json:"addresses"`
}

func (s *Store) blockRulesView(ctx context.Context, rules control.BlockRules) (blockRulesView, error) {
	routing, err := s.control.Routing(ctx)
	if err != nil {
		return blockRulesView{}, err
	}
	view := blockRulesView{Rules: rules.Rules, Hits: map[string]int64{}, Addresses: routing.Hostnames()}
	for _, rule := range rules.Rules {
		if hits, ok := s.blocks.hits.Load(rule.ID); ok {
			view.Hits[rule.ID] = hits.(*atomic.Int64).Load()
		}
	}
	return view, nil
}

func (s *Store) getBlockRules(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	rules, err := s.control.BlockRules(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	view, err := s.blockRulesView(r.Context(), rules)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}

func (s *Store) putBlockRules(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req control.BlockRules
	if !decodeLimited(w, r, &req, maxBlockRulesBody, "rules") {
		return
	}
	s.saving.Lock()
	saved, err := s.control.SetBlockRules(r.Context(), req, s.now())
	if err == nil {
		s.applyBlockRules(saved)
	}
	s.saving.Unlock()
	if errors.Is(err, control.ErrInvalidBlockRules) {
		fail(w, r, 400, "invalid_block_rules")
		return
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// The lists can be long; the log keeps which rules exist and are on.
	var enabled, disabled []string
	for _, rule := range saved.Rules {
		if rule.Enabled {
			enabled = append(enabled, rule.Name)
		} else {
			disabled = append(disabled, rule.Name)
		}
	}
	s.audited(r, "security.block_rules", "security", "block_rules", map[string]any{"enabled": enabled, "disabled": disabled})
	view, err := s.blockRulesView(r.Context(), saved)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, 200, view)
}
