package deploy

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/domain"
)

const LlamaCPPImage = catalog.LlamaCPUImage

var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func validateAdvanced(p domain.Plan) error {
	a := p.Advanced
	if a == nil {
		return nil
	}
	check := func(v *float64, min, max float64) bool {
		return v == nil || (!math.IsNaN(*v) && *v >= min && *v <= max)
	}
	if !check(a.Temperature, 0, 2) || !check(a.TopP, 0.001, 1) || !check(a.MinP, 0, 1) || !check(a.RepeatPenalty, 0.1, 3) {
		return fmt.Errorf("sampling outside supported ranges: temperature 0–2, top_p 0.001–1, min_p 0–1, repeat_penalty 0.1–3")
	}
	if !check(a.PresencePenalty, -2, 2) || !check(a.FrequencyPenalty, -2, 2) {
		return fmt.Errorf("presence_penalty and frequency_penalty must be between -2 and 2")
	}
	if a.MaxTokens != nil && (*a.MaxTokens < 1 || *a.MaxTokens >= p.Context) {
		return fmt.Errorf("max_tokens must be positive and smaller than the context window")
	}
	if a.TopK != nil && (*a.TopK < 1 || *a.TopK > 1000) ||
		a.RepeatLastN != nil && (*a.RepeatLastN < -1 || *a.RepeatLastN > p.Context) ||
		a.Seed != nil && (*a.Seed < 0 || *a.Seed > 2147483647) {
		return fmt.Errorf("invalid top_k, repeat_last_n or seed")
	}
	if len(a.Stop) > 8 {
		return fmt.Errorf("at most eight stop sequences are supported")
	}
	for _, stop := range a.Stop {
		if stop == "" || len(stop) > 256 || strings.ContainsRune(stop, 0) {
			return fmt.Errorf("stop sequences must contain 1–256 bytes")
		}
	}
	if p.Task == "embeddings" && len(ollamaParameters(a)) != 0 {
		return fmt.Errorf("sampling settings apply to text generation, not embeddings")
	}
	if p.Runtime != "ollama" && len(a.Stop) != 0 {
		return fmt.Errorf("server-side stop defaults are supported only by the Ollama adapter")
	}
	if (a.DType != "" || a.EnforceEager || a.GPUMemoryUtilization != nil) && p.Runtime != "vllm" {
		return fmt.Errorf("dtype, enforce_eager and gpu_memory_utilization apply only to vLLM")
	}
	if a.GPUMemoryUtilization != nil && (!check(a.GPUMemoryUtilization, 0.01, 0.95) || *a.GPUMemoryUtilization != p.GPUUtilization) {
		return fmt.Errorf("gpu_memory_utilization must be 0.01–0.95 and match the plan")
	}
	if a.DType != "" && a.DType != "auto" && a.DType != "half" && a.DType != "bfloat16" {
		return fmt.Errorf("dtype must be auto, half or bfloat16")
	}
	if p.Runtime == "vllm" && (a.RepeatLastN != nil || a.Seed != nil) {
		return fmt.Errorf("this vLLM adapter supports temperature, max_tokens, top_p, top_k, min_p and penalties as generation defaults")
	}
	if len(a.Environment) > 24 {
		return fmt.Errorf("at most 24 environment variables are supported")
	}
	for k, v := range a.Environment {
		if !allowedEnvironment(p.Runtime, k, v) {
			return fmt.Errorf("unsupported environment variable or value for %s: %s", p.Runtime, k)
		}
	}
	if p.Runtime == "llama.cpp" && p.Acceleration == "nvidia" {
		if value := a.Environment["LLAMA_ARG_LOG_VERBOSITY"]; value != "" {
			level, _ := strconv.Atoi(value)
			if level < 4 {
				return fmt.Errorf("llama.cpp GPU verification requires LLAMA_ARG_LOG_VERBOSITY=4 or higher")
			}
		}
	}
	if p.Runtime == "ollama" {
		cache := a.Environment["OLLAMA_KV_CACHE_TYPE"]
		if cache != "" && cache != "f16" && a.Environment["OLLAMA_FLASH_ATTENTION"] != "true" {
			return fmt.Errorf("quantized Ollama KV cache requires OLLAMA_FLASH_ATTENTION=true")
		}
	}
	return nil
}

