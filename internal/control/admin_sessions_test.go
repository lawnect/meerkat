package control

import (
	"context"
	"testing"
)

func TestRevokeAllSessionsEndsEveryAdministratorsSessions(t *testing.T) {
	ctx := context.Background()
	db, admin := openSessionsDB(t)
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	other, err := db.RecoverAdmin(ctx, hashToken(code), code, "other@example.com", "another correct horse staple")
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for _, who := range []struct{ email, password string }{{admin.Email, "correct horse battery staple"}, {admin.Email, "correct horse battery staple"}, {other.Email, "another correct horse staple"}} {
		session, err := db.Login(ctx, who.email, who.password)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, session.Token)
	}
	n, err := db.RevokeAllSessions(ctx)
	if err != nil || n != 3 {
		t.Fatalf("revoked %d, %v", n, err)
	}
	for i, token := range tokens {
		if live(t, db, token) {
			t.Fatalf("session %d is still live", i)
		}
	}
	if n, err := db.RevokeAllSessions(ctx); err != nil || n != 0 {
		t.Fatalf("again: revoked %d, %v", n, err)
	}
	if _, err := db.Login(ctx, admin.Email, "correct horse battery staple"); err != nil {
		t.Fatalf("signing in after: %v", err)
	}
}

func TestRevokeAdminSessionsEndsOnlyTheNamedAdministrators(t *testing.T) {
	ctx := context.Background()
	db, admin := openSessionsDB(t)
	code := "c6G_g2sYmBLBmx4TVQP6SyA1kE0MbeKIG-hKDmevtqY"
	other, err := db.RecoverAdmin(ctx, hashToken(code), code, "other@example.com", "another correct horse staple")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := db.Login(ctx, admin.Email, "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := db.Login(ctx, other.Email, "another correct horse staple")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.RevokeAdminSessions(ctx, []string{admin.ID, "nobody"}); err != nil || n != 1 {
		t.Fatalf("revoked %d, %v", n, err)
	}
	if live(t, db, mine.Token) || !live(t, db, theirs.Token) {
		t.Fatal("wrong sessions ended")
	}
	if n, err := db.RevokeAdminSessions(ctx, nil); err != nil || n != 0 {
		t.Fatalf("no one: revoked %d, %v", n, err)
	}
}

// Only administrators bound to a provider account are listed; and an
// administrator single sign-on created is told apart from one that also has
// a password, since only the first signs in with the provider alone.
func TestSSOAdministratorsAreListedAndMarked(t *testing.T) {
	ctx := context.Background()
	db, admin := openSessionsDB(t)
	sso, err := db.SSOAdmin(ctx, "https://idp.example.com", "sub-1", "sso@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := db.SSOAdmin(ctx, "https://idp.example.com", "sub-2", admin.Email)
	if err != nil || bound.ID != admin.ID {
		t.Fatalf("binding: %+v %v", bound, err)
	}
	listed, err := db.SSOBoundAdmins(ctx)
	if err != nil || len(listed) != 2 {
		t.Fatalf("bound administrators: %+v %v", listed, err)
	}
	// A session of the administrator single sign-on made says so; one of a
	// password administrator doesn't, even when that one is bound too.
	for _, c := range []struct {
		admin Admin
		only  bool
	}{{sso, true}, {admin, false}} {
		session, err := db.StartSession(ctx, c.admin)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := db.Session(ctx, session.Token)
		if err != nil || got.SSOOnly != c.only {
			t.Fatalf("%s: %+v %v", c.admin.Email, got, err)
		}
	}
}
