package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedCatalog(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Models) < 10 {
		t.Fatal("incomplete starter catalog")
	}
}
func TestInvalidCatalog(t *testing.T) {
	c, _ := Load("")
	c.Models = append(c.Models, c.Models[0])
	if Validate(c) == nil {
		t.Fatal("duplicate ID accepted")
	}
	c, _ = Load("")
	c.Runtimes["ollama"] = "ollama/ollama:latest"
	if Validate(c) == nil {
		t.Fatal("latest accepted")
	}
	c, _ = Load("")
	c.Models[0].Ref = "$(touch /tmp/unsafe)"
	if Validate(c) == nil {
		t.Fatal("shell string accepted")
	}
}
func TestJSONRejectsTrailingAndUnknown(t *testing.T) {
	for _, s := range []string{"{} {}", "{\"schema_version\":1,\"unknown\":1}"} {
		p := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(p, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}

func TestRuntimeImageAllowlist(t *testing.T) {
	for _, tc := range []struct {
		runtime, image string
		want           bool
	}{
		{"ollama", "ollama/ollama:0.40.14", true},
		{"vllm", "vllm/vllm-openai:v0.32.0-cu129", true},
		{"ollama", "ollama/ollama@sha256:" + strings.Repeat("a", 64), true},
		{"ollama", "evil/ollama:0.40.14", false},
		{"ollama", "ollama/ollama.evil:0.40.14", false},
		{"ollama", "ollama/ollama:latest", false},
		{"ollama", "ollama/ollama:0.40", false},
		{"ollama", "ollama/ollama:0.40.15-rc1", false},
		{"vllm", "vllm/vllm-openai:v0.33.0-dev", false},
		{"vllm", "ollama/ollama:0.40.14", false},
	} {
		if got := ValidateRuntimeImage(tc.runtime, tc.image) == nil; got != tc.want {
			t.Errorf("%s %s accepted=%v want=%v", tc.runtime, tc.image, got, tc.want)
		}
	}
}
func TestCatalogContextCeilings(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range c.Models {
		want := 32768
		if m.ID == "ollama-bge-m3" || m.ID == "ollama-nomic-embed-v15" {
			want = 8192
		}
		if m.Context != want {
			t.Errorf("%s context %d want %d", m.ID, m.Context, want)
		}
	}
}
