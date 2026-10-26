package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAuditLogIsAppendOnly(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	if err := db.AppendAudit(ctx, AuditEntry{Action: "connector.create", ResourceType: "connector", ResourceID: "m_a", Source: "local", Detail: map[string]any{"name": "edge-a"}}, now); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := db.SQL().QueryRowContext(ctx, "SELECT id FROM audit_events").Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE audit_events SET action = 'connector.delete'",
		"UPDATE audit_events SET detail = NULL WHERE id = '" + id + "'",
		"UPDATE OR REPLACE audit_events SET action = 'x'",
		"DELETE FROM audit_events",
		"DELETE FROM audit_events WHERE id = '" + id + "'",
		"INSERT OR REPLACE INTO audit_events(id, action, resource_type, resource_id, created_at) VALUES ('" + id + "', 'forged', 'connector', 'm_a', 'now')",
		"REPLACE INTO audit_events(id, action, resource_type, resource_id, created_at) VALUES ('" + id + "', 'forged', 'connector', 'm_a', 'now')",
	} {
		_, err := db.SQL().ExecContext(ctx, statement)
		if err == nil || !strings.Contains(err.Error(), "audit log is append-only") {
			t.Errorf("%s: err = %v", statement, err)
		}
	}
	events, total, err := db.Audit(ctx, AuditFilter{Limit: 10})
	if err != nil || total != 1 || events[0].Action != "connector.create" || string(events[0].Detail) != `{"name":"edge-a"}` {
		t.Fatalf("log changed: %+v %d %v", events, total, err)
	}
	// Appending still works afterwards, and nothing prunes the log.
	for i := 0; i < 500; i++ {
		if err := db.AppendAudit(ctx, AuditEntry{Action: "domain.mode", ResourceType: "domain", ResourceID: "example.com", Source: "public"}, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if _, total, _ := db.Audit(ctx, AuditFilter{Limit: 1}); total != 501 {
		t.Fatalf("total = %d", total)
	}
}

func TestAuditRejectsMalformedEntries(t *testing.T) {
	ctx, db, now := context.Background(), alertDB(t), time.Now()
	for name, entry := range map[string]AuditEntry{
		"no action":     {ResourceType: "connector", Source: "local"},
		"no resource":   {Action: "connector.create", Source: "local"},
		"unknown place": {Action: "connector.create", ResourceType: "connector", Source: "elsewhere"},
		"no source":     {Action: "connector.create", ResourceType: "connector"},
	} {
		if err := db.AppendAudit(ctx, entry, now); !errors.Is(err, ErrInvalidAudit) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := db.AppendAudit(ctx, AuditEntry{ActorID: "m_missing", Action: "connector.create", ResourceType: "connector", Source: "local"}, now); err == nil {
		t.Error("an actor that does not exist was accepted")
	}
}

func TestAuditPaginationAndFilters(t *testing.T) {
	ctx, db, start := context.Background(), alertDB(t), time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)
	add := func(i int, entry AuditEntry) {
		t.Helper()
		entry.Source = "local"
		if err := db.AppendAudit(ctx, entry, start.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 30; i++ {
		add(i, AuditEntry{ActorEmail: "admin@example.com", Action: "connector.create", ResourceType: "connector", ResourceID: fmt.Sprintf("m_conn%02d", i), Detail: map[string]any{"name": fmt.Sprintf("edge-%02d", i)}})
	}
	add(30, AuditEntry{ActorEmail: "alice@example.com", Action: "domain.mode", ResourceType: "domain", ResourceID: "example.com", Detail: map[string]any{"mode": "hosts"}})
	add(31, AuditEntry{ActorEmail: "admin@example.com", Action: "domain.add", ResourceType: "domain", ResourceID: "100%_done.example"})
	add(32, AuditEntry{Action: "telegram.update", ResourceType: "telegram", ResourceID: "settings", IP: "203.0.113.7", UserAgent: "curl/8"})

	page, total, err := db.Audit(ctx, AuditFilter{Limit: 10})
	if err != nil || total != 33 || len(page) != 10 || page[0].Action != "telegram.update" || page[1].ResourceID != "100%_done.example" || page[2].Action != "domain.mode" {
		t.Fatalf("first page: %d %d %v", len(page), total, err)
	}
	if page[0].CreatedAt != "2026-10-26T14:32:00.000Z" || page[0].ActorEmail != nil || page[0].IP == nil || *page[0].IP != "203.0.113.7" || page[0].UserAgent == nil || page[0].Detail != nil || page[0].Source != "local" {
		t.Fatalf("newest event = %+v", page[0])
	}
	if page[2].ActorEmail == nil || *page[2].ActorEmail != "alice@example.com" || page[2].IP != nil {
		t.Fatalf("alice event = %+v", page[2])
	}
	last, total, _ := db.Audit(ctx, AuditFilter{Limit: 10, Offset: 30})
	if total != 33 || len(last) != 3 || last[2].ResourceID != "m_conn00" {
		t.Fatalf("last page = %+v", last)
	}
	if empty, total, _ := db.Audit(ctx, AuditFilter{Limit: 10, Offset: 100}); total != 33 || len(empty) != 0 {
		t.Fatalf("past the end = %d, %d", len(empty), total)
	}

	for name, tc := range map[string]struct {
		filter AuditFilter
		total  int
	}{
		"action prefix":      {AuditFilter{Action: "connector."}, 30},
		"exact action":       {AuditFilter{Action: "domain.mode"}, 1},
		"prefix of two":      {AuditFilter{Action: "domain."}, 2},
		"email":              {AuditFilter{Query: "alice"}, 1},
		"email case":         {AuditFilter{Query: "ALICE@"}, 1},
		"resource id":        {AuditFilter{Query: "m_conn07"}, 1},
		"detail":             {AuditFilter{Query: "edge-1"}, 10},
		"detail value":       {AuditFilter{Query: "hosts"}, 1},
		"action and query":   {AuditFilter{Action: "connector.", Query: "admin@"}, 30},
		"percent is literal": {AuditFilter{Query: "100%_"}, 1},
		"underscore literal": {AuditFilter{Query: "m_conn0_"}, 0},
		"no match":           {AuditFilter{Query: "nobody"}, 0},
		"wildcard action":    {AuditFilter{Action: "%"}, 0},
	} {
		tc.filter.Limit = 5
		events, total, err := db.Audit(ctx, tc.filter)
		if err != nil || total != tc.total || len(events) != min(5, tc.total) {
			t.Errorf("%s: %d events, total %d, %v (want total %d)", name, len(events), total, err, tc.total)
		}
	}
	events, _, _ := db.Audit(ctx, AuditFilter{Limit: 200})
	raw, _ := json.Marshal(events[0])
	if !strings.Contains(string(raw), `"detail":null`) || !strings.Contains(string(raw), `"actor_email":null`) {
		t.Fatalf("absent values should be null: %s", raw)
	}
}
