package control

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// control.db holds the Telegram bot token, OIDC client secrets, signing keys
// and password hashes, so it and its write-ahead log are for the Outlet's
// user alone whatever umask the process runs with.
func requirePrivate(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", path, info.Mode().Perm())
		}
	}
}

func underUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestDatabaseFilesArePrivateUnderUmask022(t *testing.T) {
	underUmask(t, 0o022)
	path := filepath.Join(t.TempDir(), "state", "control.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Open has migrated, so the write-ahead log and its index exist now.
	requirePrivate(t, path, path+"-wal", path+"-shm")
}

// A deployment from before the fix has the files at 0644; opening it again
// tightens them, including leftovers of a crash.
func TestOpenTightensDatabaseFilesOfAnExistingDeployment(t *testing.T) {
	underUmask(t, 0o022)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(name, readOrEmpty(name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	requirePrivate(t, path, path+"-wal", path+"-shm")
}

func readOrEmpty(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}
