//go:build linux || darwin

package site

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// O_NONBLOCK prevents a FIFO substituted after WalkDir from blocking open.
// O_NOFOLLOW rejects a substituted final-component symlink; fstat validates the
// actual descriptor, not a pathname that could change between check and open.
func openRuntimeRegularFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("language input is not a regular file")
	}
	return f, nil
}
