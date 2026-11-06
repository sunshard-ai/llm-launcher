// Package deploy writes and manages one local, explicitly owned Compose project.
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/domain"
	"llmlauncher/internal/huggingface"
)

const (
	PlanFile     = "plan.json"
	ComposeFile  = "compose.yaml"
	ManifestFile = "manifest.json"
	envFile      = ".launcher.env"
	lockFile     = ".operation.lock"
	stateFile    = "state.json"
	ownerLabel   = "io.llm-launcher.plan"
	managedLabel = "io.llm-launcher.managed"
	envContents  = "# Intentionally empty. llm-launcher passes all settings explicitly.\n"
)

var (
	projectPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,47}$`)
	idPattern           = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,95}$`)
	ollamaRefPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_./-]*:[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	hfRefPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	revisionPattern     = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
	devicePattern       = regexp.MustCompile(`^(GPU-[a-fA-F0-9-]+|[0-9]+)$`)
	imageVersionPattern = regexp.MustCompile(`^:v?[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9_.-]+)?$`)
	imageDigestPattern  = regexp.MustCompile(`^@sha256:[a-fA-F0-9]{64}$`)
)

// Validate rejects settings that cannot be safely represented by this MVP.
func Validate(p domain.Plan) error {
	switch p.SchemaVersion {
	case 1:
		if p.Advanced != nil || p.HFSource != nil || p.Runtime == "llama.cpp" {
			return fmt.Errorf("advanced settings and Hugging Face sources require plan schema 2")
		}
	case 2:
	default:
		return fmt.Errorf("unsupported plan schema %d (expected 1 or 2)", p.SchemaVersion)
	}
	if !projectPattern.MatchString(p.Project) {
		return fmt.Errorf("project must contain 2-48 lowercase letters, digits, '-' or '_' and start with a letter/digit")
	}
	if p.Runtime != "ollama" && p.Runtime != "vllm" && p.Runtime != "llama.cpp" {
		return fmt.Errorf("unsupported runtime %q", p.Runtime)
	}
	if p.Model.Runtime != p.Runtime {
		return fmt.Errorf("model runtime %q differs from plan runtime %q", p.Model.Runtime, p.Runtime)
	}
	if !idPattern.MatchString(p.Model.ID) {
		return fmt.Errorf("invalid model ID")
	}
	if p.Host != "127.0.0.1" {
		return fmt.Errorf("this MVP publishes only on 127.0.0.1; use an authenticated gateway for remote access")
	}
	if p.Port < 1024 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	if p.Parallel != 1 {
		return fmt.Errorf("this MVP validates memory for one request at a time; parallel must equal 1")
	}
	if p.Context < 512 || p.Context > 32768 || p.Context > p.Model.Context {
		return fmt.Errorf("context must be between 512 and min(32768, model context %d)", p.Model.Context)
	}
	if p.Task != "chat" && p.Task != "code" && p.Task != "rag" && p.Task != "embeddings" {
		return fmt.Errorf("unsupported task %q", p.Task)
	}
	taskOK := false
	for _, task := range p.Model.Tasks {
		if task == p.Task {
			taskOK = true
		}
	}
	if !taskOK {
		return fmt.Errorf("model %s does not declare task %s", p.Model.ID, p.Task)
	}
	for _, n := range []float64{p.Model.MinRAMGiB, p.Model.DiskGiB} {
		if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return fmt.Errorf("model memory and disk estimates must be positive finite numbers")
		}
	}
	if p.Model.MinVRAMGiB < 0 || math.IsNaN(p.Model.MinVRAMGiB) || math.IsInf(p.Model.MinVRAMGiB, 0) {
		return fmt.Errorf("invalid VRAM estimate")
	}
	if p.Runtime == "llama.cpp" {
		expectedImage := LlamaCPPImage
		if p.Acceleration == "nvidia" {
			expectedImage = catalog.LlamaCUDAImage
		}
		if p.Image != expectedImage || p.Task == "embeddings" || p.HFSource == nil || len(p.HFSource.Files) != 0 {
			return fmt.Errorf("llama.cpp requires the pinned image for its accelerator, a single GGUF source and a text task")
		}
		if err := huggingface.Validate(*p.HFSource); err != nil {
			return err
		}
		if p.Model.Ref != p.HFSource.Repo || p.Model.Revision != p.HFSource.Revision {
			return fmt.Errorf("model reference must match its pinned Hugging Face source")
		}
	} else {
		if p.HFSource != nil {
			if p.Runtime != "vllm" || len(p.HFSource.Files) == 0 {
				return fmt.Errorf("vLLM import requires a pinned safetensors snapshot")
			}
			if err := huggingface.Validate(*p.HFSource); err != nil {
				return err
			}
			if p.Model.Ref != p.HFSource.Repo || p.Model.Revision != p.HFSource.Revision {
				return fmt.Errorf("model reference must match its pinned Hugging Face source")
			}
		}
		repo := "ollama/ollama"
		if p.Runtime == "vllm" {
			repo = "vllm/vllm-openai"
		}
		if !strings.HasPrefix(p.Image, repo) {
			return fmt.Errorf("runtime image must use the official repository %s", repo)
		}
		imageSuffix := strings.TrimPrefix(p.Image, repo)
		if !imageVersionPattern.MatchString(imageSuffix) && !imageDigestPattern.MatchString(imageSuffix) {
			return fmt.Errorf("runtime image needs a concrete version (no latest/rc) or sha256 digest")
		}
		if strings.Contains(strings.ToLower(imageSuffix), "rc") || strings.Contains(strings.ToLower(imageSuffix), "dev") {
			return fmt.Errorf("prerelease runtime images are not enabled in this MVP")
		}
	}
	if p.Acceleration != "cpu" && p.Acceleration != "nvidia" {
		return fmt.Errorf("unsupported acceleration %q", p.Acceleration)
	}
	if p.Acceleration == "cpu" && len(p.DeviceIDs) != 0 {
		return fmt.Errorf("CPU plan must not request GPU devices")
	}
	if p.Acceleration == "nvidia" && (len(p.DeviceIDs) != 1 || !devicePattern.MatchString(p.DeviceIDs[0])) {
		return fmt.Errorf("NVIDIA plan must name exactly one GPU UUID or index")
	}
	if p.Runtime == "ollama" {
		if !ollamaRefPattern.MatchString(p.Model.Ref) || strings.Contains(p.Model.Ref, "..") {
			return fmt.Errorf("Ollama model needs an explicit repository:tag reference")
		}
		if p.Model.Revision != "" {
			return fmt.Errorf("Ollama revisions are model digests observed after download, not HF revisions")
		}
	} else if p.Runtime == "vllm" {
		if p.Acceleration != "nvidia" {
			return fmt.Errorf("vLLM in this MVP requires one supported NVIDIA GPU")
		}
		if p.Task == "embeddings" {
			return fmt.Errorf("vLLM embedding profiles are not yet implemented")
		}
		if !hfRefPattern.MatchString(p.Model.Ref) || strings.Contains(p.Model.Ref, "..") {
			return fmt.Errorf("vLLM model must be a Hugging Face org/repository")
		}
		if p.Model.Revision != "" && !revisionPattern.MatchString(p.Model.Revision) {
			return fmt.Errorf("HF revision must be a full 40-character commit SHA")
		}
		if p.GPUUtilization < 0.01 || p.GPUUtilization > 0.95 || math.IsNaN(p.GPUUtilization) {
			return fmt.Errorf("vLLM GPU utilization must be between 0.01 and 0.95")
		}
	}
	return validateAdvanced(p)
}

