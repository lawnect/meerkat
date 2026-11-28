package outlet

import (
	"encoding/json"
	"testing"
)

// secondFactorEvents are the second_factor.update entries, oldest first.
func (a alertFixture) secondFactorEvents(t *testing.T) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, event := range a.auditLog(t) {
		if event.Action != "second_factor.update" {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(event.Detail, &detail); err != nil {
			t.Fatal(err)
		}
		events = append(events, detail)
	}
	return events
}

// Changing the bot or the chat, or disconnecting Telegram, turns the login
// code off as a side effect; the log says so under the same action as the
// switch itself, with the reason.
func TestTelegramChangesThatTurnTheSecondFactorOffAreAudited(t *testing.T) {
	a, cookie := secondFactorFixture(t)
	h := a.store.Handler()
	if got := a.secondFactorEvents(t); len(got) != 1 || got[0]["enabled"] != true || got[0]["reason"] != nil {
		t.Fatalf("turning it on: %v", got)
	}
	// The same chat and bot: the second factor stays, and nothing is added.
	if w := cookieRequest(h, "PUT", "/api/v1/alerts/telegram", cookie, map[string]any{"chat_id": "123456", "enabled": false}); w.Code != 200 {
		t.Fatalf("same chat: %d %s", w.Code, w.Body.String())
	}
	if got := a.secondFactorEvents(t); len(got) != 1 {
		t.Fatalf("an unchanged bot: %v", got)
	}
	// Another chat: unverified, so the code is off.
	if w := cookieRequest(h, "PUT", "/api/v1/alerts/telegram", cookie, map[string]any{"chat_id": "654321", "enabled": true}); w.Code != 200 || decodeAlerts(t, w).LoginSecondFactor {
		t.Fatalf("another chat: %d %s", w.Code, w.Body.String())
	}
	got := a.secondFactorEvents(t)
	if len(got) != 2 || got[1]["enabled"] != false || got[1]["reason"] != "telegram_changed" {
		t.Fatalf("a changed chat: %v", got)
	}
	// Turn it on again, then disconnect Telegram.
	if w := cookieRequest(h, "POST", "/api/v1/alerts/telegram/test", cookie, nil); w.Code != 200 {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if w := cookieRequest(h, "PUT", "/api/v1/security/second-factor", cookie, map[string]any{"enabled": true}); w.Code != 200 {
		t.Fatalf("turn on: %d %s", w.Code, w.Body.String())
	}
	if w := cookieRequest(h, "DELETE", "/api/v1/alerts/telegram", cookie, nil); w.Code != 200 {
		t.Fatalf("disconnect: %d %s", w.Code, w.Body.String())
	}
	got = a.secondFactorEvents(t)
	if len(got) != 4 || got[3]["enabled"] != false || got[3]["reason"] != "telegram_deleted" {
		t.Fatalf("a disconnected bot: %v", got)
	}
	// With the code already off, disconnecting turns nothing off.
	if w := cookieRequest(h, "DELETE", "/api/v1/alerts/telegram", cookie, nil); w.Code != 200 {
		t.Fatalf("disconnect again: %d %s", w.Code, w.Body.String())
	}
	if got := a.secondFactorEvents(t); len(got) != 4 {
		t.Fatalf("nothing was on: %v", got)
	}
}
