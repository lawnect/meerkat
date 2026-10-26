package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/lawnect/meerkat/internal/control"
)

// environmentDefaults reads the settings a deployment can give in
// environment variables. Values saved from the admin console override them.
//
//	MEERKAT_OIDC_ISSUER, MEERKAT_OIDC_CLIENT_ID, MEERKAT_OIDC_CLIENT_SECRET
//	MEERKAT_OIDC_LABEL, MEERKAT_OIDC_SCOPES, MEERKAT_OIDC_GROUPS_CLAIM
//	MEERKAT_ADMIN_EMAILS, MEERKAT_ADMIN_GROUPS   who signs in as administrator
//	MEERKAT_PASSWORD_LOGIN                        false: public addresses use SSO only
//	MEERKAT_TELEGRAM_BOT_TOKEN, MEERKAT_TELEGRAM_CHAT_ID, MEERKAT_TELEGRAM_ENABLED
//
// Lists are separated by commas or spaces.
func environmentDefaults(getenv func(string) string) (control.Defaults, error) {
	var defaults control.Defaults
	value := func(name string) string { return strings.TrimSpace(getenv(name)) }
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

	issuer := value("MEERKAT_OIDC_ISSUER")
	if issuer == "" {
		for _, name := range []string{"MEERKAT_OIDC_CLIENT_ID", "MEERKAT_OIDC_CLIENT_SECRET", "MEERKAT_ADMIN_EMAILS", "MEERKAT_ADMIN_GROUPS"} {
			if value(name) != "" {
				return defaults, fmt.Errorf("%s needs MEERKAT_OIDC_ISSUER", name)
			}
		}
	} else {
		passwordLogin, err := boolean("MEERKAT_PASSWORD_LOGIN", true)
		if err != nil {
			return defaults, err
		}
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
		if !ok {
			return defaults, errors.New("single sign-on from the environment needs an https MEERKAT_OIDC_ISSUER, MEERKAT_OIDC_CLIENT_ID, and valid MEERKAT_ADMIN_EMAILS (emails or @domain) or MEERKAT_ADMIN_GROUPS")
		}
		defaults.SSO = &sso
	}

	token, chat := value("MEERKAT_TELEGRAM_BOT_TOKEN"), value("MEERKAT_TELEGRAM_CHAT_ID")
	if token != "" || chat != "" {
		enabled, err := boolean("MEERKAT_TELEGRAM_ENABLED", true)
		if err != nil {
			return defaults, err
		}
		telegram, err := control.NewTelegram(token, chat, enabled)
		if err != nil {
			return defaults, errors.New("MEERKAT_TELEGRAM_BOT_TOKEN and MEERKAT_TELEGRAM_CHAT_ID must both be set and valid")
		}
		defaults.Telegram = &telegram
	}
	return defaults, nil
}