func quoted(s string) string { b, _ := json.Marshal(s); return string(b) }

func fingerprint(p domain.Plan) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Render produces deterministic Compose YAML; there is no shell interpolation.
func Render(p domain.Plan) ([]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("# Generated by llm-launcher. Create a new plan to change managed settings.\nservices:\n  model:\n")
	fmt.Fprintf(&b, "    image: %s\n    restart: unless-stopped\n    stop_grace_period: 30s\n", quoted(p.Image))
	fmt.Fprintf(&b, "    labels:\n      %s: \"true\"\n      %s: %s\n", managedLabel, ownerLabel, quoted(fingerprint(p)))
	containerPort := 11434
	if p.Runtime == "vllm" {
		containerPort = 8000
	} else if p.Runtime == "llama.cpp" {
		containerPort = 8080
	}
	fmt.Fprintf(&b, "    ports:\n      - %s\n", quoted(fmt.Sprintf("127.0.0.1:%d:%d", p.Port, containerPort)))
	b.WriteString("    logging:\n      driver: json-file\n      options:\n        max-size: \"10m\"\n        max-file: \"3\"\n")
	if p.Runtime == "ollama" {
		b.WriteString("    environment:\n      OLLAMA_HOST: \"0.0.0.0:11434\"\n")
		keepAlive := "10m"
		if p.Advanced != nil && p.Advanced.Environment["OLLAMA_KEEP_ALIVE"] != "" {
			keepAlive = p.Advanced.Environment["OLLAMA_KEEP_ALIVE"]
		}
		fmt.Fprintf(&b, "      OLLAMA_CONTEXT_LENGTH: %s\n      OLLAMA_NUM_PARALLEL: \"1\"\n      OLLAMA_MAX_LOADED_MODELS: \"1\"\n      OLLAMA_KEEP_ALIVE: %s\n      OLLAMA_NO_CLOUD: \"true\"\n", quoted(strconv.Itoa(p.Context)), quoted(keepAlive))
		if p.Acceleration == "cpu" {
			b.WriteString("      CUDA_VISIBLE_DEVICES: \"-1\"\n      ROCR_VISIBLE_DEVICES: \"-1\"\n      OLLAMA_VULKAN: \"0\"\n")
		}
		writeEnvironment(&b, p)
		b.WriteString("    volumes:\n      - model_cache:/root/.ollama\n    healthcheck:\n      test: [\"CMD\", \"ollama\", \"list\"]\n      interval: 10s\n      timeout: 5s\n      retries: 12\n      start_period: 20s\n")
	} else if p.Runtime == "vllm" {
		modelPath, dtype := p.Model.Ref, "half"
		if p.HFSource != nil {
			modelPath, dtype = "/models", "auto"
		}
		if p.Advanced != nil && p.Advanced.DType != "" {
			dtype = p.Advanced.DType
		}
		args := []string{"--model", modelPath, "--served-model-name", p.Model.ID, "--host", "0.0.0.0", "--port", "8000", "--dtype", dtype, "--max-model-len", strconv.Itoa(p.Context), "--max-num-seqs", "1", "--gpu-memory-utilization", strconv.FormatFloat(p.GPUUtilization, 'f', 6, 64)}
		if p.HFSource != nil {
			args = append(args, "--load-format", "safetensors")
		} else if p.Model.Revision != "" {
			args = append(args, "--revision", p.Model.Revision)
		}
		if p.Advanced != nil && p.Advanced.EnforceEager {
			args = append(args, "--enforce-eager")
		}
		if config := vllmGeneration(p.Advanced); len(config) != 0 {
			generation, _ := json.Marshal(config)
			args = append(args, "--override-generation-config", string(generation))
		}
		encoded, _ := json.Marshal(args)
		fmt.Fprintf(&b, "    command: %s\n", encoded)
		b.WriteString("    shm_size: \"2gb\"\n    environment:\n      HF_HUB_DISABLE_TELEMETRY: \"1\"\n      VLLM_NO_USAGE_STATS: \"1\"\n      DO_NOT_TRACK: \"1\"\n")
		if p.HFSource != nil {
			b.WriteString("      HF_HUB_OFFLINE: \"1\"\n")
		}
		writeEnvironment(&b, p)
		b.WriteString("    volumes:\n      - model_cache:/root/.cache/huggingface\n")
		if p.HFSource != nil {
			writeHubMount(&b)
		}
		b.WriteString("    healthcheck:\n")
		probe, _ := json.Marshal([]string{"CMD", "python3", "-c", "import urllib.request; urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:8000/health',timeout=4).read()"})
		fmt.Fprintf(&b, "      test: %s\n      interval: 15s\n      timeout: 5s\n      retries: 120\n      start_period: 60s\n", probe)
	} else {
		encoded, _ := json.Marshal(llamaArgs(p))
		fmt.Fprintf(&b, "    command: %s\n", encoded)
		if p.Advanced != nil && len(p.Advanced.Environment) > 0 {
			b.WriteString("    environment:\n")
			writeEnvironment(&b, p)
		}
		b.WriteString("    volumes:\n")
		writeHubMount(&b)
	}
	if p.Acceleration == "nvidia" {
		fmt.Fprintf(&b, "    deploy:\n      resources:\n        reservations:\n          devices:\n            - driver: nvidia\n              device_ids: [%s]\n              capabilities: [gpu]\n", quoted(p.DeviceIDs[0]))
	}
	if p.Runtime != "llama.cpp" {
		fmt.Fprintf(&b, "volumes:\n  model_cache:\n    labels:\n      %s: \"true\"\n      %s: %s\n", managedLabel, ownerLabel, quoted(fingerprint(p)))
	}
	fmt.Fprintf(&b, "networks:\n  default:\n    labels:\n      %s: \"true\"\n      %s: %s\n", managedLabel, ownerLabel, quoted(fingerprint(p)))
	return []byte(b.String()), nil
}

func writeHubMount(b *strings.Builder) {
	fmt.Fprintf(b, "      - type: bind\n        source: ./%s\n        target: /models\n        read_only: true\n        bind:\n          create_host_path: false\n", huggingface.CacheDir)
}
