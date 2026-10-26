package dashboard

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerServesEmbeddedSvelteEntryAndRestrictiveCSP(t *testing.T) {
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Meerkat Outlet") {
		t.Fatal("dashboard entry page is missing the application title")
	}
	if strings.Contains(recorder.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("dashboard CSP allows inline scripts or styles")
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(recorder.Body.String(), -1)
	if len(assets) == 0 {
		if !strings.Contains(recorder.Body.String(), "npm run build") {
			t.Fatal("dashboard without built assets does not explain how to generate them")
		}
	} else if len(assets) < 2 {
		t.Fatalf("dashboard entry references %d built assets, want JavaScript and CSS: %s", len(assets), recorder.Body.String())
	}
	for _, asset := range assets {
		assetResponse := httptest.NewRecorder()
		Handler().ServeHTTP(assetResponse, httptest.NewRequest("GET", asset[1], nil))
		if assetResponse.Code != 200 || assetResponse.Body.Len() == 0 {
			t.Fatalf("asset %q returned status %d and %d bytes", asset[1], assetResponse.Code, assetResponse.Body.Len())
		}
	}

	asset := httptest.NewRecorder()
	Handler().ServeHTTP(asset, httptest.NewRequest("GET", "/assets/missing.js", nil))
	if asset.Code != 404 {
		t.Fatalf("missing dashboard asset status = %d, want 404", asset.Code)
	}

	other := httptest.NewRecorder()
	Handler().ServeHTTP(other, httptest.NewRequest("GET", "/not-a-dashboard-route", nil))
	if other.Code != 404 {
		t.Fatalf("non-asset dashboard path status = %d, want 404", other.Code)
	}
}

func TestHandlerServesKoreanPagesUnderPrefix(t *testing.T) {
	for _, path := range []string{"/ko", "/ko/", "/ko/connectors", "/ko/connectors/abc", "/ko/domains", "/ko/domains/example.com", "/domains/example.com", "/ko/diagnostics"} {
		recorder := httptest.NewRecorder()
		Handler().ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), "Meerkat Outlet") {
			t.Fatalf("%s status = %d, want the dashboard entry page", path, recorder.Code)
		}
	}
	for _, path := range []string{"/korea", "/ko/assets/app.js", "/ko/ko", "/ko/connectors/a/b", "/domains/a/b"} {
		recorder := httptest.NewRecorder()
		Handler().ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 404 {
			t.Fatalf("%s status = %d, want 404", path, recorder.Code)
		}
	}
}
