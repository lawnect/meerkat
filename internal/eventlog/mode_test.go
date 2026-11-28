package eventlog

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

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

// Logs record client addresses, paths and request details, so they and their
// write-ahead logs are for the Outlet's user alone whatever umask it runs with.
func TestLogFilesArePrivateUnderUmask022(t *testing.T) {
	underUmask(t, 0o022)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "logs", "events.db")
	log, err := Open(ctx, path, testKind, Limits{MaxBytes: 64 << 20, Retention: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if err := log.Append(ctx, []testEntry{{At: time.Now(), Path: "/a", State: "passed"}}); err != nil {
		t.Fatal(err)
	}
	requirePrivate(t, path, path+"-wal", path+"-shm")
}

// A log from before the fix is at 0644; opening it again tightens its files,
// including leftovers of a crash.
func TestOpenTightensLogFilesOfAnExistingDeployment(t *testing.T) {
	underUmask(t, 0o022)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "events.db")
	limits := Limits{MaxBytes: 64 << 20, Retention: 24 * time.Hour}
	log, err := Open(ctx, path, testKind, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		data, _ := os.ReadFile(name)
		if err := os.WriteFile(name, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(name, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log, err = Open(ctx, path, testKind, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if err := log.Append(ctx, []testEntry{{At: time.Now(), Path: "/a", State: "passed"}}); err != nil {
		t.Fatal(err)
	}
	requirePrivate(t, path, path+"-wal", path+"-shm")
}
