package outlet

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Each start rewrites the file rathole's container watches: rathole, in the
// Outlet's network namespace, learns from it that the Outlet came back in a
// new one, even when server.toml didn't change.
func TestOpenMarksTheStartForRathole(t *testing.T) {
	f := setup(t)
	f.cfg.TunnelListenAddress, f.cfg.TunnelServerPrivateKey = "0.0.0.0:2333", base64.StdEncoding.EncodeToString(make([]byte, 32))
	marker := filepath.Join(f.dir, "rathole", outletStarted)
	start := func() time.Time {
		t.Helper()
		store, err := Open(f.cfg, f.dir)
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
		info, err := os.Stat(marker)
		if err != nil {
			t.Fatalf("the start isn't marked: %v", err)
		}
		return info.ModTime()
	}
	first := start()
	time.Sleep(20 * time.Millisecond)
	if again := start(); !again.After(first) {
		t.Fatalf("a second start left the mark at %v", first)
	}
}
