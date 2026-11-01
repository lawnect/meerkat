package control

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// Stored settings that can't be read close the console on public addresses
// (an access control fails closed) and say so with ErrDamagedAdminAccess.
func TestAdminAccessReportsDamagedSettings(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if a, err := db.AdminAccess(ctx); err != nil || a.Restricted || a.Allowed == nil {
		t.Fatalf("default = %+v, %v", a, err)
	}
	if _, err := db.SetAdminAccess(ctx, AdminAccess{Restricted: true, Allowed: []string{"203.0.113.7/32"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a, err := db.AdminAccess(ctx); err != nil || !a.Restricted || len(a.Allowed) != 1 || a.Allowed[0] != "203.0.113.7" {
		t.Fatalf("stored = %+v, %v", a, err)
	}
	many := `{"restricted":true,"allowed":[`
	for i := range MaxAdminAccessAllowed + 1 {
		if i > 0 {
			many += ","
		}
		many += fmt.Sprintf(`"10.0.%d.1"`, i)
	}
	many += `]}`
	for _, raw := range []string{"{", many, `{"restricted":true,"allowed":["nope"]}`} {
		if err := putSetting(ctx, db.db, settingAdminAccess, raw, time.Now()); err != nil {
			t.Fatal(err)
		}
		if a, err := db.AdminAccess(ctx); !errors.Is(err, ErrDamagedAdminAccess) || !a.Restricted || len(a.Allowed) != 0 {
			t.Fatalf("%.40s: %+v, %v", raw, a, err)
		}
	}
}
