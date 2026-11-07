// llm-launcher is a local, dependency-free planner and Docker Compose launcher.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/deploy"
	"llmlauncher/internal/domain"
	"llmlauncher/internal/hardware"
	"llmlauncher/internal/recommend"
)

var version = "0.3.4"
var desktopMode string
var hfToken string

type options struct {
	dir, task, runtime, model, catalog, inventory, gpu string
	port, context                                      int
	cpu, json, dryRun, follow                          bool
	timeout                                            time.Duration
	settingsJSON, hfURL, image                         string
	compose, interactive, infoOnly, allLogs            bool
	explicit                                           map[string]bool
}

func main() {
	hfToken = os.Getenv("LLM_LAUNCHER_HF_TOKEN")
	_ = os.Unsetenv("LLM_LAUNCHER_HF_TOKEN")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	args := os.Args[1:]
	if desktopMode == "1" {
		if len(args) == 0 {
			runDesktop(ctx)
			return
		}
		if args[0] == "--desktop-child" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			defer cancel()
			go func() {
				_, _ = io.Copy(io.Discard, os.Stdin)
				cancel()
			}()
			args = args[1:]
		}
	}
	if err := run(ctx, args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}
}

func parseOptions(command string, args []string, errOut io.Writer) (options, error) {
	var o options
	allowed := allowedFlags(command)
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(errOut)
	if command == "list" {
		f.StringVar(&o.dir, "dir", ".", "directory containing deployments")
	} else {
		f.StringVar(&o.dir, "dir", "./llm-model", "directory for a single deployment")
	}
	taskHelp := "task (chat, code, rag, or embeddings)"
	runtimeHelp := "runtime (ollama, vllm, or llama.cpp). Leave empty for automatic selection"
	contextHelp := "context length in tokens (512–32768, subject to memory checks)"
	if command == "hf-plan" {
		taskHelp = "text task (chat, code, or rag)"
		runtimeHelp = "runtime (llama.cpp on CPU by default, or vllm on NVIDIA)"
		contextHelp = "context length in tokens (512–4096 for Hugging Face imports)"
	}
	f.StringVar(&o.task, "task", "chat", taskHelp)
	f.StringVar(&o.runtime, "runtime", "", runtimeHelp)
	f.StringVar(&o.model, "model", "", "model ID from catalog or recommend")
	f.StringVar(&o.catalog, "catalog", "", "custom JSON catalog. Defaults to the cached or bundled catalog")
	f.StringVar(&o.inventory, "inventory", "", "hardware snapshot in JSON for previews. Deployments require a live scan")
	f.StringVar(&o.gpu, "gpu", "", "index or UUID of one NVIDIA GPU")
	f.IntVar(&o.port, "port", 0, "local API port. 0 uses the runtime default (Ollama 11434, vLLM 8000, llama.cpp 8080)")
	f.IntVar(&o.context, "context", 4096, contextHelp)
	f.BoolVar(&o.cpu, "cpu", false, "select a CPU deployment")
	f.BoolVar(&o.json, "json", false, "output machine-readable JSON")
	f.BoolVar(&o.interactive, "interactive", false, "start interactive model selection")
	f.BoolVar(&o.infoOnly, "info", false, "show hardware inventory only")
	f.BoolVar(&o.dryRun, "dry-run", false, "save a plan without downloading or starting the model")
	f.BoolVar(&o.follow, "follow", false, "stream logs until Ctrl+C")
	f.BoolVar(&o.allLogs, "all", false, "include all retained container logs")
	f.BoolVar(&o.compose, "compose", false, "show the validated Compose configuration")
	f.StringVar(&o.settingsJSON, "settings-json", "", "generation settings and non-secret environment variables in JSON")
	f.StringVar(&o.image, "image", "", "official vLLM image tag or digest. The latest tag resolves to a pinned digest")
	f.StringVar(&o.hfURL, "hf-url", "", "Hugging Face HTTPS URL for a GGUF file (llama.cpp) or a Safetensors repository (vLLM)")
	f.DurationVar(&o.timeout, "timeout", 30*time.Minute, "operation timeout, for example 45m")
	f.Usage = func() {
		fmt.Fprintf(errOut, "Usage: llm-launcher %s", command)
		if len(allowed) == 0 {
			fmt.Fprintln(errOut, "\nThis command takes no options.")
			return
		}
		fmt.Fprintln(errOut, " [options]\n\nOptions:")
		f.VisitAll(func(v *flag.Flag) {
			if allowed[v.Name] {
				fmt.Fprintf(errOut, "  --%s\t%s (default %q)\n", v.Name, v.Usage, v.DefValue)
			}
		})
	}
	if err := f.Parse(args); err != nil {
		return o, err
	}
	o.explicit = make(map[string]bool)
	var unsupported string
	f.Visit(func(v *flag.Flag) {
		o.explicit[v.Name] = true
		if !allowed[v.Name] && unsupported == "" {
			unsupported = v.Name
		}
	})
	if unsupported != "" {
		return o, fmt.Errorf("флаг --%s не применяется к %s; используйте %s --help. Настройки существующего плана не меняются командами управления", unsupported, command, command)
	}
	if f.NArg() != 0 {
		return o, fmt.Errorf("неизвестные аргументы: %s", strings.Join(f.Args(), " "))
	}
	if (command == "delete" || command == "restore") && !o.explicit["dir"] {
		return o, fmt.Errorf("для %s явно укажите --dir с каталогом развёртывания", command)
	}
	if o.cpu && o.gpu != "" {
		return o, errors.New("--cpu и --gpu нельзя выбирать одновременно")
	}
	if o.context < 512 || o.context > 32768 {
		return o, errors.New("--context должен быть в диапазоне 512..32768")
	}
	if o.port < 0 || o.port > 65535 || (o.port > 0 && o.port < 1024) {
		return o, errors.New("--port должен быть 1024..65535 либо 0 для автоматического выбора")
	}
	if o.timeout <= 0 {
		return o, errors.New("--timeout должен быть положительным")
	}
	if o.runtime != "" && o.runtime != "ollama" && o.runtime != "vllm" && o.runtime != "llama.cpp" {
		return o, errors.New("--runtime: используйте ollama, vllm или llama.cpp")
	}
	return o, nil
}

