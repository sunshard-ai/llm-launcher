package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"llmlauncher/internal/domain"
)

func TestRunningStatusDiscoversOwnedProjectsAcrossDirectories(t *testing.T) {
	ollama, vllm := testPlan(), testVLLM()
	vllm.Project = "llm-vllm"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = fmt.Fprint(w, `{"version":"0.40.14"}`)
		case "/api/ps":
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q}]}`, ollama.Model.Ref)
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/models":
			_, _ = fmt.Fprintf(w, `{"data":[{"id":%q}]}`, vllm.Model.ID)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, port, _ := net.SplitHostPort(server.Listener.Addr().String())
	ollama.Port, _ = strconv.Atoi(port)
	vllm.Port = ollama.Port

	var containers []containerInfo
	for _, plan := range []domain.Plan{vllm, ollama} {
		dir := filepath.Join(t.TempDir(), "nested", "deployment")
		if err := WritePlan(dir, plan); err != nil {
			t.Fatal(err)
		}
		var c containerInfo
		c.State.Running, c.State.Status = true, "running"
		c.Config.Labels = map[string]string{
			managedLabel: "true", ownerLabel: fingerprint(plan),
			"com.docker.compose.project": plan.Project, "com.docker.compose.service": "model",
			"com.docker.compose.project.working_dir": dir,
		}
		containers = append(containers, c)
	}
	var foreign containerInfo
	foreign.State.Running = true
	containers = append(containers, foreign)
	m := Manager{
		client: server.Client(),
		probeDocker: func(context.Context) domain.DockerInfo {
			return domain.DockerInfo{Installed: true, Reachable: true, Compose: true, ServerOS: "linux", Endpoint: "unix:///var/run/docker.sock"}
		},
		runCommand: func(_ context.Context, args []string, _ bool) (string, error) {
			cmd := strings.Join(args, " ")
			switch {
			case strings.Contains(cmd, " ps --filter label="+managedLabel+"=true"):
				return "one\ntwo", nil
			case strings.HasSuffix(cmd, " inspect one two"):
				encoded, err := json.Marshal(containers)
				return string(encoded), err
			default:
				return "", fmt.Errorf("unexpected command %s", cmd)
			}
		},
	}
	rows, err := m.RunningStatus(context.Background())
	if err != nil || len(rows) != 2 {
		t.Fatalf("expected two launcher deployments: %+v err=%v", rows, err)
	}
	if rows[0].Runtime != "ollama" || rows[1].Runtime != "vllm" || !rows[0].ModelLoaded || !rows[1].ModelLoaded {
		t.Fatalf("incorrect status: %+v", rows)
	}
}
