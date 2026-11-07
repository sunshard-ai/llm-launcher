// Package recommend implements a conservative, explainable fit filter. Scores
// order suitable candidates; they do not promise measured model quality or speed.
package recommend

import (
	"fmt"
	"llmlauncher/internal/catalog"
	"llmlauncher/internal/domain"
	"llmlauncher/internal/hardware"
	"math"
	"sort"
	"strings"
)

const gib = 1024.0 * 1024 * 1024

// ContextScale is the conservative estimate for a single sequence, based on
// the catalog's 4096-token baseline. It is shared by planning and preflight.
func ContextScale(tokens int) float64 {
	return math.Max(1, float64(tokens)/4096)
}

func RAMBudgetGiB(h domain.Hardware) float64 {
	total := float64(h.RAMTotalBytes) / gib
	available := float64(h.RAMAvailableBytes) / gib
	if total <= 0 || available <= 0 {
		return 0
	}
	available = math.Min(available, total)
	budget := math.Max(0, available-math.Max(2, total*.1))
	if h.Docker.Reachable {
		d := float64(h.Docker.MemoryBytes) / gib
		if d <= 0 {
			return 0
		}
		budget = math.Min(budget, math.Max(0, d-math.Max(1, d*.1)))
	}
	return budget
}
func VRAMBudgetGiB(g domain.GPU) float64 {
	total := float64(g.VRAMTotalBytes) / gib
	free := float64(g.VRAMFreeBytes) / gib
	if total <= 0 || free <= 0 || free > total {
		return 0
	}
	return math.Max(0, math.Min(.8*total, free-1))
}
func RequiredRAMGiB(m domain.Model, _ string) float64 { return m.MinRAMGiB }
func RequiredDiskGiB(m domain.Model) float64 {
	overhead := 4.0
	if m.Runtime == "vllm" {
		overhead = 20
	}
	return m.DiskGiB + overhead
}
func hasTask(m domain.Model, t string) bool {
	for _, v := range m.Tasks {
		if v == t {
			return true
		}
	}
	return false
}

