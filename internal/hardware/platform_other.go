//go:build !linux && !windows && !darwin

package hardware

import (
	"context"
	"llmlauncher/internal/domain"
)

func probePlatform(_ context.Context, h *domain.Hardware) {
	h.Warnings = append(h.Warnings, "Эта операционная система пока не поддерживается.")
}
