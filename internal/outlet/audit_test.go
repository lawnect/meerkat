package outlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/control"
)

type auditPage struct {
	Events []control.AuditEvent `json:"events"`
	Total  int                  `json:"total"`
}

// auditLog returns every entry, oldest first, once queued entries are written.
func (a alertFixture) auditLog(t *testing.T) []control.AuditEvent {
	t.Helper()
	a.store.journal.flush()
	events, _, err := a.db.Audit(context.Background(), control.AuditFilter{Limit: control.MaxAuditLimit})
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(events)
	return events
}

func actions(events []control.AuditEvent) []string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.Action
	}
	return names
}

func (a alertFixture) lastAudit(t *testing.T) control.AuditEvent {
	t.Helper()
	log := a.auditLog(t)
	if len(log) == 0 {
		t.Fatal("audit log is empty")
	}
	return log[len(log)-1]
}

func TestAuditRecordsEveryAdminAction(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	call := func(method, path string, body any, want int) []byte {
		t.Helper()
		r := jsonRequest(method, path, body)
		r.AddCookie(cookie)
		r.Header.Set("User-Agent", "audit-test/1.0")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}

	var connector control.Connector
	_ = json.Unmarshal(call("POST", "/api/v1/connectors", map[string]any{"name": "edge-a", "description": "secret note"}, 201), &connector)
	call("PATCH", "/api/v1/connectors/"+connector.ID, map[string]any{"description": "another secret note"}, 200)
	var service control.Service
	_ = json.Unmarshal(call("POST", "/api/v1/connectors/"+connector.ID+"/services", map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}}, 201), &service)
	var issued struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(call("POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens", nil, 201), &issued)
	call("POST", "/api/v1/connectors/"+connector.ID+"/enrollment-tokens?reset=true", nil, 201)
	call("PATCH", "/api/v1/connectors/"+connector.ID+"/services/"+service.ID, map[string]any{"target": map[string]any{"host": "app", "port": 3001}}, 200)
	var route control.Route
	_ = json.Unmarshal(call("POST", "/api/v1/services/"+service.ID+"/routes", map[string]any{"hostname": "app.example.com"}, 201), &route)
	call("DELETE", "/api/v1/services/"+service.ID+"/routes/"+route.ID, nil, 204)
	call("DELETE", "/api/v1/connectors/"+connector.ID+"/services/"+service.ID, nil, 204)
	call("DELETE", "/api/v1/connectors/"+connector.ID, nil, 204)

	call("PUT", "/api/v1/outlet", map[string]any{"domain": "example.com", "mode": "hosts"}, 200)
	call("POST", "/api/v1/outlet/domains", map[string]any{"domain": "example.net"}, 201)
	call("PUT", "/api/v1/outlet/domains/example.net/mode", map[string]any{"mode": "hosts"}, 200)
	call("DELETE", "/api/v1/outlet/domains/example.net", nil, 200)
	call("PUT", "/api/v1/outlet/tunnel", map[string]any{"address": "tunnel.example.com:2333"}, 200)

	call("PUT", "/api/v1/alerts/telegram", map[string]any{"bot_token": testBotToken, "chat_id": "123456", "enabled": true}, 200)
	a.telegram.fail(errTelegramChatNotFound)
	call("POST", "/api/v1/alerts/telegram/test", nil, 502)
	a.telegram.fail(nil)
	call("POST", "/api/v1/alerts/telegram/test", nil, 200)
	call("PUT", "/api/v1/alerts/rules", map[string]any{
		"tunnel_down": map[string]any{"enabled": true, "after_minutes": 10}, "service_down": map[string]any{"enabled": true, "after_minutes": 5}, "route_error": map[string]any{"enabled": true}, "dns_failure": map[string]any{"enabled": false},
		"error_spike": map[string]any{"enabled": true, "threshold_percent": 5, "min_requests": 50}, "login": map[string]any{"enabled": true},
		"rate_limit": map[string]any{"enabled": true, "min_blocked": 500},
	}, 200)
	call("PUT", "/api/v1/security/second-factor", map[string]any{"enabled": true}, 200)
	call("DELETE", "/api/v1/alerts/telegram", nil, 200)
	call("DELETE", "/api/v1/session", nil, 204)
	a.store.background.Wait()

	log := a.auditLog(t)
	want := []string{
		"setup.admin_create", "session.login",
		"connector.create", "connector.describe", "service.create", "connector.token_issue", "connector.token_reset", "service.update", "route.create",
		"route.delete", "service.delete", "connector.delete",
		"domain.set", "domain.add", "domain.mode", "domain.remove", "tunnel.update",
		"telegram.update", "telegram.test", "telegram.test", "alert_rules.update", "second_factor.update", "telegram.delete", "session.logout",
	}
	if got := actions(log); !slices.Equal(got, want) {
		t.Fatalf("actions:\n got %v\nwant %v", got, want)
	}
	detail := func(i int) string { return string(log[i].Detail) }
	for i, expect := range map[int]string{
		2:  `{"name":"edge-a"}`,
		3:  "",
		4:  `{"connector_id":"` + connector.ID + `","name":"web","protocol":"http","target":{"host":"app","port":3000}}`,
		5:  `{"name":"edge-a"}`,
		6:  `{"name":"edge-a"}`,
		7:  `{"connector_id":"` + connector.ID + `","target":{"host":"app","port":3001}}`,
		8:  `{"hostname":"app.example.com","service_id":"` + service.ID + `"}`,
		9:  `{"hostname":"app.example.com","service_id":"` + service.ID + `"}`,
		11: `{"name":"edge-a"}`,
		12: `{"mode":"hosts"}`,
		14: `{"mode":"hosts"}`,
		16: `{"address":"tunnel.example.com:2333"}`,
		17: `{"chat_id":"123456","enabled":true,"token_changed":true}`,
		18: `{"error":"telegram_chat_not_found","ok":false}`,
		19: `{"ok":true}`,
		21: `{"enabled":true}`,
	} {
		if expect == "" && log[i].Detail != nil || expect != "" && detail(i) != expect {
			t.Errorf("%s detail = %s, want %s", log[i].Action, detail(i), expect)
		}
	}
	for i, resource := range map[int][2]string{2: {"connector", connector.ID}, 4: {"service", service.ID}, 8: {"route", route.ID}, 12: {"domain", "example.com"}, 16: {"outlet", "tunnel"}, 17: {"telegram", "settings"}, 20: {"alert_rules", "rules"}, 21: {"security", "second_factor"}} {
		if log[i].ResourceType != resource[0] || log[i].ResourceID != resource[1] {
			t.Errorf("%s resource = %s/%s", log[i].Action, log[i].ResourceType, log[i].ResourceID)
		}
	}
	for _, e := range log[2:] {
		if e.ActorEmail == nil || *e.ActorEmail != "admin@example.com" || e.Source != "local" || e.IP != nil || e.UserAgent == nil || *e.UserAgent != "audit-test/1.0" || e.CreatedAt != "2026-10-26T14:00:00.000Z" {
			t.Errorf("%s: actor/source = %+v", e.Action, e)
		}
	}

	everything, _ := json.Marshal(log)
	for _, secret := range []string{adminLogin["password"].(string), "AAExample", issued.Token, "secret note", a.admin} {
		if strings.Contains(string(everything), secret) {
			t.Fatalf("audit log contains a secret: %q", secret)
		}
	}
}

