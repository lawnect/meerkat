package securefs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// RestrictUmask makes every file and directory the process creates from here
// on private to its user, as a backstop for any path that doesn't set its own
// mode. Call it first thing in main, before anything is created.
func RestrictUmask() { syscall.Umask(0o077) }

// databaseSuffixes are the files SQLite keeps beside a database.
var databaseSuffixes = []string{"", "-wal", "-shm", "-journal"}

// Database makes the SQLite database at path private (0600), together with
// the write-ahead log and index beside it, before SQLite opens it.
//
// SQLite creates a new database 0644 less the umask, and gives its -wal, -shm
// and -journal files the mode the database has. So the database is created
// here, empty, with the mode it must keep, which every file SQLite adds then
// copies whatever the umask is. A database a release before this one left at
// 0644, and any leftovers of a crash, are tightened too. Call DatabaseFiles
// again once the database is open to cover files created since.
func Database(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	err = f.Chmod(0o600)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return DatabaseFiles(path)
}

// DatabaseFiles sets the mode of whichever of path and its -wal, -shm and
// -journal files exist to 0600. It refuses to follow a symbolic link.
func DatabaseFiles(path string) error {
	var errs []error
	for _, suffix := range databaseSuffixes {
		f, err := os.OpenFile(path+suffix, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := f.Chmod(0o600); err != nil {
			errs = append(errs, &os.PathError{Op: "chmod", Path: filepath.Clean(path + suffix), Err: err})
		}
		f.Close()
	}
	return errors.Join(errs...)
}
