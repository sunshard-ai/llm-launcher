package catalog

import (
	"strings"

	"llmlauncher/internal/domain"
)

func IsDeepSeekR1Profile(model domain.Model) bool {
	switch model.Ref {
	case "deepseek-r1:8b-0528-qwen3-q4_K_M", "deepseek-ai/DeepSeek-R1-0528-Qwen3-8B", "unsloth/DeepSeek-R1-0528-Qwen3-8B-GGUF":
		return true
	}
	return false
}

func UsesReasoningTokens(model domain.Model) bool {
	if IsDeepSeekR1Profile(model) || strings.Contains(model.Ref, "gpt-oss") {
		return true
	}
	switch model.Runtime {
	case "ollama":
		return strings.HasPrefix(model.Ref, "qwen3:") && !strings.Contains(model.Ref, "instruct")
	case "llama.cpp":
		return strings.HasPrefix(model.Ref, "Qwen/Qwen3-") && strings.HasSuffix(model.Ref, "-GGUF")
	case "vllm":
		return model.Ref == "Qwen/Qwen3-0.6B" || model.Ref == "Qwen/Qwen3-32B"
	}
	return false
}

// ApplyDefaults keeps manual choices and the original model template.
func ApplyDefaults(model domain.Model, settings *domain.AdvancedSettings) *domain.AdvancedSettings {
	if !IsDeepSeekR1Profile(model) {
		return settings
	}
	if settings == nil {
		settings = &domain.AdvancedSettings{}
	}
	// See the official model card linked from internal/catalog/SOURCES.md.
	temperature, topP := 0.6, 0.95
	if settings.Temperature == nil {
		settings.Temperature = &temperature
	}
	if settings.TopP == nil {
		settings.TopP = &topP
	}
	return settings
}