func allowedFlags(command string) map[string]bool {
	byCommand := map[string]string{
		"scan": "dir json inventory interactive info dry-run catalog gpu cpu context port timeout", "doctor": "dir json", "catalog": "catalog json",
		"recommend": "dir task runtime catalog inventory gpu cpu context json image",
		"plan":      "dir task runtime model catalog inventory gpu cpu context port json settings-json image",
		"hf-plan":   "dir hf-url task context port json timeout settings-json runtime gpu image", "inspect": "dir compose json",
		"wizard": "dir catalog inventory gpu cpu context port dry-run timeout",
		"up":     "dir timeout", "status": "dir json", "logs": "dir follow all",
		"test": "dir json timeout", "down": "dir timeout", "gpu-check": "dir timeout",
		"list": "dir json", "delete": "dir timeout", "restore": "dir",
	}
	r := make(map[string]bool)
	for _, name := range strings.Fields(byCommand[command]) {
		r[name] = true
	}
	return r
}

func run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	command := "wizard"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	if command == "run" {
		command = "up"
	}
	switch command {
	case "help", "--help", "-h":
		printHelp(out)
		return nil
	case "version", "--version":
		fmt.Fprintln(out, "llm-launcher", version)
		return nil
	}
	known := map[string]bool{"scan": true, "doctor": true, "catalog": true, "recommend": true, "plan": true, "wizard": true, "up": true, "status": true, "logs": true, "test": true, "down": true, "gpu-check": true, "list": true, "delete": true, "restore": true, "hf-plan": true, "inspect": true, "update": true}
	if !known[command] {
		return fmt.Errorf("неизвестная команда %q; используйте help", command)
	}
	o, err := parseOptions(command, args, errOut)
	if err != nil {
		return err
	}
	if o.inventory != "" && command != "scan" && command != "recommend" && command != "plan" && command != "wizard" {
		return errors.New("--inventory разрешён только для scan/recommend/plan/wizard; реальное развёртывание всегда проверяет текущее железо")
	}
	if command == "update" {
		c, changed, err := catalog.Update(ctx)
		if err != nil {
			return fmt.Errorf("каталог не обновлён, сохранена текущая версия: %w", err)
		}
		if changed {
			fmt.Fprintf(out, "Каталог обновлён (%s), профилей: %d.\n", c.Updated, len(c.Models))
		} else {
			fmt.Fprintf(out, "Каталог актуален (%s), профилей: %d.\n", c.Updated, len(c.Models))
		}
		return nil
	}
	if command == "catalog" {
		cat, err := catalog.Load(o.catalog)
		if err != nil {
			return err
		}
		if o.json {
			return writeJSON(out, cat)
		}
		fmt.Fprintf(out, "Каталог %s (schema %d)\n", cat.Updated, cat.SchemaVersion)
		for _, m := range cat.Models {
			fmt.Fprintf(out, "%s | %s | %s | %s | %s\n", m.ID, m.Runtime, strings.Join(m.Tasks, ","), m.Ref, m.License)
		}
		return nil
	}
	if command == "wizard" {
		return wizard(ctx, o, in, out, errOut)
	}
	if command == "inspect" {
		p, err := deploy.ReadPlan(o.dir)
		if err != nil {
			return err
		}
		if o.compose {
			rendered, err := deploy.Render(p)
			if err != nil {
				return err
			}
			_, err = out.Write(rendered)
			return err
		}
		if o.json {
			return writeJSON(out, p)
		}
		printPlan(out, o.dir, p)
		return nil
	}
	if o.image != "" {
		if o.runtime != "vllm" {
			return errors.New("--image требует --runtime vllm")
		}
		o.image, err = catalog.ResolveVLLMImage(ctx, o.image)
		if err != nil {
			return err
		}
	}
	if command == "hf-plan" {
		return planHF(ctx, o, out)
	}
	if command == "scan" {
		if o.interactive && (o.json || o.infoOnly) {
			return errors.New("--interactive несовместим с --json и --info")
		}
		if o.interactive || (!o.json && !o.infoOnly && o.inventory == "" && terminalScan(in, out)) {
			return quickScan(ctx, o, in, out, errOut)
		}
		if o.dryRun {
			return errors.New("для пробного подбора используйте scan --interactive --dry-run")
		}
	}
	if command == "scan" || command == "doctor" || command == "recommend" || command == "plan" {
		hw, err := scan(ctx, o)
		if err != nil {
			return err
		}
		if command == "scan" || command == "doctor" {
			if o.json {
				return writeJSON(out, hw)
			}
			printHardware(out, hw)
			if command == "doctor" {
				printDoctor(out, hw)
			}
			return nil
		}
		cat, err := catalog.Load(o.catalog)
		if err != nil {
			return err
		}
		if o.image != "" {
			cat.Runtimes["vllm"] = o.image
		}
		recs, notes, err := choices(hw, cat, o)
		if err != nil {
			return err
		}
		if command == "recommend" {
			if o.json {
				if err := writeJSON(out, struct {
					Recommendations []domain.Recommendation `json:"recommendations"`
					Notes           []string                `json:"notes"`
				}{recs, notes}); err != nil {
					return err
				}
			} else {
				printRecommendations(out, recs, notes)
			}
			if len(recs) == 0 {
				return errors.New("для выбранных условий нет подходящего профиля; проверьте свободную RAM/VRAM или попробуйте --cpu")
			}
			return nil
		}
		r, err := chooseModel(recs, o.model)
		if err != nil {
			return err
		}
		p, err := makePlan(hw, cat, r, o)
		if err != nil {
			return err
		}
		if err := deploy.WritePlan(o.dir, p); err != nil {
			return err
		}
		if o.json {
			return writeJSON(out, p)
		}
		printPlan(out, o.dir, p)
		if o.inventory != "" {
			fmt.Fprintln(out, "Демонстрационный план сохранён. Для реального запуска создайте новый план без --inventory.")
		} else {
			fmt.Fprintln(out, "План сохранён. Запуск: llm-launcher up --dir", strconv.Quote(o.dir))
		}
		return nil
	}
	m := deploy.Manager{Dir: o.dir, Out: out, Err: errOut, Timeout: o.timeout, HFToken: hfToken, LogAll: o.allLogs}
	switch command {
	case "list":
		projects, err := deploy.List(o.dir)
		if err != nil {
			return err
		}
		if o.json {
			return writeJSON(out, projects)
		}
		fmt.Fprintln(out, "Сохранённое состояние. Для проверки запущенного сервиса используйте status.")
		for _, p := range projects {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", p.State, p.Runtime, p.Model, p.Dir, p.Error)
		}
	case "delete":
		if err := m.Archive(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "Проект остановлен и архивирован. План и веса сохранены. Вернуть проект: restore.")
	case "restore":
		if err := deploy.Restore(o.dir); err != nil {
			return err
		}
		fmt.Fprintln(out, "План доступен для запуска через up.")
	case "up":
		if err := m.Up(ctx); err != nil {
			return err
		}
		p, err := deploy.ReadPlan(o.dir)
		if err != nil {
			return err
		}
		printEndpoints(out, p)
	case "status":
		if !o.explicit["dir"] {
			rows, err := m.RunningStatus(ctx)
			if err != nil {
				return err
			}
			if o.json {
				return writeJSON(out, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(out, "В текущем Docker нет запущенных моделей LLM Launcher.")
			}
			for _, row := range rows {
				fmt.Fprintf(out, "%s | %s | %s | %s | API: %t | модель загружена: %t\n  %s\n", row.Project, row.Runtime, row.Model, row.Endpoint, row.Reachable, row.ModelLoaded, row.Dir)
				if row.Error != "" {
					fmt.Fprintf(out, "  Docker: %s | %s\n", row.DockerState, row.Error)
				}
			}
			return nil
		}
		s, err := m.Status(ctx)
		if o.json {
			if e := writeJSON(out, s); e != nil {
				return e
			}
		} else {
			printStatus(out, s)
		}
		return err
	case "logs":
		return m.Logs(ctx, o.follow)
	case "down":
		if err := m.Down(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "Контейнеры проекта остановлены. Скачанные веса сохранены.")
	case "test":
		r, err := m.Smoke(ctx)
		if o.json {
			if e := writeJSON(out, r); e != nil {
				return e
			}
		} else {
			fmt.Fprintf(out, "Тест: ok=%t, тип=%s, %d мс\n%s\n", r.OK, r.Kind, r.DurationMS, r.Output)
		}
		return err
	case "gpu-check":
		return m.GPUCheck(ctx)
	}
	return nil
}

