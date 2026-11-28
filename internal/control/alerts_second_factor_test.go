package control

import (
	"context"
	"testing"
	"time"
)

// The calls that turn the login second factor off as a side effect say so, so
// the caller can record it.
func TestTelegramChangesReportTurningTheSecondFactorOff(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	enable := func(chat string) {
		t.Helper()
		if _, err := db.MarkTelegramVerified(ctx, alertTestToken, chat, now); err != nil {
			t.Fatal(err)
		}
		if err := db.SetLoginSecondFactor(ctx, true, now); err != nil {
			t.Fatal(err)
		}
	}
	if off, err := db.SetTelegram(ctx, alertTestToken, "123", true, now); err != nil || off {
		t.Fatalf("first save: off=%v %v", off, err)
	}
	enable("123")
	// Nothing about the bot changes, so the second factor stays.
	if off, err := db.SetTelegram(ctx, "", "123", false, now); err != nil || off {
		t.Fatalf("same chat: off=%v %v", off, err)
	}
	if off, err := db.SetTelegram(ctx, "", "456", true, now); err != nil || !off {
		t.Fatalf("another chat: off=%v %v", off, err)
	}
	// Already off: another change turns nothing off.
	if off, err := db.SetTelegram(ctx, "", "789", true, now); err != nil || off {
		t.Fatalf("second change: off=%v %v", off, err)
	}
	enable("789")
	if off, err := db.DeleteTelegram(ctx, now); err != nil || !off {
		t.Fatalf("delete: off=%v %v", off, err)
	}
	if off, err := db.DeleteTelegram(ctx, now); err != nil || off {
		t.Fatalf("delete again: off=%v %v", off, err)
	}
}
