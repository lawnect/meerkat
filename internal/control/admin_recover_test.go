package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openSessionsDB(t *testing.T) (*Database, Admin) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	admin, err := db.Setup(ctx, hashToken(code), code, "admin@example.com", "correct horse battery staple", false)
	if err != nil {
		t.Fatal(err)
	}
	return db, admin
}

func live(t *testing.T, db *Database, token string) bool {
	t.Helper()
	_, err := db.Authenticate(context.Background(), token)
	if err != nil && !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	return err == nil
}

// Recovery sets a new password because the old one may be in someone else's
// hands; a session that password opened must not outlive it. Only that
// administrator's sessions end.
func TestRecoverAdminEndsThatAdministratorsSessions(t *testing.T) {
	ctx := context.Background()
	db, admin := openSessionsDB(t)
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	other, err := db.RecoverAdmin(ctx, hashToken(code), code, "other@example.com", "another correct horse staple")
	if err != nil {
		t.Fatal(err)
	}
	var mine []Session
	for range 2 {
		session, err := db.Login(ctx, admin.Email, "correct horse battery staple")
		if err != nil {
			t.Fatal(err)
		}
		mine = append(mine, session)
	}
	theirs, err := db.Login(ctx, other.Email, "another correct horse staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RecoverAdmin(ctx, hashToken(code), code, admin.Email, "a brand new password!"); err != nil {
		t.Fatal(err)
	}
	for i, session := range mine {
		if live(t, db, session.Token) {
			t.Fatalf("session %d of the recovered administrator survived recovery", i)
		}
	}
	if !live(t, db, theirs.Token) {
		t.Fatal("recovering one administrator ended another's session")
	}
	if _, err := db.Login(ctx, admin.Email, "a brand new password!"); err != nil {
		t.Fatalf("the new password: %v", err)
	}
}
