package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/lawnect/meerkat/internal/validation"
)

var ErrInvalidSSO = errors.New("invalid single sign-on settings")

const (
	settingSSO = "sso"

	maxSSOEntries    = 200
	maxSSOEntryBytes = 256
	maxSSOSecret     = 1024
	maxSSOLabelRunes = 32

	// ssoPasswordHash marks an administrator created by single sign-on. It is
	// no PBKDF2 encoding, so no password ever matches it.
	ssoPasswordHash = "sso"
)

// SSO is the OpenID Connect sign-in of the admin console. Only people it
// allows get in. Each list that is set must match: AllowedUsers holds
// emails and "@domain" entries, AllowedGroups is compared with the ID
// token's GroupsClaim, and with both set a person needs both.
type SSO struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"` // provider name on the sign-in button, e.g. "Google"
	Issuer  string `json:"issuer"`
	// ClientID identifies the Outlet at the provider; ClientSecret must never
	// reach an API response, use SecretHint.
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret"`
	Scopes        []string `json:"scopes"`
	GroupsClaim   string   `json:"groups_claim"`
	AllowedUsers  []string `json:"allowed_users"`
	AllowedGroups []string `json:"allowed_groups"`
	// PasswordLogin keeps email and password sign-in on public addresses.
	// The local admin port reached over SSH always accepts passwords.
	PasswordLogin bool `json:"password_login"`
}

// DefaultSSO is off, with the scopes every OpenID provider accepts.
func DefaultSSO() SSO {
	return SSO{Scopes: []string{"openid", "email", "profile"}, GroupsClaim: "groups", AllowedUsers: []string{}, AllowedGroups: []string{}, PasswordLogin: true}
}

// SecretHint is the last four characters of the client secret.
func (s SSO) SecretHint() string {
	if len(s.ClientSecret) < 8 {
		return ""
	}
	return s.ClientSecret[len(s.ClientSecret)-4:]
}

func plainSSOValue(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func normalizeSSOList(entries []string, lower bool, valid func(string) bool) ([]string, bool) {
	if len(entries) > maxSSOEntries {
		return nil, false
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if lower {
			entry = strings.ToLower(entry)
		}
		if !valid(entry) {
			return nil, false
		}
		if !containsString(out, entry) {
			out = append(out, entry)
		}
	}
	return out, true
}

// allowedUserEntry is an email address or "@domain".
func allowedUserEntry(entry string) bool {
	if domain, ok := strings.CutPrefix(entry, "@"); ok {
		return validation.Hostname(domain)
	}
	return validEmail(entry)
}

// ValidIssuer reports whether issuer is an https URL without query or
// fragment, as OpenID Connect discovery requires.
func ValidIssuer(issuer string) bool {
	u, err := url.Parse(issuer)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && len(issuer) <= maxSSOEntryBytes
}

// Normalize trims and checks the settings. Turning sign-in on needs a
// complete provider and at least one allowed user, domain or group: an
// empty list would let every account of the provider in.
func (s SSO) Normalize() (SSO, bool) {
	s.Label, s.Issuer, s.ClientID, s.GroupsClaim = strings.TrimSpace(s.Label), strings.TrimSpace(s.Issuer), strings.TrimSpace(s.ClientID), strings.TrimSpace(s.GroupsClaim)
	s.Issuer = strings.TrimSuffix(s.Issuer, "/")
	if len([]rune(s.Label)) > maxSSOLabelRunes || strings.IndexFunc(s.Label, unicode.IsControl) >= 0 ||
		(s.Issuer != "" && !ValidIssuer(s.Issuer)) || (s.ClientID != "" && !plainSSOValue(s.ClientID, maxSSOEntryBytes)) ||
		(s.ClientSecret != "" && !plainSSOValue(s.ClientSecret, maxSSOSecret)) || (s.GroupsClaim != "" && !plainSSOValue(s.GroupsClaim, 64)) {
		return s, false
	}
	var ok [3]bool
	s.Scopes, ok[0] = normalizeSSOList(s.Scopes, false, func(v string) bool { return plainSSOValue(v, 64) })
	s.AllowedUsers, ok[1] = normalizeSSOList(s.AllowedUsers, true, allowedUserEntry)
	s.AllowedGroups, ok[2] = normalizeSSOList(s.AllowedGroups, false, func(v string) bool {
		return v != "" && len(v) <= maxSSOEntryBytes && strings.IndexFunc(v, unicode.IsControl) < 0
	})
	if ok != [3]bool{true, true, true} {
		return s, false
	}
	if !containsString(s.Scopes, "openid") {
		s.Scopes = append([]string{"openid"}, s.Scopes...)
	}
	if s.GroupsClaim == "" {
		s.GroupsClaim = "groups"
	}
	if s.Enabled && (s.Issuer == "" || s.ClientID == "" || len(s.AllowedUsers)+len(s.AllowedGroups) == 0) {
		return s, false
	}
	// Turning passwords off on public addresses only makes sense with a way in.
	if !s.Enabled {
		s.PasswordLogin = true
	}
	return s, true
}

// SSO returns the settings saved from the admin console, or else the
// environment's, or else the defaults.
func (d *Database) SSO(ctx context.Context) (SSO, error) {
	settings, _, err := d.ssoWithSource(ctx)
	return settings, err
}

// SSOSource says where the current settings come from.
func (d *Database) SSOSource(ctx context.Context) (string, error) {
	_, source, err := d.ssoWithSource(ctx)
	return source, err
}

// ErrSSOFromEnvironment refuses console changes to single sign-on the
// environment sets: a wrong save there would lock every administrator out.
var ErrSSOFromEnvironment = errors.New("single sign-on is set by the environment")

// ResetSSO forgets the settings saved from the admin console.
func (d *Database) ResetSSO(ctx context.Context) error {
	if d.environment().SSO != nil {
		return ErrSSOFromEnvironment
	}
	return deleteSettings(ctx, d.db, settingSSO)
}

// ssoWithSource prefers the environment: unlike other settings, single
// sign-on set there can't be overridden from the console, which also ignores
// a value saved before the environment set it.
func (d *Database) ssoWithSource(ctx context.Context) (SSO, string, error) {
	if env := d.environment().SSO; env != nil {
		return *env, SourceEnvironment, nil
	}
	raw, err := getSetting(ctx, d.db, settingSSO)
	if err != nil {
		return DefaultSSO(), SourceDefault, err
	}
	if raw == "" {
		return DefaultSSO(), SourceDefault, nil
	}
	return decodeSSO(raw), SourceDatabase, nil
}

func decodeSSO(raw string) SSO {
	var settings SSO
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return DefaultSSO()
	}
	if settings, ok := settings.Normalize(); ok {
		return settings
	}
	return DefaultSSO()
}

