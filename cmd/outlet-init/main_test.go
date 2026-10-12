package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
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
	if err := upgradeConfig(dir, "saeoo.com"); err != nil {
		t.Fatal(err)
	}
	if err := securefs.ReadJSON(filepath.Join(configs, "outlet.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Domain != "saeoo.com" || cfg.TunnelServerPrivateKey != base64.StdEncoding.EncodeToString(private.Bytes()) || cfg.TunnelRemoteAddress != "saeoo.com:2333" {
		t.Fatalf("config = %#v", cfg)
	}
}
