package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
)

func TestEnvironmentSettingsYieldToSavedOnes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	if complete, _ := db.SetupComplete(ctx, true); complete {
		t.Fatal("setup complete with nothing configured")
	}
	envSSO, _ := SSO{Enabled: true, Issuer: "https://idp.example.com", ClientID: "meerkat", ClientSecret: "env-secret-1234", Scopes: []string{"openid"}, AllowedUsers: []string{"a@example.com"}, PasswordLogin: true}.Normalize()
	envTelegram, err := NewTelegram("123456789:AAEhBOweik6ad9r_QXMENQjcrGbqCr4K-ab", "-1001234567890", true)
	if err != nil {
		t.Fatal(err)
	}
	db.SetDefaults(Defaults{SSO: &envSSO, Telegram: &envTelegram})

	// The environment applies while nothing is saved, and lets the allowed
	// people become the first administrators.
	if sso, _ := db.SSO(ctx); sso.Issuer != "https://idp.example.com" {
		t.Fatalf("sso = %+v", sso)
	}
	if source, _ := db.SSOSource(ctx); source != SourceEnvironment {
		t.Fatalf("sso source = %s", source)
	}
	if complete, _ := db.SetupComplete(ctx, true); !complete {
		t.Fatal("setup not complete on public addresses with SSO from the environment")
	}
	if complete, _ := db.SetupComplete(ctx, false); complete {
		t.Fatal("the local admin port lost its bootstrap path")
	}
	tg, _ := db.Telegram(ctx)
	if tg.Source != SourceEnvironment || tg.ChatID != "-1001234567890" || tg.VerifiedAt != "" {
		t.Fatalf("telegram = %+v", tg)
	}

	// A test message verifies the environment's bot, and only while it is the same bot.
	if ok, err := db.MarkTelegramVerified(ctx, envTelegram.BotToken, envTelegram.ChatID, now); !ok || err != nil {
		t.Fatalf("verify: %v %v", ok, err)
	}
	if tg, _ := db.Telegram(ctx); tg.VerifiedAt == "" {
		t.Fatal("verification of the environment's bot lost")
	}
	other, _ := NewTelegram(envTelegram.BotToken, "-1009999999999", true)
	db.SetDefaults(Defaults{SSO: &envSSO, Telegram: &other})
	if tg, _ := db.Telegram(ctx); tg.VerifiedAt != "" || tg.ChatID != "-1009999999999" {
		t.Fatalf("verification kept for another chat: %+v", tg)
	}

	// Saving from the console overrides; removing the saved value falls back.
	saved, err := db.SetSSO(ctx, SSO{Enabled: true, Issuer: "https://login.example.org", ClientID: "console", Scopes: []string{"openid"}, AllowedGroups: []string{"admins"}, PasswordLogin: true}, now)
	if err != nil || saved.ClientSecret != "env-secret-1234" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if source, _ := db.SSOSource(ctx); source != SourceDatabase {
		t.Fatalf("source after saving = %s", source)
	}
	if err := db.SetTelegram(ctx, "", "-1001111111111", false, now); err != nil {
		t.Fatal(err)
	}
	if tg, _ := db.Telegram(ctx); tg.Source != SourceDatabase || tg.Enabled || tg.BotToken != envTelegram.BotToken {
		t.Fatalf("saved telegram = %+v", tg)
	}
	if err := db.ResetSSO(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteTelegram(ctx, now); err != nil {
		t.Fatal(err)
	}
	if sso, _ := db.SSO(ctx); sso.Issuer != "https://idp.example.com" {
		t.Fatalf("sso after reset = %+v", sso)
	}
	if tg, _ := db.Telegram(ctx); tg.Source != SourceEnvironment || !tg.Enabled {
		t.Fatalf("telegram after reset = %+v", tg)
	}
}

