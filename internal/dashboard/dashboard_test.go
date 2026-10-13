package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONHelperPreservesDefaultContentTypeWithRequestHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "...options,headers:{'Content-Type':'application/json'") {
		t.Fatal("dashboard JSON helper lets per-request headers replace Content-Type")
	}
}
