package deploy

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"llmlauncher/internal/huggingface"
)

func (m Manager) ImportWeights(ctx context.Context, local string) error {
	p, err := ReadPlan(m.Dir)
	if err != nil {
		return err
	}
	if p.HFSource == nil {
		return fmt.Errorf("local weights require a llama.cpp GGUF plan or a Hugging Face vLLM snapshot plan; Ollama stores downloaded models in its own volume")
	}
	unlock, err := acquireLock(m.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	ctx, cancel := m.operationContext(ctx, 30*time.Minute)
	defer cancel()
	if err := huggingface.Import(ctx, m.Dir, *p.HFSource, local); err != nil {
		return err
	}
	fmt.Fprintf(m.output(), "Local weights verified and copied to %s\n", filepath.Join(m.Dir, huggingface.CacheDir))
	return nil
}
