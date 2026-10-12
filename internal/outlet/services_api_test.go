package outlet

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

func TestServiceAPIStoresDesiredService(t *testing.T) {
	f := setup(t)
	db, err := control.Open(context.Background(), filepath.Join(f.dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := OpenWithControl(f.cfg, f.dir, db)
	if err != nil {
		t.Fatal(err)
	}
	h := store.Handler()
	w := request(h, "PUT", "/api/v1/outlet", f.admin, map[string]any{"domain": "example.com"})
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "example.com") {
		t.Fatalf("outlet status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors", f.admin, map[string]any{"name": "edge-a"})
	if w.Code != 201 {
		t.Fatalf("connector status = %d: %s", w.Code, w.Body.String())
	}
	var connector control.Connector
	if err := json.Unmarshal(w.Body.Bytes(), &connector); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/api/v1/connectors", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "edge-a") {
		t.Fatalf("managed connector list status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}})
	if w.Code != 201 {
		t.Fatalf("service status = %d: %s", w.Code, w.Body.String())
	}
	var service control.Service
	if err := json.Unmarshal(w.Body.Bytes(), &service); err != nil {
		t.Fatal(err)
	}
	w = request(h, "GET", "/api/v1/connectors/"+connector.ID+"/services", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "web") {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "POST", "/api/v1/services/"+service.ID+"/routes", f.admin, map[string]any{"hostname": "app.example.com"})
	if w.Code != 201 {
		t.Fatalf("route status = %d: %s", w.Code, w.Body.String())
	}
	w = request(h, "GET", "/api/v1/services/"+service.ID+"/routes", f.admin, nil)
	if w.Code != 200 || !containsJSON(w.Body.Bytes(), "app.example.com") {
		t.Fatalf("route list status = %d: %s", w.Code, w.Body.String())
	}
	if w = request(h, "POST", "/api/v1/connectors/missing/services", f.admin, map[string]any{"name": "web", "protocol": "http", "target": map[string]any{"host": "app", "port": 3000}}); w.Code != 404 {
		t.Fatalf("missing connector status = %d", w.Code)
	}
	if w = request(h, "POST", "/api/v1/connectors/"+connector.ID+"/services", f.admin, map[string]any{"name": "bad", "protocol": "http", "target": map[string]any{"host": "8.8.8.8", "port": 53}}); w.Code != 400 {
		t.Fatalf("unsafe target status = %d", w.Code)
	}
}

func containsJSON(body []byte, value string) bool {
	var decoded any
	return json.Unmarshal(body, &decoded) == nil && string(body) != "" && string(body) != value && jsonContains(decoded, value)
}

func jsonContains(value any, want string) bool {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if jsonContains(child, want) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if jsonContains(child, want) {
				return true
			}
		}
	case string:
		return v == want
	}
	return false
}