func TestAuditBeforeSetupHasNoActor(t *testing.T) {
	a := newAlertFixture(t)
	if w := request(a.store.Handler(), "POST", "/api/v1/connectors", a.admin, map[string]any{"name": "edge-a"}); w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}
	if e := a.lastAudit(t); e.Action != "connector.create" || e.ActorEmail != nil || e.Source != "local" {
		t.Fatalf("event = %+v", e)
	}
}

func TestAuditLoginAttemptsAndAddresses(t *testing.T) {
	a, _ := secondFactorFixture(t)
	before := len(a.auditLog(t))

	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	if w := a.publicRequest("POST", "/api/v1/session", "198.51.100.9, "+clientIP, chromeOnMac, wrong); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	failed := a.lastAudit(t)
	if failed.Action != "session.login_failed" || failed.ActorEmail == nil || *failed.ActorEmail != "admin@example.com" || failed.Source != "public" || failed.IP == nil || *failed.IP != clientIP ||
		failed.UserAgent == nil || *failed.UserAgent != chromeOnMac || string(failed.Detail) != `{"reason":"invalid_credentials"}` {
		t.Fatalf("failed login = %+v", failed)
	}
	var actorID *string
	if err := a.db.SQL().QueryRow("SELECT actor_id FROM audit_events WHERE action = 'session.login_failed'").Scan(&actorID); err != nil || actorID != nil {
		t.Fatalf("a failed login has an actor id: %v %v", actorID, err)
	}
	unknown := map[string]any{"email": "intruder@example.com", "password": "wrong password entirely"}
	a.publicRequest("POST", "/api/v1/session", clientIP, chromeOnMac, unknown)
	if e := a.lastAudit(t); e.ActorEmail == nil || *e.ActorEmail != "intruder@example.com" {
		t.Fatalf("attempted email not recorded: %+v", e)
	}

	challenge, secret := a.startChallenge(t)
	wrongCode := "000000"
	if secret == wrongCode {
		wrongCode = "000001"
	}
	a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": wrongCode})
	if e := a.lastAudit(t); e.Action != "session.second_factor_failed" || string(e.Detail) != `{"reason":"wrong_code"}` || e.ActorEmail == nil || *e.ActorEmail != "admin@example.com" {
		t.Fatalf("second factor failure = %+v", e)
	}
	// An unknown challenge is a stranger's guess and stays out of the log.
	recorded := len(a.auditLog(t))
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": "unknown", "code": secret}); w.Code != 401 {
		t.Fatalf("unknown challenge: %d", w.Code)
	}
	if len(a.auditLog(t)) != recorded {
		t.Fatal("an unknown challenge was recorded")
	}
	if w := a.publicRequest("POST", "/api/v1/session/second-factor", clientIP, chromeOnMac, map[string]any{"challenge": challenge, "code": secret}); w.Code != 201 {
		t.Fatalf("second factor: %d %s", w.Code, w.Body.String())
	}
	log := a.auditLog(t)[before:]
	if got := actions(log); !slices.Equal(got, []string{"session.login_failed", "session.login_failed", "session.second_factor_failed", "session.second_factor", "session.login"}) {
		t.Fatalf("actions = %v", got)
	}
	if e := log[4]; string(e.Detail) != `{"method":"password","second_factor":true}` || e.IP == nil || *e.IP != clientIP || e.Source != "public" {
		t.Fatalf("login = %+v", e)
	}

	// The local port is "local" with no address, whatever headers claim.
	r := jsonRequest("POST", "/api/v1/session", adminLogin)
	r.Header.Set("X-Forwarded-For", clientIP)
	w := httptest.NewRecorder()
	a.store.Handler().ServeHTTP(w, r)
	if e := a.lastAudit(t); w.Code != 201 || e.Action != "session.login" || e.Source != "local" || e.IP != nil || string(e.Detail) != `{"method":"password","second_factor":false}` {
		t.Fatalf("local login = %+v", e)
	}
	a.store.background.Wait()

	everything, _ := json.Marshal(a.auditLog(t))
	for _, secret := range []string{adminLogin["password"].(string), "wrong password entirely", "AAExample", challenge} {
		if strings.Contains(string(everything), secret) {
			t.Fatalf("audit log contains %q", secret)
		}
	}
}

