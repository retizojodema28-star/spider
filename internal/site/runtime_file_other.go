//go:build !linux && !darwin

package site

import (
	"fmt"
	"os"
)

// Fail closed on platforms without the nonblocking/no-follow implementation.
func openRuntimeRegularFile(path string) (*os.File, error) {
	return nil, fmt.Errorf("safe language file loading is supported on Linux and Darwin")
}
