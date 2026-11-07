package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/deploy"
	"llmlauncher/internal/huggingface"
)

func fixture() string { return filepath.Join("..", "..", "examples", "hardware", "cpu-16gb.json") }

func TestCatalogListsAllEnginesWithoutDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var out bytes.Buffer
	if err := run(context.Background(), []string{"catalog", "--json"}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var cat struct {
		Models []struct {
			Runtime string `json:"runtime"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	engines := map[string]bool{}
	for _, model := range cat.Models {
		engines[model.Runtime] = true
	}
	for _, runtime := range []string{"ollama", "vllm", "llama.cpp"} {
		if !engines[runtime] {
			t.Fatalf("catalog omitted %s without Docker", runtime)
		}
	}
}

func TestCLIPlanFixtureAndOverwriteProtection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deployment")
	args := []string{"plan", "--inventory", fixture(), "--task", "chat", "--runtime", "ollama", "--dir", dir}
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	p, err := deploy.ReadPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Acceleration != "cpu" || p.Host != "127.0.0.1" || p.Context != 4096 {
		t.Fatalf("unsafe/unexpected plan: %+v", p)
	}
	if len(p.Hardware.Warnings) == 0 {
		t.Fatal("demo marker missing")
	}
	before, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("overwriting an existing project must fail")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "compose.yaml"))
	if !bytes.Equal(before, after) {
		t.Fatal("refused overwrite changed compose")
	}
}

func TestCLIRejectsInventoryForMutation(t *testing.T) {
	for _, cmd := range []string{"up", "down", "gpu-check", "test"} {
		var b bytes.Buffer
		err := run(context.Background(), []string{cmd, "--inventory", fixture()}, strings.NewReader(""), &b, &b)
		if err == nil || !strings.Contains(err.Error(), "--inventory") {
			t.Fatalf("%s: %v", cmd, err)
		}
	}
}

func TestCLIImportWeightsRetainsReviewedPlan(t *testing.T) {
	o := options{inventory: fixture(), task: "chat", runtime: "llama.cpp", context: 1024, dir: filepath.Join(t.TempDir(), "deployment")}
	hw, err := scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	recs, _, err := choices(hw, cat, o)
	if err != nil || len(recs) == 0 {
		t.Fatalf("GGUF fixture unavailable: %v", err)
	}
	source := *cat.Profiles[recs[0].Model.ID].Artifact
	data := []byte("GGUFimport fixture")
	source.Bytes, source.SHA256 = int64(len(data)), fmt.Sprintf("%x", sha256.Sum256(data))
	p, err := makePlanWithSource(hw, cat, recs[0], o, &source)
	if err != nil {
		t.Fatal(err)
	}
	if err := deploy.WritePlan(o.dir, p); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(t.TempDir(), "downloaded model.gguf")
	if err := os.WriteFile(local, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(context.Background(), []string{"import-weights", "--dir", o.dir, "--from", local}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := deploy.ReadPlan(o.dir); err != nil {
		t.Fatalf("import changed the reviewed bundle: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(o.dir, huggingface.CacheDir, huggingface.FileName))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("cache=%q, error=%v", got, err)
	}
}

func TestUnknownModelIsNotSilentlySubstituted(t *testing.T) {
	var b bytes.Buffer
	err := run(context.Background(), []string{"plan", "--inventory", fixture(), "--model", "does-not-exist", "--dir", filepath.Join(t.TempDir(), "not-created")}, strings.NewReader(""), &b, &b)
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatal(err)
	}
}

func TestWizardOfflineCanCompleteAndEOFIsNotConsent(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		wantErr     bool
	}{{"accept", "\n1\n1\n1\ny\n", false}, {"end-of-input", "", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "deployment")
			var b bytes.Buffer
			err := run(context.Background(), []string{"wizard", "--inventory", fixture(), "--dry-run", "--dir", dir}, strings.NewReader(tc.input), &b, &b)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v output=%s", err, b.String())
			}
			_, statErr := os.Stat(filepath.Join(dir, "plan.json"))
			if tc.wantErr && statErr == nil {
				t.Fatal("EOF must not create a plan")
			}
			if !tc.wantErr && statErr != nil {
				t.Fatal("plan not created")
			}
		})
	}
}

func TestLongerContextFiltersButPersistsBaseEstimates(t *testing.T) {
	o := options{inventory: fixture(), task: "chat", runtime: "ollama", context: 8192, dir: t.TempDir()}
	h, err := scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	c, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	recs, _, err := choices(h, c, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) == 0 {
		t.Fatal("expected a small model to fit the 8K context fixture")
	}
	byID := map[string]float64{}
	for _, m := range c.Models {
		byID[m.ID] = m.MinRAMGiB
	}
	for _, r := range recs {
		if r.Model.MinRAMGiB != byID[r.Model.ID] {
			t.Fatal("context factor would be applied twice")
		}
	}
}

func TestFlagsAndMachineOutput(t *testing.T) {
	for _, args := range [][]string{{"plan", "--cpu", "--gpu", "0"}, {"plan", "--context", "0"}, {"unknown"}, {"up", "--timeout", "0s"}} {
		var b bytes.Buffer
		if run(context.Background(), args, strings.NewReader(""), &b, &b) == nil {
			t.Fatalf("accepted invalid args %v", args)
		}
	}
	var out, errOut bytes.Buffer
	if err := run(context.Background(), []string{"scan", "--inventory", fixture(), "--json"}, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "{\n") || strings.Contains(out.String(), "Система:") {
		t.Fatal("JSON output mixed with prose")
	}
}

func TestEnglishHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	assertEnglish := func(t *testing.T, s string) {
		t.Helper()
		if strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
			t.Fatalf("help contains untranslated text:\n%s", s)
		}
	}
	if err := run(context.Background(), []string{"--help"}, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	assertEnglish(t, out.String())
	for _, command := range strings.Fields("wizard scan doctor update catalog recommend plan hf-plan inspect list up run status test logs down delete restore gpu-check import-weights") {
		t.Run(command, func(t *testing.T) {
			if !strings.Contains(out.String(), command) {
				t.Errorf("general help omits %q", command)
			}
			var help bytes.Buffer
			err := run(context.Background(), []string{command, "--help"}, nil, io.Discard, &help)
			if !errors.Is(err, flag.ErrHelp) || !strings.Contains(help.String(), "Usage: llm-launcher") {
				t.Fatalf("expected command help, got %v and %q", err, help.String())
			}
			assertEnglish(t, help.String())
		})
	}
}

func TestDesktopTranslationsCoverSupportedLanguages(t *testing.T) {
	data, err := os.ReadFile("desktop_strings.json")
	if err != nil {
		t.Fatal(err)
	}
	var messages map[string]map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		t.Fatal(err)
	}
	placeholders := regexp.MustCompile(`\{\d+\}`)
	for key, translations := range messages {
		base := placeholders.FindAllString(translations["en"], -1)
		for _, language := range []string{"en", "de", "ru", "zh", "fr"} {
			value := translations[language]
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s is missing a %s translation", key, language)
			}
			args := placeholders.FindAllString(value, -1)
			if len(args) != len(base) {
				t.Errorf("%s in %s has incorrect placeholders: %v", key, language, args)
				continue
			}
			for _, arg := range base {
				if !strings.Contains(value, arg) {
					t.Errorf("%s in %s is missing placeholder %s", key, language, arg)
				}
			}
		}
	}
}

func TestIgnoredFlagsAreRejectedBeforeAnyAction(t *testing.T) {
	for _, args := range [][]string{
		{"up", "--cpu"}, {"up", "--port", "9000"},
		{"wizard", "--runtime", "vllm"}, {"wizard", "--task", "embeddings"},
		{"wizard", "--model", "missing"}, {"status", "--context", "8192"},
	} {
		var b bytes.Buffer
		if err := run(context.Background(), args, strings.NewReader(""), &b, &b); err == nil || !strings.Contains(err.Error(), "флаг --") {
			t.Fatalf("accepted or misreported ignored flag %v: %v", args, err)
		}
	}
}

func TestJSONRecommendationsDoNotHideNoFit(t *testing.T) {
	var out, stderr bytes.Buffer
	err := run(context.Background(), []string{"recommend", "--inventory", fixture(), "--runtime", "vllm", "--json"}, strings.NewReader(""), &out, &stderr)
	if err == nil {
		t.Fatal("JSON must preserve the no-fit failure exit status")
	}
	if !strings.Contains(out.String(), `"recommendations": []`) {
		t.Fatalf("missing machine-readable empty result: %s", out.String())
	}
}

func TestCancelInterruptsBlockedWizardInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	dir := filepath.Join(t.TempDir(), "not-created")
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"wizard", "--inventory", fixture(), "--dir", dir, "--dry-run"}, reader, io.Discard, io.Discard)
	}()
	// The fixture scanner is synchronous; cancellation must also work if the
	// wizard reaches its blocking input before the signal arrives.
	time.AfterFunc(20*time.Millisecond, cancel)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected cancellation result: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Ctrl+C cancellation left wizard blocked on stdin")
	}
	if _, err := os.Stat(filepath.Join(dir, "plan.json")); err == nil {
		t.Fatal("cancelled wizard saved a plan")
	}
}

func TestVLLMImageOverrideIsSavedInPlan(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vllm")
	inventory := filepath.Join("..", "..", "examples", "hardware", "linux-rtx4080-16gb.json")
	args := []string{"plan", "--runtime", "vllm", "--model", "vllm-qwen25-1.5b",
		"--inventory", inventory, "--dir", dir, "--image", "v0.33.0"}
	if err := run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	p, err := deploy.ReadPlan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Image != "vllm/vllm-openai:v0.33.0" {
		t.Fatalf("selected image was not saved: %s", p.Image)
	}
}

func TestVLLMDTypeDefaultsAreSavedWithoutOverridingExplicitSettings(t *testing.T) {
	inventory := filepath.Join("..", "..", "examples", "hardware", "linux-rtx4080-16gb.json")
	for _, tt := range []struct{ model, settings, want string }{
		{"vllm-qwen25-1.5b", "", "auto"},
		{"vllm-qwen25-7b-awq", "", "half"},
		{"vllm-qwen25-1.5b", `{"dtype":"half"}`, "half"},
	} {
		dir := filepath.Join(t.TempDir(), "deployment")
		args := []string{"plan", "--runtime", "vllm", "--model", tt.model, "--inventory", inventory, "--dir", dir}
		if tt.settings != "" {
			args = append(args, "--settings-json", tt.settings)
		}
		if err := run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
		p, err := deploy.ReadPlan(dir)
		if err != nil || p.Advanced == nil || p.Advanced.DType != tt.want {
			t.Fatalf("expected dtype %q in saved plan for %s, advanced=%+v err=%v", tt.want, tt.model, p.Advanced, err)
		}
	}
}

func TestLlamaGPUPlansEnableOffloadLogging(t *testing.T) {
	inventory := filepath.Join("..", "..", "examples", "hardware", "linux-rtx4080-16gb.json")
	for _, test := range []struct {
		name, settings, want string
		wantError            bool
	}{
		{"default", "", "4", false},
		{"explicit debug", `{"environment":{"LLAMA_ARG_LOG_VERBOSITY":"5"}}`, "5", false},
		{"quiet GPU logs", `{"environment":{"LLAMA_ARG_LOG_VERBOSITY":"3"}}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "deployment")
			args := []string{"plan", "--runtime", "llama.cpp", "--model", "llamacpp-qwen3-0.6b",
				"--gpu", "0", "--inventory", inventory, "--dir", dir}
			if test.settings != "" {
				args = append(args, "--settings-json", test.settings)
			}
			err := run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "VERBOSITY") {
					t.Fatalf("expected an actionable logging error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			plan, err := deploy.ReadPlan(dir)
			if err != nil || plan.Advanced == nil || plan.Advanced.Environment["LLAMA_ARG_LOG_VERBOSITY"] != test.want {
				t.Fatalf("GPU logging default did not reach the saved plan: %v", err)
			}
			compose, err := deploy.Render(plan)
			if err != nil || !strings.Contains(string(compose), `LLAMA_ARG_LOG_VERBOSITY: "`+test.want+`"`) {
				t.Fatalf("runtime environment did not receive the logging level: %v", err)
			}
			// Older saved plans retain their rendering and remain inspectable.
			plan.Advanced = nil
			oldDir := filepath.Join(t.TempDir(), "legacy")
			if err := deploy.WritePlan(oldDir, plan); err != nil {
				t.Fatal(err)
			}
			if _, err := deploy.ReadPlan(oldDir); err != nil {
				t.Fatalf("old plan became unreadable: %v", err)
			}
		})
	}
}

func TestQuickScanSelectsModelWithoutAskingForEngine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quick")
	var out bytes.Buffer
	err := run(context.Background(), []string{"scan", "--interactive", "--dry-run", "--inventory", fixture(), "--dir", dir},
		strings.NewReader("1\n1\n"), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	p, err := deploy.ReadPlan(dir)
	if err != nil || p.Task != "chat" || p.Runtime != "ollama" {
		t.Fatalf("quick scan did not save a matching plan: %+v, %v", p, err)
	}
	if strings.Contains(out.String(), "Движок [") {
		t.Fatal("quick scan asked the user to pick a runtime")
	}
}

func TestQuickScanDefaultExitDoesNotSavePlan(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "quick")
	err := run(context.Background(), []string{"scan", "--interactive", "--dry-run", "--inventory", fixture(), "--dir", dir},
		strings.NewReader("1\n\n"), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("enter at model selection should exit without creating a plan: %v", err)
	}
}
