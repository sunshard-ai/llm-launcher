// Package hardware probes the local host. It never installs software or changes drivers.
package hardware

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"llmlauncher/internal/domain"
	"llmlauncher/internal/process"
)

const GiB = uint64(1024 * 1024 * 1024)

func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	cmd := process.Command(ctx, name, args...)
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

// Scan keeps unknown memory at zero: unknown is never a free resource budget.
func Scan(ctx context.Context, diskPath string) (domain.Hardware, error) {
	if diskPath == "" {
		diskPath = "."
	}
	abs, err := filepath.Abs(diskPath)
	if err != nil {
		return domain.Hardware{}, err
	}
	h := domain.Hardware{OS: runtime.GOOS, Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(), DiskPath: abs, GPUs: []domain.GPU{}}
	probePlatform(ctx, &h)
	gpus, gpuErr := probeNVIDIA(ctx)
	if gpuErr == nil && len(gpus) > 0 {
		others := h.GPUs[:0]
		for _, g := range h.GPUs {
			if g.Vendor != "nvidia" {
				others = append(others, g)
			}
		}
		h.GPUs = append(others, gpus...)
	}
	for _, g := range h.GPUs {
		if g.Vendor == "nvidia" && g.VRAMFreeBytes == 0 {
			h.Warnings = append(h.Warnings, "Свободная VRAM NVIDIA равна нулю или неизвестна; запуск на этой GPU не будет рекомендован.")
			break
		}
	}
	h.Docker = ProbeDocker(ctx)
	if h.Docker.Error != "" {
		h.Warnings = append(h.Warnings, h.Docker.Error)
	}
	if h.CPUName == "" {
		h.CPUName = "unknown"
	}
	if h.RAMAvailableBytes == 0 {
		h.Warnings = append(h.Warnings, "Свободную оперативную память не удалось определить или она исчерпана. Рекомендации пока недоступны.")
	}
	if h.DiskFreeBytes == 0 {
		h.Warnings = append(h.Warnings, "Свободное место на диске не удалось определить. Проверьте место в хранилище Docker отдельно.")
	}
	h.Warnings = append(h.Warnings, "Место на диске измерено для выбранного каталога; образы и тома Docker могут находиться на другом диске или внутри виртуального диска Docker Desktop.")
	return h, nil
}

func probeNVIDIA(ctx context.Context) ([]domain.GPU, error) {
	name := "nvidia-smi"
	if _, err := exec.LookPath(name); err != nil && runtime.GOOS == "linux" {
		if _, err := os.Stat("/usr/lib/wsl/lib/nvidia-smi"); err == nil {
			name = "/usr/lib/wsl/lib/nvidia-smi"
		}
	}
	out, err := command(ctx, name, "--query-gpu=index,uuid,name,memory.total,memory.free,compute_cap,driver_version", "--format=csv,noheader,nounits")
	if err == nil {
		return parseNVIDIA(string(out), true)
	}
	out, err = command(ctx, name, "--query-gpu=index,uuid,name,memory.total,memory.free,driver_version", "--format=csv,noheader,nounits")
	if err != nil {
		return nil, err
	}
	return parseNVIDIA(string(out), false)
}

func parseNVIDIA(s string, hasCapability bool) ([]domain.GPU, error) {
	r := csv.NewReader(strings.NewReader(s))
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var result []domain.GPU
	for _, row := range rows {
		want := 6
		if hasCapability {
			want = 7
		}
		if len(row) != want {
			return nil, fmt.Errorf("nvidia-smi вернула %d столбцов; ожидалось %d", len(row), want)
		}
		for i := range row {
			row[i] = strings.TrimSpace(row[i])
		}
		g := domain.GPU{Index: row[0], UUID: row[1], Name: row[2], Vendor: "nvidia", VRAMTotalBytes: mib(row[3]), VRAMFreeBytes: mib(row[4])}
		if hasCapability {
			g.ComputeCapability = row[5]
			g.DriverVersion = row[6]
		} else {
			g.DriverVersion = row[5]
		}
		if g.VRAMFreeBytes > g.VRAMTotalBytes {
			g.VRAMFreeBytes = 0
		}
		result = append(result, g)
	}
	return result, nil
}
func mib(s string) uint64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 10000000 {
		return 0
	}
	return uint64(f * 1024 * 1024)
}

