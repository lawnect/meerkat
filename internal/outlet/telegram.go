package outlet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const telegramAPI = "https://api.telegram.org"

const (
	telegramResponseLimit = 64 << 10
	// Telegram supplies seconds. Bound malformed/missing values to one second
	// and excessive values to 24 hours; cooldowns are in memory only.
	maxTelegramRetryAfter = 24 * 60 * 60
	maxTelegramCooldowns  = 1024
)

// Errors never retain response descriptions, bodies, tokens, chat IDs or URLs.
// In particular, transport errors must never wrap *url.Error (its URL has the token).
var (
	errTelegramUnauthorized    = errors.New("telegram_unauthorized")
	errTelegramChatNotFound    = errors.New("telegram_chat_not_found")
	errTelegramChatForbidden   = errors.New("telegram_chat_forbidden")
	errTelegramUnreachable     = errors.New("telegram_unreachable")
	errTelegramFailed          = errors.New("telegram_failed")
	errTelegramRateLimited     = errors.New("telegram_rate_limited")
	errTelegramChatMigrated    = errors.New("telegram_chat_migrated")
	errTelegramInvalidMessage  = errors.New("telegram_invalid_message")
	errTelegramInvalidRequest  = errors.New("telegram_invalid_request")
	errTelegramUnavailable     = errors.New("telegram_unavailable")
	errTelegramInvalidResponse = errors.New("telegram_invalid_response")
)

type telegramResponseError struct {
	reason     error
	Status     int
	ErrorCode  int
	RetryAfter int
}

func (e *telegramResponseError) Error() string { return e.reason.Error() }
func (e *telegramResponseError) Unwrap() error { return e.reason }

func telegramCode(err error) string {
	for _, known := range []error{errTelegramUnauthorized, errTelegramChatNotFound, errTelegramChatForbidden, errTelegramUnreachable, errTelegramRateLimited, errTelegramChatMigrated, errTelegramInvalidMessage, errTelegramInvalidRequest, errTelegramUnavailable, errTelegramInvalidResponse} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return errTelegramFailed.Error()
}

// These are the only upstream diagnostic fields allowed in logs and admin responses.
func telegramDiagnostics(err error) map[string]any {
	detail := map[string]any{"error": telegramCode(err)}
	var response *telegramResponseError
	if errors.As(err, &response) {
		if response.Status != 0 {
			detail["http_status"] = response.Status
		}
		if response.ErrorCode != 0 {
			detail["error_code"] = response.ErrorCode
		}
		if response.RetryAfter > 0 {
			detail["retry_after"] = response.RetryAfter
		}
	}
	return detail
}

func telegramLogArgs(err error) []any {
	args := []any{"error", telegramCode(err)}
	var response *telegramResponseError
	if errors.As(err, &response) {
		args = append(args, "http_status", response.Status, "error_code", response.ErrorCode, "retry_after", response.RetryAfter)
	}
	return args
}

func telegramRetryHeader(w http.ResponseWriter, err error) bool {
	var response *telegramResponseError
	if errors.As(err, &response) && errors.Is(err, errTelegramRateLimited) && response.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(response.RetryAfter))
		return true
	}
	return false
}

type telegramClient struct {
	baseURL   string
	http      *http.Client
	now       func() time.Time
	mu        sync.Mutex
	cooldowns map[[sha256.Size]byte]time.Time
}

