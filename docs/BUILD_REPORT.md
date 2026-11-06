# Build and test results

## v0.3.1

- Go tests, the race detector, and `go vet` passed on macOS ARM64. All five binaries were built and verified against their checksums.
- Local HTTP tests covered first refresh, the 24-hour interval, forced refresh, signature rejection, and protection against reverting to an older catalog. A lock test covered simultaneous launch attempts.
- The Linux ARM64 release binary reached the public GitHub catalog, verified its signature, and reported that the bundled version was current. The interactive wizard opened its model picker with a fresh config both online and in a container with networking disabled.
- The Windows desktop executable was cross-compiled. The background check still needs a run on Windows.

## v0.3.0

| Check | Result |
| --- | --- |
| Local build | Go 1.27.3 on macOS ARM64. `go test ./...`, `go test -race ./...`, `go vet ./...`, and `scripts/build.sh` passed. All five binaries passed SHA-256 verification. PowerShell scripts passed parsing. |
| Release packaging | The package was rebuilt from the `v0.3.0` tag. The source archive, binaries, catalog, signature, and public key passed SHA-256 verification. |
| Catalog update | A local test server served a signed catalog. The launcher installed it, accepted a repeat update, and kept the cached copy after receiving a bad signature. |
| Model selection | Fixtures covered 12 VRAM sizes from 4 to 128 GiB and four tasks. Mixed-GPU cases verified that the BF16 profile selects a compatible card. |
| CPU inference | Docker Desktop with Linux ARM64. The launcher downloaded and verified the pinned 609 MiB Qwen3 0.6B Q8_0 GGUF. llama.cpp b9935 returned text at a 1024-token context. A second run reused the file. |
| NVIDIA inference and Windows GUI | Hardware verification remains open for the new profiles and desktop build. |

New llama.cpp plans select `--n-gpu-layers all` for NVIDIA or `0` for CPU. DeepSeek R1 0528 defaults extend to the new llama.cpp and vLLM profiles. Reasoning models receive a larger smoke-test response budget. vLLM dtype defaults stayed the same.

## Earlier checks

| Version | Evidence |
| --- | --- |
| 0.2.3 | The Linux ARM64 CLI completed the interactive dry-run in a Linux container. An RTX 4080 fixture produced a plan with the DeepSeek defaults. |
| 0.2.2 | On Docker Desktop, global `status` found the running llama.cpp deployment and stopped listing it after `down`. vLLM dtype changes were checked in generated plans. |
| 0.2.1 | On Ubuntu/WSL2 with an RTX 4080 SUPER, the CUDA 12.9 vLLM image saw the GPU and failed while importing `torchcodec` before model loading. The launcher switched the default to a pinned CUDA 13.0 image and added an import probe. The new image selection was checked in plans. [Upstream issue](https://github.com/vllm-project/vllm/issues/63121) |
| 0.2.0 | On Linux ARM64 in Docker Desktop, llama.cpp b9935 downloaded a 105,454,144-byte SmolLM2 135M GGUF, verified its hash, answered over the chat API, and reused the weights after restart. CLI lifecycle and PowerShell quoting were also exercised. |
| 0.1.0 | Built all four CLI targets on Linux x64. The Linux x64 binary ran scanning, recommendation, and planning. Docker was unavailable in that build environment. |

[Hardware test procedure](TESTING.md#verify-inference-on-hardware)
