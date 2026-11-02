package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func onPaths(operator string, values ...string) AccessCondition {
	return AccessCondition{Field: ConditionPath, Operator: operator, Values: values}
}

func onMethods(methods ...string) AccessCondition {
	return AccessCondition{Field: ConditionMethod, Operator: AccessIn, Values: methods}
}

func fromIPs(values ...string) AccessCondition {
	return AccessCondition{Field: ConditionClientIP, Operator: AccessIn, Values: values}
}

func withToken(ids ...string) AccessCondition {
	if len(ids) == 0 {
		return AccessCondition{Field: ConditionToken, Operator: AccessAnyToken, Values: []string{}}
	}
	return AccessCondition{Field: ConditionToken, Operator: AccessIn, Values: ids}
}

func not(c AccessCondition) AccessCondition {
	c.Not = true
	return c
}

// Path values are kept in the form they are compared in: broadly, in lower
// case, where a match protects; exactly, as written, where it opens (a
// bypass, or what a protective policy's Not leaves out).
func TestAccessConditionsNormalize(t *testing.T) {
	for _, c := range []struct {
		action string
		in     AccessCondition
		want   []string
	}{
		{AccessAllow, onPaths(BlockStartsWith, "/Admin/", "/admin/"), []string{"/admin/"}},
		{AccessAllow, not(onPaths(BlockStartsWith, "/Public/")), []string{"/Public/"}},
		{AccessBypass, onPaths(BlockStartsWith, "/API/"), []string{"/API/"}},
		{AccessBypass, not(onPaths(BlockStartsWith, "/Admin/")), []string{"/admin/"}},
		{AccessBlock, onMethods("post", "GET"), []string{"GET", "POST"}},
		{AccessBlock, fromIPs(" 사무실 ", "203.0.113.9/24", "VPN", "2001:db8::1"), []string{"사무실", "VPN", "203.0.113.0/24", "2001:db8::1"}},
		{AccessBypass, withToken("tok1", "tok1"), []string{"tok1"}},
	} {
		got, ok := normalizeAccessCondition(c.action, c.in)
		if !ok || !slices.Equal(got.Values, c.want) || got.Not != c.in.Not {
			t.Errorf("%s %+v = %+v %v, want %q", c.action, c.in, got, ok, c.want)
		}
	}
	for name, c := range map[string]AccessCondition{
		"unknown field":         {Field: "country", Operator: AccessIn, Values: []string{"KR"}},
		"path without values":   onPaths(BlockStartsWith),
		"path operator unknown": onPaths("near", "/x"),
		// What a protective policy leaves out opens, so it must be plain.
		"exclusion not plain":      not(onPaths(BlockStartsWith, "/a/../b/")),
		"method operator":          {Field: ConditionMethod, Operator: BlockEquals, Values: []string{"GET"}},
		"methods without values":   onMethods(),
		"unknown method":           onMethods("TRACE"),
		"client IP without values": fromIPs(),
		"bad range":                fromIPs("203.0.113.0/33"),
		"address-like tag":         fromIPs("999.1.1.1"),
		"token operator":           {Field: ConditionToken, Operator: BlockEquals, Values: []string{"tok1"}},
	} {
		action := AccessAllow
		if _, ok := normalizeAccessCondition(action, c); ok {
			t.Errorf("%s accepted", name)
		}
	}
	rows := make([]AccessCondition, maxAccessConditionRows+1)
	for i := range rows {
		rows[i] = onMethods("GET")
	}
	if _, ok := normalizeConditions(AccessAllow, rows); ok {
		t.Error("too many conditions accepted")
	}
	// The same condition twice is kept once.
	if got, ok := normalizeConditions(AccessAllow, []AccessCondition{onMethods("GET"), onMethods("get")}); !ok || len(got) != 1 {
		t.Errorf("duplicates = %+v %v", got, ok)
	}
}

