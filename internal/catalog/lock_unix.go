//go:build darwin || linux || dragonfly || freebsd || netbsd || openbsd || illumos

package catalog

import (
	"errors"
	"os"
	"syscall"
)

// Closing the file releases the lock, including when the launcher exits.
func tryCatalogLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return err == nil, err
}