func scan(ctx context.Context, o options) (domain.Hardware, error) {
	if o.inventory == "" {
		return hardware.Scan(ctx, o.dir)
	}
	f, err := os.Open(o.inventory)
	if err != nil {
		return domain.Hardware{}, err
	}
	defer f.Close()
	var hw domain.Hardware
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&hw); err != nil {
		return hw, fmt.Errorf("inventory: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return hw, errors.New("inventory должен содержать ровно один JSON объект")
	}
	if hw.OS == "" || hw.Arch == "" || hw.RAMTotalBytes == 0 {
		return hw, errors.New("inventory: обязательны os, arch и ram_total_bytes")
	}
	hw.Warnings = append(hw.Warnings, "DEMO_INVENTORY: это пример, а не результат проверки текущей системы; перед up требуется реальный scan.")
	return hw, nil
}

func choices(hw domain.Hardware, cat domain.Catalog, o options) ([]domain.Recommendation, []string, error) {
	selected := hw
	if o.cpu {
		selected.GPUs = nil
	}
	if o.gpu != "" {
		selected.GPUs = nil
		for _, g := range hw.GPUs {
			if g.Index == o.gpu || g.UUID == o.gpu {
				selected.GPUs = append(selected.GPUs, g)
			}
		}
		if len(selected.GPUs) != 1 {
			return nil, nil, fmt.Errorf("GPU %q не найдена однозначно; выполните scan", o.gpu)
		}
	}
	// The base catalog estimates use 4K context. Scale conservatively for longer
	// contexts, then restore the original estimates in the persisted plan.
	adjusted := cat
	adjusted.Models = append([]domain.Model(nil), cat.Models...)
	factor := recommend.ContextScale(o.context)
	for i := range adjusted.Models {
		adjusted.Models[i].MinRAMGiB *= factor
		adjusted.Models[i].MinVRAMGiB *= factor
	}
	recs, notes, err := recommend.Recommend(selected, adjusted, o.task, o.runtime)
	if err != nil {
		return nil, notes, err
	}
	byID := make(map[string]domain.Model)
	for _, m := range cat.Models {
		byID[m.ID] = m
	}
	filtered := make([]domain.Recommendation, 0, len(recs))
	for _, r := range recs {
		r.Model = byID[r.Model.ID]
		if r.Model.Context < o.context {
			continue
		}
		if o.gpu != "" && r.Acceleration != "nvidia" {
			continue
		}
		filtered = append(filtered, r)
	}
	if factor > 1 {
		notes = append(notes, fmt.Sprintf("Для контекста %d оценка памяти умножена на %.1f; это консервативный фильтр, не измерение.", o.context, factor))
	}
	return filtered, notes, nil
}