// Policies saved before conditions (0.7) read as conditions; one whose
// paths used several operators becomes one policy per operator, in a row.
func TestAccessPoliciesStoredBeforeConditions(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if _, err := db.CreateIPAddress(ctx, IPAddress{Range: "203.0.113.0/24", Tags: []string{"사무실"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw := `{"policies":[
		{"id":"api","name":"API","enabled":true,"action":"bypass","hosts":["app.example.com"],"all_paths":false,
		 "conditions":[{"operator":"starts_with","value":"/v1/"},{"operator":"equals","value":"/health"},{"operator":"starts_with","value":"/v2/"}],
		 "methods":["GET","CLIENT-IP"],"require_token":false,"tokens":[],"ip_tags":["사무실"],"ip_ranges":["192.0.2.1"],"allowed_users":[],"allowed_groups":[]},
		{"id":"staff","name":"Staff","enabled":true,"action":"allow","hosts":["app.example.com"],"all_paths":true,"conditions":[],
		 "methods":[],"require_token":true,"tokens":[],"allow_everyone":true,"allowed_users":[],"allowed_groups":[]}]}`
	if err := putSetting(ctx, db.db, settingAccessPolicies, raw, time.Now()); err != nil {
		t.Fatal(err)
	}
	read, err := db.AccessPolicies(ctx)
	if err != nil || len(read.Policies) != 3 {
		t.Fatalf("read = %+v %v", read, err)
	}
	others := []AccessCondition{onMethods("GET"), fromIPs("사무실", "192.0.2.1")}
	want := []AccessPolicy{
		{ID: "api", Name: "API", Conditions: append([]AccessCondition{onPaths(BlockStartsWith, "/v1/", "/v2/")}, others...)},
		{ID: "api-2", Name: "API (2)", Conditions: append([]AccessCondition{onPaths(BlockEquals, "/health")}, others...)},
		{ID: "staff", Name: "Staff", Conditions: []AccessCondition{withToken()}},
	}
	for i, w := range want {
		got := read.Policies[i]
		if got.ID != w.ID || got.Name != w.Name || len(got.Conditions) != len(w.Conditions) ||
			slices.ContainsFunc(w.Conditions, func(c AccessCondition) bool {
				return !slices.ContainsFunc(got.Conditions, func(g AccessCondition) bool { return sameCondition(g, c) })
			}) {
			t.Errorf("policy %d = %+v, want %+v", i, got, w)
		}
	}
	// Saved again, they are in this version's form.
	if _, err := db.SetAccessPolicies(ctx, read, time.Now()); err != nil {
		t.Fatal(err)
	}
	if again, err := db.AccessPolicies(ctx); err != nil || len(again.Policies) != 3 || again.Policies[1].ID != "api-2" {
		t.Fatalf("saved again = %+v %v", again, err)
	}
	// A split's ID fits in 32 characters and is one no saved policy has.
	long := strings.Repeat("a", 32)
	crowded := `{"policies":[
		{"id":"` + long + `","name":"Long","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":false,
		 "conditions":[{"operator":"starts_with","value":"/a/"},{"operator":"equals","value":"/b"}]},
		{"id":"x","name":"X","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":false,
		 "conditions":[{"operator":"starts_with","value":"/a/"},{"operator":"equals","value":"/b"}]},
		{"id":"x-2","name":"X2","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":true}]}`
	if err := putSetting(ctx, db.db, settingAccessPolicies, crowded, time.Now()); err != nil {
		t.Fatal(err)
	}
	split, err := db.AccessPolicies(ctx)
	ids := make([]string, len(split.Policies))
	for i, p := range split.Policies {
		ids[i] = p.ID
	}
	if err != nil || !slices.Equal(ids, []string{long, strings.Repeat("a", 30) + "-2", "x", "x-3", "x-2"}) {
		t.Fatalf("split IDs = %q, %v", ids, err)
	}
	// Policies split on reading don't count against the limit.
	var full strings.Builder
	full.WriteString(`{"policies":[`)
	for i := range MaxAccessPolicies {
		if i > 0 {
			full.WriteString(",")
		}
		fmt.Fprintf(&full, `{"id":"p%d","name":"P","enabled":true,"action":"block","hosts":["app.example.com"],"all_paths":false,"conditions":[{"operator":"starts_with","value":"/a/"},{"operator":"equals","value":"/b"}]}`, i)
	}
	full.WriteString(`]}`)
	if err := putSetting(ctx, db.db, settingAccessPolicies, full.String(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if read, err := db.AccessPolicies(ctx); err != nil || len(read.Policies) != 2*MaxAccessPolicies {
		t.Fatalf("%d split policies: %v", len(read.Policies), err)
	}
	// Damaged before stays damaged: neither all paths nor any.
	broken := `{"policies":[{"id":"x","name":"X","enabled":true,"action":"allow","hosts":["app.example.com"],"all_paths":false,"conditions":[],"allow_everyone":true}]}`
	if err := putSetting(ctx, db.db, settingAccessPolicies, broken, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AccessPolicies(ctx); !errors.Is(err, ErrStoredAccessPolicies) {
		t.Fatalf("damaged before: %v", err)
	}
}

// Policies with conditions are stored so that versions before them read
// the policies as damaged, and keep closed what they protect instead of
// reading paths that aren't: a method they refuse, no "all paths", and a
// required token on an allow policy with conditions, so their closedAccess
// lets nobody past it rather than letting whom it names in everywhere. The
// tokens and tags policies name are there for them too, so they keep those
// from being deleted.
func TestAccessPoliciesStoredForOlderVersionsToRefuse(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if slices.Contains(AccessMethods, conditionsMarker) {
		t.Fatal("the marker is a method this version accepts")
	}
	if _, err := db.CreateIPAddress(ctx, IPAddress{Range: "198.51.100.0/24", Tags: []string{"관리자"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	token, _, err := db.CreateAccessToken(ctx, AccessTokenScope{Name: "CI", Hosts: []string{"app.example.com"}}, time.Time{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	limited := AccessPolicy{Name: "admins", Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []AccessCondition{not(fromIPs("203.0.113.0/24", "관리자"))}, AllowEveryone: true}
	open := AccessPolicy{Name: "everyone", Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []AccessCondition{not(onPaths(BlockStartsWith, "/admin/"))}, AllowEveryone: true}
	ci := AccessPolicy{Name: "ci", Enabled: true, Action: AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []AccessCondition{withToken(token.ID)}}
	all := AccessPolicy{Name: "all", Enabled: true, Hosts: []string{"app.example.com"}, Conditions: []AccessCondition{}, AllowAdmins: true}
	if _, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{limited, open, ci, all}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	raw, err := getSetting(ctx, db.db, settingAccessPolicies)
	if err != nil {
		t.Fatal(err)
	}
	// How 0.7 read a policy.
	var before struct {
		Policies []struct {
			AllPaths     bool             `json:"all_paths"`
			Methods      []string         `json:"methods"`
			RequireToken bool             `json:"require_token"`
			Tokens       []string         `json:"tokens"`
			IPTags       []string         `json:"ip_tags"`
			IPRanges     []string         `json:"ip_ranges"`
			Conditions   []BlockCondition `json:"conditions"`
		} `json:"policies"`
	}
	if err := json.Unmarshal([]byte(raw), &before); err != nil {
		t.Fatal(err)
	}
	for i, p := range before.Policies {
		if p.AllPaths || !slices.Equal(p.Methods, []string{conditionsMarker}) || p.RequireToken != (i < 2) {
			t.Errorf("policy %d as 0.7 reads it = %+v", i, p)
		}
	}
	if p := before.Policies[0]; !slices.Equal(p.IPTags, []string{"관리자"}) || !slices.Equal(p.IPRanges, []string{"203.0.113.0/24"}) {
		t.Errorf("tags and ranges as 0.7 reads them = %+v", p)
	}
	if p := before.Policies[2]; !slices.Equal(p.Tokens, []string{token.ID}) {
		t.Errorf("tokens as 0.7 reads them = %+v", p)
	}
	read, err := db.AccessPolicies(ctx)
	if err != nil || len(read.Policies) != 4 || !read.Policies[0].Conditions[0].Not || read.Policies[1].Conditions[0].Values[0] != "/admin/" {
		t.Fatalf("read back = %+v, %v", read, err)
	}
}
