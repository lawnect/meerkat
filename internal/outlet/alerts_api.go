package outlet

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/lawnect/meerkat/internal/control"
)

type telegramStatus struct {
	Configured bool    `json:"configured"`
	Enabled    bool    `json:"enabled"`
	ChatID     string  `json:"chat_id"`
	TokenHint  string  `json:"token_hint"`
	VerifiedAt *string `json:"verified_at"`
	// Source is where the bot comes from: "database", "environment" or
	// "default"; Environment says whether the environment names one, which
	// deleting the saved bot falls back to.
	Source      string `json:"source"`
	Environment bool   `json:"environment"`
}

type alertsResponse struct {
	Telegram          telegramStatus       `json:"telegram"`
	Rules             control.AlertRules   `json:"rules"`
	Events            []control.AlertEvent `json:"events"`
	LoginSecondFactor bool                 `json:"login_second_factor"`
}

func (s *Store) alertsView(ctx context.Context) (alertsResponse, error) {
	telegram, err := s.control.Telegram(ctx)
	if err != nil {
		return alertsResponse{}, err
	}
	_, fromEnvironment := s.control.EnvironmentSets()
	view := alertsResponse{Telegram: telegramStatus{Configured: telegram.Configured(), Enabled: telegram.Enabled, ChatID: telegram.ChatID, TokenHint: telegram.TokenHint(), Source: telegram.Source, Environment: fromEnvironment}}
	if telegram.VerifiedAt != "" {
		view.Telegram.VerifiedAt = &telegram.VerifiedAt
	}
	if view.Rules, err = s.control.AlertRules(ctx); err != nil {
		return alertsResponse{}, err
	}
	if view.Events, err = s.control.RecentAlerts(ctx, 50); err != nil {
		return alertsResponse{}, err
	}
	view.LoginSecondFactor, err = s.control.LoginSecondFactor(ctx)
	return view, err
}

// alertsAdmin checks the administrator and the control database.
func (s *Store) alertsAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.admin(w, r) {
		return false
	}
	if s.control == nil {
		fail(w, r, 503, "control_database_unavailable")
		return false
	}
	return true
}

func (s *Store) replyAlerts(w http.ResponseWriter, r *http.Request, status int) {
	view, err := s.alertsView(r.Context())
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	reply(w, status, view)
}

func (s *Store) getAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alertsAdmin(w, r) {
		s.replyAlerts(w, r, 200)
	}
}

type telegramRequest struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	Enabled  bool   `json:"enabled"`
}

func (s *Store) putTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req telegramRequest
	if !decode(w, r, &req, "chat_id", "enabled") {
		return
	}
	switch err := s.control.SetTelegram(r.Context(), req.BotToken, req.ChatID, req.Enabled, s.now()); {
	case errors.Is(err, control.ErrInvalidTelegramToken):
		fail(w, r, 400, "invalid_telegram_token")
	case errors.Is(err, control.ErrInvalidTelegramChat):
		fail(w, r, 400, "invalid_telegram_chat")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		detail := map[string]any{"chat_id": strings.TrimSpace(req.ChatID), "enabled": req.Enabled, "token_changed": strings.TrimSpace(req.BotToken) != ""}
		s.audited(r, "telegram.update", "telegram", "settings", detail)
		s.replyAlerts(w, r, 200)
	}
}

func (s *Store) testTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	telegram, err := s.control.Telegram(r.Context())
	if err == nil && !telegram.Configured() {
		fail(w, r, 409, "telegram_not_configured")
		return
	}
	var outlet control.Outlet
	if err == nil {
		outlet, err = s.control.Outlet(r.Context())
	}
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	now := s.now()
	if err := s.telegramSend(r.Context(), telegram.BotToken, telegram.ChatID, testNotice(outlet.AdminHostname, now).HTML(outlet.AdminHostname)); err != nil {
		s.audit(r, "telegram.test", control.AuditEntry{ResourceType: "telegram", ResourceID: "settings", Detail: map[string]any{"ok": false, "error": telegramCode(err)}})
		fail(w, r, 502, telegramCode(err))
		return
	}
	verified, err := s.control.MarkTelegramVerified(r.Context(), telegram.BotToken, telegram.ChatID, now)
	if err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	// The settings changed while the message was on its way: what was
	// tested isn't what is saved.
	if !verified {
		fail(w, r, 409, "telegram_changed")
		return
	}
	s.audited(r, "telegram.test", "telegram", "settings", map[string]any{"ok": true})
	s.replyAlerts(w, r, 200)
}

func (s *Store) deleteTelegram(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	if err := s.control.DeleteTelegram(r.Context(), s.now()); err != nil {
		fail(w, r, 503, "persistence_failed")
		return
	}
	s.audited(r, "telegram.delete", "telegram", "settings", nil)
	s.replyAlerts(w, r, 200)
}

func (s *Store) putAlertRules(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var rules control.AlertRules
	if !decode(w, r, &rules, "tunnel_down", "route_error", "dns_failure", "error_spike", "login", "rate_limit") {
		return
	}
	switch err := s.control.SetAlertRules(r.Context(), rules, s.now()); {
	case errors.Is(err, control.ErrInvalidAlertRules):
		fail(w, r, 400, "invalid_alert_rules")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		detail := map[string]any{"tunnel_down": rules.TunnelDown, "route_error": rules.RouteError, "dns_failure": rules.DNSFailure, "error_spike": rules.ErrorSpike, "login": rules.Login, "rate_limit": rules.RateLimit}
		s.audited(r, "alert_rules.update", "alert_rules", "rules", detail)
		s.replyAlerts(w, r, 200)
	}
}

type secondFactorSetting struct {
	Enabled bool `json:"enabled"`
}

func (s *Store) putSecondFactor(w http.ResponseWriter, r *http.Request) {
	if !s.alertsAdmin(w, r) {
		return
	}
	var req secondFactorSetting
	if !decode(w, r, &req, "enabled") {
		return
	}
	switch err := s.control.SetLoginSecondFactor(r.Context(), req.Enabled, s.now()); {
	case errors.Is(err, control.ErrTelegramNotVerified):
		fail(w, r, 409, "telegram_not_verified")
	case err != nil:
		fail(w, r, 503, "persistence_failed")
	default:
		s.audited(r, "second_factor.update", "security", "second_factor", map[string]any{"enabled": req.Enabled})
		reply(w, 200, map[string]bool{"login_second_factor": req.Enabled})
	}
}