func chooseModel(recs []domain.Recommendation, id string) (domain.Recommendation, error) {
	if len(recs) == 0 {
		return domain.Recommendation{}, errors.New("нет подходящих моделей для выбранного железа, задачи, движка и контекста")
	}
	if id == "" {
		return recs[0], nil
	}
	for _, r := range recs {
		if r.Model.ID == id {
			return r, nil
		}
	}
	return domain.Recommendation{}, fmt.Errorf("модель %q не проходит подбор для этих условий; посмотрите recommend и catalog", id)
}

func makePlan(hw domain.Hardware, cat domain.Catalog, r domain.Recommendation, o options) (domain.Plan, error) {
	return makePlanWithSource(hw, cat, r, o, cat.Profiles[r.Model.ID].Artifact)
}

func makePlanWithSource(hw domain.Hardware, cat domain.Catalog, r domain.Recommendation, o options, source *domain.HFSource) (domain.Plan, error) {
	abs, err := filepath.Abs(o.dir)
	if err != nil {
		return domain.Plan{}, err
	}
	sum := sha256.Sum256([]byte(abs))
	name := strings.ToLower(filepath.Base(abs))
	var b strings.Builder
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			b.WriteRune(c)
		}
	}
	name = b.String()
	if len(name) > 24 {
		name = name[:24]
	}
	if name == "" {
		name = "model"
	}
	port := o.port
	if port == 0 {
		port = 11434
		switch r.Model.Runtime {
		case "vllm":
			port = 8000
		case "llama.cpp":
			port = 8080
		}
	}
	p := domain.Plan{SchemaVersion: 1, Project: fmt.Sprintf("llm-%s-%x", name, sum[:4]), CreatedAt: time.Now().UTC().Format(time.RFC3339), Runtime: r.Model.Runtime, Image: cat.Runtimes[r.Model.Runtime], Task: o.task, Acceleration: r.Acceleration, Model: r.Model, Context: o.context, Parallel: 1, Port: port, Host: "127.0.0.1", GPUUtilization: 0.85, Hardware: hw}
	if p.Runtime == "llama.cpp" && p.Acceleration == "cpu" {
		p.Image = catalog.LlamaCPUImage
	}
	if r.GPU != nil {
		id := r.GPU.UUID
		if id == "" {
			id = r.GPU.Index
		}
		p.DeviceIDs = []string{id}
		if r.GPU.VRAMTotalBytes > 0 {
			p.GPUUtilization = math.Min(0.8, recommend.VRAMBudgetGiB(*r.GPU)/gib(r.GPU.VRAMTotalBytes))
		}
	}
	advanced, err := parseAdvanced(o.settingsJSON)
	if err != nil {
		return p, err
	}
	advanced = catalog.ApplyDefaults(p.Model, advanced)
	if p.Runtime == "llama.cpp" && p.Acceleration == "nvidia" {
		if advanced == nil {
			advanced = &domain.AdvancedSettings{}
		}
		if advanced.Environment == nil {
			advanced.Environment = make(map[string]string)
		}
		if _, set := advanced.Environment["LLAMA_ARG_LOG_VERBOSITY"]; !set {
			advanced.Environment["LLAMA_ARG_LOG_VERBOSITY"] = "4"
		}
	}
	if p.Runtime == "vllm" {
		if advanced == nil {
			advanced = &domain.AdvancedSettings{}
		}
		if advanced.DType == "" {
			advanced.DType = "auto"
			if strings.EqualFold(p.Model.Quantization, "AWQ") {
				advanced.DType = "half"
			}
		}
	}
	if advanced != nil && p.Runtime == "vllm" && r.GPU != nil {
		if advanced.DType == "bfloat16" {
			cc, err := strconv.ParseFloat(r.GPU.ComputeCapability, 64)
			if err != nil || cc < 8 {
				return p, fmt.Errorf("vLLM bfloat16 требует NVIDIA compute capability 8.0 или выше")
			}
		}
		if fraction := advanced.GPUMemoryUtilization; fraction != nil {
			allocated := gib(r.GPU.VRAMTotalBytes) * *fraction
			required := p.Model.MinVRAMGiB * recommend.ContextScale(p.Context)
			if allocated < required || allocated > gib(r.GPU.VRAMFreeBytes)-0.5 {
				return p, fmt.Errorf("доля VRAM даёт %.1f ГиБ; ориентир модели %.1f ГиБ, свободно %.1f ГиБ до резерва", allocated, required, gib(r.GPU.VRAMFreeBytes))
			}
			p.GPUUtilization = *fraction
		}
	}
	if advanced != nil || source != nil {
		p.SchemaVersion, p.Advanced, p.HFSource = 2, advanced, source
	}
	if err := deploy.Validate(p); err != nil {
		return p, err
	}
	return p, nil
}