// LocalEndpoint accepts local socket transports only. A scanned host cannot
// describe a remote Docker daemon even when the TCP endpoint is localhost.
func LocalEndpoint(endpoint string) bool {
	if strings.HasPrefix(endpoint, "unix:///") && len(endpoint) > len("unix:///") {
		return true
	}
	// A Windows named pipe can address another host via UNC. Only the canonical
	// local-machine transport is allowed; npipe:////server/pipe/... is remote.
	const localPipe = "npipe:////./pipe/"
	return strings.HasPrefix(endpoint, localPipe) && len(endpoint) > len(localPipe)
}

func ProbeDocker(ctx context.Context) domain.DockerInfo {
	d := domain.DockerInfo{}
	if _, err := exec.LookPath("docker"); err != nil {
		d.Error = "Docker CLI не установлен. Установите Docker Engine + Compose v2 (Linux) или Docker Desktop (Windows/macOS)."
		return d
	}
	d.Installed = true
	if out, err := command(ctx, "docker", "context", "show"); err == nil {
		d.Context = strings.TrimSpace(string(out))
	}
	if os.Getenv("DOCKER_CONTEXT") == "" {
		d.Endpoint = os.Getenv("DOCKER_HOST")
	}
	if d.Endpoint == "" && d.Context != "" {
		if out, err := command(ctx, "docker", "context", "inspect", d.Context, "--format", "{{json .Endpoints.docker.Host}}"); err == nil {
			_ = json.Unmarshal(out, &d.Endpoint)
		}
	}
	if d.Endpoint == "" {
		d.Error = "Не удалось подтвердить, что выбранный Docker endpoint локальный."
	}
	if d.Endpoint != "" && !LocalEndpoint(d.Endpoint) {
		d.Error = "Выбран удалённый/TCP контекст Docker. Локальное железо не описывает этот сервер; переключитесь на локальный контекст Docker."
	}
	if out, err := command(ctx, "docker", "compose", "version", "--short"); err == nil && strings.TrimSpace(string(out)) != "" {
		d.Compose = true
	}
	out, err := command(ctx, "docker", "info", "--format", "{{json .}}")
	if err != nil {
		if d.Error == "" {
			d.Error = "Docker daemon недоступен или не хватает прав. Запустите Docker и проверьте доступ вашей учётной записи."
		}
		return d
	}
	var raw struct {
		OSType       string
		Architecture string
		MemTotal     uint64
		NCPU         int
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		d.Error = "Команда docker info вернула некорректный JSON."
		return d
	}
	if raw.OSType == "" {
		d.Error = "Не удалось определить ОС Docker daemon. Запустите Docker и повторите сканирование."
		return d
	}
	d.Reachable = true
	d.ServerOS = raw.OSType
	d.Architecture = raw.Architecture
	d.MemoryBytes = raw.MemTotal
	d.CPUs = raw.NCPU
	if d.Error == "" && raw.OSType != "linux" {
		d.Error = "Переключите Docker в режим Linux containers."
	}
	if d.Error == "" && !d.Compose {
		d.Error = "Плагин Docker Compose v2 не установлен."
	}
	return d
}

// RequireLocalDocker distinguishes an unavailable daemon from container mode.
func RequireLocalDocker(d domain.DockerInfo) error {
	message := ""
	switch {
	case !d.Installed:
		message = "Docker CLI не установлен. Установите Docker Engine + Compose v2 (Linux) или Docker Desktop (Windows/macOS)."
	case !d.Reachable:
		message = d.Error
		if message == "" {
			message = "Docker daemon недоступен или не хватает прав. Запустите Docker и проверьте доступ вашей учётной записи."
		}
	case d.ServerOS == "":
		message = "Не удалось определить ОС Docker daemon. Запустите Docker и повторите сканирование."
	case d.ServerOS != "linux":
		message = "Переключите Docker в режим Linux containers."
	case !d.Compose:
		message = "Плагин Docker Compose v2 не установлен."
	case !LocalEndpoint(d.Endpoint):
		message = "Выбран удалённый/TCP контекст Docker. Локальное железо не описывает этот сервер; переключитесь на локальный контекст Docker."
	}
	if message != "" {
		return errors.New(message)
	}
	return nil
}
