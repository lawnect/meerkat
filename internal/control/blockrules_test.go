package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBlockRulesAreNormalizedAndChecked(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.BlockRules(ctx)
	if err != nil || len(got.Rules) != 4 || got.Rules[0].ID != HoneypotRuleID || !got.Rules[0].Enabled || !got.Rules[0].Ban ||
		got.Rules[1].ID != "hidden-files" || !got.Rules[1].Enabled || got.Rules[1].Ban || got.Rules[2].Enabled || got.Rules[3].Enabled {
		t.Fatalf("defaults = %+v, %v", got, err)
	}
	if _, ok := DefaultBlockRules().Normalize(); !ok {
		t.Fatal("the defaults do not pass their own checks")
	}
	saved, err := db.SetBlockRules(ctx, BlockRules{Rules: []BlockRule{{
		Name: " Scanners ", Enabled: true, Hosts: []string{"Inc.Example.com", "inc.example.com", " *.Dev.Example.com "},
		Conditions: []BlockCondition{
			{BlockEquals, "/Setup/"}, {BlockEquals, "//a/../phpMyAdmin"}, {BlockStartsWith, "/CGI-BIN/"},
			{BlockEndsWith, ".PHP"}, {BlockContains, " /WP- "}, {BlockMatches, `\.(PHP|asp)$`}, {BlockEndsWith, ".php"},
		},
	}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rule := saved.Rules[0]
	want := []BlockCondition{{BlockEquals, "/setup"}, {BlockEquals, "/phpmyadmin"}, {BlockStartsWith, "/cgi-bin/"}, {BlockEndsWith, ".php"}, {BlockContains, "/wp-"}, {BlockMatches, `\.(PHP|asp)$`}}
	if rule.ID == "" || rule.Name != "Scanners" || !slices.Equal(rule.Hosts, []string{"inc.example.com", "*.dev.example.com"}) || !slices.Equal(rule.Conditions, want) {
		t.Fatalf("normalized = %+v", rule)
	}
	if again, err := db.BlockRules(ctx); err != nil || again.Rules[0].ID != rule.ID {
		t.Fatalf("stored = %+v, %v", again, err)
	}
	one := func(c BlockCondition) []BlockCondition { return []BlockCondition{c} }
	for _, bad := range []BlockRule{
		{Name: "", Conditions: one(BlockCondition{BlockContains, "x"})},
		{Name: strings.Repeat("x", 65), Conditions: one(BlockCondition{BlockContains, "x"})},
		{Name: "no conditions"},
		{Name: "bare wildcard", Hosts: []string{"*"}, Conditions: one(BlockCondition{BlockContains, "x"})},
		{Name: "inner wildcard", Hosts: []string{"a.*.example.com"}, Conditions: one(BlockCondition{BlockContains, "x"})},
		{Name: "wildcard of nothing", Hosts: []string{"*."}, Conditions: one(BlockCondition{BlockContains, "x"})},
		{Name: "bad operator", Conditions: one(BlockCondition{"wildcard", "/x*"})},
		{Name: "relative", Conditions: one(BlockCondition{BlockStartsWith, "cgi-bin"})},
		{Name: "empty", Conditions: one(BlockCondition{BlockContains, "  "})},
		{Name: "control", Conditions: one(BlockCondition{BlockContains, "a\x00b"})},
		{Name: "not RE2", Conditions: one(BlockCondition{BlockMatches, `^/(?!well-known)`})},
		{Name: "too long", Conditions: one(BlockCondition{BlockContains, strings.Repeat("x", maxBlockValueBytes+1)})},
		{ID: "bad id", Name: "x", Conditions: one(BlockCondition{BlockContains, "x"})},
	} {
		if _, err := db.SetBlockRules(ctx, BlockRules{Rules: []BlockRule{bad}}, time.Now()); !errors.Is(err, ErrInvalidBlockRules) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}
	same := BlockRule{ID: "same", Name: "x", Conditions: one(BlockCondition{BlockContains, "x"})}
	if _, err := db.SetBlockRules(ctx, BlockRules{Rules: []BlockRule{same, same}}, time.Now()); !errors.Is(err, ErrInvalidBlockRules) {
		t.Fatalf("duplicate IDs: %v", err)
	}
}

// Paths are compared decoded, cleaned, lowercased, and with trailing dots
// and spaces dropped from every segment, save from one of only those; the
// form stays the same when taken again.
func TestNormalizeBlockPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "/",
		"/.ENV":             "/.env",
		"//.env":            "/.env",
		"/a/../.ENV":        "/.env",
		`\..\.env`:          "/.env",
		"/x.php;jsessionid": "/x.php",
		"/x.php\x00.jpg":    "/x.php",
		"/setup/":           "/setup",
		"/x/..;/admin":      "/admin",
		"/Admin./x":         "/admin/x",
		"/admin. . /":       "/admin",
		"/.env.":            "/.env",
		"/a..b/c":           "/a..b/c",
		"/a/.../b":          "/a/.../b",
		"/a/. /b":           "/a/. /b",
	} {
		got := NormalizeBlockPath(in)
		if got != want || NormalizeBlockPath(got) != got {
			t.Errorf("NormalizeBlockPath(%q) = %q (again %q), want %q", in, got, NormalizeBlockPath(got), want)
		}
	}
}

// Conditions take the form request paths are compared in, where the form
// keeps their meaning: a segment surely ends before a slash, and at the end
// of an equals or ends_with value.
func TestConditionsMatchHowRequestsCompare(t *testing.T) {
	for _, c := range []struct{ in, want BlockCondition }{
		{BlockCondition{BlockEquals, "/Secret."}, BlockCondition{BlockEquals, "/secret"}},
		{BlockCondition{BlockStartsWith, "/Private./"}, BlockCondition{BlockStartsWith, "/private/"}},
		{BlockCondition{BlockStartsWith, `/a\b`}, BlockCondition{BlockStartsWith, "/a/b"}},
		{BlockCondition{BlockStartsWith, "/adm."}, BlockCondition{BlockStartsWith, "/adm."}},
		{BlockCondition{BlockStartsWith, "/."}, BlockCondition{BlockStartsWith, "/."}},
		{BlockCondition{BlockEndsWith, ".PHP."}, BlockCondition{BlockEndsWith, ".php"}},
		{BlockCondition{BlockEndsWith, "."}, BlockCondition{BlockEndsWith, "."}},
		{BlockCondition{BlockEndsWith, "/.."}, BlockCondition{BlockEndsWith, "/.."}},
		{BlockCondition{BlockContains, "../"}, BlockCondition{BlockContains, "../"}},
		{BlockCondition{BlockContains, "/.env"}, BlockCondition{BlockContains, "/.env"}},
		{BlockCondition{BlockContains, "a. /b"}, BlockCondition{BlockContains, "a/b"}},
		{BlockCondition{BlockMatches, `^/Admin\.$`}, BlockCondition{BlockMatches, `^/Admin\.$`}},
	} {
		got, ok := normalizeCondition(c.in)
		if !ok || got != c.want {
			t.Errorf("%+v = %+v %v, want %+v", c.in, got, ok, c.want)
		}
		if again, _ := normalizeCondition(got); again != got {
			t.Errorf("%+v again = %+v", got, again)
		}
	}
	// The default rules are already in that form.
	defaults, ok := DefaultBlockRules().Normalize()
	if !ok || fmt.Sprint(defaults) != fmt.Sprint(DefaultBlockRules()) {
		t.Fatalf("defaults changed: %+v", defaults)
	}
}

// A rule that bans keeps doing so through saving and reading, and the
// defaults start with the honeypot.
func TestBlockRuleBanRoundTrips(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defaults := DefaultBlockRules()
	first := defaults.Rules[0]
	if first.ID != HoneypotRuleID || first.Name != "Honeypot" || !first.Enabled || !first.Ban || len(first.Hosts) != 0 {
		t.Fatalf("first default rule = %+v", first)
	}
	for _, rule := range defaults.Rules[1:] {
		if rule.Ban {
			t.Errorf("%s bans by default", rule.ID)
		}
	}
	one := func(c BlockCondition) []BlockCondition { return []BlockCondition{c} }
	saved, err := db.SetBlockRules(ctx, BlockRules{Rules: []BlockRule{
		{ID: "trap", Name: "Trap", Enabled: true, Ban: true, Conditions: one(BlockCondition{BlockStartsWith, "/Admin-Backup"})},
		{ID: "plain", Name: "Plain", Enabled: true, Conditions: one(BlockCondition{BlockEquals, "/x"})},
	}}, time.Now())
	if err != nil || !saved.Rules[0].Ban || saved.Rules[1].Ban {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if again, err := db.BlockRules(ctx); err != nil || !again.Rules[0].Ban || again.Rules[1].Ban {
		t.Fatalf("stored = %+v, %v", again, err)
	}
	raw, err := json.Marshal(saved.Rules)
	if err != nil || !strings.Contains(string(raw), `"ban":true`) || !strings.Contains(string(raw), `"ban":false`) {
		t.Fatalf("json = %s, %v", raw, err)
	}
	// A request that leaves the field out does not ban.
	var read BlockRules
	if err := json.Unmarshal([]byte(`{"rules":[{"id":"a","name":"a","enabled":true,"hosts":[],"conditions":[{"operator":"equals","value":"/x"}]}]}`), &read); err != nil || read.Rules[0].Ban {
		t.Fatalf("read = %+v, %v", read, err)
	}
}

// Migration 15 puts the honeypot first in the rules an administrator
// saved, once: not when they have it, not when there is no room for it, and
// not again after they delete it.
func TestHoneypotMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	reopen := func() *Database {
		t.Helper()
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	// Rewind to before the migration: save what the previous version held,
	// and forget that 15 was applied.
	rewind := func(db *Database, saved string) {
		t.Helper()
		queries := []string{"DELETE FROM schema_migrations WHERE version = 15", "DELETE FROM outlet_settings WHERE key = 'block_rules'"}
		if saved != "" {
			queries = append(queries, "INSERT INTO outlet_settings(key, value, updated_at) VALUES ('block_rules', '"+strings.ReplaceAll(saved, "'", "''")+"', 'then')")
		}
		for _, query := range queries {
			if _, err := db.SQL().ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
		}
	}
	stored := func(db *Database) (string, bool) {
		t.Helper()
		var value string
		err := db.SQL().QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = 'block_rules'").Scan(&value)
		return value, err == nil
	}
	rules := func(ids ...string) string {
		list := make([]BlockRule, 0, len(ids))
		for _, id := range ids {
			list = append(list, BlockRule{ID: id, Name: id, Enabled: id != "off", Hosts: []string{"a.example.com"}, Conditions: []BlockCondition{{BlockStartsWith, "/x"}}})
		}
		raw, err := json.Marshal(BlockRules{Rules: list})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	ids := func(db *Database) []string {
		t.Helper()
		got, err := db.BlockRules(ctx)
		if err != nil {
			t.Fatal(err)
		}
		list := []string{}
		for _, rule := range got.Rules {
			list = append(list, rule.ID)
		}
		return list
	}
	many := make([]string, MaxBlockRules)
	for i := range many {
		many[i] = fmt.Sprint("rule", i)
	}

	db := reopen()
	defaults := []string{"honeypot", "hidden-files", "scanners", "unused-paths"}
	for _, c := range []struct {
		name    string
		saved   string   // what the previous version had saved; "" for nothing
		changed bool     // whether the migration rewrote it
		want    []string // the rules in force afterwards
	}{
		{"nothing saved", "", false, defaults},
		{"rules without it", rules("hidden-files", "off", "mine"), true, []string{"honeypot", "hidden-files", "off", "mine"}},
		{"an empty list", `{"rules":[]}`, true, []string{"honeypot"}},
		{"it is there already", rules("mine", "honeypot", "off"), false, []string{"mine", "honeypot", "off"}},
		{"room for one more", rules(many[:MaxBlockRules-1]...), true, append([]string{"honeypot"}, many[:MaxBlockRules-1]...)},
		{"no room", rules(many...), false, many},
		{"unreadable", `{"rules":`, false, defaults},
	} {
		rewind(db, c.saved)
		db.Close()
		db = reopen()
		got, saved := stored(db)
		if c.saved == "" && saved || c.saved != "" && (got != c.saved) != c.changed {
			t.Errorf("%s: stored %q (was %q), changed %v", c.name, got, c.saved, c.changed)
		}
		if list := ids(db); !slices.Equal(list, c.want) {
			t.Errorf("%s: rules %v, want %v", c.name, list, c.want)
		}
		if version, err := db.Version(ctx); err != nil || version != migrations[len(migrations)-1].version {
			t.Errorf("%s: version %d, %v", c.name, version, err)
		}
	}

	// What it adds is the honeypot as the defaults have it, in front of the
	// rules as they were.
	rewind(db, rules("hidden-files", "off"))
	db.Close()
	db = reopen()
	list, err := db.BlockRules(ctx)
	if err != nil || len(list.Rules) != 3 || !reflect.DeepEqual(list.Rules[0], DefaultBlockRules().Rules[0]) ||
		list.Rules[1].ID != "hidden-files" || !list.Rules[1].Enabled || list.Rules[1].Ban || !slices.Equal(list.Rules[1].Hosts, []string{"a.example.com"}) || list.Rules[2].Enabled {
		t.Fatalf("migrated: %+v, %v", list, err)
	}

	// The administrator deletes it; neither opening again nor the other
	// changes bring it back.
	rewind(db, rules("hidden-files"))
	db.Close()
	db = reopen()
	if got := ids(db); !slices.Equal(got, []string{"honeypot", "hidden-files"}) {
		t.Fatalf("migrated: %v", got)
	}
	if _, err := db.SetBlockRules(ctx, BlockRules{Rules: []BlockRule{{ID: "hidden-files", Name: "Hidden", Enabled: true, Conditions: []BlockCondition{{BlockStartsWith, "/."}}}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = reopen()
	defer db.Close()
	if got := ids(db); !slices.Equal(got, []string{"hidden-files"}) {
		t.Fatalf("after deleting it: %v", got)
	}
}
