//go:build !windows

package process

import (
	"context"
	"os/exec"
)

func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
