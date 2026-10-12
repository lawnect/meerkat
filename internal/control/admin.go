package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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

func validEmail(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email && len(email) <= 254
}

func validPassword(password string) bool { return len(password) >= 12 && len(password) <= 1024 }

func (d *Database) SetupComplete(ctx context.Context) (bool, error) {
	var count int
	if err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return false, err
	}
	return count != 0, nil
}

// Setup creates the only initial administrator. bootstrapCodeHash must be the
// SHA-256 digest stored by the deployment bootstrap, never a plaintext secret.
func (d *Database) Setup(ctx context.Context, bootstrapCodeHash, code, email, password string) (Admin, error) {
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

func (d *Database) Login(ctx context.Context, email, password string) (Session, error) {
	var admin Admin
	var passwordHash string
	err := d.db.QueryRowContext(ctx, "SELECT id, email, password_hash FROM admins WHERE email = ?", email).Scan(&admin.ID, &admin.Email, &passwordHash)
	if err != nil || !verifyPassword(password, passwordHash) {
		return Session{}, ErrInvalidCredentials
	}
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
	if _, err := d.db.ExecContext(ctx, "INSERT INTO sessions(id, admin_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)", sessionID, admin.ID, hashToken(token), expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Session{}, err
	}
	if _, err := d.db.ExecContext(ctx, "UPDATE admins SET last_login_at = ? WHERE id = ?", now.Format(time.RFC3339Nano), admin.ID); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expiresAt, Admin: admin}, nil
}

func (d *Database) Authenticate(ctx context.Context, token string) (Admin, error) {
	var admin Admin
	err := d.db.QueryRowContext(ctx, `
SELECT a.id, a.email FROM sessions s JOIN admins a ON a.id = s.admin_id
WHERE s.token_hash = ? AND s.revoked_at IS NULL AND julianday(s.expires_at) > julianday('now')`, hashToken(token)).Scan(&admin.ID, &admin.Email)
	if err != nil {
		return Admin{}, ErrInvalidCredentials
	}
	return admin, nil
}

func (d *Database) RevokeSession(ctx context.Context, token string) error {
	_, err := d.db.ExecContext(ctx, "UPDATE sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE token_hash = ? AND revoked_at IS NULL", hashToken(token))
	return err
}
