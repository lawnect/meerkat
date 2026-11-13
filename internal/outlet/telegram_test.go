package outlet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testBotToken = "123456789:AAExampleExampleExampleExample_ab-cd"

func telegramServer(t *testing.T, status int, body string) (*httptest.Server, *map[string]any) {
	t.Helper()
	received := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bot"+testBotToken+"/sendMessage" && r.Method == http.MethodPost && r.Header.Get("Content-Type") == "application/json" {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &received)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, &received
}

func TestTelegramClientSendsHTMLMessage(t *testing.T) {
	server, received := telegramServer(t, 200, `{"ok":true}`)
	client := newTelegramClient(server.URL)
	if err := client.send(context.Background(), testBotToken, "-100123", "<b>hi</b>"); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"chat_id": "-100123", "text": "<b>hi</b>", "parse_mode": "HTML", "disable_web_page_preview": true}
	for key, value := range want {
		if (*received)[key] != value {
			t.Errorf("%s = %v, want %v", key, (*received)[key], value)
		}
	}
}

func TestTelegramClientMapsErrorsWithoutLeakingTheToken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"unauthorized", 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, errTelegramUnauthorized},
		{"malformed token", 404, `{"ok":false,"error_code":404,"description":"Not Found"}`, errTelegramUnauthorized},
		{"chat not found", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, errTelegramChatNotFound},
		{"bot blocked", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, errTelegramChatForbidden},
		{"other bad request", 400, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`, errTelegramInvalidRequest},
		{"server error", 500, `oops`, errTelegramUnavailable},
		{"rate limited", 429, `{"ok":false,"error_code":429}`, errTelegramRateLimited},
	} {
		server, _ := telegramServer(t, tc.status, tc.body)
		err := newTelegramClient(server.URL).send(context.Background(), testBotToken, "1", "x")
		if !errors.Is(err, tc.want) || telegramCode(err) != tc.want.Error() {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), "AAExample") {
			t.Errorf("%s: error leaks the token: %v", tc.name, err)
		}
	}
}

func TestTelegramClientTimeoutAndNetworkErrors(t *testing.T) {
	// The server only notices a closed connection after reading the body.
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer stalled.Close()
	defer close(release)
	client := newTelegramClient(stalled.URL)
	client.http.Timeout = 50 * time.Millisecond
	err := client.send(context.Background(), testBotToken, "1", "x")
	if !errors.Is(err, errTelegramUnreachable) || strings.Contains(err.Error(), "AAExample") || strings.Contains(err.Error(), stalled.URL) {
		t.Fatalf("timeout err = %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	err = newTelegramClient(closed.URL).send(context.Background(), testBotToken, "1", "x")
	if !errors.Is(err, errTelegramUnreachable) || strings.Contains(err.Error(), "AAExample") {
		t.Fatalf("connection err = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newTelegramClient(stalled.URL).send(cancelled, testBotToken, "1", "x"); !errors.Is(err, errTelegramUnreachable) {
		t.Fatalf("cancelled err = %v", err)
	}
}

func TestTelegramClientDoesNotFollowRedirects(t *testing.T) {
	var followed bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { followed = true }))
	defer target.Close()
	redirect := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusTemporaryRedirect))
	defer redirect.Close()
	if err := newTelegramClient(redirect.URL).send(context.Background(), testBotToken, "1", "x"); !errors.Is(err, errTelegramFailed) || followed {
		t.Fatalf("redirect err = %v, followed = %v", err, followed)
	}
}

func TestTelegramClientValidatesSuccessEnvelope(t *testing.T) {
	for _, body := range []string{
		`{"ok":false}`, `{"ok":false,"error_code":400}`, `{"result":{}}`,
		`{"ok":null}`, `{"ok":"true"}`, `{"ok":true}garbage`, `{"ok":true}{"ok":true}`,
		`{"ok":true`, `null`, `[]`, `<html>bad gateway</html>`, "",
		`{"ok":true,"result":"` + strings.Repeat("x", telegramResponseLimit) + `"}`,
	} {
		t.Run(fmt.Sprintf("%.30s", body), func(t *testing.T) {
			server, _ := telegramServer(t, 200, body)
			if err := newTelegramClient(server.URL).send(context.Background(), testBotToken, "1", "x"); err == nil {
				t.Fatal("invalid success was accepted")
			}
		})
	}
}

func TestTelegramClientSafeResponseDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
		code   int
	}{
		{"bot recipient", 400, `{"ok":false,"error_code":400,"description":"Bad Request: bots can't send messages to bots"}`, errTelegramChatNotFound, 400},
		{"status takes precedence", 503, `{"ok":false,"error_code":401}`, errTelegramUnavailable, 401},
		{"empty chat", 400, `{"ok":false,"error_code":400,"description":"Bad Request: chat_id is empty"}`, errTelegramChatNotFound, 400},
		{"invalid peer", 400, `{"ok":false,"error_code":400,"description":"Bad Request: PEER_ID_INVALID"}`, errTelegramChatNotFound, 400},
		{"no rights", 400, `{"ok":false,"error_code":400,"description":"Bad Request: not enough rights to send text messages to the chat"}`, errTelegramChatForbidden, 400},
		{"not member", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot is not a member of the channel chat"}`, errTelegramChatForbidden, 403},
		{"2xx rejected token", 200, `{"ok":false,"error_code":401}`, errTelegramUnauthorized, 401},
		{"migrated", 400, `{"ok":false,"error_code":400,"parameters":{"migrate_to_chat_id":-100999}}`, errTelegramChatMigrated, 400},
		{"entities", 400, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`, errTelegramInvalidMessage, 400},
		{"tag", 400, `{"ok":false,"error_code":400,"description":"Bad Request: Unsupported start tag"}`, errTelegramInvalidMessage, 400},
		{"request", 400, `{"ok":false,"error_code":400,"description":"anything"}`, errTelegramInvalidRequest, 400},
		{"unavailable", 503, `{"ok":false,"error_code":503}`, errTelegramUnavailable, 503},
		{"missing ok", 200, `{}`, errTelegramInvalidResponse, 0},
		{"malformed", 200, `oops`, errTelegramInvalidResponse, 0},
		{"malformed 400", 400, `oops`, errTelegramInvalidResponse, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := telegramServer(t, tc.status, tc.body)
			err := newTelegramClient(server.URL).send(context.Background(), testBotToken, "1", "x")
			var response *telegramResponseError
			if !errors.Is(err, tc.want) || !errors.As(err, &response) || response.Status != tc.status || response.ErrorCode != tc.code {
				t.Fatalf("error = %#v, want %s status %d code %d", err, tc.want, tc.status, tc.code)
			}
		})
	}
}

func TestTelegramClientRateLimitCooldown(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if hits.Add(1) == 1 {
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":30}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	client := newTelegramClient(server.URL)
	clock := &testClock{t: time.Now()}
	client.now = clock.now
	check := func(ctx context.Context, want int) {
		t.Helper()
		err := client.send(ctx, testBotToken, "other chat", "different message")
		var response *telegramResponseError
		if !errors.Is(err, errTelegramRateLimited) || !errors.As(err, &response) || response.RetryAfter != want {
			t.Fatalf("cooldown = %#v, want %ds", err, want)
		}
	}
	check(context.Background(), 30)
	clock.advance(1100 * time.Millisecond)
	check(context.Background(), 29)
	// A cancelled request during cooldown still reports the shared bot limit,
	// without starting transport work or disclosing a URL-bearing context error.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	check(cancelled, 29)
	if hits.Load() != 1 {
		t.Fatal("cooldown sent another HTTP request")
	}
	if err := client.send(context.Background(), testBotToken+"rotated", "1", "x"); err != nil {
		t.Fatal(err)
	}
	clock.advance(29 * time.Second)
	if err := client.send(context.Background(), testBotToken, "1", "x"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("HTTP calls = %d", hits.Load())
	}
}

func TestTelegramRetryAfterBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"30", 30}, {"0", 1}, {"-1", 1}, {"1.5", 1}, {`"30"`, 1},
		{"null", 1}, {"", 1}, {"86401", maxTelegramRetryAfter},
		{"999999999999999999999999999999", maxTelegramRetryAfter},
	} {
		t.Run(tc.value, func(t *testing.T) {
			parameters := ""
			if tc.value != "" {
				parameters = `,"parameters":{"retry_after":` + tc.value + `}`
			}
			server, _ := telegramServer(t, 429, `{"ok":false,"error_code":429`+parameters+`}`)
			err := newTelegramClient(server.URL).send(context.Background(), testBotToken, "1", "x")
			var response *telegramResponseError
			if !errors.As(err, &response) || response.RetryAfter != tc.want {
				t.Fatalf("error = %#v", err)
			}
		})
	}
}

func TestTelegramCooldownConcurrentExtensionAndCapacity(t *testing.T) {
	client := newTelegramClient("unused")
	clock := &testClock{t: time.Now()}
	client.now = clock.now
	key := sha256.Sum256([]byte(testBotToken))
	var workers sync.WaitGroup
	for seconds := 1; seconds <= 100; seconds++ {
		workers.Go(func() { client.cooldown(key, seconds) })
	}
	workers.Wait()
	if got := client.cooldown(key, 0); got != 100 {
		t.Fatalf("concurrent cooldown = %d", got)
	}
	clock.advance(time.Second)
	if got := client.cooldown(key, 1); got != 99 {
		t.Fatalf("shortened cooldown = %d", got)
	}
	for i := 0; i < maxTelegramCooldowns+10; i++ {
		client.cooldown(sha256.Sum256([]byte(strconv.Itoa(i))), 5)
	}
	if len(client.cooldowns) != maxTelegramCooldowns {
		t.Fatalf("map size = %d", len(client.cooldowns))
	}
	clock.advance(101 * time.Second)
	client.cooldown(key, 0)
	if len(client.cooldowns) != 0 {
		t.Fatal("expired cooldowns retained")
	}
}

func TestTelegramSensitiveResponseNeverEscapes(t *testing.T) {
	secret := "OTP-987654-chat--100999-" + testBotToken
	body := `{"ok":false,"error_code":400,"description":"can't parse entities ` + secret + `","parameters":{"migrate_to_chat_id":-100999}}`
	server, _ := telegramServer(t, 400, body)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	err := newTelegramClient(server.URL).send(context.Background(), testBotToken, "-100999", "987654")
	diagnostics, _ := json.Marshal(telegramDiagnostics(err))
	for _, got := range []string{err.Error(), fmt.Sprintf("%+v", err), string(diagnostics), output.String()} {
		for _, sensitive := range []string{testBotToken, "987654", "-100999", server.URL, "can't parse entities"} {
			if strings.Contains(got, sensitive) {
				t.Fatalf("sensitive value %q escaped in %s", sensitive, got)
			}
		}
	}
	if !strings.Contains(output.String(), `"http_status":400`) {
		t.Fatal("safe numeric diagnostic not logged")
	}
}

func TestTelegramMalformedRateLimitUsesShortFallback(t *testing.T) {
	server, _ := telegramServer(t, 429, `{"ok":"bad","parameters":{"retry_after":86400}}`)
	client := newTelegramClient(server.URL)
	err := client.send(context.Background(), testBotToken, "1", "x")
	var response *telegramResponseError
	if !errors.As(err, &response) || response.RetryAfter != 1 || response.ErrorCode != 0 {
		t.Fatalf("malformed rate limit: %#v", err)
	}
	if wait := client.cooldown(sha256.Sum256([]byte(testBotToken)), 0); wait != 1 {
		t.Fatalf("cooldown = %d", wait)
	}
}
