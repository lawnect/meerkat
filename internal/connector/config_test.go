package connector

import (
	"encoding/base64"
	"github.com/lawnect/meerkat/internal/contract"
	"testing"
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
