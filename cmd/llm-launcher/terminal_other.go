//go:build !linux

package main

import "io"

func terminalScan(io.Reader, io.Writer) bool { return false }
