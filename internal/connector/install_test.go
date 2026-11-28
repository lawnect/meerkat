package connector

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lawnect/meerkat/internal/contract"
)

const testRunToken = "abcdefghijklmnopqrstuvwxyz0123456789"

func installCodeFor(t *testing.T, outlet, name, token string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"v": 1, "outlet": outlet, "name": name, "token": token})
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func clearInstallEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"MEERKAT_INSTALL_CODE", "MEERKAT_INSTALL_CODE_FILE", "MEERKAT_ALLOWED_TARGETS"} {
		t.Setenv(key, "")
	}
}

// The code used to be an argument, which any local user reads from
// /proc/<pid>/cmdline. It still works for existing deployments.
func TestConnectTakesTheInstallCodeFromTheArgument(t *testing.T) {
	clearInstallEnvironment(t)
	code := installCodeFor(t, "https://outlet.example.com", "office", testRunToken)
	cfg, token, err := Connect([]string{code})
	if err != nil || token != testRunToken || cfg.Name != "office" || cfg.OutletURL != "https://outlet.example.com" {
		t.Fatalf("argument: %#v %q %v", cfg, token, err)
	}
}

func TestConnectTakesTheInstallCodeFromTheEnvironmentAndForgetsIt(t *testing.T) {
	clearInstallEnvironment(t)
	code := installCodeFor(t, "https://outlet.example.com", "office", testRunToken)
	t.Setenv("MEERKAT_INSTALL_CODE", code)
	cfg, token, err := Connect(nil)
	if err != nil || token != testRunToken || cfg.Name != "office" {
		t.Fatalf("environment: %#v %q %v", cfg, token, err)
	}
	// What is left in the environment is what a child process would inherit.
	if value, set := os.LookupEnv("MEERKAT_INSTALL_CODE"); set || value != "" {
		t.Fatalf("install code stays in the environment: %q", value)
	}
	for _, entry := range os.Environ() {
		if strings.Contains(entry, code) {
			t.Fatalf("install code still in the environment: %s", entry[:24])
		}
	}
}

func TestConnectTakesTheInstallCodeFromAFile(t *testing.T) {
	clearInstallEnvironment(t)
	code := installCodeFor(t, "https://outlet.example.com", "office", testRunToken)
	path := filepath.Join(t.TempDir(), "install-code")
	// Secret files commonly end in a newline.
	if err := os.WriteFile(path, []byte(code+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEERKAT_INSTALL_CODE_FILE", path)
	_, token, err := Connect(nil)
	if err != nil || token != testRunToken {
		t.Fatalf("file: %q %v", token, err)
	}
	if value, set := os.LookupEnv("MEERKAT_INSTALL_CODE_FILE"); set || value != "" {
		t.Fatalf("file variable stays in the environment: %q", value)
	}
}

func TestConnectNeedsExactlyOneInstallCodeSource(t *testing.T) {
	clearInstallEnvironment(t)
	code := installCodeFor(t, "https://outlet.example.com", "office", testRunToken)
	path := filepath.Join(t.TempDir(), "install-code")
	if err := os.WriteFile(path, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Connect(nil); err == nil {
		t.Fatal("connect without a code started")
	}
	for name, setup := range map[string]func(){
		"argument and environment": func() { t.Setenv("MEERKAT_INSTALL_CODE", code) },
		"argument and file":        func() { t.Setenv("MEERKAT_INSTALL_CODE_FILE", path) },
	} {
		clearInstallEnvironment(t)
		setup()
		if _, _, err := Connect([]string{code}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	clearInstallEnvironment(t)
	t.Setenv("MEERKAT_INSTALL_CODE", code)
	t.Setenv("MEERKAT_INSTALL_CODE_FILE", path)
	if _, _, err := Connect(nil); err == nil {
		t.Fatal("environment and file accepted")
	}
	// A refused start still clears the variables.
	if os.Getenv("MEERKAT_INSTALL_CODE") != "" {
		t.Fatal("install code stays in the environment after an error")
	}
}

func TestConnectErrorsDoNotRevealTheCode(t *testing.T) {
	clearInstallEnvironment(t)
	secret := installCodeFor(t, "https://outlet.example.com", "office", testRunToken)
	t.Setenv("MEERKAT_INSTALL_CODE_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, _, err := Connect(nil); err == nil {
		t.Fatal("missing file accepted")
	}
	clearInstallEnvironment(t)
	bad := installCodeFor(t, "http://outlet.example.com", "office", testRunToken)
	_, _, err := Connect([]string{bad})
	if err == nil {
		t.Fatal("plain HTTP outlet accepted")
	}
	if strings.Contains(err.Error(), bad) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), testRunToken) {
		t.Fatalf("error repeats the code: %v", err)
	}
	if _, _, err := Connect([]string{"not-base64!"}); err == nil {
		t.Fatal("garbage accepted")
	}
}

// connect mode never sets MEERKAT_TARGET, so the allowlist has to reach it.
func TestConnectReadsTheAllowlist(t *testing.T) {
	clearInstallEnvironment(t)
	t.Setenv("MEERKAT_ALLOWED_TARGETS", "app:3000")
	cfg, _, err := Connect([]string{installCodeFor(t, "https://outlet.example.com", "office", testRunToken)})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.permits(contract.Target{Host: "app", Port: 3000}) || cfg.permits(contract.Target{Host: "ip-10-0-1-23", Port: 22}) {
		t.Fatalf("allowlist not applied in connect mode: %v", cfg.AllowedTargets)
	}
	t.Setenv("MEERKAT_ALLOWED_TARGETS", "example.com")
	if _, _, err := Connect([]string{installCodeFor(t, "https://outlet.example.com", "office", testRunToken)}); err == nil {
		t.Fatal("invalid allowlist accepted")
	}
}
