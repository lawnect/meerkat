package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestHSTSIsOnUntilTurnedOff(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, err := db.HSTS(ctx); err != nil || !got.Enabled {
		t.Fatalf("default = %+v, %v", got, err)
	}
	if _, err := db.SetHSTS(ctx, HSTS{Enabled: false}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.HSTS(ctx); err != nil || got.Enabled {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	if _, err := db.SetHSTS(ctx, HSTS{Enabled: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := db.HSTS(ctx); err != nil || !got.Enabled {
		t.Fatalf("turned back on = %+v, %v", got, err)
	}
}
