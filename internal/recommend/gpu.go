package recommend

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"llmlauncher/internal/domain"
	"llmlauncher/internal/hardware"
)

// OllamaGPUSupported applies the NVIDIA prerequisites published at
// https://docs.ollama.com/gpu . Successful admission still requires a container
// GPU probe and real inference; host metadata does not prove runtime readiness.
func OllamaGPUSupported(g domain.GPU) (bool, string) {
	cc, driver, reason := gpuMetadata(g)
	if reason != "" {
		return false, reason
	}
	if cc < 5 {
		return false, fmt.Sprintf("compute capability %s ниже требуемой Ollama 5.0", g.ComputeCapability)
	}
	required := 550
	if cc <= 6.2 {
		required = 570
	}
	if driver[0] < required {
		return false, fmt.Sprintf("драйвер NVIDIA %s старее требуемой для compute capability %s ветки %d", g.DriverVersion, g.ComputeCapability, required)
	}
	return true, ""
}

// VLLMPlatformSupported admits Windows only when the local Linux Docker backend
// has been identified. GPU passthrough still has to pass a container probe.
func VLLMPlatformSupported(h domain.Hardware) bool {
	if h.Arch != "amd64" {
		return false
	}
	return h.OS == "linux" || (h.OS == "windows" && h.Docker.Reachable &&
		h.Docker.Compose && h.Docker.ServerOS == "linux" && hardware.LocalEndpoint(h.Docker.Endpoint) &&
		(h.Docker.Architecture == "x86_64" || h.Docker.Architecture == "amd64"))
}

// VLLMGPUSupported checks a preliminary driver floor for the selected image.
// The container probe still verifies that the actual libraries can load.
func VLLMGPUSupported(g domain.GPU, image string) (bool, string) {
	cc, driver, reason := gpuMetadata(g)
	if reason != "" {
		return false, reason
	}
	if cc < 7.5 {
		return false, fmt.Sprintf("compute capability %s ниже требуемой vLLM 7.5", g.ComputeCapability)
	}
	tag := strings.TrimPrefix(image, "vllm/vllm-openai")
	cuda12 := strings.Contains(tag, "-cu12") || tag == "@sha256:bc0445dac61accbf1c3b17fbb524a71eee0b1b32f55ba45952095e3be8991c3f"
	if cuda12 {
		if !versionAtLeast(driver, [3]int{525, 60, 13}) {
			return false, fmt.Sprintf("драйвер NVIDIA %s ниже предварительного минимума CUDA 12.x: 525.60.13", g.DriverVersion)
		}
	} else if driver[0] < 580 {
		return false, fmt.Sprintf("для выбранного образа vLLM нужен драйвер NVIDIA R580 или новее (найден %s)", g.DriverVersion)
	}
	return true, ""
}

// VLLMModelGPUSupported adds the floor for the catalog's BF16 and MXFP4 profiles.
func VLLMModelGPUSupported(g domain.GPU, quantization string) (bool, string) {
	if quantization != "BF16" && quantization != "MXFP4" {
		return true, ""
	}
	cc, _, reason := gpuMetadata(g)
	if reason != "" {
		return false, reason
	}
	if cc < 8 {
		return false, fmt.Sprintf("профиль vLLM %s требует compute capability 8.0+ (найдена %s)", quantization, g.ComputeCapability)
	}
	return true, ""
}

func gpuMetadata(g domain.GPU) (float64, [3]int, string) {
	if g.Vendor != "nvidia" {
		return 0, [3]int{}, "эта версия адаптера поддерживает только NVIDIA"
	}
	cc, err := strconv.ParseFloat(g.ComputeCapability, 64)
	if err != nil || math.IsNaN(cc) || math.IsInf(cc, 0) || cc <= 0 {
		return 0, [3]int{}, "compute capability не определена; проверьте вывод nvidia-smi и драйвер"
	}
	driver, ok := parseDriverVersion(g.DriverVersion)
	if !ok {
		return 0, [3]int{}, "версия драйвера NVIDIA не определена; GPU-вариант не предлагается до повторной диагностики"
	}
	return cc, driver, ""
}

func parseDriverVersion(s string) ([3]int, bool) {
	var result [3]int
	parts := strings.Split(s, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return result, false
	}
	for i, p := range parts {
		if p == "" {
			return result, false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return result, false
			}
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 100000 {
			return result, false
		}
		result[i] = n
	}
	return result, result[0] > 0
}

func versionAtLeast(actual, required [3]int) bool {
	for i := 0; i < 3; i++ {
		if actual[i] != required[i] {
			return actual[i] > required[i]
		}
	}
	return true
}

// LlamaGPUSupported is the conservative floor for the pinned CUDA 12 server.
func LlamaGPUSupported(g domain.GPU) (bool, string) {
	cc, driver, reason := gpuMetadata(g)
	if reason != "" {
		return false, reason
	}
	if cc < 7.5 || !versionAtLeast(driver, [3]int{550, 0, 0}) {
		return false, "профиль llama.cpp CUDA требует compute capability 7.5+ и драйвер NVIDIA R550+"
	}
	return true, ""
}
