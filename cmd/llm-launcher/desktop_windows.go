package main

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"llmlauncher/internal/process"
)

//go:embed desktop.ps1
var desktopScript []byte

func runDesktop(ctx context.Context) {
	exe, err := os.Executable()
	if err == nil {
		systemRoot := os.Getenv("SystemRoot")
		powershell := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		const bootstrap = `[Console]::InputEncoding = [Text.Encoding]::UTF8; [Console]::OutputEncoding = [Text.Encoding]::UTF8; try { & ([scriptblock]::Create([Console]::In.ReadToEnd())) } catch { [Console]::Error.WriteLine($_.ToString()); exit 1 }`
		cmd := process.Command(ctx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", bootstrap)
		cmd.Env = append(os.Environ(), "LLM_LAUNCHER_DESKTOP_EXE="+exe)
		cmd.Stdin = bytes.NewReader(desktopScript)
		var output []byte
		output, err = cmd.CombinedOutput()
		if err != nil && len(output) > 0 {
			err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(output))
		}
	}
	if err != nil {
		title, _ := syscall.UTF16PtrFromString("LLM Launcher")
		message, _ := syscall.UTF16PtrFromString("Не удалось открыть мастер. Проверьте доступ к Windows PowerShell.\n\n" + err.Error())
		messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
		_, _, _ = messageBox.Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
	}
}
