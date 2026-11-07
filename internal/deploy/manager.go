package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"llmlauncher/internal/domain"
	"llmlauncher/internal/hardware"
	"llmlauncher/internal/huggingface"
	"llmlauncher/internal/process"
	"llmlauncher/internal/recommend"
)

// Manager runs an existing reviewed plan. Timeout is the overall operation
// deadline; the default for downloads/startup is 30 minutes.
type Manager struct {
	HFToken string // only for direct Hugging Face artifact downloads
	Dir     string
	Out     io.Writer
	Err     io.Writer
	Timeout time.Duration
	LogAll  bool
	// Injection points are deliberately private: production commands always use
	// the local Docker CLI, while tests can simulate failures without a daemon.
	runCommand     func(context.Context, []string, bool) (string, error)
	probeDocker    func(context.Context) domain.DockerInfo
	scanHardware   func(context.Context, string) (domain.Hardware, error)
	client         *http.Client
	pinnedEndpoint string
}

func (m Manager) output() io.Writer {
	if m.Out == nil {
		return io.Discard
	}
	return m.Out
}
func (m Manager) errorOutput() io.Writer {
	if m.Err == nil {
		return m.output()
	}
	return m.Err
}

func (m Manager) operationContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	d := m.Timeout
	if d <= 0 {
		d = fallback
	}
	return context.WithTimeout(ctx, d)
}

type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		return n, nil
	}
	if len(b.data)+len(p) > b.limit {
		b.data = append(b.data[:0], b.data[len(b.data)+len(p)-b.limit:]...)
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (m Manager) command(ctx context.Context, args []string, stream bool) (string, error) {
	if m.pinnedEndpoint != "" {
		args = append([]string{"--host", m.pinnedEndpoint}, args...)
	}
	if m.runCommand != nil {
		return m.runCommand(ctx, args, stream)
	}
	cmd := process.Command(ctx, "docker", args...)
	cmd.WaitDelay = 5 * time.Second
	// -f/-p/--env-file below define this project, not inherited Compose settings.
	for _, e := range os.Environ() {
		upper := strings.ToUpper(e)
		if !strings.HasPrefix(upper, "COMPOSE_") && !strings.HasPrefix(upper, "DOCKER_CONTEXT=") && !strings.HasPrefix(upper, "DOCKER_HOST=") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	out, stderr := &tailBuffer{limit: 2 << 20}, &tailBuffer{limit: 32 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	if stream {
		cmd.Stdout = io.MultiWriter(m.output(), out)
		cmd.Stderr = io.MultiWriter(m.errorOutput(), stderr)
	}
	err := cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return string(out.data), fmt.Errorf("Docker operation cancelled or timed out: %w", ctx.Err())
		}
		detail := strings.TrimSpace(string(stderr.data))
		if detail == "" {
			detail = strings.TrimSpace(string(out.data))
		}
		if len(detail) > 4000 {
			detail = detail[len(detail)-4000:]
		}
		return string(out.data), fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, detail)
	}
	return string(out.data), nil
}

func (m Manager) composeArgs(p domain.Plan, args ...string) []string {
	dir, _ := canonicalDir(m.Dir)
	base := []string{"compose", "--project-name", p.Project, "--project-directory", dir, "--env-file", filepath.Join(dir, envFile), "--file", filepath.Join(dir, ComposeFile)}
	return append(base, args...)
}

func localEndpoint(endpoint string) bool {
	return hardware.LocalEndpoint(endpoint)
}