// SetSSO stores the settings. An empty ClientSecret keeps the stored one.
func (d *Database) SetSSO(ctx context.Context, settings SSO, now time.Time) (SSO, error) {
	if d.environment().SSO != nil {
		return settings, ErrSSOFromEnvironment
	}
	if settings.ClientSecret == "" {
		stored, err := d.SSO(ctx)
		if err != nil {
			return settings, err
		}
		settings.ClientSecret = stored.ClientSecret
	}
	settings, ok := settings.Normalize()
	if !ok {
		return settings, ErrInvalidSSO
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return settings, err
	}
	return settings, putSetting(ctx, d.db, settingSSO, string(raw), now)
}

// ErrSSOIdentityMismatch means the email belongs to an administrator bound to
// another account of the same provider.
var ErrSSOIdentityMismatch = errors.New("administrator is bound to another single sign-on account")

// SSOAdmin returns the administrator for a single sign-on identity: the one
// bound to the issuer and subject, or else the one with the verified email,
// which is then bound to them; with neither, a new administrator that cannot
// sign in with a password. An administrator bound to another subject of the
// same issuer is never reused, so a provider account that later claims the
// same email cannot take it over. A changed issuer, which only an
// administrator can configure, binds again.
func (d *Database) SSOAdmin(ctx context.Context, issuer, subject, email string) (Admin, error) {
	if !validEmail(email) || issuer == "" || subject == "" {
		return Admin{}, ErrInvalidCredentials
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, err
	}
	defer tx.Rollback()
	var admin Admin
	err = tx.QueryRowContext(ctx, "SELECT id, email FROM admins WHERE sso_issuer = ? AND sso_subject = ?", issuer, subject).Scan(&admin.ID, &admin.Email)
	if err == nil {
		return admin, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Admin{}, err
	}
	var boundIssuer, boundSubject string
	err = tx.QueryRowContext(ctx, "SELECT id, email, COALESCE(sso_issuer, ''), COALESCE(sso_subject, '') FROM admins WHERE email = ?", email).Scan(&admin.ID, &admin.Email, &boundIssuer, &boundSubject)
	switch {
	case err == nil && boundIssuer == issuer && boundSubject != "":
		return Admin{}, ErrSSOIdentityMismatch
	case err == nil:
		if _, err := tx.ExecContext(ctx, "UPDATE admins SET sso_issuer = ?, sso_subject = ? WHERE id = ?", issuer, subject, admin.ID); err != nil {
			return Admin{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		if admin.ID, err = randomID(); err != nil {
			return Admin{}, err
		}
		admin.Email = email
		if _, err := tx.ExecContext(ctx, "INSERT INTO admins(id, email, password_hash, created_at, sso_issuer, sso_subject) VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?, ?)", admin.ID, email, ssoPasswordHash, issuer, subject); err != nil {
			return Admin{}, err
		}
	default:
		return Admin{}, err
	}
	return admin, tx.Commit()
}
