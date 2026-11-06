package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const VLLMRepository = "vllm/vllm-openai"

// ResolveVLLMImage accepts a release tag or official image reference. The
// latest channel is consulted only while planning and persisted as a digest.
func ResolveVLLMImage(ctx context.Context, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "latest" || value == VLLMRepository+":latest" {
		ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://hub.docker.com/v2/repositories/"+VLLMRepository+"/tags/latest", nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("resolve official vLLM latest image: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("Docker Hub returned HTTP %d; specify a vLLM release tag explicitly", resp.StatusCode)
		}
		var tag struct {
			Digest string `json:"digest"`
			Images []struct {
				OS           string `json:"os"`
				Architecture string `json:"architecture"`
			} `json:"images"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tag); err != nil {
			return "", fmt.Errorf("invalid Docker Hub image metadata: %w", err)
		}
		for _, image := range tag.Images {
			if image.OS == "linux" && image.Architecture == "amd64" {
				ref := VLLMRepository + "@" + tag.Digest
				return ref, ValidateRuntimeImage("vllm", ref)
			}
		}
		return "", fmt.Errorf("vLLM latest has no Linux amd64 image; specify a compatible release tag")
	}
	if !strings.ContainsAny(value, "/:@") {
		value = VLLMRepository + ":" + value
	}
	return value, ValidateRuntimeImage("vllm", value)
}
