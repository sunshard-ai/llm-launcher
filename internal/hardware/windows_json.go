package hardware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"llmlauncher/internal/domain"
)

// applyWindowsHardwareJSON is platform independent so the PowerShell payload
// contract and its conversion to inventory can be exercised without Windows.
func applyWindowsHardwareJSON(data []byte, h *domain.Hardware) error {
	data = bytes.TrimSpace(data)
	// The script explicitly writes UTF-8 without a BOM. Tolerate a BOM when an
	// outer Windows host adds one, without accepting a different text encoding.
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	var raw struct {
		OS, CPU         string
		LogicalCPUs     int
		TotalKB, FreeKB uint64
		GPUs            []string
		Disks           []struct {
			DeviceID  string
			FreeSpace uint64
		}
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.LogicalCPUs < 0 || raw.TotalKB > math.MaxUint64/1024 || raw.FreeKB > math.MaxUint64/1024 {
		return fmt.Errorf("invalid Windows hardware counters")
	}
	h.Distro = raw.OS
	h.CPUName = raw.CPU
	if raw.LogicalCPUs > 0 {
		h.LogicalCPUs = raw.LogicalCPUs
	}
	h.RAMTotalBytes = raw.TotalKB * 1024
	h.RAMAvailableBytes = raw.FreeKB * 1024
	if h.RAMAvailableBytes > h.RAMTotalBytes {
		h.RAMAvailableBytes = h.RAMTotalBytes
	}
	for _, name := range raw.GPUs {
		low := strings.ToLower(name)
		vendor := "unknown"
		switch {
		case strings.Contains(low, "nvidia"):
			vendor = "nvidia"
		case strings.Contains(low, "amd") || strings.Contains(low, "radeon"):
			vendor = "amd"
		case strings.Contains(low, "intel"):
			vendor = "intel"
		}
		h.GPUs = append(h.GPUs, domain.GPU{Name: name, Vendor: vendor})
	}
	for _, disk := range raw.Disks {
		if strings.HasPrefix(strings.ToUpper(h.DiskPath), strings.ToUpper(disk.DeviceID)+"\\") {
			h.DiskFreeBytes = disk.FreeSpace
			break
		}
	}
	return nil
}
