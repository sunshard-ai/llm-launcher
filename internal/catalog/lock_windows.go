package catalog

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func tryCatalogLock(file *os.File) (bool, error) {
	const failImmediately, exclusive, lockViolation = 1, 2, syscall.Errno(33)
	var offset syscall.Overlapped
	result, _, err := lockFileEx.Call(file.Fd(), failImmediately|exclusive, 0, 1, 0, uintptr(unsafe.Pointer(&offset)))
	if result != 0 {
		return true, nil
	}
	if errors.Is(err, lockViolation) {
		return false, nil
	}
	return false, err
}
