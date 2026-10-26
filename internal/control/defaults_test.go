package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
