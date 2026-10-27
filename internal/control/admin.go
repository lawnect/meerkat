package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"crypto/pbkdf2"
)

const (
	passwordIterations = 600000
	passwordKeyLength  = 32
	sessionLifetime    = 24 * time.Hour
)

var (
	ErrAlreadySetup       = errors.New("initial administrator already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

type Admin struct {
	ID    string
	Email string
}

type Session struct {
	Token     string
	ExpiresAt time.Time
	Admin     Admin
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// randomID is safe wherever the control plane requires a Label, including a
// URL path segment and rathole service name. Raw URL-safe base64 may begin
// with '-' or '_', which Label intentionally rejects.
func randomID() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	return "m_" + token, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != fmt.Sprint(passwordIterations) {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordKeyLength {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyLength)
	return err == nil && subtle.ConstantTimeCompare(actual, expected) == 1
}

// unknownAdminHash is verified against when no administrator has the email,
// so an unknown address costs as much as a wrong password and login timing
// does not reveal the administrator's email.
var unknownAdminHash = fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, base64.RawStdEncoding.EncodeToString(make([]byte, 16)), base64.RawStdEncoding.EncodeToString(make([]byte, passwordKeyLength)))

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 254
}

func validPassword(password string) bool { return len(password) >= 12 && len(password) <= 1024 }

// SetupComplete reports whether the console needs no first administrator.
// One existing ends setup everywhere. On public addresses single sign-on
// from the environment ends it too, since its allowed people become
// administrators when they first sign in; the local admin port keeps the
// bootstrap token until an administrator exists, so SSH is always a way in,
// even before a domain makes single sign-on reachable.
func (d *Database) SetupComplete(ctx context.Context, public bool) (bool, error) {
	var count int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return false, err
	}
	if count != 0 {
		return true, nil
	}
	if !public {
		return false, nil
	}
	sso, source, err := d.ssoWithSource(ctx)
	return err == nil && source == SourceEnvironment && sso.Enabled, err
}

// Setup creates the only initial administrator. bootstrapCodeHash must be the
// SHA-256 digest stored by the deployment bootstrap, never a plaintext secret.
func (d *Database) Setup(ctx context.Context, bootstrapCodeHash, code, email, password string, public bool) (Admin, error) {
	if !validEmail(email) || !validPassword(password) || len(bootstrapCodeHash) != 64 {
		return Admin{}, ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(code)), []byte(bootstrapCodeHash)) != 1 {
		return Admin{}, ErrInvalidCredentials
	}
	// Single sign-on from the environment completes setup on public
	// addresses; the bootstrap token must not add a password administrator
	// there. Over SSH it still creates the first one.
	if public {
		if sso, source, err := d.ssoWithSource(ctx); err != nil {
			return Admin{}, err
		} else if source == SourceEnvironment && sso.Enabled {
			return Admin{}, ErrAlreadySetup
		}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Admin{}, err
	}
	id, err := randomID()
	if err != nil {
		return Admin{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return Admin{}, err
	}
	if count != 0 {
		return Admin{}, ErrAlreadySetup
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admins(id, email, password_hash, created_at) VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))", id, email, hash); err != nil {
		return Admin{}, err
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, err
	}
	return Admin{ID: id, Email: email}, nil
}

// RecoverAdmin sets the password of the administrator with email, or adds
// one, when the bootstrap token matches. The Outlet offers it only on the
// local admin port: it is the way back in over SSH when single sign-on is
// broken and no administrator has a password. A provider binding stays, so
// the administrator keeps signing in with single sign-on too.
func (d *Database) RecoverAdmin(ctx context.Context, bootstrapCodeHash, code, email, password string) (Admin, error) {
	if !validEmail(email) || !validPassword(password) || len(bootstrapCodeHash) != 64 {
		return Admin{}, ErrInvalidCredentials
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(code)), []byte(bootstrapCodeHash)) != 1 {
		return Admin{}, ErrInvalidCredentials
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Admin{}, err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, err
	}
	defer tx.Rollback()
	admin := Admin{Email: email}
	err = tx.QueryRowContext(ctx, "SELECT id, email FROM admins WHERE email = ?", email).Scan(&admin.ID, &admin.Email)
	switch {
	case err == nil:
		_, err = tx.ExecContext(ctx, "UPDATE admins SET password_hash = ? WHERE id = ?", hash, admin.ID)
	case errors.Is(err, sql.ErrNoRows):
		if admin.ID, err = randomID(); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO admins(id, email, password_hash, created_at) VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))", admin.ID, email, hash)
		}
	}
	if err != nil {
		return Admin{}, err
	}
	return admin, tx.Commit()
}

