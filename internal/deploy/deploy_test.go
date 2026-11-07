package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"llmlauncher/internal/domain"
)

func testPlan() domain.Plan {
	return domain.Plan{SchemaVersion: 1, Project: "llm-test", CreatedAt: "2026-11-06T00:00:00Z", Runtime: "ollama", Image: "ollama/ollama:0.40.14", Task: "chat", Acceleration: "cpu", Context: 4096, Parallel: 1, Port: 11434, Host: "127.0.0.1", GPUUtilization: .8,
		Model:    domain.Model{ID: "qwen-test", Name: "Qwen test", Runtime: "ollama", Ref: "qwen2.5:0.5b-instruct-q4_K_M", Tasks: []string{"chat", "code", "rag"}, ParametersB: .5, MinRAMGiB: 2, MinVRAMGiB: 1, DiskGiB: 1, Context: 32768, License: "Apache-2.0"},
		Hardware: domain.Hardware{OS: "linux", Arch: "amd64"},
	}
}

func testVLLM() domain.Plan {
	p := testPlan()
	p.Runtime = "vllm"
	p.Image = "vllm/vllm-openai:v0.32.0-cu129"
	p.Acceleration = "nvidia"
	p.DeviceIDs = []string{"GPU-12345678-1111-2222-3333-123456789abc"}
	p.Model.Runtime = "vllm"
	p.Model.Ref = "Qwen/Qwen2.5-1.5B-Instruct"
	p.Model.MinVRAMGiB = 4
	return p
}

func TestComposeIsBoundedAndDeterministic(t *testing.T) {
	for _, p := range []domain.Plan{testPlan(), testVLLM()} {
		data, err := Render(p)
		if err != nil {
			t.Fatal(err)
		}
		data2, err := Render(p)
		if err != nil || string(data) != string(data2) {
			t.Fatal("non-deterministic render")
		}
		s := string(data)
		for _, required := range []string{"127.0.0.1:", "restart: unless-stopped", "model_cache:", "max-size: \"10m\"", fingerprint(p)} {
			if !strings.Contains(s, required) {
				t.Errorf("missing %s", required)
			}
		}
		for _, forbidden := range []string{"privileged:", "network_mode: host", "trust-remote-code", "image: latest", "/var/run/docker.sock"} {
			if strings.Contains(s, forbidden) {
				t.Errorf("unsafe setting %s", forbidden)
			}
		}
		if p.Runtime == "ollama" {
			if strings.Contains(s, "devices:") {
				t.Fatal("CPU Compose requested GPU")
			}
			if !strings.Contains(s, "OLLAMA_CONTEXT_LENGTH: \"4096\"") {
				t.Fatal("context not passed to Ollama")
			}
		} else {
			for _, required := range []string{p.DeviceIDs[0], "--max-model-len", "--max-num-seqs", "--dtype", "--gpu-memory-utilization"} {
				if !strings.Contains(s, required) {
					t.Errorf("missing vLLM setting %s", required)
				}
			}
		}
	}
}

