# Roadmap

## Current scope

LLM Launcher manages one model per Compose project on a local Docker daemon. It supports Ollama, vLLM, and llama.cpp, with one NVIDIA GPU per deployment where supported. The CLI and Windows desktop app share the same scanner, planner, and lifecycle commands.

| Area | Code |
| --- | --- |
| CLI and desktop input | `cmd/llm-launcher` |
| Inventory and compatibility | `internal/hardware`, `internal/recommend` |
| Catalog and schema | `catalog.json`, `internal/catalog`, `internal/domain` |
| Compose, inference checks, and state | `internal/deploy` |

## Next milestones

1. **Qualify the existing profiles.** Run the [hardware test matrix](TESTING.md#verify-inference-on-hardware) on Linux and Windows. Record exact images, model revisions, VRAM, and smoke-test results. Use those runs to refine the memory estimates and recovery instructions.
2. **Make storage predictable.** Measure free space in the Docker backend where possible and let users choose a reusable location for weights. Report download and load progress separately.
3. **Add an explicit multi-GPU profile.** Start with vLLM tensor parallel on a Linux host with two compatible NVIDIA GPUs. Check each card, topology, partitioning, and actual inference before expanding to other engines or Windows.
4. **Add a server deployment mode.** Cover authentication, network binding, upgrades, rollback, monitoring, and load tests. Keep it separate from the local desktop flow.

AMD, Intel, Apple Metal, vision, audio, and reranking need separate compatibility profiles and tests. The [catalog](MODELS.md) can expand within the supported engines independently of launcher releases.
