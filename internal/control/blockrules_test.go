package control

import (
	"context"
	"errors"
	"fmt"
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
		"/a/../.ENV":        "/.env",
		`\..\.env`:          "/.env",
		"/x.php;jsessionid": "/x.php",
		"/x.php\x00.jpg":    "/x.php",
		"/setup/":           "/setup",
		"/x/..;/admin":      "/admin",
		// As released: dots and spaces inside the path stay.
		"/Admin./x": "/admin./x",
	} {
		if got := NormalizeBlockPath(in); got != want {
			t.Errorf("NormalizeBlockPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// The trimmed form drops trailing dots and spaces from every segment and
// stays the same when taken again.
func TestTrimmedBlockPath(t *testing.T) {
	for in, want := range map[string]string{
		"/Admin./x":   "/admin/x",
		"/admin. . /": "/admin",
		"/a/.../b":    "/a/b",
		"/a/.. /b":    "/a/b",
		"/x/../. /y":  "/y",
		"/.env.":      "/.env",
		"/":           "/",
		"/a..b/c":     "/a..b/c",
	} {
		got := TrimmedBlockPath(in)
		if got != want || TrimmedBlockPath(got) != got {
			t.Errorf("TrimmedBlockPath(%q) = %q (again %q), want %q", in, got, TrimmedBlockPath(got), want)
		}
	}
}

// Access conditions take the form request paths are compared in, where the
// form keeps their meaning: a segment surely ends before a slash, and at the
// end of an equals or ends_with value. Block rules keep the form they were
// released with.
func TestConditionsMatchHowRequestsCompare(t *testing.T) {
	for _, c := range []struct{ in, access, block BlockCondition }{
		{BlockCondition{BlockEquals, "/Secret."}, BlockCondition{BlockEquals, "/secret"}, BlockCondition{BlockEquals, "/secret."}},
		{BlockCondition{BlockEquals, "/a/.../b"}, BlockCondition{BlockEquals, "/a/b"}, BlockCondition{BlockEquals, "/a/.../b"}},
		{BlockCondition{BlockStartsWith, "/Private./"}, BlockCondition{BlockStartsWith, "/private/"}, BlockCondition{BlockStartsWith, "/private./"}},
		{BlockCondition{BlockStartsWith, `/a\b`}, BlockCondition{BlockStartsWith, "/a/b"}, BlockCondition{BlockStartsWith, `/a\b`}},
		{BlockCondition{BlockStartsWith, "/adm."}, BlockCondition{BlockStartsWith, "/adm."}, BlockCondition{BlockStartsWith, "/adm."}},
		{BlockCondition{BlockStartsWith, "/."}, BlockCondition{BlockStartsWith, "/."}, BlockCondition{BlockStartsWith, "/."}},
		{BlockCondition{BlockEndsWith, ".PHP."}, BlockCondition{BlockEndsWith, ".php"}, BlockCondition{BlockEndsWith, ".php."}},
		{BlockCondition{BlockEndsWith, "."}, BlockCondition{BlockEndsWith, "."}, BlockCondition{BlockEndsWith, "."}},
		{BlockCondition{BlockEndsWith, "/.."}, BlockCondition{BlockEndsWith, "/.."}, BlockCondition{BlockEndsWith, "/.."}},
		{BlockCondition{BlockContains, "../"}, BlockCondition{BlockContains, "../"}, BlockCondition{BlockContains, "../"}},
		{BlockCondition{BlockContains, "/.env"}, BlockCondition{BlockContains, "/.env"}, BlockCondition{BlockContains, "/.env"}},
		{BlockCondition{BlockContains, "a. /b"}, BlockCondition{BlockContains, "a/b"}, BlockCondition{BlockContains, "a. /b"}},
		{BlockCondition{BlockMatches, `^/Admin\.$`}, BlockCondition{BlockMatches, `^/Admin\.$`}, BlockCondition{BlockMatches, `^/Admin\.$`}},
	} {
		for _, form := range []struct {
			trim bool
			want BlockCondition
		}{{true, c.access}, {false, c.block}} {
			got, ok := normalizeCondition(c.in, form.trim)
			if !ok || got != form.want {
				t.Errorf("%+v (trim %v) = %+v %v, want %+v", c.in, form.trim, got, ok, form.want)
			}
			if again, _ := normalizeCondition(got, form.trim); again != got {
				t.Errorf("%+v (trim %v) again = %+v", got, form.trim, again)
			}
		}
	}
	// Saved rules keep loading as they were.
	defaults, ok := DefaultBlockRules().Normalize()
	if again, _ := defaults.Normalize(); !ok || fmt.Sprint(again) != fmt.Sprint(DefaultBlockRules()) {
		t.Fatalf("defaults changed: %+v", again)
	}
}
