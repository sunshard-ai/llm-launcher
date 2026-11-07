package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/process"
)

//go:embed desktop.ps1
var desktopScript []byte

//go:embed desktop_strings.json
var desktopStrings []byte

func runDesktop(ctx context.Context) {
	go catalog.Refresh(ctx)
	exe, err := os.Executable()
	if err == nil {
		var cleanup func()
		exe, cleanup, err = desktopWorker(exe)
		if err == nil {
			defer cleanup()
		}
	}
	if err == nil {
		systemRoot := os.Getenv("SystemRoot")
		powershell := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		const bootstrap = `[Console]::InputEncoding = [Text.Encoding]::UTF8; [Console]::OutputEncoding = [Text.Encoding]::UTF8; try { & ([scriptblock]::Create([Console]::In.ReadToEnd())) } catch { [Console]::Error.WriteLine($_.ToString()); exit 1 }`
		cmd := process.Command(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", bootstrap)
		cmd.Dir = filepath.Dir(exe)
		cmd.Env = append(os.Environ(), "LLM_LAUNCHER_DESKTOP_EXE="+exe)
		// The translation table is bundled with the executable and sent as a
		// literal UTF-8 here-string before the script.
		cmd.Stdin = io.MultiReader(
			strings.NewReader("$desktopStringsJson = @'\n"),
			bytes.NewReader(desktopStrings),
			strings.NewReader("\n'@\n"),
			bytes.NewReader(desktopScript),
		)
		var output []byte
		output, err = cmd.CombinedOutput()
		if err != nil && len(output) > 0 {
			err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(output))
		}
	}
	if err != nil {
		title, _ := syscall.UTF16PtrFromString("LLM Launcher")
		message, _ := syscall.UTF16PtrFromString("Could not open the desktop launcher. Check that Windows PowerShell is available.\n\n" + err.Error())
		messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
		_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
	}
}
