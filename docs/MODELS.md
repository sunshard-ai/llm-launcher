# Choosing a model

[The catalog](../catalog.json) lists 52 profiles for specific combinations of model, weights, and engine. Use `llm-launcher recommend --task chat` to see the profiles that fit your machine. Set `--runtime` to filter by engine.

| Task | Small starting point | More room for weights |
| --- | --- | --- |
| Chat, summaries, and explanations | Qwen3 0.6B or Gemma 3 1B | Qwen3 4B Instruct, Mistral 7B Instruct, or Granite 3.3 |
| Code | Qwen2.5 Coder 0.5B or 1.5B | Qwen2.5 Coder 7B, Phi-4-mini, or Qwen3 Coder 30B |
| Multi-step reasoning | Qwen3 1.7B or 8B | DeepSeek R1 0528 Qwen3 8B, gpt-oss-20b, or gpt-oss-120b |
| Answers over supplied passages (`rag`) | Gemma 3 1B or Qwen3 4B Instruct | Mistral 7B or Qwen3 14B/32B |
| Search embeddings | Nomic Embed Text v1.5 | BGE-M3 for multilingual search |

For RAG, your application retrieves the relevant passages and passes them to the model. Use an Ollama embedding profile to build search vectors.

## GPU memory

The examples assume one compatible NVIDIA GPU, a 4096-token context, one request at a time, and enough free RAM and disk. The launcher uses the card's free VRAM after a reserve.

| Total VRAM | Example profiles |
| --- | --- |
| 4 GiB | Qwen3 0.6B or Gemma 3 1B in Ollama or llama.cpp. Qwen2.5 Coder 0.5B and Nomic Embed in Ollama. |
| 6–8 GiB | Qwen3 4B in Ollama or llama.cpp. Qwen3 0.6B BF16 in vLLM if the card meets its compute requirements. |
| 12–16 GiB | Mistral 7B, Qwen3 8B, or DeepSeek R1 8B GGUF. Qwen3 4B BF16 and Qwen2.5 Coder 7B AWQ in vLLM as free VRAM allows. |
| 24 GiB | Qwen3 14B GGUF or gpt-oss-20b MXFP4. |
| 32–48 GiB | Qwen3 Coder 30B Q4_K_M in Ollama or Qwen3 32B Q4_K_M in llama.cpp with enough free memory. |
| 64–80 GiB | Larger GGUF profiles. Qwen3 30B-A3B Instruct BF16 in vLLM can fit at the upper end. |
| 96–128 GiB | Qwen3 32B BF16, Qwen3 Coder 30B BF16, or gpt-oss-120b MXFP4 in vLLM. |

Weights, the KV cache, and runtime allocations all consume memory. Longer contexts raise the estimate. The launcher checks one GPU per project. For the hardware and inference tests behind these examples, see the [build report](BUILD_REPORT.md).

## Engines

| Engine | Supported paths in this release | Launch defaults |
| --- | --- | --- |
| Ollama | CPU or one NVIDIA GPU. Text and embeddings. | Explicit quantization tag and the model's template. |
| vLLM | One NVIDIA GPU on Linux x64 or Windows x64 with Docker Desktop/WSL2. Text models. | `dtype=auto` for ordinary weights and `half` for AWQ. BF16 and MXFP4 profiles require compute capability 8.0 or higher. |
| llama.cpp | CPU or one NVIDIA GPU for catalog GGUF. Manual GGUF imports use CPU. | Pinned GGUF and CPU/CUDA images. CUDA plans offload layers to the GPU and check the startup log. |

Linux ARM64 and macOS Apple Silicon use CPU containers. Each plan serves one request at a time. New plans for DeepSeek R1 0528 use temperature 0.6 and Top P 0.95 unless you override them.

## Update the catalog

The desktop app and interactive CLI check [this repository's catalog](../catalog.json) in the background. The first launch checks right away. Subsequent launches check at most once every 24 hours. The picker can open immediately with the latest verified local copy or the catalog built into the executable.

Run `llm-launcher update` or click **Обновить список моделей** in Windows to check immediately. A successful check reports whether a newer catalog was installed. If the network or signature check fails, the launcher keeps the current list. New recommendations use the latest verified catalog, and saved deployments use their recorded plans.

To try a local file, pass `--catalog /path/to/catalog.json` to `catalog`, `recommend`, or `plan`.

### Maintainers

1. Add an entry with a model ID, runtime, tasks, license, exact weight reference, and resource estimates. Under `profiles`, record its intended use, format, source, and verification status. For GGUF, include the commit, filename, byte count, and SHA-256 hash.
2. Set new profiles to `estimated` and add hardware results to the [build report](BUILD_REPORT.md) once tested. Set `updated` to a newer UTC timestamp such as `2026-11-07T12:00:00Z`. Full timestamps let you publish several revisions on the same day.
3. Regenerate the signature with the publisher key and commit the catalog and signature together.

   ```bash
   openssl pkeyutl -sign -rawin -inkey "$CATALOG_SIGNING_KEY" -in catalog.json -out catalog.json.sig
   go test ./internal/catalog ./internal/recommend
   ```

Keep the publisher key outside the repository. Schema 2 adds profile metadata and pinned GGUF files. The launcher still accepts schema 1 via `--catalog`. [Model and runtime sources](SOURCES.md)

Push the signed files together to `main`, then verify that `llm-launcher update` reaches them without a GitHub login. Catalog updates are published independently of binary releases.
