package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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
