//go:build linux

package hardware

import (
	"path/filepath"
	"testing"
)

func TestMeminfo(t *testing.T) {
	total, free := parseMeminfo("MemTotal: 16777216 kB\nMemFree: 100 kB\nMemAvailable: 12582912 kB\n")
	if total != 16*GiB || free != 12*GiB {
		t.Fatalf("%d %d", total, free)
	}
}
func TestUnknownMemAvailable(t *testing.T) {
	_, free := parseMeminfo("MemTotal: 10000 kB\nMemFree: 9000 kB\n")
	if free != 0 {
		t.Fatal("MemFree is not MemAvailable")
	}
}
func TestPCI(t *testing.T) {
	g := parsePCI("00:00.0 \"Host bridge\" \"Intel\"\n01:00.0 \"VGA compatible controller\" \"Advanced Micro Devices, Inc. [AMD/ATI]\" \"Navi 31\"\n02:00.0 \"3D controller\" \"NVIDIA Corporation\" \"H200\"\n")
	if len(g) != 2 || g[0].Vendor != "amd" || g[1].Vendor != "nvidia" {
		t.Fatalf("%#v", g)
	}
	for _, x := range g {
		if x.VRAMTotalBytes != 0 {
			t.Fatal("PCI cannot infer VRAM")
		}
	}
}
func TestMissingDiskPath(t *testing.T) {
	dir := t.TempDir()
	if got := existingAncestor(filepath.Join(dir, "missing", "nested")); got != dir {
		t.Fatalf("%q != %q", got, dir)
	}
}
