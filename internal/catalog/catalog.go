// Package catalog embeds an explicit, versioned allowlist of models and images.
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"llmlauncher"
	"llmlauncher/internal/domain"
	"llmlauncher/internal/huggingface"
)

const LlamaCPUImage = "ghcr.io/ggml-org/llama.cpp@sha256:295dc9897fa8a643e4a513fbcaada51d3b8db4b0afa4fda7aeae2386757de58b"
const LlamaCUDAImage = "ghcr.io/ggml-org/llama.cpp@sha256:502fde462776339020cec39425525e9ce78f17cd9f7b14123f55f5197b1da00a"

func Load(path string) (domain.Catalog, error) {
	if path == "" {
		if u, err := defaultUpdater(); err == nil {
			return u.current()
		}
		return Decode([]byte(llmlauncher.EmbeddedCatalog()))
	}
	f, err := os.Open(path)
	if err != nil {
		return domain.Catalog{}, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxCatalog+1))
	if err != nil {
		return domain.Catalog{}, err
	}
	if len(b) > maxCatalog {
		return domain.Catalog{}, fmt.Errorf("catalog exceeds 2 MiB")
	}
	return Decode(b)
}

func Decode(b []byte) (domain.Catalog, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c domain.Catalog
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("catalog: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("catalog contains trailing data")
	}
	return c, Validate(c)
}

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)
var modelRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:-]*$`)
var revision = regexp.MustCompile(`^[a-f0-9]{40}$`)
var imageVersion = regexp.MustCompile(`^:v?[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9_.-]+)?$`)
var imageDigest = regexp.MustCompile(`^@sha256:[a-fA-F0-9]{64}$`)

// ValidateRuntimeImage restricts startup to official runtime repositories and
// concrete release versions or immutable digests, matching deploy.ValidatePlan.
func ValidateRuntimeImage(runtime, image string) error {
	repos := map[string]string{"ollama": "ollama/ollama", "vllm": "vllm/vllm-openai"}
	repo, ok := repos[runtime]
	if !ok || !strings.HasPrefix(image, repo) {
		return fmt.Errorf("движок %s должен использовать официальный образ %s", runtime, repo)
	}
	suffix := strings.TrimPrefix(image, repo)
	if !imageVersion.MatchString(suffix) && !imageDigest.MatchString(suffix) {
		return fmt.Errorf("для %s нужна полная версия образа или sha256 digest", runtime)
	}
	if strings.Contains(strings.ToLower(suffix), "rc") || strings.Contains(strings.ToLower(suffix), "dev") {
		return fmt.Errorf("предварительные версии образов не включены в этот MVP")
	}
	return nil
}

func Validate(c domain.Catalog) error {
	if c.SchemaVersion != 1 && c.SchemaVersion != 2 {
		return fmt.Errorf("unsupported catalog schema %d", c.SchemaVersion)
	}
	if len(c.Models) == 0 || len(c.Models) > 200 {
		return fmt.Errorf("catalog must contain 1..200 models")
	}
	for _, r := range []string{"ollama", "vllm"} {
		if err := ValidateRuntimeImage(r, c.Runtimes[r]); err != nil {
			return err
		}
	}
	if c.SchemaVersion == 1 && len(c.Profiles) != 0 {
		return fmt.Errorf("catalog profiles require schema 2")
	}
	seen := map[string]bool{}
	for _, m := range c.Models {
		if !identifier.MatchString(m.ID) || seen[m.ID] {
			return fmt.Errorf("invalid or duplicate model id %q", m.ID)
		}
		seen[m.ID] = true
		if m.Runtime != "ollama" && m.Runtime != "vllm" && m.Runtime != "llama.cpp" {
			return fmt.Errorf("unknown runtime for %s", m.ID)
		}
		profile, hasProfile := c.Profiles[m.ID]
		if hasProfile && (strings.TrimSpace(profile.Purpose) == "" || profile.Format == "" ||
			profile.DownloadBytes < 0 || !strings.HasPrefix(profile.Source, "https://") ||
			(profile.Verification != "estimated" && profile.Verification != "hardware-tested")) {
			return fmt.Errorf("invalid profile metadata for %s", m.ID)
		}
		if m.Runtime == "llama.cpp" {
			if c.SchemaVersion != 2 || c.Runtimes["llama.cpp"] != LlamaCUDAImage || profile.Artifact == nil ||
				len(profile.Artifact.Files) != 0 || profile.Artifact.Repo != m.Ref || profile.Artifact.Revision != m.Revision ||
				profile.DownloadBytes != profile.Artifact.Bytes || huggingface.Validate(*profile.Artifact) != nil {
				return fmt.Errorf("llama.cpp profile %s requires pinned single-file GGUF metadata", m.ID)
			}
		} else if profile.Artifact != nil {
			return fmt.Errorf("unexpected GGUF artifact for %s", m.ID)
		}
		if !modelRef.MatchString(m.Ref) || strings.Contains(m.Ref, "..") {
			return fmt.Errorf("invalid model ref for %s", m.ID)
		}
		if m.Runtime == "ollama" && (!strings.Contains(m.Ref, ":") || strings.HasSuffix(m.Ref, ":latest")) {
			return fmt.Errorf("Ollama model %s needs an explicit tag", m.ID)
		}
		if m.Revision != "" && !revision.MatchString(m.Revision) {
			return fmt.Errorf("%s revision must be a full lowercase commit SHA", m.ID)
		}
		if m.MinRAMGiB <= 0 || m.MinVRAMGiB <= 0 || m.DiskGiB <= 0 || m.ParametersB <= 0 || m.Context < 512 || m.Context > 32768 {
			return fmt.Errorf("invalid resource estimates for %s", m.ID)
		}
		if len(m.Tasks) == 0 {
			return fmt.Errorf("%s has no tasks", m.ID)
		}
		for _, t := range m.Tasks {
			if t != "chat" && t != "code" && t != "rag" && t != "embeddings" {
				return fmt.Errorf("unknown task %q", t)
			}
		}
		if m.License == "" || !strings.HasPrefix(m.URL, "https://") {
			return fmt.Errorf("%s lacks license/source metadata", m.ID)
		}
	}
	for id := range c.Profiles {
		if !seen[id] {
			return fmt.Errorf("metadata for unknown model %s", id)
		}
	}
	return nil
}
