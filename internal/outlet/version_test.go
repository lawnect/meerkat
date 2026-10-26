package outlet

import (
	"encoding/json"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
)

func TestOutletVersionIsExposed(t *testing.T) {
	a := newAlertFixture(t)
	h := a.store.Handler()
	// The login page shows the version before anyone has logged in.
	w := request(h, "GET", "/api/v1/setup/status", "", nil)
	var status map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || w.Code != 200 || status["version"] != contract.Version || status["setup_complete"] != false || len(status) != 2 {
		t.Fatalf("setup status: %d %s", w.Code, w.Body.String())
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/v1/outlet", nil},
		{"PUT", "/api/v1/outlet", map[string]any{"domain": "example.com"}},
		{"PUT", "/api/v1/outlet/tunnel", map[string]any{"address": "tunnel.example.com:2333"}},
		{"POST", "/api/v1/outlet/domains", map[string]any{"domain": "example.net"}},
	} {
		w := request(h, call.method, call.path, a.admin, call.body)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code >= 300 || body["version"] != contract.Version {
			t.Errorf("%s %s: %d %s", call.method, call.path, w.Code, w.Body.String())
		}
	}
}