func TestValidateRefusesUnsafeAndInconsistentPlans(t *testing.T) {
	cases := map[string]func(*domain.Plan){
		"public port":          func(p *domain.Plan) { p.Host = "0.0.0.0" },
		"unversioned image":    func(p *domain.Plan) { p.Image = "ollama/ollama:latest" },
		"lookalike repo":       func(p *domain.Plan) { p.Image = "ollama/ollama-evil:0.40.14" },
		"interpolation":        func(p *domain.Plan) { p.Model.Ref = "qwen:${TOKEN}" },
		"path traversal":       func(p *domain.Plan) { p.Model.Ref = "../../qwen:tag" },
		"unsupported task":     func(p *domain.Plan) { p.Task = "vision" },
		"unsupported parallel": func(p *domain.Plan) { p.Parallel = 8 },
		"context above model":  func(p *domain.Plan) { p.Context = 64000 },
		"extra GPU":            func(p *domain.Plan) { p.DeviceIDs = []string{"0"} },
		"runtime mismatch":     func(p *domain.Plan) { p.Model.Runtime = "vllm" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := testPlan()
			change(&p)
			if _, err := Render(p); err == nil {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
}

func TestBundleDetectsDriftAndNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	p := testPlan()
	if err := WritePlan(dir, p); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPlan(dir)
	if err != nil || got.Model.Ref != p.Model.Ref {
		t.Fatalf("roundtrip: %v", err)
	}
	if err := WritePlan(dir, p); err == nil {
		t.Fatal("overwrote an existing plan")
	}
	composePath := filepath.Join(dir, ComposeFile)
	original, _ := os.ReadFile(composePath)
	changed := append(append([]byte{}, original...), []byte("# manual drift\n")...)
	if err := os.WriteFile(composePath, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPlan(dir); err == nil {
		t.Fatal("manual drift accepted")
	}
	// Updating a checksum cannot bypass regeneration from validated settings.
	mb, _ := os.ReadFile(filepath.Join(dir, ManifestFile))
	var m manifest
	_ = json.Unmarshal(mb, &m)
	m.ComposeSHA256 = digest(changed)
	mb, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dir, ManifestFile), mb, 0600)
	if _, err := ReadPlan(dir); err == nil {
		t.Fatal("regenerated Compose mismatch accepted")
	}
}

func TestExclusiveLockAndMalformedState(t *testing.T) {
	dir := t.TempDir()
	unlock, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireLock(dir); err == nil {
		t.Fatal("simultaneous operation lock accepted")
	}
	unlock()
	if err := WritePlan(dir, testPlan()); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"status":"ready"}`), 0600)
	if _, err := loadState(dir, testPlan()); err == nil {
		t.Fatal("unowned/corrupt state accepted")
	}
}

func planAtServer(p domain.Plan, s *httptest.Server) domain.Plan {
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(s.URL, "http://"))
	p.Host = host
	p.Port, _ = strconv.Atoi(port)
	return p
}

func TestSmokeChecksBodyNotJustHTTP(t *testing.T) {
	cases := []struct {
		name, runtime, task, body string
		ok                        bool
	}{
		{"ollama success", "ollama", "chat", `{"choices":[{"message":{"content":"OK"}}]}`, true},
		{"ollama empty", "ollama", "chat", `{"choices":[{"message":{"content":" "}}]}`, false},
		{"native response cannot prove OpenAI endpoint", "ollama", "chat", `{"done":true,"message":{"content":"OK"}}`, false},
		{"ollama error", "ollama", "chat", `{"error":"out of memory"}`, false},
		{"invalid JSON", "ollama", "chat", `not json`, false},
		{"vllm success", "vllm", "chat", `{"choices":[{"message":{"content":"OK"}}]}`, true},
		{"vllm no choices", "vllm", "chat", `{"choices":[]}`, false},
		{"embedding success", "ollama", "embeddings", `{"data":[{"embedding":[0.1,0.2,-0.3]}]}`, true},
		{"embedding empty", "ollama", "embeddings", `{"data":[]}`, false},
		{"embedding zero", "ollama", "embeddings", `{"data":[{"embedding":[0,0]}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			p := testPlan()
			p.Runtime = tc.runtime
			p.Task = tc.task
			p = planAtServer(p, s)
			result, err := (Manager{}).smoke(context.Background(), p)
			if tc.ok != (err == nil) || result.OK != tc.ok {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestHTTPBypassesProxyAndUsesSelectedModel(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body struct {
			Model     string `json:"model"`
			MaxTokens int    `json:"max_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.MaxTokens != 32 || body.Model != testPlan().Model.Ref {
			t.Errorf("unexpected request %+v", body)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
	}))
	defer s.Close()
	p := planAtServer(testPlan(), s)
	p.Context = 2048
	if _, err := (Manager{}).smoke(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestLocalAPIRejectsRedirect(t *testing.T) {
	targetReached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetReached = true }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	if _, err := (Manager{}).smoke(context.Background(), planAtServer(testPlan(), s)); err == nil {
		t.Fatal("redirect accepted")
	}
	if targetReached {
		t.Fatal("redirect target received inference request")
	}
}

func TestGPUStatusRequiresActualVRAMUse(t *testing.T) {
	for _, vram := range []uint64{0, 512 << 20} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/ps" {
				t.Errorf("expected actual running models, got %s", r.URL.Path)
			}
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"size_vram":%d}]}`, testPlan().Model.Ref, vram)
		}))
		p := planAtServer(testPlan(), s)
		p.Acceleration = "nvidia"
		err := (Manager{}).verifyOllamaGPU(context.Background(), p)
		if (err == nil) != (vram > 0) {
			t.Errorf("vram %d error %v", vram, err)
		}
		s.Close()
	}
}

