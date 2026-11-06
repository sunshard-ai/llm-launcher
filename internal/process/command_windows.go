package process

import (
	"context"
	"os/exec"
	"syscall"
)

func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	// Launcher subprocesses use pipes. Keep the Windows desktop wizard free
	// of extra consoles when probes and Docker commands run.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}