func TestAuditOutageNeverHoldsUpRequests(t *testing.T) {
	a := newAlertFixture(t)
	cookie := a.adminSession(t)
	h := a.store.Handler()
	before := len(a.auditLog(t))
	// A full disk or a broken table: every audit insert now fails.
	if _, err := a.db.SQL().Exec("CREATE TRIGGER broken_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT, 'disk full'); END"); err != nil {
		t.Fatal(err)
	}

	if w := cookieRequest(h, "POST", "/api/v1/connectors", cookie, map[string]any{"name": "edge-a"}); w.Code != 201 {
		t.Fatalf("mutation: %d %s", w.Code, w.Body.String())
	}
	if w := request(h, "POST", "/api/v1/session", "", adminLogin); w.Code != 201 || len(w.Result().Cookies()) == 0 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	wrong := map[string]any{"email": "admin@example.com", "password": "wrong password entirely"}
	if w := request(h, "POST", "/api/v1/session", "", wrong); w.Code != 401 {
		t.Fatalf("wrong password: %d", w.Code)
	}
	a.store.background.Wait()
	a.store.journal.flush()

	// The entries waited; once the log takes writes again they arrive in order.
	if _, err := a.db.SQL().Exec("DROP TRIGGER broken_audit"); err != nil {
		t.Fatal(err)
	}
	if got := actions(a.auditLog(t)[before:]); !slices.Equal(got, []string{"connector.create", "session.login", "session.login_failed"}) {
		t.Fatalf("actions after recovery = %v", got)
	}
}

