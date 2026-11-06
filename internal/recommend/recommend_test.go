package recommend

import (
	"llmlauncher/internal/catalog"
	"llmlauncher/internal/domain"
	"testing"
)

const G = uint64(1024 * 1024 * 1024)

func machine() domain.Hardware {
	return domain.Hardware{OS: "linux", Arch: "amd64", RAMTotalBytes: 32 * G, RAMAvailableBytes: 27 * G, DiskFreeBytes: 200 * G, LogicalCPUs: 16, Docker: domain.DockerInfo{Installed: true, Reachable: true, Compose: true, ServerOS: "linux", MemoryBytes: 30 * G, Endpoint: "unix:///var/run/docker.sock"}, GPUs: []domain.GPU{{Index: "0", UUID: "GPU-test", Vendor: "nvidia", Name: "RTX 4080 SUPER", VRAMTotalBytes: 16 * G, VRAMFreeBytes: 15 * G, ComputeCapability: "8.9", DriverVersion: "580.95.05"}}}
}
func run(t *testing.T, h domain.Hardware, task, rt string) []domain.Recommendation {
	t.Helper()
	c, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := Recommend(h, c, task, rt)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestDesktopGPU(t *testing.T) {
	r := run(t, machine(), "chat", "ollama")
	if len(r) == 0 || r[0].Acceleration != "nvidia" || r[0].Model.MinVRAMGiB > VRAMBudgetGiB(machine().GPUs[0]) {
		t.Fatalf("%#v", r)
	}
}
func Test16GiBLaptop(t *testing.T) {
	h := machine()
	h.RAMTotalBytes = 16 * G
	h.RAMAvailableBytes = 12 * G
	h.Docker.MemoryBytes = 8 * G
	r := run(t, h, "chat", "ollama")
	if len(r) == 0 {
		t.Fatal("small models should fit the Docker memory cap")
	}
	for _, candidate := range r {
		if candidate.Model.MinRAMGiB > 7 {
			t.Fatalf("Docker memory cap ignored for %s", candidate.Model.ID)
		}
	}
}
func TestCPUOnly(t *testing.T) {
	h := machine()
	h.GPUs = nil
	r := run(t, h, "chat", "auto")
	if len(r) == 0 {
		t.Fatal("no CPU candidates")
	}
	for _, x := range r {
		if x.Acceleration != "cpu" || (x.Model.Runtime != "ollama" && x.Model.Runtime != "llama.cpp") || x.Model.ParametersB > 8 {
			t.Fatalf("unsafe CPU candidate %#v", x)
		}
	}
}
func TestVeryLowMemory(t *testing.T) {
	h := machine()
	h.RAMAvailableBytes = G
	if len(run(t, h, "chat", "auto")) != 0 {
		t.Fatal("no RAM should yield no candidates")
	}
}
func TestUnknownFreeMemory(t *testing.T) {
	h := machine()
	h.GPUs[0].VRAMFreeBytes = 0
	if len(run(t, h, "chat", "vllm")) != 0 {
		t.Fatal("unknown free VRAM must block vLLM")
	}
	h.RAMAvailableBytes = 0
	if len(run(t, h, "chat", "ollama")) != 0 {
		t.Fatal("unknown free RAM must block")
	}
}
func TestMultiGPUNoSumming(t *testing.T) {
	h := machine()
	h.GPUs[0].VRAMTotalBytes = 8 * G
	h.GPUs[0].VRAMFreeBytes = 7 * G
	g := h.GPUs[0]
	g.Index = "1"
	g.UUID = "GPU-two"
	h.GPUs = append(h.GPUs, g)
	for _, x := range run(t, h, "chat", "vllm") {
		if x.Model.MinVRAMGiB > 6 {
			t.Fatalf("summed GPU memory %#v", x)
		}
	}
}

func TestCatalogCoversVRAMTiers(t *testing.T) {
	for _, tier := range []uint64{4, 6, 8, 12, 16, 24, 32, 48, 64, 80, 96, 128} {
		h := machine()
		h.GPUs[0].VRAMTotalBytes = tier * G
		h.GPUs[0].VRAMFreeBytes = tier*G - G/4
		h.RAMTotalBytes, h.RAMAvailableBytes, h.Docker.MemoryBytes = 128*G, 115*G, 120*G
		for _, task := range []string{"chat", "code", "rag", "embeddings"} {
			count := 0
			for _, choice := range run(t, h, task, "auto") {
				if choice.Acceleration == "nvidia" {
					count++
					if choice.Model.MinVRAMGiB > VRAMBudgetGiB(h.GPUs[0]) {
						t.Fatalf("%d GiB / %s: %s exceeds VRAM budget", tier, task, choice.Model.ID)
					}
				}
			}
			if count == 0 {
				t.Fatalf("%d GiB / %s: no GPU profile", tier, task)
			}
		}
		if tier == 128 {
			found := false
			for _, choice := range run(t, h, "chat", "vllm") {
				found = found || choice.Model.ID == "vllm-gpt-oss-120b"
			}
			if !found {
				t.Fatal("128 GiB GPU should offer gpt-oss-120b")
			}
		}
	}
}

func TestVLLMSelectsGPUThatSupportsModelDType(t *testing.T) {
	h := machine()
	old := h.GPUs[0]
	old.UUID, old.ComputeCapability = "GPU-turing", "7.5"
	old.VRAMTotalBytes, old.VRAMFreeBytes = 24*G, 23*G
	h.GPUs = append(h.GPUs, old)
	seen := false
	for _, choice := range run(t, h, "chat", "vllm") {
		if choice.Model.ID == "vllm-qwen3-0.6b" {
			seen = true
			if choice.GPU.UUID != "GPU-test" {
				t.Fatal("selected Turing for a BF16 profile")
			}
		}
	}
	if !seen {
		t.Fatal("the newer GPU should offer the BF16 profile")
	}
}
func TestBusyGPU(t *testing.T) {
	h := machine()
	h.GPUs[0].VRAMFreeBytes = G
	if len(run(t, h, "chat", "vllm")) != 0 {
		t.Fatal("busy GPU accepted")
	}
}
func TestOldGPUDoesNotHideNewGPU(t *testing.T) {
	h := machine()
	old := h.GPUs[0]
	old.UUID = "GPU-old"
	old.VRAMTotalBytes = 40 * G
	old.VRAMFreeBytes = 39 * G
	old.ComputeCapability = "6.1"
	h.GPUs = append([]domain.GPU{old}, h.GPUs...)
	r := run(t, h, "chat", "vllm")
	if len(r) == 0 || r[0].GPU.UUID == "GPU-old" {
		t.Fatalf("unsupported GPU selected %#v", r)
	}
}
func TestDockerMemoryCap(t *testing.T) {
	h := machine()
	h.Docker.MemoryBytes = 4 * G
	r := run(t, h, "chat", "auto")
	if len(r) == 0 {
		t.Fatal("tiny starter should fit")
	}
	for _, x := range r {
		if x.Model.MinRAMGiB > 3 {
			t.Fatal("ignored Docker VM memory limit")
		}
	}
}
func TestWindowsVLLMRequiresLocalLinuxDocker(t *testing.T) {
	h := machine()
	h.OS = "windows"
	h.Docker.Endpoint = "npipe:////./pipe/docker_engine"
	h.Docker.Architecture = "x86_64"
	if len(run(t, h, "chat", "vllm")) == 0 {
		t.Fatal("eligible Windows vLLM profile missing")
	}
	offline := h
	offline.Docker.Reachable = false
	if len(run(t, offline, "chat", "vllm")) != 0 {
		t.Fatal("Windows vLLM must verify the Linux Docker backend first")
	}
	if len(run(t, h, "chat", "ollama")) == 0 {
		t.Fatal("Windows Ollama missing")
	}
}
func TestMacCPUOnly(t *testing.T) {
	h := machine()
	h.OS = "darwin"
	h.Arch = "arm64"
	for _, x := range run(t, h, "code", "auto") {
		if x.Acceleration != "cpu" {
			t.Fatal("Docker Metal claimed")
		}
	}
}
func TestRemoteDockerRejected(t *testing.T) {
	h := machine()
	h.Docker.Endpoint = "tcp://host:2376"
	c, _ := catalog.Load("")
	if _, _, err := Recommend(h, c, "chat", "auto"); err == nil {
		t.Fatal("remote daemon accepted")
	}
}
func TestTinyDisk(t *testing.T) {
	h := machine()
	h.DiskFreeBytes = G
	if len(run(t, h, "chat", "auto")) != 0 {
		t.Fatal("full disk accepted")
	}
}
func TestEmbeddingsSeparate(t *testing.T) {
	r := run(t, machine(), "embeddings", "auto")
	if len(r) < 2 {
		t.Fatalf("expected more than one embedding option: %#v", r)
	}
	for _, candidate := range r {
		if !hasTask(candidate.Model, "embeddings") || hasTask(candidate.Model, "chat") {
			t.Fatalf("non-embedding model in embedding recommendations: %#v", candidate.Model)
		}
	}
}
func TestMalformedSelection(t *testing.T) {
	c, _ := catalog.Load("")
	for _, p := range [][2]string{{"vision", "auto"}, {"chat", "unknown"}} {
		if _, _, err := Recommend(machine(), c, p[0], p[1]); err == nil {
			t.Fatalf("accepted %v", p)
		}
	}
}

func TestOllamaNVIDIAGating(t *testing.T) {
	tests := []struct {
		name, cc, driver string
		want             bool
	}{
		{"modern", "8.9", "550.54.15", true},
		{"too-old-driver", "8.9", "535.104.05", false},
		{"pascal-needs570", "6.1", "550.54.15", false},
		{"pascal-supported", "6.1", "570.124.06", true},
		{"maxwell-supported", "5.0", "570.1", true},
		{"kepler-unsupported", "3.7", "580.1", false},
		{"unknown-capability", "", "580.1", false},
		{"unknown-driver", "8.9", "", false},
		{"nan-capability", "NaN", "580.1", false},
		{"inf-capability", "+Inf", "580.1", false},
		{"invalid-driver", "8.9", "580.bad", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := machine()
			h.GPUs[0].ComputeCapability = tc.cc
			h.GPUs[0].DriverVersion = tc.driver
			ok, reason := OllamaGPUSupported(h.GPUs[0])
			if ok != tc.want {
				t.Fatalf("support=%v, want=%v: %s", ok, tc.want, reason)
			}
			if !tc.want && reason == "" {
				t.Fatal("missing user-facing rejection reason")
			}
			r := run(t, h, "chat", "ollama")
			if len(r) == 0 {
				t.Fatal("CPU fallback should remain available")
			}
			if !tc.want {
				for _, candidate := range r {
					if candidate.Acceleration != "cpu" {
						t.Fatal("unsupported GPU was recommended")
					}
				}
			}
		})
	}
}
func TestVLLMDriverFloor(t *testing.T) {
	for _, tc := range []struct {
		driver string
		want   bool
	}{{"525.60.13", true}, {"525.60.12", false}, {"525.59.99", false}, {"535.1", true}, {"575.57.08", true}, {"580.95.05", true}, {"", false}, {"[N/A]", false}} {
		h := machine()
		h.GPUs[0].DriverVersion = tc.driver
		ok, _ := VLLMGPUSupported(h.GPUs[0], "vllm/vllm-openai:v0.32.0-cu129")
		if ok != tc.want {
			t.Errorf("driver %q got=%v want=%v", tc.driver, ok, tc.want)
		}
		r := run(t, h, "chat", "vllm")
		wantDefault := tc.driver == "580.95.05"
		if wantDefault && len(r) == 0 {
			t.Errorf("driver %q unexpectedly excluded", tc.driver)
		}
		if !wantDefault && len(r) != 0 {
			t.Errorf("driver %q incorrectly recommended", tc.driver)
		}
	}
}
func TestOllamaRejectDoesNotHideVLLM(t *testing.T) {
	h := machine()
	h.GPUs[0].DriverVersion = "535.104.05"
	c, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	c.Runtimes["vllm"] = "vllm/vllm-openai:v0.31.0-cu129"
	r, _, err := Recommend(h, c, "chat", "auto")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range r {
		if candidate.Model.Runtime == "vllm" {
			found = true
		}
		if candidate.Model.Runtime == "ollama" && candidate.Acceleration != "cpu" {
			t.Fatal("Ollama driver prerequisite ignored")
		}
	}
	if !found {
		t.Fatal("Ollama restriction incorrectly hid eligible vLLM preliminary options")
	}
}
