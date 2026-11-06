//go:build linux

package hardware

import (
	"context"
	"llmlauncher/internal/domain"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func probePlatform(ctx context.Context, h *domain.Hardware) {
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				h.Distro = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\"")
			}
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.WSL = strings.Contains(strings.ToLower(string(b)), "microsoft")
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			p := strings.SplitN(line, ":", 2)
			if len(p) == 2 && (strings.TrimSpace(p[0]) == "model name" || strings.TrimSpace(p[0]) == "Hardware") {
				h.CPUName = strings.TrimSpace(p[1])
				break
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		h.RAMTotalBytes, h.RAMAvailableBytes = parseMeminfo(string(b))
	}
	// Respect the current cgroup if this diagnostic itself is containerized.
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		if limit, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64); err == nil && limit < h.RAMTotalBytes {
			h.RAMTotalBytes = limit
			if h.RAMAvailableBytes > limit {
				h.RAMAvailableBytes = limit
			}
			if used, err := os.ReadFile("/sys/fs/cgroup/memory.current"); err == nil {
				n, _ := strconv.ParseUint(strings.TrimSpace(string(used)), 10, 64)
				if n < limit {
					h.RAMAvailableBytes = limit - n
				} else {
					h.RAMAvailableBytes = 0
				}
			}
			h.Warnings = append(h.Warnings, "Бюджет оперативной памяти ограничен текущей Linux cgroup.")
		}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(existingAncestor(h.DiskPath), &stat); err == nil {
		h.DiskFreeBytes = stat.Bavail * uint64(stat.Bsize)
	}
	if out, err := command(ctx, "lspci", "-mm"); err == nil {
		h.GPUs = parsePCI(string(out))
	}
	if len(h.GPUs) == 0 {
		h.GPUs = probeDRM()
	}
}
func parseMeminfo(s string) (uint64, uint64) {
	var total, available uint64
	for _, line := range strings.Split(s, "\n") {
		p := strings.Fields(line)
		if len(p) < 2 {
			continue
		}
		n, err := strconv.ParseUint(p[1], 10, 64)
		if err != nil {
			continue
		}
		switch p[0] {
		case "MemTotal:":
			total = n * 1024
		case "MemAvailable:":
			available = n * 1024
		}
	}
	return total, available
}
func parsePCI(s string) []domain.GPU {
	var result []domain.GPU
	for _, line := range strings.Split(s, "\n") {
		low := strings.ToLower(line)
		if !strings.Contains(low, "vga compatible") && !strings.Contains(low, "3d controller") && !strings.Contains(low, "display controller") {
			continue
		}
		vendor := "unknown"
		switch {
		case strings.Contains(low, "nvidia"):
			vendor = "nvidia"
		case strings.Contains(low, "amd") || strings.Contains(low, "advanced micro"):
			vendor = "amd"
		case strings.Contains(low, "intel"):
			vendor = "intel"
		}
		result = append(result, domain.GPU{Vendor: vendor, Name: line})
	}
	return result
}
func existingAncestor(path string) string {
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func probeDRM() []domain.GPU {
	var result []domain.GPU
	entries, _ := filepath.Glob("/sys/class/drm/card[0-9]*/device")
	seen := map[string]bool{}
	for _, entry := range entries {
		resolved, err := filepath.EvalSymlinks(entry)
		if err != nil || seen[resolved] {
			continue
		}
		seen[resolved] = true
		b, err := os.ReadFile(filepath.Join(entry, "vendor"))
		if err != nil {
			continue
		}
		vendor := "unknown"
		switch strings.TrimSpace(string(b)) {
		case "0x10de":
			vendor = "nvidia"
		case "0x1002":
			vendor = "amd"
		case "0x8086":
			vendor = "intel"
		}
		g := domain.GPU{Vendor: vendor, Name: strings.ToUpper(vendor) + " GPU (PCI " + filepath.Base(resolved) + ")"}
		if b, err := os.ReadFile(filepath.Join(entry, "mem_info_vram_total")); err == nil {
			g.VRAMTotalBytes, _ = strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		}
		if b, err := os.ReadFile(filepath.Join(entry, "mem_info_vram_used")); err == nil {
			used, e := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
			if e == nil && used < g.VRAMTotalBytes {
				g.VRAMFreeBytes = g.VRAMTotalBytes - used
			}
		}
		result = append(result, g)
	}
	return result
}
