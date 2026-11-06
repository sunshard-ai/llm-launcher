# Catalog provenance

[The catalog](../../catalog.json) identifies the exact runtime artifact in `url` and its upstream source in `profiles.source`. Check licensing and model provenance for the individual profile.

- Ollama entries use explicit quantization tags. The launcher records the observed model digest after download. [Qwen2.5 tags](https://ollama.com/library/qwen2.5/tags) · [Qwen2.5 Coder tags](https://ollama.com/library/qwen2.5-coder/tags)
- New vLLM entries pin the Hugging Face revision. Older entries that follow the default revision are flagged in recommendation output. [Qwen2.5 7B AWQ](https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-AWQ)
- llama.cpp entries specify a revision, filename, byte count, and SHA-256 hash. A repackaged GGUF links to its conversion and the upstream model card.
- The DeepSeek R1 0528 profile uses temperature 0.6 and Top P 0.95 from the publisher's usage and evaluation settings. [DeepSeek model card](https://huggingface.co/deepseek-ai/DeepSeek-R1-0528-Qwen3-8B)

The memory fields are planning estimates for a 4096-token context and one request. GPU estimates include weights, KV cache, and runtime overhead. The planner adds disk space for runtime images and scales memory estimates conservatively for longer contexts.

[Model selection and catalog maintenance](../../docs/MODELS.md) · [Runtime references](../../docs/SOURCES.md)
