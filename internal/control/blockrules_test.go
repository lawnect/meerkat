package control

import (
	"context"
	"errors"
	"path/filepath"
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
	if err != nil || len(got.Rules) != 3 || !got.Rules[0].Enabled || got.Rules[1].Enabled || got.Rules[2].Enabled {
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

func TestNormalizeBlockPath(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "/",
		"/.ENV":             "/.env",
		"//.env":            "/.env",
		"/a/../.env":        "/.env",
		`\..\.env`:          "/.env",
		"/x.php;jsessionid": "/x.php",
		"/x.php\x00.jpg":    "/x.php",
		"/setup/":           "/setup",
	} {
		if got := NormalizeBlockPath(in); got != want {
			t.Errorf("NormalizeBlockPath(%q) = %q, want %q", in, got, want)
		}
	}
}
