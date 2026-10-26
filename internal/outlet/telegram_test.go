package outlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"bot blocked", 403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, errTelegramChatNotFound},
		{"other bad request", 400, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`, errTelegramFailed},
		{"server error", 500, `oops`, errTelegramFailed},
		{"rate limited", 429, `{"ok":false,"error_code":429}`, errTelegramFailed},
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
