package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/outlet"
	"github.com/lawnect/meerkat/internal/rathole"
	"github.com/lawnect/meerkat/internal/securefs"
)

func TestUpgradeConfigPreservesExistingNoiseKey(t *testing.T) {
	dir := t.TempDir()
	configs := filepath.Join(dir, "configs")
	if err := securefs.Directory(configs); err != nil {
		t.Fatal(err)
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server, err := rathole.Server("0.0.0.0:2333", base64.StdEncoding.EncodeToString(private.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := securefs.Write(filepath.Join(configs, "server.toml"), server); err != nil {
		t.Fatal(err)
	}
	cfg := outlet.Config{ListenAddress: "0.0.0.0:8443", AdminTokenHash: outlet.Hash(outlet.Token()), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 600}
	if err := securefs.WriteJSON(filepath.Join(configs, "outlet.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if err := upgradeConfig(dir, "example.com"); err != nil {
		t.Fatal(err)
	}
	if err := securefs.ReadJSON(filepath.Join(configs, "outlet.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "example.com" || cfg.TunnelServerPrivateKey != base64.StdEncoding.EncodeToString(private.Bytes()) || cfg.TunnelRemoteAddress != "example.com:2333" {
		t.Fatalf("config = %#v", cfg)
	}
}

// An interrupted bootstrap leaves files but no outlet.json; running again
// finishes it with a matching admin token, and a finished one is refused.
func TestBootstrapRecoversFromAPartialRun(t *testing.T) {
	dir := t.TempDir()
	configs := filepath.Join(dir, "configs")
	if err := os.MkdirAll(configs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configs, "admin.token"), []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"outlet-init", "-dir", dir}
	flag.CommandLine = flag.NewFlagSet("outlet-init", flag.ContinueOnError)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(filepath.Join(configs, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg outlet.Config
	raw, err := os.ReadFile(filepath.Join(configs, "outlet.json"))
	if err != nil || json.Unmarshal(raw, &cfg) != nil {
		t.Fatalf("outlet.json: %v", err)
	}
	if string(token) == "stale\n" || cfg.AdminTokenHash != outlet.Hash(strings.TrimSpace(string(token))) {
		t.Fatal("admin token doesn't match outlet.json")
	}
	flag.CommandLine = flag.NewFlagSet("outlet-init", flag.ContinueOnError)
	if err := run(); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("second run: %v", err)
	}
}
