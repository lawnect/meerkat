package outlet

import (
	"errors"
	"os"
	"syscall"
)

// readDisk reads the filesystem holding path, with the space a process
// without privileges can use as free, as df does.
func readDisk(path string) (freeSpace, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return freeSpace{}, err
	}
	// Block counts are in fragments (Frsize), which Bsize can differ from.
	unit := uint64(st.Frsize)
	if unit == 0 {
		unit = uint64(st.Bsize)
	}
	if st.Blocks == 0 {
		return freeSpace{}, errors.New("filesystem reports no size")
	}
	return freeSpace{uint64(st.Bavail) * unit, uint64(st.Blocks) * unit}, nil
}

func readMemory() (memoryReading, error) {
	return memoryFrom(func(path string) (string, bool) {
		b, err := os.ReadFile(path)
		return string(b), err == nil
	})
}
