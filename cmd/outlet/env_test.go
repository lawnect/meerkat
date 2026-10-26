package main

import (
	"slices"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestEnvironmentDefaults(t *testing.T) {
	defaults, warnings := environmentDefaults(env(nil))
	if len(warnings) != 0 || defaults.SSO != nil || defaults.Telegram != nil {
		t.Fatalf("empty environment: %+v, %v", defaults, warnings)
	}
	defaults, warnings = environmentDefaults(env(map[string]string{
		"MEERKAT_OIDC_ISSUER":        "https://idp.example.com/realms/inside/",
		"MEERKAT_OIDC_CLIENT_ID":     "meerkat",
		"MEERKAT_OIDC_CLIENT_SECRET": "secret-value-1234",
		"MEERKAT_OIDC_LABEL":         " Keycloak ",
		"MEERKAT_ADMIN_EMAILS":       "Admin@Example.com, ops@example.org",
		"MEERKAT_ADMIN_GROUPS":       "/platform-admins",
		"MEERKAT_PASSWORD_LOGIN":     "false",
		"MEERKAT_TELEGRAM_BOT_TOKEN": "123456789:AAEhBOweik6ad9r_QXMENQjcrGbqCr4K-ab",
		"MEERKAT_TELEGRAM_CHAT_ID":   "-1001234567890",
	}))
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	sso := defaults.SSO
	if sso == nil || !sso.Enabled || sso.Issuer != "https://idp.example.com/realms/inside" || sso.Label != "Keycloak" || sso.PasswordLogin ||
		!slices.Equal(sso.AllowedUsers, []string{"admin@example.com", "ops@example.org"}) || !slices.Equal(sso.AllowedGroups, []string{"/platform-admins"}) ||
		!slices.Equal(sso.Scopes, []string{"openid", "email", "profile"}) || sso.GroupsClaim != "groups" {
		t.Fatalf("sso = %+v", sso)
	}
	if tg := defaults.Telegram; tg == nil || !tg.Enabled || tg.ChatID != "-1001234567890" {
		t.Fatalf("telegram = %+v", tg)
	}
	for name, values := range map[string]map[string]string{
		"admins without issuer":   {"MEERKAT_ADMIN_EMAILS": "a@example.com"},
		"issuer without admins":   {"MEERKAT_OIDC_ISSUER": "https://idp.example.com", "MEERKAT_OIDC_CLIENT_ID": "m"},
		"http issuer":             {"MEERKAT_OIDC_ISSUER": "http://idp.example.com", "MEERKAT_OIDC_CLIENT_ID": "m", "MEERKAT_ADMIN_EMAILS": "a@example.com"},
		"bad email":               {"MEERKAT_OIDC_ISSUER": "https://idp.example.com", "MEERKAT_OIDC_CLIENT_ID": "m", "MEERKAT_ADMIN_EMAILS": "not-an-email"},
		"bad password flag":       {"MEERKAT_OIDC_ISSUER": "https://idp.example.com", "MEERKAT_OIDC_CLIENT_ID": "m", "MEERKAT_ADMIN_EMAILS": "a@example.com", "MEERKAT_PASSWORD_LOGIN": "nope"},
		"telegram token only":     {"MEERKAT_TELEGRAM_BOT_TOKEN": "123456789:AAEhBOweik6ad9r_QXMENQjcrGbqCr4K-ab"},
		"telegram malformed chat": {"MEERKAT_TELEGRAM_BOT_TOKEN": "123456789:AAEhBOweik6ad9r_QXMENQjcrGbqCr4K-ab", "MEERKAT_TELEGRAM_CHAT_ID": "chat 1"},
	} {
		// The setting is left out with a warning naming the variable; the
		// Outlet still starts.
		defaults, warnings := environmentDefaults(env(values))
		if defaults.SSO != nil || defaults.Telegram != nil || len(warnings) == 0 || !strings.Contains(warnings[0], "MEERKAT_") {
			t.Errorf("%s: %+v %v", name, defaults, warnings)
		}
	}
}
