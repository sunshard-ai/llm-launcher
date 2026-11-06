//go:build darwin

package hardware

import (
	"context"
	"encoding/json"
	"llmlauncher/internal/domain"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func probePlatform(ctx context.Context, h *domain.Hardware) {
	h.Distro = "macOS"
	if b, err := command(ctx, "sw_vers", "-productVersion"); err == nil {
		h.Distro += " " + strings.TrimSpace(string(b))
	}
	if b, err := command(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
		h.CPUName = strings.TrimSpace(string(b))
	}
	if b, err := command(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
		h.RAMTotalBytes, _ = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	}
	if b, err := command(ctx, "vm_stat"); err == nil {
		pageSize := uint64(4096)
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "page size of ") {
				p := strings.SplitN(line, "page size of ", 2)
				f := strings.Fields(p[1])
				if len(f) > 0 {
					if n, e := strconv.ParseUint(f[0], 10, 64); e == nil {
						pageSize = n
					}
				}
			}
			p := strings.SplitN(line, ":", 2)
			if len(p) == 2 && (p[0] == "Pages free" || p[0] == "Pages inactive") {
				n, _ := strconv.ParseUint(strings.Trim(strings.TrimSpace(p[1]), "."), 10, 64)
				h.RAMAvailableBytes += n * pageSize
			}
		}
	}
	path := h.DiskPath
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		p := filepath.Dir(path)
		if p == path {
			break
		}
		path = p
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err == nil {
		h.DiskFreeBytes = stat.Bavail * uint64(stat.Bsize)
	}
	if b, err := command(ctx, "system_profiler", "SPDisplaysDataType", "-json"); err == nil {
		var raw struct {
			Displays []struct {
				Name string `json:"sppci_model"`
			} `json:"SPDisplaysDataType"`
		}
		if json.Unmarshal(b, &raw) == nil {
			for _, g := range raw.Displays {
				h.GPUs = append(h.GPUs, domain.GPU{Vendor: "apple-or-display", Name: g.Name})
			}
		}
	}
	h.Warnings = append(h.Warnings, "В этой версии запуск через Docker на macOS использует CPU; ускорение Apple Metal запланировано для отдельного нативного адаптера Ollama.")
}
