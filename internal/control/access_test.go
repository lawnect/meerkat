package control

import (
	"context"
	"errors"
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
	token, value, err := db.CreateAccessToken(ctx, " Deploy ", time.Time{}, now)
	if err != nil || token.Name != "Deploy" || token.ExpiresAt != "" || token.SecretHash != AccessTokenHash(value) || !strings.HasPrefix(value, token.ID+".") || len(value) < 50 {
		t.Fatalf("token = %+v %q %v", token, value, err)
	}
	for name, expires := range map[string]time.Time{"": {}, "past": now.Add(-time.Minute), strings.Repeat("x", 65): {}, "a\nb": {}} {
		if _, _, err := db.CreateAccessToken(ctx, name, expires, now); !errors.Is(err, ErrInvalidAccessToken) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	for i := 1; i < MaxAccessTokens; i++ {
		if _, _, err := db.CreateAccessToken(ctx, "t", now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := db.CreateAccessToken(ctx, "one too many", time.Time{}, now); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("over the cap: %v", err)
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
