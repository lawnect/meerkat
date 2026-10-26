package control

import (
	"context"
	"errors"
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
		"encoded path": func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockEquals, Value: "/%61dmin"}} },
		"encoded part": func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockContains, Value: "%2F"}} },
		"parameter":    func(p *AccessPolicy) { p.Conditions = []BlockCondition{{Operator: BlockStartsWith, Value: "/a;b/"}} },
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
