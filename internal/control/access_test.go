package control

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// Policies need a name, addresses, paths unless all, and someone to let in.
func TestAccessPoliciesNormalize(t *testing.T) {
	ok := AccessPolicy{Name: "Staff", Enabled: true, Hosts: []string{" App.Example.com ", "*.example.org"}, Conditions: []BlockCondition{{Operator: BlockStartsWith, Value: "/Admin/"}}, AllowedUsers: []string{"Alice@Example.com", "@example.org"}}
	normalized, valid := AccessPolicies{Policies: []AccessPolicy{ok}}.Normalize()
	if !valid || normalized.Policies[0].ID == "" || normalized.Policies[0].Hosts[0] != "app.example.com" || normalized.Policies[0].Conditions[0].Value != "/admin/" || normalized.Policies[0].AllowedUsers[0] != "alice@example.com" {
		t.Fatalf("normalized = %+v %v", normalized, valid)
	}
	for name, change := range map[string]func(*AccessPolicy){
		"no name":             func(p *AccessPolicy) { p.Name = " " },
		"no hosts":            func(p *AccessPolicy) { p.Hosts = nil },
		"bad host":            func(p *AccessPolicy) { p.Hosts = []string{"https://app.example.com"} },
		"no paths":            func(p *AccessPolicy) { p.Conditions = nil },
		"bad pattern":         func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockMatches, Value: "("}} },
		"nobody":              func(p *AccessPolicy) { p.AllowedUsers = nil },
		"bad user":            func(p *AccessPolicy) { p.AllowedUsers = []string{"not an email"} },
		"duplicate id":        func(p *AccessPolicy) { p.ID = "same" },
		"control in the name": func(p *AccessPolicy) { p.Name = "a\nb" },
		// Request paths are compared decoded and without ";parameters".
		"encoded path":            func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockEquals, Value: "/%61dmin"}} },
		"encoded part":            func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockContains, Value: "%2F"}} },
		"parameter":               func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockStartsWith, Value: "/a;b/"}} },
		"equals with a parameter": func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockEquals, Value: "/admin;foo"}} },
	} {
		policy := ok
		change(&policy)
		policies := []AccessPolicy{policy}
		if name == "duplicate id" {
			policies = append(policies, policy)
		}
		if _, valid := (AccessPolicies{Policies: policies}).Normalize(); valid {
			t.Fatalf("%s accepted", name)
		}
	}
	pattern := ok
	pattern.Conditions = []BlockCondition{{Operator: BlockMatches, Value: "^/files/100%$"}, {Operator: BlockEquals, Value: "/100%"}}
	if _, valid := (AccessPolicies{Policies: []AccessPolicy{pattern}}).Normalize(); !valid {
		t.Fatal("a percent sign that isn't an escape refused")
	}
	// Bypass and block policies name nobody; an allow policy may let in
	// everyone the provider signs in, and then names nobody either.
	for _, c := range []struct {
		policy AccessPolicy
		action string
	}{
		{AccessPolicy{Name: "Open", Hosts: []string{"app.example.com"}, AllPaths: true, Action: AccessBypass, AllowedUsers: []string{"a@example.com"}, AllowEveryone: true}, AccessBypass},
		{AccessPolicy{Name: "Shut", Hosts: []string{"app.example.com"}, AllPaths: true, Action: AccessBlock, AllowAdmins: true}, AccessBlock},
		{AccessPolicy{Name: "Everyone", Hosts: []string{"app.example.com"}, AllPaths: true, AllowEveryone: true, AllowedGroups: []string{"x"}, AllowAdmins: true}, AccessAllow},
	} {
		normalized, valid := (AccessPolicies{Policies: []AccessPolicy{c.policy}}).Normalize()
		if got := normalized.Policies[0]; !valid || got.Action != c.action || len(got.AllowedUsers)+len(got.AllowedGroups) != 0 || got.AllowAdmins || got.AllowEveryone != (c.action == AccessAllow) {
			t.Fatalf("%s = %+v %v", c.policy.Name, normalized, valid)
		}
	}
	if _, valid := (AccessPolicies{Policies: []AccessPolicy{{Name: "x", Hosts: []string{"app.example.com"}, AllPaths: true, Action: "maybe", AllowEveryone: true}}}).Normalize(); valid {
		t.Fatal("unknown action accepted")
	}
	all := AccessPolicy{Name: "All", Hosts: []string{"app.example.com"}, AllPaths: true, Conditions: []BlockCondition{{Operator: BlockStartsWith, Value: "/x/"}}, AllowAdmins: true}
	if normalized, valid := (AccessPolicies{Policies: []AccessPolicy{all}}).Normalize(); !valid || len(normalized.Policies[0].Conditions) != 0 {
		t.Fatalf("all paths = %+v %v", normalized, valid)
	}
}

