package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The rathole container mounts only outlet-data/rathole. A bind mount whose
// source is missing is created by Docker, owned by root, which the Outlet
// then can't write to, so the directory must exist, owned by the Outlet's
// user, before Compose first starts.
func TestBootstrapCreatesTheDirectoryRatholeMounts(t *testing.T) {
	dir := t.TempDir()
	if err := runInit(t, "-dir", dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "outlet-data", "rathole")
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("%s: %v, %v", path, info, err)
	}
	if entries, err := os.ReadDir(path); err != nil || len(entries) != 0 {
		t.Fatalf("%s should start empty (the Outlet writes server.toml): %v, %v", path, entries, err)
	}
}
