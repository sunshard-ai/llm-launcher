package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopWorkerSurvivesOriginalMove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.exe")
	if err := os.WriteFile(path, []byte("worker fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	worker, cleanup, err := desktopWorker(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(worker)
	if err != nil || string(got) != "worker fixture" {
		t.Fatalf("session lost its command executable: %q, %v", got, err)
	}
	cleanup()
	if _, err := os.Stat(worker); !os.IsNotExist(err) {
		t.Fatal("session worker was not cleaned up")
	}
	if _, err := os.Stat(path + ".moved"); err != nil {
		t.Fatal("cleanup touched the user's executable")
	}
}
