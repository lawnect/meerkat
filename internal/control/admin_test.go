package control

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestSetupLoginAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	admin, err := db.Setup(ctx, hashToken(code), code, "admin@example.com", "correct horse battery staple", false)
	if err != nil {
		t.Fatal(err)
	}
	if admin.Email != "admin@example.com" {
		t.Fatalf("email = %q", admin.Email)
	}
	if _, err := db.Setup(ctx, hashToken(code), code, "other@example.com", "correct horse battery staple", false); !errors.Is(err, ErrAlreadySetup) {
		t.Fatalf("second setup error = %v", err)
	}
	if _, err := db.Login(ctx, admin.Email, "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password error = %v", err)
	}
	session, err := db.Login(ctx, admin.Email, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Authenticate(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Authenticate(ctx, session.Token); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("revoked session error = %v", err)
	}
}

// Starting a session clears the ones that are of no use any more: expired
// ones, and revoked ones past a month. Live ones, revoked or not, stay.
func TestStartSessionPrunesSessionsOfNoUse(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	admin, err := db.Setup(ctx, hashToken(code), code, "admin@example.com", "correct horse battery staple", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.RFC3339Nano) }
	for _, s := range []struct {
		id      string
		expires string
		revoked any
	}{
		{"expired", at(-time.Hour), nil},
		{"expired-and-revoked", at(-time.Hour), at(-2 * time.Hour)},
		{"live", at(time.Hour), nil},
		{"live-revoked-today", at(time.Hour), at(-time.Minute)},
		{"live-revoked-long-ago", at(time.Hour), at(-31 * 24 * time.Hour)},
	} {
		if _, err := db.SQL().ExecContext(ctx, "INSERT INTO sessions(id, admin_id, token_hash, expires_at, created_at, revoked_at) VALUES (?, ?, ?, ?, ?, ?)",
			s.id, admin.ID, s.id, s.expires, at(-48*time.Hour), s.revoked); err != nil {
			t.Fatal(err)
		}
	}
	session, err := db.StartSession(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.SQL().QueryContext(ctx, "SELECT id FROM sessions ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var left []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	if len(left) != 3 {
		t.Fatalf("sessions left = %v", left)
	}
	for _, id := range []string{"live", "live-revoked-today"} {
		if !slices.Contains(left, id) {
			t.Fatalf("%s was pruned: %v", id, left)
		}
	}
	if _, err := db.Authenticate(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
}