// VerifyCredentials checks an email and password without starting a session,
// so a second factor can run between the two steps.
func (d *Database) VerifyCredentials(ctx context.Context, email, password string) (Admin, error) {
	var admin Admin
	var passwordHash string
	err := d.db.QueryRowContext(ctx, "SELECT id, email, password_hash FROM admins WHERE email = ?", email).Scan(&admin.ID, &admin.Email, &passwordHash)
	// Administrators created by single sign-on have no password; they take
	// the same time to fail as an unknown email.
	if err == nil && passwordHash == ssoPasswordHash {
		err = ErrInvalidCredentials
	}
	if err != nil {
		passwordHash = unknownAdminHash
	}
	if !verifyPassword(password, passwordHash) || err != nil {
		return Admin{}, ErrInvalidCredentials
	}
	return admin, nil
}

// StartSession issues a session for an administrator who has passed every login step.
func (d *Database) StartSession(ctx context.Context, admin Admin) (Session, error) {
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	sessionID, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(sessionLifetime)
	// Sessions are only ever added, so each sign-in clears what is of no use
	// any more: an expired session can't authenticate, and a revoked one is
	// kept for a month at most. The table stays small enough to scan, which
	// is why no index is needed. A clean-up that fails waits for the next
	// sign-in rather than refuse this one.
	_, _ = d.db.ExecContext(ctx, `DELETE FROM sessions
		WHERE julianday(expires_at) <= julianday('now') OR julianday(revoked_at) < julianday('now', '-30 days')`)
	if _, err := d.db.ExecContext(ctx, "INSERT INTO sessions(id, admin_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)", sessionID, admin.ID, hashToken(token), expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Session{}, err
	}
	if _, err := d.db.ExecContext(ctx, "UPDATE admins SET last_login_at = ? WHERE id = ?", now.Format(time.RFC3339Nano), admin.ID); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expiresAt, Admin: admin}, nil
}

func (d *Database) Login(ctx context.Context, email, password string) (Session, error) {
	admin, err := d.VerifyCredentials(ctx, email, password)
	if err != nil {
		return Session{}, err
	}
	return d.StartSession(ctx, admin)
}

func (d *Database) Authenticate(ctx context.Context, token string) (Admin, error) {
	admin, _, err := d.Session(ctx, token)
	return admin, err
}

// Session returns the administrator of a live session and when it expires.
func (d *Database) Session(ctx context.Context, token string) (Admin, time.Time, error) {
	var admin Admin
	var expires string
	err := d.db.QueryRowContext(ctx, `
SELECT a.id, a.email, s.expires_at FROM sessions s JOIN admins a ON a.id = s.admin_id
WHERE s.token_hash = ? AND s.revoked_at IS NULL AND julianday(s.expires_at) > julianday('now')`, hashToken(token)).Scan(&admin.ID, &admin.Email, &expires)
	if err != nil {
		return Admin{}, time.Time{}, ErrInvalidCredentials
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return Admin{}, time.Time{}, ErrInvalidCredentials
	}
	return admin, expiresAt, nil
}

func (d *Database) RevokeSession(ctx context.Context, token string) error {
	_, err := d.db.ExecContext(ctx, "UPDATE sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE token_hash = ? AND revoked_at IS NULL", hashToken(token))
	return err
}

const settingDeviceKey = "device_key"

// DeviceKey is the secret that signs trusted-device cookies. It is created
// on first use and never leaves the database or the Outlet's memory.
func (d *Database) DeviceKey(ctx context.Context, now time.Time) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := d.db.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO NOTHING`,
		settingDeviceKey, base64.StdEncoding.EncodeToString(fresh), stamp(now)); err != nil {
		return nil, err
	}
	raw, err := getSetting(ctx, d.db, settingDeviceKey)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("stored device key is invalid")
	}
	return key, nil
}