func writeJSON(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}
func gib(v uint64) float64 { return float64(v) / (1 << 30) }

func printHardware(w io.Writer, h domain.Hardware) {
	fmt.Fprintf(w, "\nСистема: %s / %s / %s (WSL=%t)\nCPU: %s — %d логических CPU\nRAM: %.1f GiB всего, %.1f GiB доступно\nДиск: %.1f GiB свободно (%s)\n", h.OS, h.Distro, h.Arch, h.WSL, h.CPUName, h.LogicalCPUs, gib(h.RAMTotalBytes), gib(h.RAMAvailableBytes), gib(h.DiskFreeBytes), h.DiskPath)
	if len(h.GPUs) == 0 {
		fmt.Fprintln(w, "GPU: поддерживаемая карта не обнаружена; доступен подбор для CPU.")
	}
	for _, g := range h.GPUs {
		fmt.Fprintf(w, "GPU %s: %s [%s], VRAM %.1f GiB всего / %.1f GiB свободно, compute=%s\n", g.Index, g.Name, g.Vendor, gib(g.VRAMTotalBytes), gib(g.VRAMFreeBytes), g.ComputeCapability)
	}
	fmt.Fprintf(w, "Docker: установлен=%t, доступен=%t, Compose=%t, ОС контейнеров=%s, RAM Docker=%.1f GiB\n", h.Docker.Installed, h.Docker.Reachable, h.Docker.Compose, h.Docker.ServerOS, gib(h.Docker.MemoryBytes))
	if h.Docker.Error != "" {
		fmt.Fprintln(w, "Docker:", h.Docker.Error)
	}
	for _, n := range h.Warnings {
		fmt.Fprintln(w, "Примечание:", n)
	}
}

