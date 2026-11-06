package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"llmlauncher/internal/catalog"
	"llmlauncher/internal/domain"
)

func endpoint(p domain.Plan) string {
	return "http://" + net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}
func APIModel(p domain.Plan) string {
	if p.Runtime != "ollama" {
		return p.Model.ID
	}
	if len(ollamaParameters(p.Advanced)) != 0 {
		return p.Project + ":configured"
	}
	return p.Model.Ref
}

func (m Manager) httpClient() *http.Client {
	if m.client != nil {
		return m.client
	}
	return &http.Client{
		Timeout:   5 * time.Minute,
		Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 4 * time.Minute, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("local model API must not redirect")
		},
	}
}

func (m Manager) request(ctx context.Context, p domain.Plan, method, path string, payload any, result any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint(p)+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("model API response exceeds 4 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(data))
		if len(detail) > 1000 {
			detail = detail[:1000]
		}
		return fmt.Errorf("%s returned HTTP %d: %s", path, resp.StatusCode, detail)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("%s returned invalid JSON: %w", path, err)
		}
	}
	return nil
}

func (m Manager) probeReady(ctx context.Context, p domain.Plan) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if p.Runtime == "ollama" {
		var v struct {
			Version string `json:"version"`
		}
		if err := m.request(ctx, p, "GET", "/api/version", nil, &v); err != nil {
			return err
		}
		if v.Version == "" {
			return fmt.Errorf("Ollama /api/version returned no version")
		}
		return nil
	}
	if err := m.request(ctx, p, "GET", "/health", nil, nil); err != nil {
		return err
	}
	loaded, err := m.modelLoaded(ctx, p)
	if err != nil {
		return err
	}
	if !loaded {
		return fmt.Errorf("vLLM API is reachable but selected model %s is not listed", p.Model.ID)
	}
	return nil
}

