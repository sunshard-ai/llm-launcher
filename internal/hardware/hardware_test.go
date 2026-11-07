package hardware

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
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

func TestDockerReadinessDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name string
		d    domain.DockerInfo
		want string
	}{
		{"not installed", domain.DockerInfo{}, "CLI"},
		{"stopped with stale mode", domain.DockerInfo{Installed: true, ServerOS: "windows"}, "Запустите Docker"},
		{"unknown server", domain.DockerInfo{Installed: true, Reachable: true}, "Не удалось определить ОС"},
		{"Windows containers", domain.DockerInfo{Installed: true, Reachable: true, ServerOS: "windows"}, "Linux containers"},
		{"missing Compose", domain.DockerInfo{Installed: true, Reachable: true, ServerOS: "linux"}, "Compose"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := RequireLocalDocker(test.d)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("incorrect diagnostic: %v", err)
			}
		})
	}
}

func TestClientOnlyDockerInfoIsNotAReachableServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process fixture; readiness cases are platform independent")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$1 $2\" in\n'context show') echo default;;\n'context inspect') echo '\"unix:///var/run/docker.sock\"';;\n'compose version') echo 2.38.2;;\n'info --format') echo '{\"ClientInfo\":{}}';;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	d := ProbeDocker(context.Background())
	if !d.Installed || d.Reachable || !strings.Contains(d.Error, "Не удалось определить ОС") || strings.Contains(d.Error, "Linux containers") {
		t.Fatalf("client-only response was treated as a server: %+v", d)
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
