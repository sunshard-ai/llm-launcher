package hardware

import (
	"testing"
)

func TestNVIDIACSV(t *testing.T) {
	g, err := parseNVIDIA("0, GPU-abc, NVIDIA RTX 4080 SUPER, 16376, 15250, 8.9, 575.51.03\n1, GPU-def, Tesla T4, 15360, [N/A], 7.5, 575.51.03\n", true)
	if err != nil || len(g) != 2 {
		t.Fatalf("parse: %v %#v", err, g)
	}
	if g[0].VRAMFreeBytes != 15250*1024*1024 || g[0].UUID != "GPU-abc" || g[0].ComputeCapability != "8.9" {
		t.Fatalf("bad GPU %#v", g[0])
	}
	if g[1].VRAMFreeBytes != 0 {
		t.Fatal("unknown free memory must be zero")
	}
}
func TestNVIDIALegacy(t *testing.T) {
	g, err := parseNVIDIA("0, GPU-a, NVIDIA GTX 1080, 8192, 7900, 570.1\n", false)
	if err != nil || g[0].ComputeCapability != "" {
		t.Fatalf("%v %#v", err, g)
	}
}
func TestNVIDIARejectMalformed(t *testing.T) {
	if _, err := parseNVIDIA("0, GPU-a, Missing\n", true); err == nil {
		t.Fatal("malformed output accepted")
	}
}
func TestImpossibleFreeMemory(t *testing.T) {
	g, err := parseNVIDIA("0, GPU-a, NVIDIA, 4096, 8000, 8.0, 570\n", true)
	if err != nil || g[0].VRAMFreeBytes != 0 {
		t.Fatalf("%v %#v", err, g)
	}
}
func TestLocalEndpoints(t *testing.T) {
	for _, s := range []string{"unix:///var/run/docker.sock", "npipe:////./pipe/docker_engine", "npipe:////./pipe/dockerDesktopLinuxEngine"} {
		if !LocalEndpoint(s) {
			t.Errorf("local %q", s)
		}
	}
	for _, s := range []string{"", "ssh://host", "tcp://127.0.0.1:2375", "https://host:2376", "npipe:////server/pipe/docker_engine", "npipe://server/pipe/docker_engine", "npipe:////127.0.0.1/pipe/docker_engine", "npipe:////./pipe/", "unix:///"} {
		if LocalEndpoint(s) {
			t.Errorf("unsafe %q", s)
		}
	}
}
func TestMibRejectsInvalid(t *testing.T) {
	for _, s := range []string{"[N/A]", "-1", "not a number", "999999999999", "NaN", "+Inf"} {
		if mib(s) != 0 {
			t.Errorf("%q accepted", s)
		}
	}
}