func (m *Manager) ensureDocker(ctx context.Context, p domain.Plan) error {
	probe := m.probeDocker
	if probe == nil {
		probe = hardware.ProbeDocker
	}
	d := probe(ctx)
	if !d.Installed {
		return fmt.Errorf("Docker CLI is missing; install Docker Engine + Compose v2 (Linux) or Docker Desktop with WSL2/Linux containers (Windows)")
	}
	if !d.Reachable {
		return fmt.Errorf("Docker daemon is unavailable: %s", d.Error)
	}
	if !d.Compose {
		return fmt.Errorf("Docker Compose v2 plugin is unavailable; check 'docker compose version'")
	}
	if d.ServerOS != "linux" {
		return fmt.Errorf("this plan requires Linux containers; Docker reports %q", d.ServerOS)
	}
	if !localEndpoint(d.Endpoint) {
		return fmt.Errorf("remote or unverified Docker endpoint %q is rejected: hardware was scanned on this computer; use a local unix/npipe Docker context", d.Endpoint)
	}
	if p.Hardware.Docker.Reachable && p.Hardware.Docker.Endpoint != "" && p.Hardware.Docker.Endpoint != d.Endpoint {
		return fmt.Errorf("Docker endpoint changed since planning (%s -> %s); create a new plan for the current local context", p.Hardware.Docker.Endpoint, d.Endpoint)
	}
	m.pinnedEndpoint = d.Endpoint
	return nil
}

