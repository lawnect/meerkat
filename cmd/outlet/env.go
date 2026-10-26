package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/lawnect/meerkat/internal/control"
	"github.com/lawnect/meerkat/internal/proxy"
	"github.com/lawnect/meerkat/internal/validation"
)

// environmentDefaults reads the settings a deployment can give in
// environment variables. Values saved from the admin console override them.
//
//	MEERKAT_DOMAIN, MEERKAT_ADMIN_HOSTNAME        primary domain and the management host directly under it
//	MEERKAT_OIDC_ISSUER, MEERKAT_OIDC_CLIENT_ID, MEERKAT_OIDC_CLIENT_SECRET
//	MEERKAT_OIDC_LABEL, MEERKAT_OIDC_SCOPES, MEERKAT_OIDC_GROUPS_CLAIM
//	MEERKAT_ADMIN_EMAILS, MEERKAT_ADMIN_GROUPS   who signs in as administrator
//	MEERKAT_PASSWORD_LOGIN                        false: public addresses use SSO only
//	MEERKAT_TELEGRAM_BOT_TOKEN, MEERKAT_TELEGRAM_CHAT_ID, MEERKAT_TELEGRAM_ENABLED
//
// Lists are separated by commas or spaces. A missing or invalid value never
// stops the Outlet, which keeps tunnels running: that setting is left out
// and a warning names the variable.
func environmentDefaults(getenv func(string) string) (control.Defaults, []string) {
	var defaults control.Defaults
	var warnings []string
	value := func(name string) string { return strings.TrimSpace(unquote(strings.TrimSpace(getenv(name)))) }
	list := func(name string) []string {
		return strings.FieldsFunc(getenv(name), func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	}
	boolean := func(name string, fallback bool) (bool, error) {
		raw := value(name)
		if raw == "" {
			return fallback, nil
		}
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s must be true or false", name)
		}
		return parsed, nil
	}

	// The management host is one label under the primary domain, so it names
	// the domain by itself; given both, they must agree.
	domain := strings.ToLower(strings.TrimSuffix(value("MEERKAT_DOMAIN"), "."))
	if domain != "" && !validation.Hostname(domain) {
		warnings = append(warnings, "MEERKAT_DOMAIN must be a domain name, such as example.com; it is ignored")
		domain = ""
	}
	if admin := strings.ToLower(strings.TrimSuffix(value("MEERKAT_ADMIN_HOSTNAME"), ".")); admin != "" {
		label, under, _ := strings.Cut(admin, ".")
		switch {
		case !validation.Hostname(admin) || !validation.Hostname(under):
			warnings = append(warnings, "MEERKAT_ADMIN_HOSTNAME must be a host name directly under the domain, such as console.example.com; it is ignored")
		case domain != "" && under != domain:
			warnings = append(warnings, "MEERKAT_ADMIN_HOSTNAME must be directly under MEERKAT_DOMAIN; it is ignored")
		default:
			domain, defaults.AdminLabel = under, label
		}
	}
	defaults.Domain = domain

	issuer := value("MEERKAT_OIDC_ISSUER")
	if issuer == "" {
		for _, name := range []string{"MEERKAT_OIDC_CLIENT_ID", "MEERKAT_OIDC_CLIENT_SECRET", "MEERKAT_ADMIN_EMAILS", "MEERKAT_ADMIN_GROUPS"} {
			if value(name) != "" {
				warnings = append(warnings, name+" is ignored until MEERKAT_OIDC_ISSUER is set")
			}
		}
	} else if passwordLogin, err := boolean("MEERKAT_PASSWORD_LOGIN", true); err != nil {
		warnings = append(warnings, "single sign-on from the environment is off: "+err.Error())
	} else {
		scopes := list("MEERKAT_OIDC_SCOPES")
		if len(scopes) == 0 {
			scopes = control.DefaultSSO().Scopes
		}
		sso, ok := control.SSO{
			Enabled: true, Label: value("MEERKAT_OIDC_LABEL"), Issuer: issuer,
			ClientID: value("MEERKAT_OIDC_CLIENT_ID"), ClientSecret: value("MEERKAT_OIDC_CLIENT_SECRET"),
			Scopes: scopes, GroupsClaim: value("MEERKAT_OIDC_GROUPS_CLAIM"),
			AllowedUsers: list("MEERKAT_ADMIN_EMAILS"), AllowedGroups: list("MEERKAT_ADMIN_GROUPS"),
			PasswordLogin: passwordLogin,
		}.Normalize()
		if ok {
			defaults.SSO = &sso
		} else {
			warnings = append(warnings, "single sign-on from the environment is off: it needs an https MEERKAT_OIDC_ISSUER, MEERKAT_OIDC_CLIENT_ID, and valid MEERKAT_ADMIN_EMAILS (emails or @domain) or MEERKAT_ADMIN_GROUPS")
		}
	}

	token, chat := value("MEERKAT_TELEGRAM_BOT_TOKEN"), value("MEERKAT_TELEGRAM_CHAT_ID")
	if token != "" || chat != "" {
		enabled, err := boolean("MEERKAT_TELEGRAM_ENABLED", true)
		if err != nil {
			warnings = append(warnings, "Telegram from the environment is off: "+err.Error())
			return defaults, warnings
		}
		telegram, err := control.NewTelegram(token, chat, enabled)
		if err != nil {
			warnings = append(warnings, "Telegram from the environment is off: MEERKAT_TELEGRAM_BOT_TOKEN and MEERKAT_TELEGRAM_CHAT_ID must both be set and valid")
			return defaults, warnings
		}
		defaults.Telegram = &telegram
	}
	return defaults, warnings
}

// caddyAddresses reads where Caddy and the Outlet reach each other, for
// deployments whose services aren't called outlet and caddy:
//
//	MEERKAT_CADDY_UPSTREAM  host:port Caddy uses for the Outlet (outlet:8080)
//	MEERKAT_CADDY_METRICS   host:port of Caddy's metrics (caddy:9090)
//
// Empty values give the default. An invalid value stops the Outlet instead:
// falling back to outlet or caddy could reach another stack's container on
// a shared network.
func caddyAddresses(getenv func(string) string) (upstream, metrics string, err error) {
	address := func(name, fallback string) string {
		raw := strings.ToLower(unquote(strings.TrimSpace(getenv(name))))
		if raw == "" {
			return fallback
		}
		if !validation.ServiceAddress(raw) && err == nil {
			err = fmt.Errorf("%s must be host:port, such as %s", name, fallback)
		}
		return raw
	}
	upstream, metrics = address("MEERKAT_CADDY_UPSTREAM", proxy.DefaultUpstream), address("MEERKAT_CADDY_METRICS", proxy.DefaultMetrics)
	return upstream, metrics, err
}

// unquote removes one pair of quotes around a value: an env file read by
// Compose drops them, but a value pasted into a deployment tool's form may
// keep them.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