// The provider needs an issuer and a client to be on, and a sane session.
func TestAccessProviderNormalizeAndStorage(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if p, _ := db.AccessProvider(ctx); p.Enabled || p.SessionHours != defaultAccessSession {
		t.Fatalf("default = %+v", p)
	}
	for name, provider := range map[string]AccessProvider{
		"no issuer":     {Enabled: true, ClientID: "c"},
		"http issuer":   {Enabled: true, Issuer: "http://idp.example.com", ClientID: "c"},
		"no client":     {Enabled: true, Issuer: "https://idp.example.com"},
		"long session":  {Issuer: "https://idp.example.com", SessionHours: 721},
		"short session": {Issuer: "https://idp.example.com", SessionHours: -1},
	} {
		if _, err := db.SetAccessProvider(ctx, provider, time.Now()); !errors.Is(err, ErrInvalidAccessProvider) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	saved, err := db.SetAccessProvider(ctx, AccessProvider{Enabled: true, Issuer: "https://idp.example.com/", ClientID: "c", ClientSecret: "secret-1234"}, time.Now())
	if err != nil || saved.Issuer != "https://idp.example.com" || saved.Scopes[0] != "openid" || saved.SessionHours != defaultAccessSession {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	// An empty secret keeps the stored one.
	if saved, err = db.SetAccessProvider(ctx, AccessProvider{Enabled: true, Issuer: "https://idp.example.com", ClientID: "c"}, time.Now()); err != nil || saved.ClientSecret != "secret-1234" {
		t.Fatalf("kept secret = %+v %v", saved, err)
	}
	first, err := db.AccessKey(ctx, time.Now())
	if err != nil || len(first) != 32 {
		t.Fatal(err)
	}
	if again, _ := db.AccessKey(ctx, time.Now()); string(again) != string(first) {
		t.Fatal("key changed without a rotation")
	}
	rotated, err := db.RotateAccessKey(ctx, time.Now())
	if err != nil || string(rotated) == string(first) {
		t.Fatalf("rotation: %v", err)
	}
	if again, _ := db.AccessKey(ctx, time.Now()); string(again) != string(rotated) {
		t.Fatal("rotation not kept")
	}
}

// Saved policies that no longer pass the checks come back as read, with an
// error saying so; unreadable ones come back as none.
func TestAccessPoliciesSavedInvalid(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	if _, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{{Name: "Staff", Enabled: true, Hosts: []string{"App.example.com"}, AllPaths: true, AllowAdmins: true}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if policies, err := db.AccessPolicies(ctx); err != nil || policies.Policies[0].Hosts[0] != "app.example.com" {
		t.Fatalf("valid = %+v %v", policies, err)
	}
	for raw, names := range map[string]int{
		`{"policies":[{"id":"old","name":"Old","enabled":true,"hosts":["app.example.com"],"conditions":[{"operator":"matches","value":"("}],"allow_admins":true}]}`: 1,
		`not json`: 0,
	} {
		if err := putSetting(ctx, db.db, settingAccessPolicies, raw, time.Now()); err != nil {
			t.Fatal(err)
		}
		if policies, err := db.AccessPolicies(ctx); !errors.Is(err, ErrStoredAccessPolicies) || len(policies.Policies) != names || policies.Policies == nil {
			t.Fatalf("%s = %+v %v", raw, policies, err)
		}
	}
}

// Access tokens keep only a hash of their value, expire when told, are
// capped, and can't be deleted while a policy names them.
func TestAccessTokenStorage(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	now := time.Now()
	scope := func(name string) AccessTokenScope {
		return AccessTokenScope{Name: name, Hosts: []string{"app.example.com"}}
	}
	token, value, err := db.CreateAccessToken(ctx, AccessTokenScope{Name: " Deploy ", Hosts: []string{" App.Example.com", "*.example.org", "app.example.com"}, Ranges: []string{"10.0.1.7/24", "2001:db8::1", " 10.0.1.0/24"}}, time.Time{}, now)
	if err != nil || token.Name != "Deploy" || token.ExpiresAt != "" || token.SecretHash != AccessTokenHash(value) || !strings.HasPrefix(value, token.ID+".") || len(value) < 50 ||
		fmt.Sprint(token.Hosts) != "[app.example.com *.example.org]" || fmt.Sprint(token.Ranges) != "[10.0.1.0/24 2001:db8::1/128]" {
		t.Fatalf("token = %+v %q %v", token, value, err)
	}
	// IPv4-mapped ranges are kept as the IPv4 ranges clients compare as.
	mapped, _, err := db.CreateAccessToken(ctx, AccessTokenScope{Name: "Mapped", Hosts: []string{"app.example.com"}, Ranges: []string{"::ffff:198.51.100.30", "::ffff:203.0.113.0/120"}}, time.Time{}, now)
	if err != nil || fmt.Sprint(mapped.Ranges) != "[198.51.100.30/32 203.0.113.0/24]" {
		t.Fatalf("mapped = %+v %v", mapped, err)
	}
	if _, _, err := db.CreateAccessToken(ctx, AccessTokenScope{Name: "Mapped", Hosts: []string{"app.example.com"}, Ranges: []string{"::ffff:0:0/95"}}, time.Time{}, now); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("mapped range wider than IPv4: %v", err)
	}
	if _, err := db.DeleteAccessToken(ctx, mapped.ID, now); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		scope   AccessTokenScope
		expires time.Time
	}{
		"no name":     {AccessTokenScope{Hosts: []string{"app.example.com"}}, time.Time{}},
		"past":        {scope("past"), now.Add(-time.Minute)},
		"long name":   {scope(strings.Repeat("x", 65)), time.Time{}},
		"control":     {scope("a\nb"), time.Time{}},
		"no hosts":    {AccessTokenScope{Name: "x"}, time.Time{}},
		"bad host":    {AccessTokenScope{Name: "x", Hosts: []string{"https://app.example.com"}}, time.Time{}},
		"bad range":   {AccessTokenScope{Name: "x", Hosts: []string{"app.example.com"}, Ranges: []string{"10.0.0.0/33"}}, time.Time{}},
		"zoned range": {AccessTokenScope{Name: "x", Hosts: []string{"app.example.com"}, Ranges: []string{"fe80::1%eth0"}}, time.Time{}},
	} {
		if _, _, err := db.CreateAccessToken(ctx, c.scope, c.expires, now); !errors.Is(err, ErrInvalidAccessToken) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for i := 1; i < MaxAccessTokens; i++ {
		if _, _, err := db.CreateAccessToken(ctx, scope("t"), now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := db.CreateAccessToken(ctx, scope("one too many"), time.Time{}, now); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("over the cap: %v", err)
	}
	// The scope changes; the value stays.
	updated, err := db.UpdateAccessToken(ctx, token.ID, AccessTokenScope{Name: "Deploy CI", Hosts: []string{"ci.example.com"}}, now)
	if err != nil || updated.Name != "Deploy CI" || fmt.Sprint(updated.Hosts) != "[ci.example.com]" || len(updated.Ranges) != 0 || updated.SecretHash != token.SecretHash {
		t.Fatalf("updated = %+v %v", updated, err)
	}
	if _, err := db.UpdateAccessToken(ctx, "nosuchtoken", scope("x"), now); !errors.Is(err, ErrAccessTokenNotFound) {
		t.Fatalf("update a missing token: %v", err)
	}
	if _, err := db.UpdateAccessToken(ctx, token.ID, AccessTokenScope{Name: "x"}, now); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("update without hosts: %v", err)
	}
	// Last use keeps the later time, for tokens that exist.
	if err := db.RecordAccessTokenUse(ctx, map[string]time.Time{token.ID: now, "nosuchtoken": now}, now); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordAccessTokenUse(ctx, map[string]time.Time{token.ID: now.Add(-time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	if used, err := db.AccessTokenUse(ctx); err != nil || len(used) != 1 || !used[token.ID].Equal(now.Truncate(time.Second)) {
		t.Fatalf("used = %v %v", used, err)
	}
	if _, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{{Name: "CI", Hosts: []string{"app.example.com"}, AllPaths: true, AllowedTokens: []string{token.ID}}}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DeleteAccessToken(ctx, token.ID, now); !errors.Is(err, ErrAccessTokenInUse) {
		t.Fatalf("in use: %v", err)
	}
	if _, err := db.SetAccessPolicies(ctx, AccessPolicies{Policies: []AccessPolicy{}}, now); err != nil {
		t.Fatal(err)
	}
	if deleted, err := db.DeleteAccessToken(ctx, token.ID, now); err != nil || deleted.ID != token.ID {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	if err := db.RecordAccessTokenUse(ctx, map[string]time.Time{}, now); err != nil {
		t.Fatal(err)
	}
	if used, _ := db.AccessTokenUse(ctx); len(used) != 0 {
		t.Fatalf("use of a deleted token kept: %v", used)
	}
	if _, err := db.DeleteAccessToken(ctx, token.ID, now); !errors.Is(err, ErrAccessTokenNotFound) {
		t.Fatalf("delete again: %v", err)
	}
	// Saved tokens that can't be read let nobody in.
	if err := putSetting(ctx, db.db, settingAccessTokens, "not json", now); err != nil {
		t.Fatal(err)
	}
	if tokens, err := db.AccessTokens(ctx); err != nil || len(tokens) != 0 || tokens == nil {
		t.Fatalf("unreadable tokens = %v %v", tokens, err)
	}
}

// A bypass condition is kept as written, case and all, and must be a path
// an app reads only one way.
func TestBypassConditions(t *testing.T) {
	for _, c := range []struct {
		in BlockCondition
		ok bool
	}{
		{BlockCondition{BlockStartsWith, "/API/v1/"}, true},
		{BlockCondition{BlockStartsWith, "/files/report."}, true},
		{BlockCondition{BlockEquals, "/health"}, true},
		{BlockCondition{BlockEndsWith, ".PNG"}, true},
		{BlockCondition{BlockContains, "/public/"}, true},
		{BlockCondition{BlockMatches, "^/v[0-9]+/models$"}, true},
		{BlockCondition{BlockStartsWith, "/v1/../admin/"}, false},
		{BlockCondition{BlockStartsWith, "/v1./"}, false},
		{BlockCondition{BlockStartsWith, "/v1 /"}, false},
		{BlockCondition{BlockEquals, "/health."}, false},
		{BlockCondition{BlockStartsWith, "//v1/"}, false},
		{BlockCondition{BlockStartsWith, "/v1/./x"}, false},
		{BlockCondition{BlockStartsWith, `/v1\x`}, false},
		{BlockCondition{BlockStartsWith, "v1/"}, false},
		{BlockCondition{BlockMatches, "("}, false},
	} {
		got, ok := bypassCondition(c.in)
		if ok != c.ok || ok && got.Value != c.in.Value {
			t.Errorf("%+v = %+v %v, want %v", c.in, got, ok, c.ok)
		}
	}
	policy := AccessPolicy{Name: "API", Action: AccessBypass, Hosts: []string{"app.example.com"}, Conditions: []BlockCondition{{BlockStartsWith, "/API/"}}}
	if normalized, ok := (AccessPolicies{Policies: []AccessPolicy{policy}}).Normalize(); !ok || normalized.Policies[0].Conditions[0].Value != "/API/" {
		t.Fatalf("bypass policy = %+v %v", normalized, ok)
	}
	policy.Conditions = []BlockCondition{{BlockStartsWith, "/v1;x/"}}
	if _, ok := (AccessPolicies{Policies: []AccessPolicy{policy}}).Normalize(); ok {
		t.Fatal("bypass condition with a parameter accepted")
	}
}

// A policy can be limited to some methods, kept in one order; GET takes
// HEAD along, and none named means every method.
func TestAccessPolicyMethods(t *testing.T) {
	policy := AccessPolicy{Name: "API", Action: AccessBypass, Hosts: []string{"app.example.com"}, AllPaths: true}
	for _, c := range []struct {
		in   []string
		want []string
	}{
		{nil, []string{}},
		{[]string{" post", "get", "GET"}, []string{"GET", "POST"}},
		{[]string{"OPTIONS", "delete", "patch", "put"}, []string{"PUT", "PATCH", "DELETE", "OPTIONS"}},
		{[]string{"HEAD"}, nil},
		{[]string{"TRACE"}, nil},
		{[]string{"GET", ""}, nil},
	} {
		policy.Methods = c.in
		normalized, ok := (AccessPolicies{Policies: []AccessPolicy{policy}}).Normalize()
		if ok != (c.want != nil) || ok && !slices.Equal(normalized.Policies[0].Methods, c.want) {
			t.Errorf("%q = %+v %v, want %q", c.in, normalized.Policies, ok, c.want)
		}
	}
	for _, c := range []struct {
		methods []string
		method  string
		covered bool
	}{
		{nil, "PROPFIND", true},
		{[]string{"GET"}, "GET", true},
		{[]string{"GET"}, "HEAD", true},
		{[]string{"GET"}, "POST", false},
		{[]string{"POST"}, "HEAD", false},
		{[]string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, "PROPFIND", false},
	} {
		if CoversMethod(c.methods, c.method) != c.covered {
			t.Errorf("CoversMethod(%q, %s) != %v", c.methods, c.method, c.covered)
		}
	}
}

// A saved policy without an ID was damaged: it isn't given a new one.
func TestAccessPoliciesSavedWithoutID(t *testing.T) {
	ctx := context.Background()
	db := openRouting(t)
	raw := `{"policies":[{"name":"Open","enabled":true,"action":"bypass","hosts":["app.example.com"],"all_paths":true,"allowed_users":[],"allowed_groups":[],"allowed_tokens":[]}]}`
	if err := putSetting(ctx, db.db, settingAccessPolicies, raw, time.Now()); err != nil {
		t.Fatal(err)
	}
	if policies, err := db.AccessPolicies(ctx); !errors.Is(err, ErrStoredAccessPolicies) || len(policies.Policies) != 1 || policies.Policies[0].ID != "" {
		t.Fatalf("policies = %+v %v", policies, err)
	}
}
