//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package storage

import (
	"errors"
	"os"
)

func lockFile(f *os.File) error {
	return errors.New("filesystem quota locking is unsupported on this platform")
}
func unlockFile(f *os.File) {}
