package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"llmlauncher/internal/domain"
)

// RunningStatus describes a model service discovered on the active local daemon.
type RunningStatus struct {
	domain.Status
	Dir         string `json:"dir"`
	DockerState string `json:"docker_state"`
}

// RunningStatus discovers only launcher-managed Compose model services. It
// verifies each saved plan before querying its endpoint.
func (m Manager) RunningStatus(ctx context.Context) ([]RunningStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.ensureDocker(ctx, domain.Plan{}); err != nil {
		return nil, err
	}
	const service = "com.docker.compose.service"
	const workingDir = "com.docker.compose.project.working_dir"
	ids, err := m.command(ctx, []string{"ps", "--filter", "label=" + managedLabel + "=true",
		"--filter", "label=" + service + "=model", "--format", "{{.ID}}"}, false)
	if err != nil {
		return nil, err
	}
	result := []RunningStatus{}
	fields := strings.Fields(ids)
	if len(fields) == 0 {
		return result, nil
	}
	data, err := m.command(ctx, append([]string{"inspect"}, fields...), false)
	if err != nil {
		return nil, err
	}
	var containers []containerInfo
	if err := json.Unmarshal([]byte(data), &containers); err != nil {
		return nil, fmt.Errorf("parse Docker container inspection: %w", err)
	}
	for _, c := range containers {
		labels := c.Config.Labels
		if (!c.State.Running && !c.State.Restarting) || labels[managedLabel] != "true" ||
			labels[service] != "model" || labels[ownerLabel] == "" {
			continue
		}
		row := RunningStatus{Dir: labels[workingDir], DockerState: c.State.Status}
		row.Project = labels["com.docker.compose.project"]
		if !filepath.IsAbs(row.Dir) {
			row.Error = "plan directory is unavailable from this OS or Docker client"
		} else if p, err := ReadPlan(row.Dir); err != nil {
			row.Error = fmt.Sprintf("could not verify saved plan: %v", err)
		} else if !ownedLabels(labels, p) {
			row.Error = "container ownership does not match the saved plan"
		} else if d := p.Hardware.Docker; d.Reachable && d.Endpoint != "" && d.Endpoint != m.pinnedEndpoint {
			row.Error = "Docker endpoint differs from the saved plan"
		} else {
			row.Status = planStatus(p)
			if c.State.Restarting {
				row.Error = "model container is restarting"
			} else {
				probeCtx, stop := context.WithTimeout(ctx, 5*time.Second)
				m.probeModelStatus(probeCtx, p, &row.Status)
				stop()
			}
		}
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Project < result[j].Project })
	return result, nil
}

func planStatus(p domain.Plan) domain.Status {
	return domain.Status{Project: p.Project, Runtime: p.Runtime, Endpoint: endpoint(p) + "/v1", Model: APIModel(p)}
}

func (m Manager) probeModelStatus(ctx context.Context, p domain.Plan, s *domain.Status) {
	if err := m.probeReady(ctx, p); err != nil {
		s.Error = err.Error()
		return
	}
	s.Reachable = true
	loaded, err := m.modelLoaded(ctx, p)
	s.ModelLoaded = loaded
	if err != nil {
		s.Error = err.Error()
	}
}
