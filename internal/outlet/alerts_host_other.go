//go:build !linux

package outlet

import (
	"errors"
	"syscall"
)

// readDisk reads the filesystem holding path, with the space a process
// without privileges can use as free, as df does.
func readDisk(path string) (freeSpace, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return freeSpace{}, err
	}
	if st.Blocks == 0 {
		return freeSpace{}, errors.New("filesystem reports no size")
	}
	unit := uint64(st.Bsize)
	return freeSpace{uint64(st.Bavail) * unit, uint64(st.Blocks) * unit}, nil
}

// Memory is read only on Linux, where the Outlet runs; elsewhere (a
// development machine) memory alerts never fire.
func readMemory() (memoryReading, error) {
	return memoryReading{}, errors.ErrUnsupported
}
