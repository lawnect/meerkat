package outlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

const telegramAPI = "https://api.telegram.org"

// The errors carry only a code. The bot token is part of the request URL, so
// a transport error is never wrapped: *url.Error would print it.
var (
	errTelegramUnauthorized = errors.New("telegram_unauthorized")
	errTelegramChatNotFound = errors.New("telegram_chat_not_found")
	errTelegramUnreachable  = errors.New("telegram_unreachable")
	errTelegramFailed       = errors.New("telegram_failed")
)

// telegramCode is the API error code for a failed send.
func telegramCode(err error) string {
	for _, known := range []error{errTelegramUnauthorized, errTelegramChatNotFound, errTelegramUnreachable} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return errTelegramFailed.Error()
}

type telegramClient struct {
	baseURL string
	http    *http.Client
}

func newTelegramClient(baseURL string) *telegramClient {
	return &telegramClient{baseURL: baseURL, http: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var defaultTelegram = newTelegramClient(telegramAPI)

// send posts an HTML message. Callers escape every dynamic value.
func (c *telegramClient) send(ctx context.Context, token, chatID, text string) error {
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
	var reply struct {
		Description string `json:"description"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&reply)
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	// Telegram answers 404 for a token that is not shaped like a bot token.
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusNotFound:
		return errTelegramUnauthorized
	// 403: the user never started the bot, blocked it, or the bot left the group.
	case res.StatusCode == http.StatusForbidden, res.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(reply.Description), "chat not found"):
		return errTelegramChatNotFound
	}
	return errTelegramFailed
}

// telegramSend delivers through the injected sender in tests.
func (s *Store) telegramSend(ctx context.Context, token, chatID, text string) error {
	if s.sendTelegram != nil {
		return s.sendTelegram(ctx, token, chatID, text)
	}
	return defaultTelegram.send(ctx, token, chatID, text)
}
