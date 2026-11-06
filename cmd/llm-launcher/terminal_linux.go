package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

func terminalScan(in io.Reader, out io.Writer) bool {
	return isTTY(in) && isTTY(out)
}

func isTTY(stream any) bool {
	f, ok := stream.(*os.File)
	if !ok {
		return false
	}
	var termios syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return err == 0
}