func printDoctor(w io.Writer, h domain.Hardware) {
	fmt.Fprintln(w, "\nСледующий шаг:")
	if !h.Docker.Installed {
		fmt.Fprintln(w, "Установите Docker Engine + Compose plugin (Linux) или Docker Desktop с WSL2 (Windows). Инструкции: docs/TROUBLESHOOTING.md")
		return
	}
	if !h.Docker.Reachable {
		fmt.Fprintln(w, "Запустите Docker и проверьте доступ текущего пользователя: docker info")
		return
	}
	if !h.Docker.Compose {
		fmt.Fprintln(w, "Установите Compose v2 plugin. Проверка: docker compose version")
		return
	}
	if h.Docker.ServerOS != "linux" {
		fmt.Fprintln(w, "Переключите Docker Desktop в режим Linux containers.")
		return
	}
	fmt.Fprintln(w, "Можно подготовить план через wizard. Реальная проверка GPU и запрос к модели выполняются командой up.")
}

func printRecommendations(w io.Writer, recs []domain.Recommendation, notes []string) {
	for i, r := range recs {
		fmt.Fprintf(w, "%d) %s | %s / %s | %s\n   Лицензия: %s\n", i+1, r.Model.ID, r.Model.Runtime, r.Acceleration, r.Model.Ref, r.Model.License)
		for _, n := range r.Notes {
			fmt.Fprintln(w, "  ", n)
		}
	}
	for _, n := range notes {
		fmt.Fprintln(w, "Примечание:", n)
	}
	fmt.Fprintln(w, "Оценки приведены для выбранного контекста и одного параллельного запроса. Финальная готовность подтверждается тестом модели.")
}

