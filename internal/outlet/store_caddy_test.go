package outlet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/control"
)

func TestOutletStartsWithEmptyCaddyfile(t *testing.T) {
	dir := t.TempDir()
	db, err := control.Open(context.Background(), filepath.Join(dir, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config := Config{ListenAddress: "127.0.0.1:8443", AdminTokenHash: Hash(Token()), EnrollmentTTLSeconds: 600, CredentialTTLSeconds: 600}
	if _, err := OpenWithControl(config, dir, db); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join(dir, "caddy", "Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), ":9090 {") || strings.Contains(string(contents), "issuer acme") {
		t.Fatalf("invalid pre-domain Caddyfile: %q", contents)
	}
}