func connectorID(t *testing.T, a alertFixture, name string) string {
	t.Helper()
	connectors, err := a.db.ListConnectors(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range connectors {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("no connector %s", name)
	return ""
}

func TestAuditAPIPaginationFilterAndShape(t *testing.T) {
	a := newAlertFixture(t)
	ctx, h := context.Background(), a.store.Handler()
	for i := 0; i < 60; i++ {
		email, action := "alice@example.com", "connector.create"
		if i%3 == 0 {
			email, action = "bob@example.com", "domain.mode"
		}
		entry := control.AuditEntry{ActorEmail: email, Action: action, ResourceType: "connector", ResourceID: fmt.Sprintf("m_res%02d", i), Source: "public", IP: "203.0.113.7", UserAgent: "agent", Detail: map[string]any{"name": fmt.Sprintf("edge-%02d", i)}}
		if err := a.db.AppendAudit(ctx, entry, a.clock.now().Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query string) (int, auditPage, []byte) {
		w := request(h, "GET", "/api/v1/audit"+query, a.admin, nil)
		var page auditPage
		_ = json.Unmarshal(w.Body.Bytes(), &page)
		return w.Code, page, w.Body.Bytes()
	}

	code, page, raw := get("")
	if code != 200 || page.Total != 60 || len(page.Events) != 50 || page.Events[0].ResourceID != "m_res59" || page.Events[49].ResourceID != "m_res10" {
		t.Fatalf("default page: %d total %d len %d", code, page.Total, len(page.Events))
	}
	var shape struct {
		Events []map[string]any `json:"events"`
		Total  int              `json:"total"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, 10)
	for key := range shape.Events[0] {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if want := []string{"action", "actor_email", "created_at", "detail", "id", "ip", "resource_id", "resource_type", "source", "user_agent"}; !slices.Equal(keys, want) {
		t.Fatalf("event keys = %v", keys)
	}
	if _, page, _ := get("?limit=10&offset=55"); len(page.Events) != 5 || page.Total != 60 || page.Events[4].ResourceID != "m_res00" {
		t.Fatalf("offset page: %+v", page)
	}
	if _, page, _ := get("?limit=200"); len(page.Events) != 60 {
		t.Fatalf("limit 200 returned %d", len(page.Events))
	}
	if _, page, _ := get("?action=domain."); page.Total != 20 || len(page.Events) != 20 {
		t.Fatalf("action filter: total %d", page.Total)
	}
	if _, page, _ := get("?q=BOB"); page.Total != 20 {
		t.Fatalf("email filter: total %d", page.Total)
	}
	if _, page, _ := get("?q=m_res07"); page.Total != 1 || page.Events[0].ResourceID != "m_res07" {
		t.Fatalf("resource filter: %+v", page)
	}
	if _, page, _ := get("?q=edge-4&action=connector.&limit=3"); page.Total != 7 || len(page.Events) != 3 {
		t.Fatalf("combined filter: total %d len %d", page.Total, len(page.Events))
	}
	for _, query := range []string{"?limit=0", "?limit=201", "?limit=abc", "?offset=-1", "?offset=x", "?q=" + strings.Repeat("a", 129)} {
		if code, _, _ := get(query); code != 400 {
			t.Errorf("%s: status %d", query, code)
		}
	}
	if w := request(h, "GET", "/api/v1/audit", "wrong", nil); w.Code != 401 {
		t.Fatalf("unauthenticated: %d", w.Code)
	}

	// There is no way to change or clear the log over HTTP.
	for _, method := range []string{"DELETE", "PUT", "POST", "PATCH"} {
		for _, path := range []string{"/api/v1/audit", "/api/v1/audit/" + page.Events[0].ID} {
			if w := request(h, method, path, a.admin, map[string]any{}); w.Code < 400 {
				t.Errorf("%s %s: status %d", method, path, w.Code)
			}
		}
	}
	if _, page, _ := get(""); page.Total != 60 {
		t.Fatalf("log changed: total %d", page.Total)
	}
}
