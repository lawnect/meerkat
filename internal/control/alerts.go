package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrInvalidTelegramToken = errors.New("invalid telegram bot token")
	ErrInvalidTelegramChat  = errors.New("invalid telegram chat")
	ErrTelegramNotVerified  = errors.New("telegram is not verified")
	ErrInvalidAlertRules    = errors.New("invalid alert rules")
)

const (
	settingTelegramToken    = "telegram_bot_token"
	settingTelegramChat     = "telegram_chat_id"
	settingTelegramEnabled  = "telegram_enabled"
	settingTelegramVerified = "telegram_verified_at"
	// settingTelegramVerifiedFor is telegramKey of the bot and chat the
	// verification was for.
	settingTelegramVerifiedFor = "telegram_verified_for"
	settingSecondFactor        = "login_second_factor"
	settingAlertRules          = "alert_rules"

	// maxAlertEvents bounds the history. Open events are never pruned.
	maxAlertEvents = 200
)

var (
	telegramTokenPattern = regexp.MustCompile(`^\d{5,}:[A-Za-z0-9_-]{30,}$`)
	telegramChatPattern  = regexp.MustCompile(`^(-?\d{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)
)

// conn is satisfied by both the database and a transaction.
type conn interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func putSetting(ctx context.Context, q conn, key, value string, now time.Time) error {
	_, err := q.ExecContext(ctx, `INSERT INTO outlet_settings(key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, stamp(now))
	return err
}

func deleteSettings(ctx context.Context, q conn, keys ...string) error {
	for _, key := range keys {
		if _, err := q.ExecContext(ctx, "DELETE FROM outlet_settings WHERE key = ?", key); err != nil {
			return err
		}
	}
	return nil
}

func getSetting(ctx context.Context, q conn, key string) (string, error) {
	var value string
	err := q.QueryRowContext(ctx, "SELECT value FROM outlet_settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func flag(enabled bool) string {
	if enabled {
		return "1"
	}
	return "0"
}

// Telegram holds the one bot per Outlet. The token is a secret and must
// never reach an API response; use TokenHint instead.
type Telegram struct {
	BotToken   string
	ChatID     string
	Enabled    bool
	VerifiedAt string
	// Source is SourceDatabase, SourceEnvironment or, when nothing is set,
	// SourceDefault.
	Source string
}

func (t Telegram) Configured() bool { return t.BotToken != "" && t.ChatID != "" }

// NewTelegram checks a bot token and chat ID given outside the admin
// console, such as in the environment.
func NewTelegram(token, chatID string, enabled bool) (Telegram, error) {
	token, chatID = strings.TrimSpace(token), strings.TrimSpace(chatID)
	if !telegramTokenPattern.MatchString(token) {
		return Telegram{}, ErrInvalidTelegramToken
	}
	if !telegramChatPattern.MatchString(chatID) {
		return Telegram{}, ErrInvalidTelegramChat
	}
	return Telegram{BotToken: token, ChatID: chatID, Enabled: enabled}, nil
}

// TokenHint is the last four characters of the token.
func (t Telegram) TokenHint() string {
	if len(t.BotToken) < 4 {
		return ""
	}
	return t.BotToken[len(t.BotToken)-4:]
}

// readTelegram returns the bot saved from the admin console or, when none
// is, the environment's.
func (d *Database) readTelegram(ctx context.Context, q conn) (Telegram, error) {
	t, err := readStoredTelegram(ctx, q)
	if err != nil || t.BotToken != "" || t.ChatID != "" {
		t.Source = SourceDatabase
		return t, err
	}
	env := d.environment().Telegram
	if env == nil {
		t.Source = SourceDefault
		return t, nil
	}
	verifiedFor, err := getSetting(ctx, q, settingTelegramVerifiedFor)
	if err != nil {
		return Telegram{}, err
	}
	verifiedAt := t.VerifiedAt
	t = *env
	t.Source, t.VerifiedAt = SourceEnvironment, ""
	if verifiedFor == telegramKey(env.BotToken, env.ChatID) {
		t.VerifiedAt = verifiedAt
	}
	return t, nil
}

func readStoredTelegram(ctx context.Context, q conn) (Telegram, error) {
	var t Telegram
	for key, target := range map[string]*string{settingTelegramToken: &t.BotToken, settingTelegramChat: &t.ChatID, settingTelegramVerified: &t.VerifiedAt} {
		value, err := getSetting(ctx, q, key)
		if err != nil {
			return Telegram{}, err
		}
		*target = value
	}
	enabled, err := getSetting(ctx, q, settingTelegramEnabled)
	t.Enabled = enabled == "1"
	return t, err
}

func (d *Database) Telegram(ctx context.Context) (Telegram, error) { return d.readTelegram(ctx, d.db) }

// SetTelegram stores the bot settings. An empty token keeps the current one.
// Changing the token or chat invalidates the verification and, with it, the
// login second factor that depends on it.
func (d *Database) SetTelegram(ctx context.Context, token, chatID string, enabled bool, now time.Time) error {
	token, chatID = strings.TrimSpace(token), strings.TrimSpace(chatID)
	if token != "" && !telegramTokenPattern.MatchString(token) {
		return ErrInvalidTelegramToken
	}
	if !telegramChatPattern.MatchString(chatID) {
		return ErrInvalidTelegramChat
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := d.readTelegram(ctx, tx)
	if err != nil {
		return err
	}
	if token == "" {
		token = current.BotToken
	}
	if token == "" {
		return ErrInvalidTelegramToken
	}
	for key, value := range map[string]string{settingTelegramToken: token, settingTelegramChat: chatID, settingTelegramEnabled: flag(enabled)} {
		if err := putSetting(ctx, tx, key, value, now); err != nil {
			return err
		}
	}
	if token != current.BotToken || chatID != current.ChatID {
		if err := deleteSettings(ctx, tx, settingTelegramVerified, settingTelegramVerifiedFor); err != nil {
			return err
		}
		if err := putSetting(ctx, tx, settingSecondFactor, "0", now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkTelegramVerified records a delivered test message, but only if the
// settings are still the ones the message went to.
func (d *Database) MarkTelegramVerified(ctx context.Context, token, chatID string, at time.Time) (bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current, err := d.readTelegram(ctx, tx)
	if err != nil || current.BotToken != token || current.ChatID != chatID {
		return false, err
	}
	if err := putSetting(ctx, tx, settingTelegramVerified, stamp(at), at); err != nil {
		return false, err
	}
	if err := putSetting(ctx, tx, settingTelegramVerifiedFor, telegramKey(token, chatID), at); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// DeleteTelegram forgets the bot saved from the admin console and everything
// that depends on it. A bot from the environment, if any, takes its place.
func (d *Database) DeleteTelegram(ctx context.Context, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := deleteSettings(ctx, tx, settingTelegramToken, settingTelegramChat, settingTelegramEnabled, settingTelegramVerified, settingTelegramVerifiedFor); err != nil {
		return err
	}
	if err := putSetting(ctx, tx, settingSecondFactor, "0", now); err != nil {
		return err
	}
	// Without a bot nothing would ever announce the recovery.
	if _, err := tx.ExecContext(ctx, "UPDATE alert_events SET resolved_at = ? WHERE resolved_at IS NULL", stamp(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *Database) LoginSecondFactor(ctx context.Context) (bool, error) {
	value, err := getSetting(ctx, d.db, settingSecondFactor)
	return value == "1", err
}

// SetLoginSecondFactor turns the Telegram login code on or off. Enabling
// needs a bot that has delivered a test message, so the code cannot be sent
// into the void and lock internet logins out.
func (d *Database) SetLoginSecondFactor(ctx context.Context, enabled bool, now time.Time) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if enabled {
		telegram, err := d.readTelegram(ctx, tx)
		if err != nil {
			return err
		}
		if !telegram.Configured() || telegram.VerifiedAt == "" {
			return ErrTelegramNotVerified
		}
	}
	if err := putSetting(ctx, tx, settingSecondFactor, flag(enabled), now); err != nil {
		return err
	}
	return tx.Commit()
}

type ToggleRule struct {
	Enabled bool `json:"enabled"`
}

type TunnelDownRule struct {
	Enabled      bool `json:"enabled"`
	AfterMinutes int  `json:"after_minutes"`
}

// ServiceDownRule fires when a service of a connected tunnel isn't serving
// for this long.
type ServiceDownRule struct {
	Enabled      bool `json:"enabled"`
	AfterMinutes int  `json:"after_minutes"`
}

type ErrorSpikeRule struct {
	Enabled          bool    `json:"enabled"`
	ThresholdPercent float64 `json:"threshold_percent"`
	MinRequests      int     `json:"min_requests"`
}

type RateLimitAlertRule struct {
	Enabled    bool `json:"enabled"`
	MinBlocked int  `json:"min_blocked"`
}

// LatencyRule fires when an hour's p95, of the Outlet's own time or of the
// services', is IncreasePercent above the 7 days before it, over at least
// MinRequests requests.
type LatencyRule struct {
	Enabled         bool    `json:"enabled"`
	IncreasePercent float64 `json:"increase_percent"`
	MinRequests     int     `json:"min_requests"`
}

// FreeSpaceRule fires when less than MinFreePercent is left.
type FreeSpaceRule struct {
	Enabled        bool `json:"enabled"`
	MinFreePercent int  `json:"min_free_percent"`
}

// TunnelUnstableRule fires when a tunnel reconnects, or its tunnel process
// restarts, MinReconnects times within an hour.
type TunnelUnstableRule struct {
	Enabled       bool `json:"enabled"`
	MinReconnects int  `json:"min_reconnects"`
}

type AlertRules struct {
	TunnelDown  TunnelDownRule  `json:"tunnel_down"`
	ServiceDown ServiceDownRule `json:"service_down"`
	RouteError  ToggleRule      `json:"route_error"`
	DNSFailure  ToggleRule      `json:"dns_failure"`
	ErrorSpike  ErrorSpikeRule  `json:"error_spike"`
	Login       ToggleRule      `json:"login"`
	// RateLimit fires when this many requests were rejected within 10 minutes.
	RateLimit RateLimitAlertRule `json:"rate_limit"`
	// The rules below came after the first six; a body that leaves one out
	// keeps it as stored.
	Latency        LatencyRule        `json:"latency"`
	OutletRestart  ToggleRule         `json:"outlet_restart"`
	DiskLow        FreeSpaceRule      `json:"disk_low"`
	MemoryLow      FreeSpaceRule      `json:"memory_low"`
	TunnelUnstable TunnelUnstableRule `json:"tunnel_unstable"`
}

func DefaultAlertRules() AlertRules {
	return AlertRules{
		TunnelDown:     TunnelDownRule{Enabled: true, AfterMinutes: 5},
		ServiceDown:    ServiceDownRule{Enabled: true, AfterMinutes: 5},
		RouteError:     ToggleRule{Enabled: true},
		DNSFailure:     ToggleRule{Enabled: true},
		ErrorSpike:     ErrorSpikeRule{Enabled: true, ThresholdPercent: 5, MinRequests: 50},
		Login:          ToggleRule{Enabled: true},
		RateLimit:      RateLimitAlertRule{Enabled: true, MinBlocked: 500},
		Latency:        LatencyRule{Enabled: true, IncreasePercent: 50, MinRequests: 100},
		OutletRestart:  ToggleRule{Enabled: true},
		DiskLow:        FreeSpaceRule{Enabled: true, MinFreePercent: 10},
		MemoryLow:      FreeSpaceRule{Enabled: true, MinFreePercent: 10},
		TunnelUnstable: TunnelUnstableRule{Enabled: true, MinReconnects: 3},
	}
}

func (r AlertRules) Valid() bool {
	return r.TunnelDown.AfterMinutes >= 1 && r.TunnelDown.AfterMinutes <= 1440 &&
		r.ServiceDown.AfterMinutes >= 1 && r.ServiceDown.AfterMinutes <= 1440 &&
		r.ErrorSpike.ThresholdPercent >= 0.1 && r.ErrorSpike.ThresholdPercent <= 100 &&
		r.ErrorSpike.MinRequests >= 1 && r.ErrorSpike.MinRequests <= 1_000_000 &&
		r.RateLimit.MinBlocked >= 1 && r.RateLimit.MinBlocked <= 1_000_000 &&
		r.Latency.IncreasePercent >= 10 && r.Latency.IncreasePercent <= 1000 &&
		r.Latency.MinRequests >= 1 && r.Latency.MinRequests <= 1_000_000 &&
		r.DiskLow.MinFreePercent >= 1 && r.DiskLow.MinFreePercent <= 50 &&
		r.MemoryLow.MinFreePercent >= 1 && r.MemoryLow.MinFreePercent <= 50 &&
		r.TunnelUnstable.MinReconnects >= 2 && r.TunnelUnstable.MinReconnects <= 100
}

// AlertRules returns the stored rules over the defaults, so a rule added in
// a later release is on until the administrator saves a choice.
func (d *Database) AlertRules(ctx context.Context) (AlertRules, error) {
	rules := DefaultAlertRules()
	raw, err := getSetting(ctx, d.db, settingAlertRules)
	if err != nil || raw == "" {
		return rules, err
	}
	if err := json.Unmarshal([]byte(raw), &rules); err != nil || !rules.Valid() {
		return DefaultAlertRules(), nil
	}
	return rules, nil
}

func (d *Database) SetAlertRules(ctx context.Context, rules AlertRules, now time.Time) error {
	if !rules.Valid() {
		return ErrInvalidAlertRules
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	return putSetting(ctx, d.db, settingAlertRules, string(raw), now)
}

// AlertEvent is one notification. Message is plain text for the dashboard;
// Ref is the admin page path the Telegram message links to.
type AlertEvent struct {
	ID         int64   `json:"id"`
	Key        string  `json:"-"`
	Kind       string  `json:"kind"`
	Subject    string  `json:"subject"`
	Message    string  `json:"message"`
	Ref        string  `json:"-"`
	OpenedAt   string  `json:"opened_at"`
	ResolvedAt *string `json:"resolved_at"`
	Sent       bool    `json:"sent"`
	Error      *string `json:"error"`
}

// OpenAlert records a condition that stays open until ResolveAlert. It does
// nothing when the key is already open or opened within cooldown.
func (d *Database) OpenAlert(ctx context.Context, e AlertEvent, cooldown time.Duration, now time.Time) (AlertEvent, bool, error) {
	return d.insertAlert(ctx, e, cooldown, now, false)
}

// RecordAlert records a one-shot notification that is resolved on arrival.
func (d *Database) RecordAlert(ctx context.Context, e AlertEvent, cooldown time.Duration, now time.Time) (AlertEvent, bool, error) {
	return d.insertAlert(ctx, e, cooldown, now, true)
}

func (d *Database) insertAlert(ctx context.Context, e AlertEvent, cooldown time.Duration, now time.Time, instant bool) (AlertEvent, bool, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return e, false, err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM alert_events WHERE key = ? AND (resolved_at IS NULL OR opened_at > ?))", e.Key, stamp(now.Add(-cooldown))).Scan(&exists); err != nil || exists {
		return e, false, err
	}
	e.OpenedAt = stamp(now)
	var resolved any
	if instant {
		e.ResolvedAt, resolved = &e.OpenedAt, e.OpenedAt
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO alert_events(key, kind, subject, message, ref, opened_at, resolved_at) VALUES (?, ?, ?, ?, ?, ?, ?)", e.Key, e.Kind, e.Subject, e.Message, e.Ref, e.OpenedAt, resolved)
	if err != nil {
		return e, false, err
	}
	if e.ID, err = result.LastInsertId(); err != nil {
		return e, false, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM alert_events WHERE resolved_at IS NOT NULL AND id NOT IN (SELECT id FROM alert_events ORDER BY id DESC LIMIT ?)", maxAlertEvents); err != nil {
		return e, false, err
	}
	return e, true, tx.Commit()
}

func (d *Database) ResolveAlert(ctx context.Context, id int64, now time.Time) error {
	_, err := d.db.ExecContext(ctx, "UPDATE alert_events SET resolved_at = ? WHERE id = ? AND resolved_at IS NULL", stamp(now), id)
	return err
}

// MarkAlertSent records the delivery result. failure is a short error code.
func (d *Database) MarkAlertSent(ctx context.Context, id int64, sent bool, failure string) error {
	var reason any
	if failure != "" {
		reason = failure
	}
	_, err := d.db.ExecContext(ctx, "UPDATE alert_events SET sent = ?, error = ? WHERE id = ?", sent, reason, id)
	return err
}

func (d *Database) queryAlerts(ctx context.Context, where string, args ...any) ([]AlertEvent, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT id, key, kind, subject, message, ref, opened_at, resolved_at, sent, error FROM alert_events "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []AlertEvent{}
	for rows.Next() {
		var e AlertEvent
		var resolved, failure sql.NullString
		if err := rows.Scan(&e.ID, &e.Key, &e.Kind, &e.Subject, &e.Message, &e.Ref, &e.OpenedAt, &resolved, &e.Sent, &failure); err != nil {
			return nil, err
		}
		if resolved.Valid {
			e.ResolvedAt = &resolved.String
		}
		if failure.Valid {
			e.Error = &failure.String
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// OpenAlerts lists the unresolved events of one kind.
func (d *Database) OpenAlerts(ctx context.Context, kind string) ([]AlertEvent, error) {
	return d.queryAlerts(ctx, "WHERE kind = ? AND resolved_at IS NULL", kind)
}

// RecentAlerts lists the newest events first.
func (d *Database) RecentAlerts(ctx context.Context, limit int) ([]AlertEvent, error) {
	return d.queryAlerts(ctx, "ORDER BY id DESC LIMIT ?", limit)
}