type containerInfo struct {
	ID           string `json:"Id"`
	Image        string `json:"Image"`
	RestartCount int    `json:"RestartCount"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	State struct {
		Running    bool   `json:"Running"`
		Status     string `json:"Status"`
		Restarting bool   `json:"Restarting"`
		StartedAt  string `json:"StartedAt"`
	} `json:"State"`
}

func ownedLabels(labels map[string]string, p domain.Plan) bool {
	return labels[managedLabel] == "true" && labels[ownerLabel] == fingerprint(p) && labels["com.docker.compose.project"] == p.Project
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ownedResources checks both running and stopped containers plus the names a
// new Compose project would reuse. An unrelated resource is never adopted.
func (m Manager) ownedResources(ctx context.Context, p domain.Plan) ([]containerInfo, error) {
	ids, err := m.command(ctx, []string{"ps", "--all", "--filter", "label=com.docker.compose.project=" + p.Project, "--format", "{{.ID}}"}, false)
	if err != nil {
		return nil, err
	}
	var containers []containerInfo
	if fields := strings.Fields(ids); len(fields) > 0 {
		data, err := m.command(ctx, append([]string{"inspect"}, fields...), false)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &containers); err != nil {
			return nil, fmt.Errorf("parse Docker container inspection: %w", err)
		}
		dir, _ := canonicalDir(m.Dir)
		for _, c := range containers {
			if !ownedLabels(c.Config.Labels, p) {
				return nil, fmt.Errorf("project %s collides with a container not owned by this plan; choose a different project name", p.Project)
			}
			if wd := c.Config.Labels["com.docker.compose.project.working_dir"]; wd != "" && !sameDir(wd, dir) {
				return nil, fmt.Errorf("project %s was started from %s; manage it from its original directory", p.Project, wd)
			}
		}
	}
	for _, resource := range []struct{ kind, name string }{{"volume", p.Project + "_model_cache"}, {"network", p.Project + "_default"}} {
		names, err := m.command(ctx, []string{resource.kind, "ls", "--filter", "name=" + resource.name, "--format", "{{.Name}}"}, false)
		if err != nil {
			return nil, err
		}
		exists := false
		for _, name := range strings.Fields(names) {
			if name == resource.name {
				exists = true
			}
		}
		if !exists {
			continue
		}
		data, err := m.command(ctx, []string{resource.kind, "inspect", resource.name}, false)
		if err != nil {
			return nil, err
		}
		var objects []struct {
			Labels map[string]string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(data), &objects); err != nil || len(objects) != 1 {
			return nil, fmt.Errorf("invalid Docker %s inspection", resource.kind)
		}
		if !ownedLabels(objects[0].Labels, p) {
			return nil, fmt.Errorf("%s %s is not owned by this plan; choose a different project name", resource.kind, resource.name)
		}
	}
	return containers, nil
}

func anyRunning(containers []containerInfo) bool {
	for _, c := range containers {
		if c.State.Running {
			return true
		}
	}
	return false
}

func isDemo(p domain.Plan) bool {
	for _, w := range p.Hardware.Warnings {
		if strings.Contains(strings.ToUpper(w), "DEMO_INVENTORY") {
			return true
		}
	}
	return false
}

func (m Manager) preflight(ctx context.Context, p domain.Plan) error {
	scan := m.scanHardware
	if scan == nil {
		scan = hardware.Scan
	}
	h, err := scan(ctx, m.Dir)
	if err != nil {
		return fmt.Errorf("fresh hardware scan: %w", err)
	}
	if m.pinnedEndpoint != "" && (h.Docker.Endpoint != m.pinnedEndpoint || !h.Docker.Reachable || h.Docker.ServerOS != "linux") {
		return fmt.Errorf("Docker context changed or became unavailable during the fresh scan; select the original local context and retry")
	}
	const gib = float64(uint64(1) << 30)
	scale := recommend.ContextScale(p.Context)
	budget := recommend.RAMBudgetGiB(h)
	if budget < p.Model.MinRAMGiB*scale {
		return fmt.Errorf("not enough currently available RAM: planning budget %.1f GiB, estimated need %.1f GiB (context %d); close applications or choose a smaller model/context", budget, p.Model.MinRAMGiB*scale, p.Context)
	}
	if h.DiskFreeBytes == 0 {
		return fmt.Errorf("could not verify free space on the project filesystem")
	}
	if float64(h.DiskFreeBytes)/gib < recommend.RequiredDiskGiB(p.Model) {
		return fmt.Errorf("not enough disk space on %s: %.1f GiB free, estimated %.1f GiB required including runtime image reserve; Docker storage may be on a different filesystem and also needs space", h.DiskPath, float64(h.DiskFreeBytes)/gib, recommend.RequiredDiskGiB(p.Model))
	}
	if p.Runtime == "vllm" && !recommend.VLLMPlatformSupported(h) {
		return fmt.Errorf("vLLM requires Linux amd64 or a verified local Linux Docker backend on Windows amd64")
	}
	if p.Acceleration == "nvidia" {
		var selected *domain.GPU
		for i := range h.GPUs {
			g := &h.GPUs[i]
			if g.UUID == p.DeviceIDs[0] || g.Index == p.DeviceIDs[0] {
				selected = g
				break
			}
		}
		if selected == nil || !strings.EqualFold(selected.Vendor, "nvidia") {
			return fmt.Errorf("selected NVIDIA GPU %s is no longer available", p.DeviceIDs[0])
		}
		check := recommend.OllamaGPUSupported
		switch p.Runtime {
		case "vllm":
			check = func(g domain.GPU) (bool, string) { return recommend.VLLMGPUSupported(g, p.Image) }
		case "llama.cpp":
			check = recommend.LlamaGPUSupported
		}
		if ok, reason := check(*selected); !ok {
			return fmt.Errorf("GPU preflight: %s", reason)
		}
		free, total := float64(selected.VRAMFreeBytes)/gib, float64(selected.VRAMTotalBytes)/gib
		if free <= 0 || total <= 0 {
			return fmt.Errorf("cannot verify free VRAM for the selected GPU")
		}
		if recommend.VRAMBudgetGiB(*selected) < p.Model.MinVRAMGiB*scale {
			return fmt.Errorf("selected GPU has %.1f GiB usable VRAM after reserves; model/context estimated need %.1f GiB", recommend.VRAMBudgetGiB(*selected), p.Model.MinVRAMGiB*scale)
		}
		if p.Runtime == "vllm" {
			if ok, reason := recommend.VLLMModelGPUSupported(*selected, p.Model.Quantization); !ok {
				return fmt.Errorf("GPU preflight: %s", reason)
			}
			allocation := total * p.GPUUtilization
			if allocation > free-0.5 {
				return fmt.Errorf("vLLM will reserve %.1f GiB but only %.1f GiB is free; free the GPU or create a new plan", allocation, free)
			}
			if allocation < p.Model.MinVRAMGiB*scale {
				return fmt.Errorf("vLLM memory allocation %.1f GiB is below estimated need %.1f GiB; create a smaller-model or higher-budget plan", allocation, p.Model.MinVRAMGiB*scale)
			}
		}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(p.Host, strconv.Itoa(p.Port)))
	if err != nil {
		return fmt.Errorf("local port %d is busy or unavailable; create a plan with another port: %w", p.Port, err)
	}
	_ = ln.Close()
	return nil
}

type imageInfo struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
}

func (m Manager) image(ctx context.Context, p domain.Plan) (imageInfo, error) {
	var result []imageInfo
	data, err := m.command(ctx, []string{"image", "inspect", p.Image}, false)
	if err != nil {
		return imageInfo{}, err
	}
	if err := json.Unmarshal([]byte(data), &result); err != nil || len(result) != 1 || result[0].ID == "" {
		return imageInfo{}, fmt.Errorf("could not resolve runtime image ID")
	}
	return result[0], nil
}

func (m Manager) ensureRuntimeImage(ctx context.Context, p domain.Plan) (imageInfo, error) {
	// Inspect the exact reviewed reference before asking Compose to pull it.
	// Compose 2.38.2's pull --policy missing only recognizes tagged cache entries.
	if image, err := m.image(ctx, p); err == nil {
		fmt.Fprintln(m.output(), "Using the cached runtime image.")
		return image, nil
	}
	if err := ctx.Err(); err != nil {
		return imageInfo{}, err
	}
	fmt.Fprintln(m.output(), "Downloading the runtime image...")
	if _, err := m.command(ctx, m.composeArgs(p, "pull", "--policy", "missing"), true); err != nil {
		if strings.Contains(err.Error(), "error getting credentials") {
			return imageInfo{}, fmt.Errorf("runtime image download: Docker credential helper failed; check registry access with 'docker pull %s' in the same terminal, then retry up: %w", p.Image, err)
		}
		return imageInfo{}, fmt.Errorf("runtime image download: %w", err)
	}
	return m.image(ctx, p)
}

func (m Manager) gpuCheck(ctx context.Context, p domain.Plan, pull string) error {
	if p.Acceleration != "nvidia" {
		return nil
	}
	name := p.Project + "-probe-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	args := []string{"run", "--rm", "--name", name, "--label", managedLabel + "=true", "--label", ownerLabel + "=" + fingerprint(p), "--pull=" + pull, "--gpus", "device=" + p.DeviceIDs[0], "--entrypoint", "nvidia-smi", p.Image, "--query-gpu=uuid,name,memory.total", "--format=csv,noheader"}
	output, err := m.command(ctx, args, false)
	if err != nil {
		// Only this uniquely named short-lived probe can be removed here.
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = m.command(cleanup, []string{"rm", "--force", name}, false)
		cancel()
		return fmt.Errorf("Docker cannot use selected GPU (Linux: check NVIDIA Container Toolkit; Windows: WSL2 + NVIDIA driver + Docker Desktop): %w", err)
	}
	if strings.TrimSpace(output) == "" {
		return fmt.Errorf("GPU probe returned no devices")
	}
	fmt.Fprintf(m.output(), "GPU is visible inside the runtime container: %s\n", strings.TrimSpace(output))
	return nil
}

func (m Manager) checkVLLMImport(ctx context.Context, p domain.Plan) error {
	fmt.Fprintln(m.output(), "Checking that vLLM can load its runtime libraries...")
	probeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	name := p.Project + "-import-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	args := []string{"run", "--rm", "--name", name, "--label", managedLabel + "=true",
		"--label", ownerLabel + "=" + fingerprint(p), "--network", "none", "--pull=never",
		"--gpus", "device=" + p.DeviceIDs[0], "--entrypoint", "python3", p.Image,
		"-c", "import vllm.entrypoints.cli.main"}
	_, err := m.command(probeCtx, args, false)
	if err == nil {
		return nil
	}
	if probeCtx.Err() != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		_, _ = m.command(cleanup, []string{"rm", "--force", name}, false)
		stop()
	}
	return fmt.Errorf("vLLM import failed inside %s before model startup; create a new plan with a compatible --image: %w", p.Image, err)
}

// GPUCheck proves container visibility; Up additionally verifies real inference.
func (m Manager) GPUCheck(ctx context.Context) error {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return err
	}
	if isDemo(p) {
		return fmt.Errorf("demo inventory cannot be deployed; create a fresh plan from a real hardware scan")
	}
	unlock, err := acquireLock(m.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := m.operationContext(ctx, 15*time.Minute)
	defer cancel()
	if err := m.ensureDocker(ctx, p); err != nil {
		return err
	}
	return m.gpuCheck(ctx, p, "missing")
}

// Up starts the selected runtime, downloads weights as needed and reports
// success only after an actual generation/embedding request succeeds.
func (m Manager) Up(ctx context.Context) error {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return err
	}
	if isDemo(p) {
		return fmt.Errorf("demo inventory cannot be deployed; create a fresh plan from a real hardware scan")
	}
	unlock, err := acquireLock(m.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	if archived, err := isArchived(m.Dir, p); err != nil {
		return err
	} else if archived {
		return fmt.Errorf("deployment is archived; run 'restore' before 'up'")
	}
	ctx, cancel := m.operationContext(ctx, 30*time.Minute)
	defer cancel()
	s, err := loadState(m.Dir, p)
	if err != nil {
		return err
	}
	if err := m.ensureDocker(ctx, p); err != nil {
		return err
	}
	containers, err := m.ownedResources(ctx, p)
	if err != nil {
		return err
	}
	if anyRunning(containers) {
		fmt.Fprintln(m.output(), "This plan already has a running container; checking its model and API.")
		if s.ImageID != "" {
			for _, c := range containers {
				if c.State.Running && c.Image != s.ImageID {
					return fmt.Errorf("running image differs from the recorded deployment; refusing to adopt it")
				}
			}
		}
	} else {
		if err := m.preflight(ctx, p); err != nil {
			return err
		}
		if _, err := m.command(ctx, m.composeArgs(p, "config", "--quiet"), false); err != nil {
			return fmt.Errorf("Compose validation: %w", err)
		}
		image, err := m.ensureRuntimeImage(ctx, p)
		if err != nil {
			return err
		}
		if s.ImageID != "" && s.ImageID != image.ID {
			return fmt.Errorf("runtime image changed under the same version tag; create a new reviewed plan to upgrade (recorded %s, current %s)", s.ImageID, image.ID)
		}
		if err := m.gpuCheck(ctx, p, "never"); err != nil {
			return err
		}
		if p.Runtime == "vllm" {
			if err := m.checkVLLMImport(ctx, p); err != nil {
				return err
			}
		}
		if p.HFSource != nil {
			if err := huggingface.Ensure(ctx, m.Dir, *p.HFSource, m.HFToken, m.output()); err != nil {
				return fmt.Errorf("Hugging Face model: %w", err)
			}
		}
		fmt.Fprintln(m.output(), "Starting the model service...")
		if _, err := m.command(ctx, m.composeArgs(p, "up", "--detach", "--pull", "never"), true); err != nil {
			return fmt.Errorf("Compose startup: %w", err)
		}
	}
	if err := m.waitReady(ctx, p); err != nil {
		return err
	}
	if p.Runtime == "ollama" {
		digest, err := m.modelDigest(ctx, p)
		if err != nil {
			return err
		}
		if digest == "" {
			fmt.Fprintf(m.output(), "Downloading model %s...\n", p.Model.Ref)
			if _, err := m.command(ctx, m.composeArgs(p, "exec", "-T", "model", "ollama", "pull", p.Model.Ref), true); err != nil {
				return fmt.Errorf("model download (rerun up to resume): %w", err)
			}
			digest, err = m.modelDigest(ctx, p)
			if err != nil {
				return err
			}
		}
		if digest == "" {
			return fmt.Errorf("download completed but the selected model is absent from /api/tags")
		}
		if s.ModelDigest != "" && s.ModelDigest != digest {
			return fmt.Errorf("selected model digest changed; create a new plan to approve new weights")
		}
		s.ModelDigest = digest
		if err := m.applyOllamaDefaults(ctx, p); err != nil {
			return err
		}
	}
	fmt.Fprintln(m.output(), "Checking a real model request...")
	smoke, err := m.smoke(ctx, p)
	if err != nil {
		return fmt.Errorf("model inference test failed (inspect 'logs', then rerun 'up'): %w", err)
	}
	if p.Runtime == "ollama" && p.Acceleration == "nvidia" {
		if err := m.verifyOllamaGPU(ctx, p); err != nil {
			return err
		}
	}
	image, err := m.image(ctx, p)
	if err != nil {
		return err
	}
	verifiedContainers, err := m.ownedResources(ctx, p)
	if err != nil {
		return err
	}
	if !anyRunning(verifiedContainers) {
		return fmt.Errorf("model container stopped before readiness could be recorded")
	}
	for _, container := range verifiedContainers {
		if container.State.Running && container.Image != image.ID {
			return fmt.Errorf("running container image differs from the selected runtime image; deployment is not marked ready")
		}
		if container.State.Running && p.Runtime == "llama.cpp" && p.Acceleration == "nvidia" && (!anyRunning(containers) || s.LastReadyAt == "") {
			if err := m.verifyLlamaOffload(ctx, container); err != nil {
				return err
			}
		}
	}
	// On an idempotent retry inspect the running image, not only its mutable tag.
	if s.ImageID != "" && s.ImageID != image.ID {
		return fmt.Errorf("runtime tag now resolves to another image; recorded running service is left unchanged")
	}
	s.ImageID, s.ImageDigests = image.ID, image.RepoDigests
	s.Smoke, s.Status = smoke, "ready"
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.LastReadyAt = s.UpdatedAt
	if err := saveState(m.Dir, s); err != nil {
		return fmt.Errorf("model is responding but recording deployment state failed: %w", err)
	}
	fmt.Fprintf(m.output(), "Ready: %s/v1 (model %s)\n", endpoint(p), APIModel(p))
	return nil
}

// Down stops only this owned project; downloaded model volumes are retained.
func (m Manager) Down(ctx context.Context) error {
	return m.stop(ctx, false)
}

// Archive removes the running service and retains the reviewed plan and weights.
func (m Manager) Archive(ctx context.Context) error {
	return m.stop(ctx, true)
}

func (m Manager) stop(ctx context.Context, archive bool) error {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return err
	}
	unlock, err := acquireLock(m.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	if archive {
		if archived, err := isArchived(m.Dir, p); err != nil || archived {
			return err
		}
	}
	ctx, cancel := m.operationContext(ctx, 2*time.Minute)
	defer cancel()
	s, err := loadState(m.Dir, p)
	if err != nil {
		return err
	}
	if err := m.ensureDocker(ctx, p); err != nil {
		return err
	}
	if _, err := m.ownedResources(ctx, p); err != nil {
		return err
	}
	if _, err := m.command(ctx, m.composeArgs(p, "down", "--timeout", "30"), true); err != nil {
		return err
	}
	s.Status = "stopped"
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := saveState(m.Dir, s); err != nil {
		return err
	}
	if archive {
		return exclusiveFile(filepath.Join(m.Dir, archiveFile), []byte(fingerprint(p)+"\n"))
	}
	if p.HFSource != nil {
		fmt.Fprintln(m.output(), "Stopped. Downloaded weights remain in the project model directory.")
	} else {
		fmt.Fprintln(m.output(), "Stopped. Downloaded weights remain in the project volume.")
	}
	return nil
}

func (m Manager) Logs(ctx context.Context, follow bool) error {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return err
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 30*time.Second)
	defer checkCancel()
	if err := m.ensureDocker(checkCtx, p); err != nil {
		return err
	}
	if _, err := m.ownedResources(checkCtx, p); err != nil {
		return err
	}
	checkCancel()
	if !follow {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	tail := "200"
	if m.LogAll {
		tail = "all"
	}
	args := []string{"logs", "--no-color", "--tail", tail}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, "model")
	_, err = m.command(ctx, m.composeArgs(p, args...), true)
	return err
}

// Smoke is read-only with respect to Docker configuration; inference may load
// the selected model into RAM/VRAM.
func (m Manager) Smoke(ctx context.Context) (domain.SmokeResult, error) {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return domain.SmokeResult{}, err
	}
	ctx, cancel := m.operationContext(ctx, 5*time.Minute)
	defer cancel()
	if err := m.ensureDocker(ctx, p); err != nil {
		return domain.SmokeResult{}, err
	}
	containers, err := m.ownedResources(ctx, p)
	if err != nil {
		return domain.SmokeResult{}, err
	}
	if !anyRunning(containers) {
		return domain.SmokeResult{}, fmt.Errorf("this project's model container is not running")
	}
	result, err := m.smoke(ctx, p)
	if err == nil && p.Runtime == "ollama" && p.Acceleration == "nvidia" {
		err = m.verifyOllamaGPU(ctx, p)
		if err != nil {
			result.OK = false
		}
	}
	return result, err
}

func (m Manager) Status(ctx context.Context) (domain.Status, error) {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return domain.Status{}, err
	}
	s := planStatus(p)
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := m.ensureDocker(ctx, p); err != nil {
		s.Error = err.Error()
		return s, nil
	}
	containers, err := m.ownedResources(ctx, p)
	if err != nil {
		s.Error = err.Error()
		return s, nil
	}
	data, err := m.command(ctx, m.composeArgs(p, "ps", "--all", "--format", "json"), false)
	if err != nil {
		s.Error = err.Error()
		return s, nil
	}
	s.Containers = strings.TrimSpace(data)
	if !anyRunning(containers) {
		return s, nil
	}
	m.probeModelStatus(ctx, p, &s)
	return s, nil
}

var llamaOffload = regexp.MustCompile(`offloaded [1-9][0-9]*/[1-9][0-9]* layers to GPU`)
var llamaQuietLog = regexp.MustCompile(`verbosity = [0-3]([^0-9]|$)`)

// Keep the match even when later output exceeds the command's diagnostic buffer.
type offloadLog struct {
	tail      []byte
	confirmed bool
	quiet     bool
}

func (l *offloadLog) Write(data []byte) (int, error) {
	size := len(data)
	if l.confirmed {
		return size, nil
	}
	l.tail = append(l.tail, data...)
	l.confirmed = llamaOffload.Match(l.tail)
	l.quiet = l.quiet || llamaQuietLog.Match(l.tail)
	if len(l.tail) > 256 {
		l.tail = append(l.tail[:0], l.tail[len(l.tail)-256:]...)
	}
	return size, nil
}

func (m Manager) verifyLlamaOffload(ctx context.Context, container containerInfo) error {
	started, err := time.Parse(time.RFC3339Nano, container.State.StartedAt)
	if err != nil || started.IsZero() || container.ID == "" {
		return fmt.Errorf("cannot verify llama.cpp GPU offload: current container start time is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stdout, stderr := &offloadLog{}, &offloadLog{}
	m.Out, m.Err = stdout, stderr
	logs, err := m.command(ctx, []string{"logs", "--since", container.State.StartedAt, container.ID}, true)
	if err != nil {
		return fmt.Errorf("check llama.cpp GPU offload: %w", err)
	}
	// The injected Docker runner used by orchestration tests returns stdout directly.
	_, _ = stdout.Write([]byte(logs))
	if !stdout.confirmed && !stderr.confirmed {
		if stdout.quiet || stderr.quiet {
			return fmt.Errorf("llama.cpp startup logging hides GPU offload details; create a new plan with LLAMA_ARG_LOG_VERBOSITY=4, then inspect logs with --all")
		}
		return fmt.Errorf("llama.cpp answered but GPU layer offload was not confirmed; inspect logs with --all")
	}
	return nil
}