func allowedEnvironment(runtime, key, value string) bool {
	if !envName.MatchString(key) || len(value) > 512 || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	if strings.HasPrefix(key, "LLM_USER_") {
		for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "KEY", "CREDENTIAL"} {
			if strings.Contains(key, marker) {
				return false
			}
		}
		return !strings.HasPrefix(value, "hf_")
	}
	if runtime == "vllm" {
		return key == "VLLM_LOGGING_LEVEL" && (value == "DEBUG" || value == "INFO" || value == "WARNING" || value == "ERROR")
	}
	if runtime == "llama.cpp" {
		level, err := strconv.Atoi(value)
		return key == "LLAMA_ARG_LOG_VERBOSITY" && err == nil && level >= 0 && level <= 5
	}
	if runtime != "ollama" {
		return false
	}
	switch key {
	case "OLLAMA_FLASH_ATTENTION":
		return value == "true" || value == "false"
	case "OLLAMA_KV_CACHE_TYPE":
		return value == "f16" || value == "q8_0" || value == "q4_0"
	case "OLLAMA_KEEP_ALIVE", "OLLAMA_LOAD_TIMEOUT":
		d, err := time.ParseDuration(value)
		return err == nil && d >= time.Minute && d <= 24*time.Hour
	case "OLLAMA_MAX_QUEUE":
		n, err := strconv.Atoi(value)
		return err == nil && n >= 1 && n <= 4096
	}
	return false
}

func writeEnvironment(b *strings.Builder, p domain.Plan) {
	if p.Advanced == nil {
		return
	}
	env := p.Advanced.Environment
	keys := make([]string, 0, len(env))
	for k := range env {
		if k != "OLLAMA_KEEP_ALIVE" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "      %s: %s\n", k, quoted(strings.ReplaceAll(env[k], "$", "$$")))
	}
}

func ollamaParameters(a *domain.AdvancedSettings) map[string]any {
	result := map[string]any{}
	if a == nil {
		return result
	}
	for k, v := range map[string]*float64{"temperature": a.Temperature, "top_p": a.TopP, "min_p": a.MinP, "repeat_penalty": a.RepeatPenalty, "presence_penalty": a.PresencePenalty, "frequency_penalty": a.FrequencyPenalty} {
		if v != nil {
			result[k] = *v
		}
	}
	for k, v := range map[string]*int{"num_predict": a.MaxTokens, "top_k": a.TopK, "repeat_last_n": a.RepeatLastN, "seed": a.Seed} {
		if v != nil {
			result[k] = *v
		}
	}
	if len(a.Stop) != 0 {
		result["stop"] = a.Stop
	}
	return result
}

func vllmGeneration(a *domain.AdvancedSettings) map[string]any {
	result := ollamaParameters(a)
	if v, ok := result["num_predict"]; ok {
		delete(result, "num_predict")
		result["max_new_tokens"] = v
	}
	if v, ok := result["repeat_penalty"]; ok {
		delete(result, "repeat_penalty")
		result["repetition_penalty"] = v
	}
	return result
}

func llamaArgs(p domain.Plan) []string {
	layers := "0"
	if p.Acceleration == "nvidia" {
		layers = "all"
	}
	args := []string{"--model", "/models/weights.gguf", "--alias", p.Model.ID, "--host", "0.0.0.0", "--port", "8080", "--ctx-size", strconv.Itoa(p.Context), "--parallel", "1", "--n-gpu-layers", layers}
	if a := p.Advanced; a != nil {
		for _, option := range []struct {
			flag  string
			value *float64
		}{{"--temp", a.Temperature}, {"--top-p", a.TopP}, {"--min-p", a.MinP}, {"--repeat-penalty", a.RepeatPenalty}, {"--presence-penalty", a.PresencePenalty}, {"--frequency-penalty", a.FrequencyPenalty}} {
			if option.value != nil {
				args = append(args, option.flag, strconv.FormatFloat(*option.value, 'g', -1, 64))
			}
		}
		for _, option := range []struct {
			flag  string
			value *int
		}{{"--n-predict", a.MaxTokens}, {"--top-k", a.TopK}, {"--repeat-last-n", a.RepeatLastN}, {"--seed", a.Seed}} {
			if option.value != nil {
				args = append(args, option.flag, strconv.Itoa(*option.value))
			}
		}
	}
	return args
}
