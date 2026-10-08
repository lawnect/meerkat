package securefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretPermissionsAndExclusiveLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	path := filepath.Join(dir, "secret.json")
	if err := WriteJSON(path, map[string]string{"secret": "value"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		mode os.FileMode
	}{{dir, 0700}, {path, 0600}} {
		info, err := os.Stat(tc.path)
		if err != nil || info.Mode().Perm() != tc.mode {
			t.Fatal("incorrect secret file permissions")
		}
	}
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := Lock(dir); err == nil {
		duplicate()
		t.Fatal("concurrent owner accepted")
	}
	release()
	release, err = Lock(dir)
	if err != nil {
		t.Fatal("released lock cannot be reacquired")
	}
	release()
}
