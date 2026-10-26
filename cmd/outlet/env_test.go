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

func TestCaddyAddresses(t *testing.T) {
	if upstream, metrics, warnings := caddyAddresses(env(nil)); upstream != "outlet:8080" || metrics != "caddy:9090" || len(warnings) != 0 {
		t.Fatalf("defaults: %q, %q, %v", upstream, metrics, warnings)
	}
	upstream, metrics, warnings := caddyAddresses(env(map[string]string{"MEERKAT_CADDY_UPSTREAM": " Outlet-K3X9:8080 ", "MEERKAT_CADDY_METRICS": "10.0.0.5:9090"}))
	if upstream != "outlet-k3x9:8080" || metrics != "10.0.0.5:9090" || len(warnings) != 0 {
		t.Fatalf("set: %q, %q, %v", upstream, metrics, warnings)
	}
	for _, raw := range []string{"outlet", "outlet:8080 {", "outlet:8080\nimport x", "outlet:0", "127.0.0.1:8080", "a b:8080"} {
		upstream, metrics, warnings := caddyAddresses(env(map[string]string{"MEERKAT_CADDY_UPSTREAM": raw, "MEERKAT_CADDY_METRICS": raw}))
		if upstream != "outlet:8080" || metrics != "caddy:9090" || len(warnings) != 2 {
			t.Fatalf("%q accepted: %q, %q, %v", raw, upstream, metrics, warnings)
		}
	}
}

func TestEnvironmentDomain(t *testing.T) {
	for _, c := range []struct{ domain, admin, wantDomain, wantLabel string }{
		{"", "", "", ""},
		{" Example.COM. ", "", "example.com", ""},
		{"", "Console.Example.com", "example.com", "console"},
		{"example.com", "cjfqbg92t6wkdp9gqctoimeue9fij7ia.example.com", "example.com", "cjfqbg92t6wkdp9gqctoimeue9fij7ia"},
		{"tunnel.example.com", "admin.tunnel.example.com", "tunnel.example.com", "admin"},
	} {
		defaults, warnings := environmentDefaults(env(map[string]string{"MEERKAT_DOMAIN": c.domain, "MEERKAT_ADMIN_HOSTNAME": c.admin}))
		if len(warnings) != 0 || defaults.Domain != c.wantDomain || defaults.AdminLabel != c.wantLabel {
			t.Fatalf("%+v: %q %q %v", c, defaults.Domain, defaults.AdminLabel, warnings)
		}
	}
	for _, c := range []struct{ domain, admin, wantDomain string }{
		{"https://example.com", "", ""},
		{"*.example.com", "", ""},
		{"example.com", "console.other.com", "example.com"},
		{"example.com", "a.b.example.com", "example.com"},
		{"", "example", ""},
		{"", "con sole.example.com", ""},
	} {
		defaults, warnings := environmentDefaults(env(map[string]string{"MEERKAT_DOMAIN": c.domain, "MEERKAT_ADMIN_HOSTNAME": c.admin}))
		if len(warnings) != 1 || defaults.Domain != c.wantDomain || defaults.AdminLabel != "" {
			t.Fatalf("%+v accepted: %q %q %v", c, defaults.Domain, defaults.AdminLabel, warnings)
		}
	}
}