func (m Manager) waitReady(ctx context.Context, p domain.Plan) error {
	fmt.Fprintln(m.output(), "Waiting for the runtime API (first model load can take several minutes)...")
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var last error
	nextInspect := time.Now().Add(10 * time.Second)
	restartingChecks := 0
	for {
		if err := m.probeReady(ctx, p); err == nil {
			return nil
		} else {
			last = err
		}
		if time.Now().After(nextInspect) && ctx.Err() == nil {
			checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			containers, err := m.ownedResources(checkCtx, p)
			cancel()
			if err != nil {
				return fmt.Errorf("cannot inspect startup state: %w", err)
			}
			if len(containers) == 0 {
				return fmt.Errorf("model container disappeared during startup; inspect logs and rerun up")
			}
			restarting := false
			for _, c := range containers {
				if c.State.Status == "exited" || c.State.Status == "dead" {
					return fmt.Errorf("model container %s during startup; inspect logs before retrying", c.State.Status)
				}
				if c.RestartCount >= 3 {
					return fmt.Errorf("model container restarted %d times; inspect logs (possible model/CUDA/OOM error) before retrying", c.RestartCount)
				}
				if c.State.Restarting {
					restarting = true
				}
			}
			if restarting {
				restartingChecks++
			} else {
				restartingChecks = 0
			}
			if restartingChecks >= 2 {
				return fmt.Errorf("model container is repeatedly restarting; inspect logs before retrying")
			}
			nextInspect = time.Now().Add(10 * time.Second)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("runtime is not ready: %w; last probe: %v; inspect logs and rerun up to resume", ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

type ollamaModels struct {
	Models []struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		Digest   string `json:"digest"`
		SizeVRAM uint64 `json:"size_vram"`
	} `json:"models"`
}

func sameModel(a, b string) bool {
	// Ollama may add the explicit default tag to a returned model name.
	if !strings.Contains(a, ":") {
		a += ":latest"
	}
	if !strings.Contains(b, ":") {
		b += ":latest"
	}
	return a == b
}

func (m Manager) modelDigest(ctx context.Context, p domain.Plan) (string, error) {
	var models ollamaModels
	if err := m.request(ctx, p, "GET", "/api/tags", nil, &models); err != nil {
		return "", err
	}
	for _, model := range models.Models {
		if sameModel(model.Name, p.Model.Ref) || sameModel(model.Model, p.Model.Ref) {
			return model.Digest, nil
		}
	}
	return "", nil
}

func (m Manager) applyOllamaDefaults(ctx context.Context, p domain.Plan) error {
	parameters := ollamaParameters(p.Advanced)
	if len(parameters) == 0 {
		return nil
	}
	alias := APIModel(p)
	aliasPlan := p
	aliasPlan.Model.Ref = alias
	digest, err := m.modelDigest(ctx, aliasPlan)
	if err != nil || digest != "" {
		return err
	}
	parameters["num_ctx"] = p.Context
	payload := map[string]any{"from": p.Model.Ref, "model": alias, "parameters": parameters, "stream": false}
	var response struct {
		Status string `json:"status"`
	}
	if err := m.request(ctx, p, "POST", "/api/create", payload, &response); err != nil {
		return fmt.Errorf("create Ollama profile: %w", err)
	}
	if response.Status != "success" {
		return fmt.Errorf("Ollama did not confirm creation of the configured model")
	}
	return nil
}

func (m Manager) modelLoaded(ctx context.Context, p domain.Plan) (bool, error) {
	if p.Runtime == "ollama" {
		var models ollamaModels
		if err := m.request(ctx, p, "GET", "/api/ps", nil, &models); err != nil {
			return false, err
		}
		for _, model := range models.Models {
			if sameModel(model.Name, APIModel(p)) || sameModel(model.Model, APIModel(p)) {
				if p.Acceleration == "nvidia" && model.SizeVRAM == 0 {
					return false, fmt.Errorf("selected model is loaded on CPU despite the GPU plan; inspect logs and driver/runtime compatibility")
				}
				return true, nil
			}
		}
		return false, nil
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := m.request(ctx, p, "GET", "/v1/models", nil, &models); err != nil {
		return false, err
	}
	for _, model := range models.Data {
		if model.ID == p.Model.ID {
			return true, nil
		}
	}
	return false, nil
}

func (m Manager) verifyOllamaGPU(ctx context.Context, p domain.Plan) error {
	loaded, err := m.modelLoaded(ctx, p)
	if err != nil {
		return err
	}
	if !loaded {
		return fmt.Errorf("inference responded but the selected GPU model is not present in /api/ps; deployment is not marked ready")
	}
	return nil
}

// smoke exercises the exact OpenAI-compatible endpoint exposed to the user.
// Ollama's configured context and keep-alive come from the managed environment.
func (m Manager) smoke(ctx context.Context, p domain.Plan) (domain.SmokeResult, error) {
	started := time.Now()
	result := domain.SmokeResult{Kind: "chat"}
	if p.Task == "embeddings" {
		result.Kind = "embeddings"
		var response struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
			Error json.RawMessage `json:"error"`
		}
		payload := map[string]any{"model": APIModel(p), "input": []string{"Local model deployment test."}, "encoding_format": "float"}
		if err := m.request(ctx, p, "POST", "/v1/embeddings", payload, &response); err != nil {
			return result, err
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return result, fmt.Errorf("embedding API error: %s", response.Error)
		}
		if len(response.Data) != 1 || len(response.Data[0].Embedding) == 0 {
			return result, fmt.Errorf("embedding API returned no vector")
		}
		norm := 0.0
		for _, n := range response.Data[0].Embedding {
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return result, fmt.Errorf("embedding API returned a non-finite number")
			}
			norm += n * n
		}
		if norm == 0 || math.IsInf(norm, 0) {
			return result, fmt.Errorf("embedding API returned an invalid zero/non-finite vector")
		}
		result.Output = fmt.Sprintf("Embedding vector: %d dimensions", len(response.Data[0].Embedding))
	} else {
		messages := []map[string]string{{"role": "user", "content": "Reply with the single word OK."}}
		maxTokens := 32
		payload := map[string]any{"model": APIModel(p), "messages": messages, "stream": false}
		if catalog.UsesReasoningTokens(p.Model) {
			maxTokens = min(512, p.Context-64)
		} else {
			payload["temperature"] = 0.0
		}
		if p.Advanced != nil && p.Advanced.MaxTokens != nil {
			maxTokens = min(maxTokens, *p.Advanced.MaxTokens)
		}
		payload["max_tokens"] = maxTokens
		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Error json.RawMessage `json:"error"`
		}
		if err := m.request(ctx, p, "POST", "/v1/chat/completions", payload, &response); err != nil {
			return result, err
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return result, fmt.Errorf("chat API error: %s", response.Error)
		}
		if len(response.Choices) > 0 {
			result.Output = strings.TrimSpace(response.Choices[0].Message.Content)
		}
		if result.Output == "" {
			return result, fmt.Errorf("model returned HTTP success but no generated text")
		}
	}
	result.DurationMS = time.Since(started).Milliseconds()
	result.OK = true
	return result, nil
}