func Recommend(h domain.Hardware, c domain.Catalog, task, runtimeFilter string) ([]domain.Recommendation, []string, error) {
	if err := catalog.Validate(c); err != nil {
		return nil, nil, err
	}
	if task != "chat" && task != "code" && task != "rag" && task != "embeddings" {
		return nil, nil, fmt.Errorf("неизвестная задача %q; доступны chat, code, rag, embeddings", task)
	}
	if runtimeFilter == "" {
		runtimeFilter = "auto"
	}
	if runtimeFilter != "auto" && runtimeFilter != "ollama" && runtimeFilter != "vllm" && runtimeFilter != "llama.cpp" {
		return nil, nil, fmt.Errorf("неизвестный движок %q; доступны auto, ollama, vllm, llama.cpp", runtimeFilter)
	}
	var result []domain.Recommendation
	warnings := []string{"Базовые оценки памяти рассчитаны на 4096 токенов и один запрос; для большего контекста мастер увеличивает бюджет. Итоговая проверка — настоящий запрос к модели."}
	if task == "rag" {
		warnings = append(warnings, "Задача RAG выбирает модель для генерации ответа; загрузка документов, поиск и векторная БД в этот MVP не входят.")
	}
	if h.OS != "linux" && h.OS != "windows" && h.OS != "darwin" {
		return result, warnings, fmt.Errorf("операционная система %q пока не поддерживается", h.OS)
	}
	if h.Arch != "amd64" && h.Arch != "arm64" {
		return result, warnings, fmt.Errorf("архитектура %q пока не поддерживается", h.Arch)
	}
	if h.Docker.Endpoint != "" && !hardware.LocalEndpoint(h.Docker.Endpoint) {
		return result, warnings, fmt.Errorf("выбран удалённый Docker context: переключитесь на локальный endpoint перед подбором модели")
	}
	if h.Docker.Reachable && h.Docker.ServerOS != "linux" {
		return result, warnings, hardware.RequireLocalDocker(h.Docker)
	}
	if !h.Docker.Reachable || !h.Docker.Compose {
		warnings = append(warnings, "Проверка Docker/Compose не завершена: план можно сохранить, но для запуска нужен работающий локальный Docker daemon.")
	}
	if h.DiskFreeBytes == 0 {
		warnings = append(warnings, "Свободное место неизвестно. Перед загрузкой модели проверьте место в хранилище Docker.")
	}
	ram := RAMBudgetGiB(h)
	if ram <= 0 {
		warnings = append(warnings, "После резервирования системной памяти не осталось подтверждённого бюджета RAM для модели.")
		return result, warnings, nil
	}
	nvidiaAllowed := (h.OS == "linux" || h.OS == "windows") && h.Arch == "amd64"
	vllmAllowed := VLLMPlatformSupported(h)
	var gpu, llamaGPU *domain.GPU
	var vllmGPUs []domain.GPU
	if nvidiaAllowed {
		for _, candidate := range h.GPUs {
			if candidate.Vendor != "nvidia" {
				continue
			}
			if VRAMBudgetGiB(candidate) <= 0 {
				warnings = append(warnings, fmt.Sprintf("GPU %s: свободная VRAM неизвестна или после резерва не остаётся памяти; эта GPU исключена.", candidate.Name))
				continue
			}
			if runtimeFilter == "auto" || runtimeFilter == "ollama" {
				if ok, reason := OllamaGPUSupported(candidate); !ok {
					warnings = append(warnings, fmt.Sprintf("GPU %s исключена из вариантов Ollama: %s.", candidate.Name, reason))
				} else if gpu == nil || VRAMBudgetGiB(candidate) > VRAMBudgetGiB(*gpu) {
					copy := candidate
					gpu = &copy
				}
			}
			if runtimeFilter == "auto" || runtimeFilter == "llama.cpp" {
				if ok, _ := LlamaGPUSupported(candidate); ok && (llamaGPU == nil || VRAMBudgetGiB(candidate) > VRAMBudgetGiB(*llamaGPU)) {
					copy := candidate
					llamaGPU = &copy
				}
			}
			if vllmAllowed && (runtimeFilter == "auto" || runtimeFilter == "vllm") {
				if ok, reason := VLLMGPUSupported(candidate, c.Runtimes["vllm"]); !ok {
					warnings = append(warnings, fmt.Sprintf("GPU %s исключена из вариантов vLLM: %s.", candidate.Name, reason))
				} else {
					vllmGPUs = append(vllmGPUs, candidate)
				}
			}
		}
	}
	if len(h.GPUs) > 1 {
		warnings = append(warnings, "Для каждого движка выбирается одна совместимая GPU с наибольшим бюджетом свободной памяти; VRAM разных карт не суммируется.")
	}
	if h.OS == "windows" && gpu != nil {
		warnings = append(warnings, "Для NVIDIA на Windows нужны Docker Desktop и WSL2; доступ к GPU будет проверен внутри контейнера.")
	}
	if h.OS == "darwin" {
		warnings = append(warnings, "Запуск через Docker на macOS в этом MVP использует CPU; нативный адаптер Metal запланирован отдельно.")
	}
	if runtimeFilter == "vllm" && !vllmAllowed {
		warnings = append(warnings, "Профиль vLLM требует x86-64 с Linux или Windows с работающим локальным Docker Desktop в режиме Linux-контейнеров и NVIDIA через WSL2.")
	} else if runtimeFilter == "vllm" && len(vllmGPUs) == 0 {
		warnings = append(warnings, "Для vLLM не найдена совместимая NVIDIA GPU с подтверждёнными параметрами и свободной VRAM.")
	} else if runtimeFilter != "vllm" && gpu == nil {
		warnings = append(warnings, "Для Ollama нет совместимой NVIDIA GPU с подтверждёнными параметрами и свободной VRAM: доступны подходящие CPU-модели. Адаптеры AMD/Intel/Apple GPU запланированы отдельно.")
	}
	for _, m := range c.Models {
		if !hasTask(m, task) || (runtimeFilter != "auto" && m.Runtime != runtimeFilter) {
			continue
		}
		if ram < m.MinRAMGiB {
			continue
		}
		if h.DiskFreeBytes > 0 && float64(h.DiskFreeBytes)/gib < RequiredDiskGiB(m) {
			continue
		}
		acceleration := "cpu"
		var selected *domain.GPU
		if m.Runtime == "vllm" {
			for i := range vllmGPUs {
				candidate := &vllmGPUs[i]
				if ok, _ := VLLMModelGPUSupported(*candidate, m.Quantization); ok && (selected == nil || VRAMBudgetGiB(*candidate) > VRAMBudgetGiB(*selected)) {
					selected = candidate
				}
			}
			if selected == nil || VRAMBudgetGiB(*selected) < m.MinVRAMGiB {
				continue
			}
			acceleration = "nvidia"
		} else {
			candidate := gpu
			if m.Runtime == "llama.cpp" {
				candidate = llamaGPU
			}
			if candidate != nil && VRAMBudgetGiB(*candidate) >= m.MinVRAMGiB {
				acceleration, selected = "nvidia", candidate
			}
		}
		if acceleration == "cpu" && m.ParametersB > 8 {
			continue
		}
		score := int(math.Min(m.ParametersB, 35) * 5)
		if acceleration == "nvidia" {
			score += 100
		}
		if m.Runtime == "ollama" {
			score += 10
		}
		if acceleration == "cpu" && m.ParametersB > 3 {
			score -= 40
		}
		notes := []string{fmt.Sprintf("Доступный бюджет RAM: %.1f GiB; оценка модели: %.1f GiB. Требуется около %.0f GiB места, включая резерв под образ движка.", ram, m.MinRAMGiB, RequiredDiskGiB(m))}
		if profile, ok := c.Profiles[m.ID]; ok {
			notes = append(notes, profile.Purpose)
			if profile.Verification == "estimated" {
				notes = append(notes, "Совместимость этого профиля рассчитана. Инференс на целевом GPU предстоит подтвердить.")
			}
		}
		if selected != nil {
			notes = append(notes, fmt.Sprintf("Одна GPU: %s; доступный бюджет VRAM: %.1f GiB; оценка модели: %.1f GiB.", selected.Name, VRAMBudgetGiB(*selected), m.MinVRAMGiB))
		} else {
			notes = append(notes, "Запуск на CPU: скорость зависит от процессора и пропускной способности памяти; заранее измеренной производительности нет.")
		}
		if m.ParametersB < 2 && task != "embeddings" {
			notes = append(notes, "Небольшая модель подходит для первого запуска; сложность рассуждений и качество ответов ограничены.")
		}
		if m.Runtime == "vllm" {
			notes = append(notes, "Порог драйвера зависит от выбранного образа; совместимость с конкретным образом, PTX и CUDA подтвердят проверка контейнера и запрос к модели.")
		}
		if m.Runtime == "vllm" && m.Revision == "" {
			notes = append(notes, "В этой записи не закреплена ревизия Hugging Face; для точного воспроизведения укажите проверенный полный commit SHA.")
		}
		if strings.EqualFold(m.License, "qwen") {
			notes = append(notes, "Перед использованием ознакомьтесь с отдельной лицензией модели.")
		}
		result = append(result, domain.Recommendation{Model: m, Acceleration: acceleration, GPU: selected, Score: score, Notes: notes})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].Model.ID < result[j].Model.ID
		}
		return result[i].Score > result[j].Score
	})
	if len(result) == 0 {
		warnings = append(warnings, fmt.Sprintf("В каталоге нет модели для выбранной задачи и движка, подходящей под %.1f GiB доступного бюджета RAM и текущие ограничения GPU/диска.", ram))
	}
	return result, warnings, nil
}
