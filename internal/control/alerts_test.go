package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

const alertTestToken = "123456789:AAExampleExampleExampleExample_ab-cd"

func alertDB(t *testing.T) *Database {
	t.Helper()
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTelegramSettingsValidationAndInvalidation(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	for token, want := range map[string]error{"": ErrInvalidTelegramToken, "abc": ErrInvalidTelegramToken, "123:short": ErrInvalidTelegramToken} {
		if err := db.SetTelegram(ctx, token, "123", true, now); !errors.Is(err, want) {
			t.Fatalf("token %q: err = %v", token, err)
		}
	}
	for _, chat := range []string{"", "abc", "12.5", "@ab", "@1channel", "123456789012345678901"} {
		if err := db.SetTelegram(ctx, alertTestToken, chat, true, now); !errors.Is(err, ErrInvalidTelegramChat) {
			t.Fatalf("chat %q: err = %v", chat, err)
		}
	}
	for _, chat := range []string{"123", "-1001234567890", "@meerkat_alerts"} {
		if err := db.SetTelegram(ctx, alertTestToken, chat, true, now); err != nil {
			t.Fatalf("chat %q: %v", chat, err)
		}
	}
	if err := db.SetTelegram(ctx, alertTestToken, "123", true, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.MarkTelegramVerified(ctx, alertTestToken, "999", now); err != nil || ok {
		t.Fatalf("stale verification accepted: %v %v", ok, err)
	}
	if ok, err := db.MarkTelegramVerified(ctx, alertTestToken, "123", now); err != nil || !ok {
		t.Fatalf("verification: %v %v", ok, err)
	}
	if err := db.SetLoginSecondFactor(ctx, true, now); err != nil {
		t.Fatal(err)
	}
	// Same chat, empty token: nothing changes.
	if err := db.SetTelegram(ctx, "", "123", false, now); err != nil {
		t.Fatal(err)
	}
	telegram, _ := db.Telegram(ctx)
	if telegram.BotToken != alertTestToken || telegram.VerifiedAt == "" || telegram.Enabled || telegram.TokenHint() != "b-cd" {
		t.Fatalf("telegram = %+v", telegram)
	}
	if on, _ := db.LoginSecondFactor(ctx); !on {
		t.Fatal("second factor dropped without a change")
	}
	// A new chat is unverified, so the second factor that relied on it turns off.
	if err := db.SetTelegram(ctx, "", "456", true, now); err != nil {
		t.Fatal(err)
	}
	telegram, _ = db.Telegram(ctx)
	if telegram.VerifiedAt != "" {
		t.Fatalf("verification survived a chat change: %+v", telegram)
	}
	if on, _ := db.LoginSecondFactor(ctx); on {
		t.Fatal("second factor survived a chat change")
	}
	if err := db.SetLoginSecondFactor(ctx, true, now); !errors.Is(err, ErrTelegramNotVerified) {
		t.Fatalf("enable unverified: %v", err)
	}
}

func TestDeleteTelegramClearsEverything(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	if err := db.SetTelegram(ctx, alertTestToken, "123", true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.MarkTelegramVerified(ctx, alertTestToken, "123", now); err != nil {
		t.Fatal(err)
	}
	if err := db.SetLoginSecondFactor(ctx, true, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.OpenAlert(ctx, AlertEvent{Key: "tunnel_down:a", Kind: "tunnel_down"}, time.Hour, now); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteTelegram(ctx, now); err != nil {
		t.Fatal(err)
	}
	telegram, _ := db.Telegram(ctx)
	if telegram != (Telegram{}) {
		t.Fatalf("telegram = %+v", telegram)
	}
	if on, _ := db.LoginSecondFactor(ctx); on {
		t.Fatal("second factor survived deleting the bot")
	}
	if open, _ := db.OpenAlerts(ctx, "tunnel_down"); len(open) != 0 {
		t.Fatalf("open alerts = %v", open)
	}
}

func TestAlertRulesDefaultsAndRanges(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Now()
	rules, err := db.AlertRules(ctx)
	if err != nil || rules != DefaultAlertRules() || !rules.TunnelDown.Enabled || rules.TunnelDown.AfterMinutes != 5 || rules.ErrorSpike.MinRequests != 50 || !rules.Login.Enabled {
		t.Fatalf("defaults = %+v, %v", rules, err)
	}
	custom := DefaultAlertRules()
	custom.TunnelDown.AfterMinutes, custom.ErrorSpike.ThresholdPercent, custom.Login.Enabled = 30, 0.5, false
	if err := db.SetAlertRules(ctx, custom, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.AlertRules(ctx); got != custom {
		t.Fatalf("rules = %+v", got)
	}
	for name, mutate := range map[string]func(*AlertRules){
		"minutes low":   func(r *AlertRules) { r.TunnelDown.AfterMinutes = 0 },
		"minutes high":  func(r *AlertRules) { r.TunnelDown.AfterMinutes = 1441 },
		"percent low":   func(r *AlertRules) { r.ErrorSpike.ThresholdPercent = 0.05 },
		"percent high":  func(r *AlertRules) { r.ErrorSpike.ThresholdPercent = 100.1 },
		"requests low":  func(r *AlertRules) { r.ErrorSpike.MinRequests = 0 },
		"requests high": func(r *AlertRules) { r.ErrorSpike.MinRequests = 1_000_001 },
	} {
		bad := DefaultAlertRules()
		mutate(&bad)
		if err := db.SetAlertRules(ctx, bad, now); !errors.Is(err, ErrInvalidAlertRules) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	edge := DefaultAlertRules()
	edge.TunnelDown.AfterMinutes, edge.ErrorSpike.ThresholdPercent, edge.ErrorSpike.MinRequests = 1440, 100, 1_000_000
	if err := db.SetAlertRules(ctx, edge, now); err != nil {
		t.Fatalf("edge values rejected: %v", err)
	}
	// A rule added later keeps its default when the stored JSON predates it.
	if _, err := db.db.ExecContext(ctx, `UPDATE outlet_settings SET value = '{"tunnel_down":{"enabled":false,"after_minutes":9}}' WHERE key = 'alert_rules'`); err != nil {
		t.Fatal(err)
	}
	got, _ := db.AlertRules(ctx)
	if got.TunnelDown.Enabled || got.TunnelDown.AfterMinutes != 9 || !got.Login.Enabled || got.ErrorSpike.MinRequests != 50 {
		t.Fatalf("partial rules = %+v", got)
	}
}

func TestAlertEventsOpenOnceCooldownAndPrune(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	event := AlertEvent{Key: "tunnel_down:a", Kind: "tunnel_down", Subject: "edge-a", Message: "down", Ref: "/connectors/a"}
	first, opened, err := db.OpenAlert(ctx, event, time.Hour, now)
	if err != nil || !opened || first.ID == 0 || first.ResolvedAt != nil {
		t.Fatalf("open: %+v %v %v", first, opened, err)
	}
	if _, opened, err := db.OpenAlert(ctx, event, time.Hour, now.Add(time.Minute)); err != nil || opened {
		t.Fatalf("duplicate open: %v %v", opened, err)
	}
	if err := db.ResolveAlert(ctx, first.ID, now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, opened, _ := db.OpenAlert(ctx, event, time.Hour, now.Add(30*time.Minute)); opened {
		t.Fatal("reopened inside the cooldown")
	}
	again, opened, err := db.OpenAlert(ctx, event, time.Hour, now.Add(61*time.Minute))
	if err != nil || !opened || again.ID == first.ID {
		t.Fatalf("reopen after cooldown: %+v %v %v", again, opened, err)
	}
	if err := db.MarkAlertSent(ctx, again.ID, false, "telegram_unreachable"); err != nil {
		t.Fatal(err)
	}
	open, _ := db.OpenAlerts(ctx, "tunnel_down")
	if len(open) != 1 || open[0].Sent || open[0].Error == nil || *open[0].Error != "telegram_unreachable" || open[0].Ref != "/connectors/a" {
		t.Fatalf("open = %+v", open)
	}

	shot, opened, err := db.RecordAlert(ctx, AlertEvent{Key: "login:1", Kind: "login", Subject: "admin@example.com", Message: "in"}, 0, now)
	if err != nil || !opened || shot.ResolvedAt == nil || *shot.ResolvedAt != shot.OpenedAt {
		t.Fatalf("record: %+v %v %v", shot, opened, err)
	}
	// Open events are never pruned, however long the history grows.
	for i := 0; i < maxAlertEvents+20; i++ {
		if _, _, err := db.RecordAlert(ctx, AlertEvent{Key: "login:" + time.Duration(i).String(), Kind: "login"}, 0, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	events, err := db.RecentAlerts(ctx, 1000)
	if err != nil || len(events) != maxAlertEvents+1 {
		t.Fatalf("events = %d, %v", len(events), err)
	}
	if events[0].ID < events[1].ID {
		t.Fatal("events are not newest first")
	}
	if open, _ := db.OpenAlerts(ctx, "tunnel_down"); len(open) != 1 {
		t.Fatal("open event was pruned")
	}
}
