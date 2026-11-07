package deploy

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOffloadLogKeepsStartupEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		chunks []string
		want   bool
	}{
		{"before long log", []string{"load_tensors: offloaded 29/29 layers to GPU\n", strings.Repeat("server request completed\n", 120000)}, true},
		{"split across writes", []string{"load_tensors: offloaded 29/", "29 layers to G", "PU\n"}, true},
		{"CPU", []string{"offloaded 0/29 layers to GPU\n"}, false},
		{"visibility only", []string{"Found CUDA device 0: NVIDIA RTX 4080 SUPER\n"}, false},
		{"no evidence", []string{"server is ready\n"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			log := &offloadLog{}
			for _, chunk := range test.chunks {
				n, err := io.WriteString(log, chunk)
				if err != nil || n != len(chunk) {
					t.Fatalf("log writer returned %d, %v", n, err)
				}
			}
			if log.confirmed != test.want {
				t.Fatalf("GPU offload confirmation = %t, want %t", log.confirmed, test.want)
			}
		})
	}
}

func TestLlamaOffloadUsesCurrentContainerStart(t *testing.T) {
	container := containerInfo{ID: "owned-model"}
	container.State.StartedAt = "2026-11-07T19:15:00.123456789Z"
	for _, cpuOnly := range []bool{false, true} {
		manager := Manager{runCommand: func(_ context.Context, args []string, _ bool) (string, error) {
			currentStart := len(args) == 4 && args[0] == "logs" && args[1] == "--since" &&
				args[2] == container.State.StartedAt && args[3] == container.ID
			if !currentStart {
				// A previous successful start must not qualify a CPU-only restart.
				return "offloaded 29/29 layers to GPU\n", nil
			}
			if cpuOnly {
				return "offloaded 0/29 layers to GPU\n", nil
			}
			return "offloaded 29/29 layers to GPU\n" + strings.Repeat("server request\n", 400), nil
		}}
		if err := manager.verifyLlamaOffload(context.Background(), container); (err != nil) != cpuOnly {
			t.Fatalf("CPU-only=%t: %v", cpuOnly, err)
		}
	}
	container.State.StartedAt = ""
	if err := (Manager{}).verifyLlamaOffload(context.Background(), container); err == nil {
		t.Fatal("accepted unavailable container start time")
	}
}

func TestLlamaOffloadReadsStderrBeforeDiagnosticTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture exercises the real Docker process path on Unix")
	}
	dir := t.TempDir()
	docker := "#!/bin/sh\nprintf 'load_tensors: offloaded 29/29 layers to GPU\\n' >&2\ni=0\nwhile [ \"$i\" -lt 5000 ]; do printf 'server request completed with a long diagnostic line\\n' >&2; i=$((i+1)); done\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	container := containerInfo{ID: "owned-model"}
	container.State.StartedAt = "2026-11-07T19:15:00Z"
	if err := (Manager{}).verifyLlamaOffload(context.Background(), container); err != nil {
		t.Fatal(err)
	}
}

func TestLlamaOffloadPropagatesDockerError(t *testing.T) {
	manager := Manager{runCommand: func(context.Context, []string, bool) (string, error) {
		return "offloaded 29/29 layers to GPU\n", errors.New("log driver unavailable")
	}}
	container := containerInfo{ID: "owned-model"}
	container.State.StartedAt = "2026-11-07T19:15:00Z"
	if err := manager.verifyLlamaOffload(context.Background(), container); err == nil {
		t.Fatal("accepted an unsuccessful Docker logs command")
	}
}

func TestLlamaQuietStartupGivesRecoveryInstructions(t *testing.T) {
	manager := Manager{runCommand: func(context.Context, []string, bool) (string, error) {
		return "common_params_print_info: verbosity = 3 (adjust with the -lv N CLI arg)\nserver: model loaded\n", nil
	}}
	container := containerInfo{ID: "owned-model"}
	container.State.StartedAt = "2026-11-07T19:15:00Z"
	err := manager.verifyLlamaOffload(context.Background(), container)
	if err == nil || !strings.Contains(err.Error(), "LLAMA_ARG_LOG_VERBOSITY=4") {
		t.Fatalf("expected instructions for an older quiet plan, got %v", err)
	}
}