// The environment owns the primary domain and the management host: the
// console can add domains and switch this one's DNS mode, not replace it.
func TestEnvironmentOwnsThePrimaryDomain(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetDefaults(Defaults{Domain: "example.com", AdminLabel: "cjfqbg92t6wkdp9gqctoimeue9fij7ia"})
	outlet, err := db.ApplyEnvironmentDomain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if outlet.Domain != "example.com" || outlet.AdminHostname != "cjfqbg92t6wkdp9gqctoimeue9fij7ia.example.com" || outlet.Modes["example.com"] != DomainHosts || outlet.DomainSource != SourceEnvironment {
		t.Fatalf("outlet = %+v", outlet)
	}
	if _, err := db.SetDomain(ctx, "other.com", ""); !errors.Is(err, ErrDomainFromEnvironment) {
		t.Fatalf("replacing the domain: %v", err)
	}
	if outlet, err = db.SetDomainMode(ctx, "example.com", DomainWildcard); err != nil || outlet.Modes["example.com"] != DomainWildcard {
		t.Fatalf("mode: %+v %v", outlet, err)
	}
	if outlet, err = db.AddDomain(ctx, "other.com", ""); err != nil || len(outlet.Domains) != 2 {
		t.Fatalf("adding a domain: %+v %v", outlet, err)
	}
	// Starting again changes nothing, and keeps the chosen mode.
	if outlet, err = db.ApplyEnvironmentDomain(ctx); err != nil || outlet.Modes["example.com"] != DomainWildcard || outlet.AdminHostname != "cjfqbg92t6wkdp9gqctoimeue9fij7ia.example.com" {
		t.Fatalf("again: %+v %v", outlet, err)
	}
	// A new management host from the environment replaces the old one.
	db.SetDefaults(Defaults{Domain: "example.com", AdminLabel: "console"})
	if outlet, err = db.ApplyEnvironmentDomain(ctx); err != nil || outlet.AdminHostname != "console.example.com" {
		t.Fatalf("new host: %+v %v", outlet, err)
	}
	// Without the environment, the console owns the domain again.
	db.SetDefaults(Defaults{})
	if outlet, err = db.Outlet(ctx); err != nil || outlet.DomainSource != SourceDatabase {
		t.Fatalf("source: %+v %v", outlet, err)
	}
}

// A management host from the environment can't take over a public address,
// and saved rate limits move to it even when the new name has its own.
func TestEnvironmentAdminHostnameChanges(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetDefaults(Defaults{Domain: "example.com", AdminLabel: "console"})
	if _, err := db.ApplyEnvironmentDomain(ctx); err != nil {
		t.Fatal(err)
	}
	connector, err := db.CreateConnector(ctx, "edge")
	if err != nil {
		t.Fatal(err)
	}
	service, err := db.CreateService(ctx, connector.ID, "web", "http", contract.Target{Host: "app", Port: 3000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateHTTPRoute(ctx, service.ID, "app.example.com"); err != nil {
		t.Fatal(err)
	}
	db.SetDefaults(Defaults{Domain: "example.com", AdminLabel: "app"})
	if _, err := db.ApplyEnvironmentDomain(ctx); !errors.Is(err, ErrAdminHostnameTaken) {
		t.Fatalf("taking a public address: %v", err)
	}
	if outlet, _ := db.Outlet(ctx); outlet.AdminHostname != "console.example.com" || outlet.DomainSource != SourceEnvironment {
		t.Fatalf("outlet = %+v", outlet)
	}

	now := time.Now()
	settings, err := db.RateLimit(ctx, "console.example.com")
	if err != nil {
		t.Fatal(err)
	}
	settings.Endpoints = []EndpointLimit{
		{Host: "console.example.com", PathPrefix: "/api/", RPS: 5, Burst: 10, Per: "ip"},
		{Host: "console.example.com", PathPrefix: "/login", RPS: 1, Burst: 2, Per: "ip"},
		{Host: "admin.example.com", PathPrefix: "/api/", RPS: 9, Burst: 9, Per: "ip"},
	}
	if _, err := db.SetRateLimit(ctx, settings, now); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameRateLimitHost(ctx, "console.example.com", "admin.example.com", now); err != nil {
		t.Fatal(err)
	}
	moved, err := db.RateLimit(ctx, "admin.example.com")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, endpoint := range moved.Endpoints {
		if endpoint.Host != "admin.example.com" {
			t.Fatalf("left behind: %+v", endpoint)
		}
		got[endpoint.PathPrefix] = endpoint.RPS
	}
	if len(got) != 2 || got["/api/"] != 9 || got["/login"] != 1 {
		t.Fatalf("endpoints = %+v", moved.Endpoints)
	}
}
