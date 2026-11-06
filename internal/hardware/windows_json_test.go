package hardware

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
)

func TestWindowsHardwareJSON(t *testing.T) {
	for _, tc := range []struct {
		name, gpus string
		count      int
		bom        bool
	}{
		{"no GPU", `[]`, 0, false},
		{"one GPU", `["NVIDIA GeForce RTX 4080 SUPER"]`, 1, false},
		{"one GPU with BOM", `["NVIDIA GeForce RTX 4080 SUPER"]`, 1, true},
		{"two GPUs", `["NVIDIA GeForce RTX 4080 SUPER","Intel UHD Graphics"]`, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(fmt.Sprintf(`{"OS":"Windows 11 Профессиональная","CPU":"AMD Ryzen 9 7950X","LogicalCPUs":32,"TotalKB":33554432,"FreeKB":25165824,"GPUs":%s,"Disks":[{"DeviceID":"C:","FreeSpace":10737418240},{"DeviceID":"D:","FreeSpace":214748364800}]}`, tc.gpus))
			if tc.bom {
				payload = append([]byte{0xef, 0xbb, 0xbf}, payload...)
			}
			h := domain.Hardware{OS: "windows", Arch: "amd64", DiskPath: `d:\Models\new`, LogicalCPUs: 1}
			if err := applyWindowsHardwareJSON(payload, &h); err != nil {
				t.Fatal(err)
			}
			if h.LogicalCPUs != 32 || h.RAMTotalBytes != 32*GiB || h.RAMAvailableBytes != 24*GiB {
				t.Fatalf("incorrect counters: %#v", h)
			}
			if h.Distro != "Windows 11 Профессиональная" || h.CPUName != "AMD Ryzen 9 7950X" {
				t.Fatalf("lost Unicode/text: %#v", h)
			}
			if h.OS != "windows" || h.Arch != "amd64" || h.DiskFreeBytes != 200*GiB {
				t.Fatalf("incorrect host/disk selection: %#v", h)
			}
			if len(h.GPUs) != tc.count {
				t.Fatalf("got %d GPUs, want %d", len(h.GPUs), tc.count)
			}
			if len(h.GPUs) > 0 && (h.GPUs[0].Vendor != "nvidia" || h.GPUs[0].VRAMTotalBytes != 0) {
				t.Fatalf("incorrect unverified GPU: %#v", h.GPUs[0])
			}
			if len(h.GPUs) > 1 && h.GPUs[1].Vendor != "intel" {
				t.Fatalf("incorrect second GPU: %#v", h.GPUs[1])
			}
		})
	}
}

func TestWindowsHardwareJSONRejectsInvalidCounters(t *testing.T) {
	base := `{"OS":"Windows","CPU":"CPU","LogicalCPUs":16,"TotalKB":16777216,"FreeKB":8388608,"GPUs":[],"Disks":[]}`
	for _, payload := range []string{
		strings.Replace(base, `"LogicalCPUs":16`, `"LogicalCPUs":16.5`, 1),
		strings.Replace(base, `"TotalKB":16777216`, `"TotalKB":18446744073709551615`, 1),
		strings.Replace(base, `"LogicalCPUs":16`, `"LogicalCPUs":-1`, 1),
		`not JSON`,
	} {
		h := domain.Hardware{CPUName: "unchanged", LogicalCPUs: 4}
		if err := applyWindowsHardwareJSON([]byte(payload), &h); err == nil {
			t.Fatalf("accepted %q", payload)
		}
		if h.CPUName != "unchanged" || h.LogicalCPUs != 4 {
			t.Fatal("invalid JSON partially changed inventory")
		}
	}
	// Verify the integer payload emitted after the PowerShell cast is accepted.
	h := domain.Hardware{}
	if err := applyWindowsHardwareJSON(bytes.NewBufferString(base).Bytes(), &h); err != nil || h.LogicalCPUs != 16 {
		t.Fatalf("integer JSON rejected: %#v %v", h, err)
	}
}
