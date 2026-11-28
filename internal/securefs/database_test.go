package securefs

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func underUmask(t *testing.T, mask int) {
	t.Helper()
	old := syscall.Umask(mask)
	t.Cleanup(func() { syscall.Umask(old) })
}

func TestRestrictUmaskMakesNewFilesPrivate(t *testing.T) {
	underUmask(t, 0o022)
	RestrictUmask()
	dir := t.TempDir()
	file, directory := filepath.Join(dir, "file"), filepath.Join(dir, "dir")
	if err := os.WriteFile(file, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{file: 0o600, directory: 0o700} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s mode = %v, %v; want %o", path, info.Mode().Perm(), err, want)
		}
	}
}

func TestDatabaseCreatesAndTightensItsFiles(t *testing.T) {
	underUmask(t, 0o022)
	path := filepath.Join(t.TempDir(), "state.db")
	if err := Database(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 || info.Size() != 0 {
		t.Fatalf("new database: %v, %v", info, err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.WriteFile(path+suffix, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path+suffix, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Database(path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		info, err := os.Stat(path + suffix)
		if err != nil || info.Mode().Perm() != 0o600 || info.Size() != 1 {
			t.Errorf("%s: %v, %v", path+suffix, info, err)
		}
	}
}

func TestDatabaseDoesNotFollowSymbolicLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "state.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Database(link); err == nil {
		t.Fatal("a symbolic link was accepted as a database")
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("the link's target was changed: %v, %v", info, err)
	}
}