func TestDemoAndRemoteDockerCannotMutate(t *testing.T) {
	t.Run("demo", func(t *testing.T) {
		dir := t.TempDir()
		p := testPlan()
		p.Hardware.Warnings = []string{"DEMO_INVENTORY"}
		if err := WritePlan(dir, p); err != nil {
			t.Fatal(err)
		}
		m := Manager{Dir: dir, runCommand: func(context.Context, []string, bool) (string, error) { t.Fatal("demo invoked Docker"); return "", nil }}
		if err := m.Up(context.Background()); err == nil {
			t.Fatal("demo deployed")
		}
	})
	for _, endpoint := range []string{"tcp://127.0.0.1:2375", "ssh://remote", "npipe:////server/pipe/docker_engine"} {
		t.Run(endpoint, func(t *testing.T) {
			m := Manager{probeDocker: func(context.Context) domain.DockerInfo {
				return domain.DockerInfo{Installed: true, Reachable: true, Compose: true, ServerOS: "linux", Endpoint: endpoint}
			}}
			if err := m.ensureDocker(context.Background(), testPlan()); err == nil {
				t.Fatalf("unverified endpoint %s accepted", endpoint)
			}
		})
	}
}

func TestUnownedProjectCollisionStopsBeforeMutation(t *testing.T) {
	dir := t.TempDir()
	p := testPlan()
	if err := WritePlan(dir, p); err != nil {
		t.Fatal(err)
	}
	calls := 0
	m := Manager{Dir: dir, probeDocker: func(context.Context) domain.DockerInfo {
		return domain.DockerInfo{Installed: true, Reachable: true, Compose: true, ServerOS: "linux", Endpoint: "unix:///var/run/docker.sock"}
	}}
	m.runCommand = func(_ context.Context, args []string, _ bool) (string, error) {
		calls++
		if len(args) < 3 || args[0] != "--host" {
			t.Fatal("Docker host not pinned")
		}
		args = args[2:]
		if args[0] == "ps" {
			return "foreign-container\n", nil
		}
		if args[0] == "inspect" {
			return `[{"Id":"foreign-container","Config":{"Labels":{"com.docker.compose.project":"llm-test"}},"State":{"Running":true}}]`, nil
		}
		t.Fatalf("unowned project reached command %v", args)
		return "", nil
	}
	if err := m.Up(context.Background()); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("expected ownership error, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("unexpected commands before rejection: %d", calls)
	}
}

