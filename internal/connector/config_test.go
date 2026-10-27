package connector

import (
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"github.com/lawnect/meerkat/internal/contract"
	"github.com/lawnect/meerkat/internal/securefs"
)

func TestOutletCannotOverrideLocalTarget(t *testing.T) {
	cfg := Config{Target: contract.Target{Host: "app", Port: 3000}}
	c := contract.Configuration{ConnectorID: "connector_1", ServiceID: "service_1", Revision: 1, Target: cfg.Target, Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service_1", ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"}}
	if err := allowed(cfg, c); err != nil {
		t.Fatal(err)
	}
	c.Target = contract.Target{Host: "postgres", Port: 5432}
	if allowed(cfg, c) == nil {
		t.Fatal("outlet changed local allowlisted target")
	}
}

func TestTokenEnvironmentIsRemoved(t *testing.T) {
	t.Setenv("MEERKAT_ENROLLMENT_TOKEN", "test-token")
	t.Setenv("MEERKAT_ENROLLMENT_TOKEN_FILE", "")
	token, err := EnrollmentToken()
	if err != nil || token != "test-token" {
		t.Fatal("token load failed")
	}
	token, err = EnrollmentToken()
	if err != nil || token != "" {
		t.Fatal("registration token reused from environment")
	}
}

func TestLoadFillsOutletAndNameFromSavedSession(t *testing.T) {
	dir := t.TempDir()
	for _, key := range []string{"MEERKAT_OUTLET", "MEERKAT_NAME", "MEERKAT_TARGET", "MEERKAT_ROOT_CA", "MEERKAT_DEVELOPMENT_HTTP"} {
		t.Setenv(key, "")
	}
	t.Setenv("MEERKAT_DATA_DIR", dir)
	// Written the way Run saves it, with the enrollment next to the outlet and name.
	saved := session{"https://outlet.example.com", "office", contract.EnrollmentResponse{
		Configuration: contract.Configuration{ConnectorID: "connector-test", Revision: 1, Services: []contract.ServiceConfiguration{{
			ServiceID: "service-test", Target: contract.Target{Host: "app", Port: 3000},
			Tunnel: contract.Tunnel{RemoteAddress: "outlet:2333", ServerPublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), ServiceName: "service-test", ServiceToken: "abcdefghijklmnopqrstuvwxyz0123456789"},
		}}},
		Credential: "abcdefghijklmnopqrstuvwxyz0123456789", CredentialExpiresAt: time.Now().Add(time.Hour),
	}}
	if err := securefs.WriteJSON(filepath.Join(dir, "session.json"), saved); err != nil {
		t.Fatal(err)
	}
	c, err := Load("")
	if err != nil {
		t.Fatalf("restart without a target or invitation: %v", err)
	}
	if c.OutletURL != saved.OutletURL || c.Name != saved.Name {
		t.Fatalf("saved outlet and name not applied: %q %q", c.OutletURL, c.Name)
	}
	// Values given now still win over the saved ones.
	t.Setenv("MEERKAT_NAME", "warehouse")
	if c, err = Load(""); err != nil || c.Name != "warehouse" || c.OutletURL != saved.OutletURL {
		t.Fatalf("explicit name: %q %q %v", c.OutletURL, c.Name, err)
	}
}

func TestLoadWithoutSessionNeedsTarget(t *testing.T) {
	t.Setenv("MEERKAT_DATA_DIR", t.TempDir())
	t.Setenv("MEERKAT_OUTLET", "https://outlet.example.com")
	t.Setenv("MEERKAT_NAME", "office")
	t.Setenv("MEERKAT_TARGET", "")
	if _, err := Load(""); err == nil {
		t.Fatal("first start without a target or a saved session loaded")
	}
}
