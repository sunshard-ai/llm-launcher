package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/deploy"
	"llmlauncher/internal/domain"
	"llmlauncher/internal/hardware"
	"llmlauncher/internal/huggingface"
	"llmlauncher/internal/recommend"
)

func parseAdvanced(raw string) (*domain.AdvancedSettings, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 12<<10 {
		return nil, fmt.Errorf("проф. настройки превышают 12 КиБ")
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	var a *domain.AdvancedSettings
	if err := d.Decode(&a); err != nil || a == nil {
		return nil, fmt.Errorf("ожидается JSON-объект с поддерживаемыми проф. настройками")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("проф. настройки должны содержать один JSON-объект")
	}
	return a, nil
}

func planHF(ctx context.Context, o options, out io.Writer) error {
	if o.task != "chat" && o.task != "code" && o.task != "rag" {
		return fmt.Errorf("импорт Hugging Face поддерживает текстовые задачи chat, code и rag")
	}
	if o.context > 4096 {
		return fmt.Errorf("первый профиль импорта Hugging Face ограничен контекстом 4096")
	}
	if o.hfURL == "" || o.runtime == "ollama" || (o.gpu != "" && o.runtime != "vllm") {
		return fmt.Errorf("укажите --hf-url и выберите llama.cpp на CPU или vLLM на NVIDIA")
	}
	h, err := scan(ctx, o)
	if err != nil {
		return err
	}
	if !h.Docker.Reachable || !h.Docker.Compose || h.Docker.ServerOS != "linux" || !hardware.LocalEndpoint(h.Docker.Endpoint) {
		return fmt.Errorf("для импорта нужен локальный Docker с Compose и Linux-контейнерами")
	}
	ctx, cancel := context.WithTimeout(ctx, min(o.timeout, 30*time.Second))
	defer cancel()
	var source domain.HFSource
	if o.runtime == "vllm" {
		if !recommend.VLLMPlatformSupported(h) {
			return fmt.Errorf("vLLM требует Linux x86-64 или локальный Linux Docker Desktop на Windows x86-64")
		}
		source, err = huggingface.ResolveSnapshot(ctx, o.hfURL, hfToken)
	} else {
		source, err = huggingface.Resolve(ctx, o.hfURL, hfToken)
	}
	if err != nil {
		return err
	}
	weights := gib(uint64(source.Bytes))
	model := domain.Model{
		ID: "hf-gguf", Name: path.Base(source.File), Runtime: "llama.cpp",
		Tasks: []string{o.task}, Ref: source.Repo, Revision: source.Revision,
		Context: 4096, MinRAMGiB: weights + 1, DiskGiB: weights, Quantization: "GGUF",
		License: "См. карточку исходной модели", URL: "https://huggingface.co/" + source.Repo,
	}
	if ram := recommend.RAMBudgetGiB(h); ram < model.MinRAMGiB {
		return fmt.Errorf("доступно %.1f ГиБ RAM, ориентир для весов %.1f ГиБ; выберите меньший файл", ram, model.MinRAMGiB)
	}
	if gib(h.DiskFreeBytes) < recommend.RequiredDiskGiB(model) {
		return fmt.Errorf("недостаточно свободного места для весов и образа")
	}
	cat := domain.Catalog{Runtimes: map[string]string{"llama.cpp": deploy.LlamaCPPImage}}
	r := domain.Recommendation{Model: model, Acceleration: "cpu"}
	if o.runtime == "vllm" {
		cat, err = catalog.Load("")
		if err != nil {
			return err
		}
		model.ID, model.Name, model.Runtime, model.Quantization = "hf-vllm", source.Repo, "vllm", "Safetensors"
		awq, err := huggingface.SnapshotIsAWQ(ctx, source, hfToken)
		if err != nil {
			return err
		}
		if awq {
			model.Quantization = "AWQ"
		}
		model.ParametersB, model.MinRAMGiB, model.MinVRAMGiB = max(0.1, weights/2), max(4, weights+1), weights+2
		if o.image != "" {
			cat.Runtimes["vllm"] = o.image
		}
		cat.Models = []domain.Model{model}
		recs, _, err := choices(h, cat, o)
		if err != nil {
			return err
		}
		if len(recs) == 0 {
			return fmt.Errorf("для этого снимка vLLM не найдена совместимая GPU с подтверждённой свободной VRAM или не хватает RAM/диска")
		}
		r = recs[0]
	}
	p, err := makePlanWithSource(h, cat, r, o, &source)
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
	return nil
}