func printPlan(w io.Writer, dir string, p domain.Plan) {
	fmt.Fprintf(w, "\nПлан: %s\nПроект: %s\nДвижок: %s\nОбраз: %s\nМодель: %s (%s)\nУскорение: %s %s\nКонтекст: %d токенов, одновременных запросов: %d\nЛицензия модели: %s\nКарточка модели: %s\n", dir, p.Project, p.Runtime, p.Image, p.Model.Name, p.Model.Ref, p.Acceleration, strings.Join(p.DeviceIDs, ","), p.Context, p.Parallel, p.Model.License, p.Model.URL)
	if p.HFSource != nil {
		fmt.Fprintf(w, "Ревизия HF: %s\n", p.HFSource.Revision)
		if p.HFSource.File != "" {
			fmt.Fprintf(w, "Файл: %s\n", p.HFSource.File)
		} else {
			fmt.Fprintf(w, "Файлы safetensors и конфигурации: %d\n", len(p.HFSource.Files))
		}
		fmt.Fprintf(w, "Размер загрузки: %.2f GiB\n", gib(uint64(p.HFSource.Bytes)))
	}
	if p.Advanced != nil {
		settings, _ := json.MarshalIndent(p.Advanced, "", "  ")
		fmt.Fprintf(w, "Проф. настройки:\n%s\n", settings)
	}
	fmt.Fprintf(w, "API после успешного запуска: http://127.0.0.1:%d/v1\n", p.Port)
}

func printEndpoints(w io.Writer, p domain.Plan) {
	model := deploy.APIModel(p)
	fmt.Fprintf(w, "\nМодель ответила на тестовый запрос.\nOpenAI base_url: http://127.0.0.1:%d/v1\nmodel: %s\nGET http://127.0.0.1:%d/v1/models\n", p.Port, model, p.Port)
	if p.Task == "embeddings" {
		fmt.Fprintf(w, "POST http://127.0.0.1:%d/v1/embeddings\n", p.Port)
	} else {
		fmt.Fprintf(w, "POST http://127.0.0.1:%d/v1/chat/completions\n", p.Port)
	}
	fmt.Fprintln(w, "API доступен локально; клиентам, требующим ключ, можно передать api_key=local.")
}

func printStatus(w io.Writer, s domain.Status) {
	fmt.Fprintf(w, "Проект: %s\nДвижок: %s\nEndpoint: %s\nМодель: %s\nAPI отвечает: %t\nModelLoaded: %t\n%s\n", s.Project, s.Runtime, s.Endpoint, s.Model, s.Reachable, s.ModelLoaded, s.Containers)
	if s.Error != "" {
		fmt.Fprintln(w, s.Error)
	}
	fmt.Fprintln(w, "Проверка генерации/эмбеддингов: команда test.")
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `llm-launcher %s starts local models with Docker Compose.

Usage: llm-launcher [command] [options]

Commands:
  wizard       Select a model interactively and review the plan (also the default).
  scan         Scan hardware. In a Linux terminal, open the interactive picker.
               Use --info or --json for inventory only.
  doctor       Diagnose the environment without downloading anything.
  update       Check for updates to the signed model catalog.
  catalog      Show the current model catalog.
  recommend    Find compatible models for chat, code, RAG, or embeddings.
  plan         Save a deployment plan from the catalog.
  hf-plan      Save a plan for a Hugging Face GGUF file (llama.cpp on CPU)
               or a Safetensors repository (vLLM on NVIDIA).
  inspect      Show a saved plan or its Compose configuration.
  list         Find saved deployments and show their recorded state.
  up, run      Verify, download, start, and test a deployment.
  status       Check running deployments or one deployment selected with --dir.
  test         Send a chat or embedding request to the model API.
  logs         Show container logs. Use --all for startup logs or --follow to stream.
  down         Stop a deployment and keep its downloaded weights.
  delete       Stop and archive a deployment, keeping the plan and weights.
  restore      Restore an archived deployment for a later launch.
  gpu-check    Check NVIDIA access from a deployment's container.
  help         Show this command list.
  version      Show the launcher version.

Use llm-launcher COMMAND --help for command options. Put flags after the command.
Use --dir to select a deployment directory. The wizard supports --dry-run.
Hardware snapshots (--inventory) are for scan, recommend, plan, and wizard previews.
Set LLM_LAUNCHER_HF_TOKEN for hf-plan and up when using restricted Hugging Face weights.

The wizard checks for catalog updates in the background at most once per day.
The rag task selects a model for generating answers in a RAG app. Document
indexing and retrieval are handled by the app. The Windows desktop wizard
is available as LLM-Launcher-Windows-x64.exe.
`, version)
}

// Use a stable ordering even if a custom catalog lists runtimes differently.
func runtimes(recs []domain.Recommendation) []string {
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Model.Runtime] = true
	}
	var result []string
	for k := range seen {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
