package main

import (
	"io"
	"os"
	"path/filepath"
)

// A desktop session owns its worker copy and working directory. Commands do
// not depend on an archive extraction folder or the user's current directory.
func desktopWorker(executable string) (string, func(), error) {
	source, err := os.Open(executable)
	if err != nil {
		return "", nil, err
	}
	defer source.Close()
	dir, err := os.MkdirTemp("", "llm-launcher-session-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, "llm-launcher-worker.exe")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err == nil {
		_, err = io.Copy(file, source)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}