// This integration test executes the real orchestration and HTTP checks while
// replacing only Docker and the hardware scan. No Docker daemon is required.
func TestManagedLifecycleAndIdempotentUp(t *testing.T) {
	dir := t.TempDir()
	p := testPlan()
	var running, downloaded, loaded, network, volume bool
	var mu sync.Mutex
	var commands []string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			_, _ = io.WriteString(w, `{"version":"0.40.14"}`)
		case "/api/tags":
			if downloaded {
				_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"digest":"sha256:model1"}]}`, p.Model.Ref)
			} else {
				_, _ = io.WriteString(w, `{"models":[]}`)
			}
		case "/v1/chat/completions":
			loaded = true
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"OK"}}]}`)
		case "/api/ps":
			if loaded {
				_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"size_vram":0}]}`, p.Model.Ref)
			} else {
				_, _ = io.WriteString(w, `{"models":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewUnstartedServer(handler)
	addr := server.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	p.Port, _ = strconv.Atoi(port)
	_ = server.Listener.Close() // Startup must own the port only after preflight.
	defer func() { server.Close() }()
	if err := WritePlan(dir, p); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{managedLabel: "true", ownerLabel: fingerprint(p), "com.docker.compose.project": p.Project, "com.docker.compose.project.working_dir": dir}
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	docker := domain.DockerInfo{Installed: true, Reachable: true, Compose: true, ServerOS: "linux", Endpoint: "unix:///var/run/docker.sock", MemoryBytes: 64 << 30}
	m := Manager{Dir: dir, Timeout: 5 * time.Second, probeDocker: func(context.Context) domain.DockerInfo { return docker }, scanHardware: func(context.Context, string) (domain.Hardware, error) {
		return domain.Hardware{OS: "linux", Arch: "amd64", RAMTotalBytes: 64 << 30, RAMAvailableBytes: 56 << 30, DiskFreeBytes: 100 << 30, Docker: docker}, nil
	}}
	freshScan := m.scanHardware
	m.runCommand = func(_ context.Context, args []string, _ bool) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) < 3 || args[0] != "--host" || args[1] != docker.Endpoint {
			t.Fatalf("Docker endpoint was not pinned: %v", args)
		}
		args = args[2:]
		command := strings.Join(args, " ")
		commands = append(commands, command)
		switch args[0] {
		case "ps":
			if running {
				return "test-container\n", nil
			}
			return "", nil
		case "inspect":
			c := containerInfo{ID: "test-container", Image: "sha256:image1"}
			c.Config.Labels = labels
			c.State.Running = running
			return encode([]containerInfo{c}), nil
		case "volume", "network":
			exists := volume
			if args[0] == "network" {
				exists = network
			}
			name := p.Project + "_model_cache"
			if args[0] == "network" {
				name = p.Project + "_default"
			}
			if args[1] == "ls" {
				if exists {
					return name + "\n", nil
				}
				return "", nil
			}
			return encode([]map[string]any{{"Labels": labels}}), nil
		case "image":
			return encode([]imageInfo{{ID: "sha256:image1", RepoDigests: []string{"ollama/ollama@sha256:image1"}}}), nil
		case "compose":
			if strings.Contains(command, " pull --policy ") {
				return "", fmt.Errorf("error getting credentials: cached runtime must not contact a registry")
			}
			if strings.Contains(command, " up --detach") {
				if !strings.Contains(command, "--pull never") {
					t.Fatal("startup allowed an implicit image download")
				}
				ln, err := net.Listen("tcp", addr)
				if err != nil {
					return "", err
				}
				if server.URL != "" {
					server = httptest.NewUnstartedServer(handler)
					_ = server.Listener.Close()
				}
				server.Listener = ln
				server.Start()
				running = true
				volume = true
				network = true
			}
			if strings.Contains(command, " exec -T model ollama pull ") {
				downloaded = true
			}
			if strings.Contains(command, " down --timeout ") {
				running = false
				network = false
				server.Close()
			}
			if strings.Contains(command, " ps --all ") {
				return `[{"Service":"model","State":"running"}]`, nil
			}
			return "", nil
		}
		return "", fmt.Errorf("unexpected test command %s", command)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(dir, p)
	if err != nil || state.Status != "ready" || !state.Smoke.OK || state.ModelDigest != "sha256:model1" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	firstCommands := len(commands)
	m.scanHardware = func(context.Context, string) (domain.Hardware, error) {
		t.Fatal("idempotent up rechecked memory consumed by its own model")
		return domain.Hardware{}, nil
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, command := range commands[firstCommands:] {
		if strings.Contains(command, " pull ") || strings.Contains(command, " up --detach") {
			t.Fatalf("idempotent up mutated runtime: %s", command)
		}
	}
	status, err := m.Status(context.Background())
	if err != nil || !status.Reachable || !status.ModelLoaded {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !volume {
		t.Fatal("down deleted downloaded model volume")
	}
	state, err = loadState(dir, p)
	if err != nil || state.Status != "stopped" {
		t.Fatalf("state after down=%+v err=%v", state, err)
	}
	// A stopped project must start again from its cached image even when the
	// registry credential helper fails. Ownership and API checks still apply.
	m.scanHardware = freshScan
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("cached restart failed: %v", err)
	}
	if err := m.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Archive(context.Background()); err != nil {
		t.Fatal(err)
	}
	projects, err := List(dir)
	if err != nil || len(projects) != 1 || projects[0].State != "archived" || !volume {
		t.Fatalf("archive did not preserve the deployment and weights: projects=%+v volume=%t err=%v", projects, volume, err)
	}
	before := len(commands)
	if err := m.Up(context.Background()); err == nil || !strings.Contains(err.Error(), "archived") || len(commands) != before {
		t.Fatalf("archive must block up before Docker calls: err=%v", err)
	}
	if err := Restore(dir); err != nil {
		t.Fatal(err)
	}
	projects, err = List(dir)
	if err != nil || len(projects) != 1 || projects[0].State != "stopped" || !volume {
		t.Fatalf("restore must preserve the stopped plan and weights: projects=%+v volume=%t err=%v", projects, volume, err)
	}
	for _, command := range commands {
		if strings.Contains(command, "--volumes") || strings.Contains(command, "system prune") {
			t.Fatalf("destructive command %s", command)
		}
	}
}