func newTelegramClient(baseURL string) *telegramClient {
	return &telegramClient{baseURL: baseURL, now: time.Now, http: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var defaultTelegram = newTelegramClient(telegramAPI)

// cooldown shares a bot's limit across alerts, test sends and login codes. Only
// token hashes are retained, so token rotation starts independently. No handler
// sleeps or retries. Concurrent responses may extend, never shorten, a deadline.
func (c *telegramClient) cooldown(key [sha256.Size]byte, extend int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for key, until := range c.cooldowns {
		if !now.Before(until) {
			delete(c.cooldowns, key)
		}
	}
	until := c.cooldowns[key]
	if extend > 0 {
		if c.cooldowns == nil {
			c.cooldowns = make(map[[sha256.Size]byte]time.Time)
		}
		if until.IsZero() && len(c.cooldowns) >= maxTelegramCooldowns {
			var oldest [sha256.Size]byte
			var earliest time.Time
			for candidate, expires := range c.cooldowns {
				if earliest.IsZero() || expires.Before(earliest) {
					oldest, earliest = candidate, expires
				}
			}
			delete(c.cooldowns, oldest)
		}
		if candidate := now.Add(time.Duration(extend) * time.Second); candidate.After(until) {
			until = candidate
			c.cooldowns[key] = until
		}
	}
	if !now.Before(until) {
		return 0
	}
	return int((until.Sub(now) + time.Second - 1) / time.Second)
}

type telegramReply struct {
	OK          *bool  `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter      json.RawMessage `json:"retry_after"`
		MigrateToChatID json.RawMessage `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

func telegramRetryAfter(raw json.RawMessage) int {
	value := string(raw)
	if value == "" {
		return 1
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 1
		}
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil || seconds > maxTelegramRetryAfter {
		return maxTelegramRetryAfter
	}
	if seconds == 0 {
		return 1
	}
	return int(seconds)
}

func telegramDescriptionContains(description string, patterns ...string) bool {
	description = strings.ToLower(description)
	for _, pattern := range patterns {
		if strings.Contains(description, pattern) {
			return true
		}
	}
	return false
}

// send posts an HTML message. Callers escape every dynamic value.
func (c *telegramClient) send(ctx context.Context, token, chatID, text string) (sendErr error) {
	cached := false
	defer func() {
		if sendErr != nil && !cached {
			slog.Warn("telegram send failed", telegramLogArgs(sendErr)...)
		}
	}()
	key := sha256.Sum256([]byte(token))
	if wait := c.cooldown(key, 0); wait > 0 {
		cached = true
		return &telegramResponseError{reason: errTelegramRateLimited, Status: 429, ErrorCode: 429, RetryAfter: wait}
	}
	body, err := json.Marshal(map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true})
	if err != nil {
		return errTelegramFailed
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return errTelegramFailed
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errTelegramUnreachable
	}
	defer res.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(res.Body, telegramResponseLimit+1))
	var reply telegramReply
	valid := readErr == nil && len(raw) <= telegramResponseLimit && json.Unmarshal(raw, &reply) == nil && reply.OK != nil
	if valid && res.StatusCode >= 200 && res.StatusCode < 300 && *reply.OK {
		return nil
	}
	failure := &telegramResponseError{reason: errTelegramFailed, Status: res.StatusCode}
	if valid && reply.ErrorCode >= 100 && reply.ErrorCode <= 599 {
		failure.ErrorCode = reply.ErrorCode
	}
	status := res.StatusCode
	if valid && !*reply.OK && res.StatusCode >= 200 && res.StatusCode < 300 && failure.ErrorCode != 0 {
		status = failure.ErrorCode
	}
	switch {
	case res.StatusCode == 429 || status == 429:
		failure.reason = errTelegramRateLimited
		retry := 1
		if valid {
			retry = telegramRetryAfter(reply.Parameters.RetryAfter)
		}
		failure.RetryAfter = c.cooldown(key, retry)
	case status == 401 || status == 404:
		failure.reason = errTelegramUnauthorized
	case status == 403:
		failure.reason = errTelegramChatForbidden
	case valid && status == 400 && telegramDescriptionContains(reply.Description, "chat not found", "chat_id is empty", "chat_id is invalid", "chat_id_invalid", "peer_id_invalid", "peer id invalid", "bots can't send messages to bots"):
		failure.reason = errTelegramChatNotFound
	case valid && status == 400 && telegramDescriptionContains(reply.Description, "not enough rights", "bot is not a member", "bot was kicked", "chat_write_forbidden"):
		failure.reason = errTelegramChatForbidden
	case status >= 500 && status <= 599:
		failure.reason = errTelegramUnavailable
	case res.StatusCode >= 300 && res.StatusCode < 400:
		// Redirects are rejected without following their Location.
	case !valid:
		failure.reason = errTelegramInvalidResponse
	case status == 400 && len(reply.Parameters.MigrateToChatID) > 0 && string(reply.Parameters.MigrateToChatID) != "null":
		failure.reason = errTelegramChatMigrated
	case status == 400 && telegramDescriptionContains(reply.Description, "parse entities", "unsupported start tag", "can't find end tag", "can't find end of the entity"):
		failure.reason = errTelegramInvalidMessage
	case status == 400:
		failure.reason = errTelegramInvalidRequest
	}
	return failure
}

// telegramSend delivers through the injected sender in tests.
func (s *Store) telegramSend(ctx context.Context, token, chatID, text string) error {
	if s.sendTelegram != nil {
		return s.sendTelegram(ctx, token, chatID, text)
	}
	return defaultTelegram.send(ctx, token, chatID, text)
}
