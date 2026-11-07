# Build and test results

## v0.3.4

### Windows feedback fixes

- Go tests, race checks, vet and all five target builds passed on macOS ARM64. Tests cover stopped/unknown/Windows-container Docker states, a client-only `docker info` response, an occupied port with automatic fallback, local GGUF and Safetensors import, mismatched hashes, cache preservation, cancellation and retention of reviewed plan files.
- PowerShell 7.6.9 parsed the embedded interface and ran journal/workflow checks with mock controls and a native process fixture. Checks covered catalog refresh and recommendation reload, local-weight command paths, selection of a new project folder, a missing command executable followed by two successful starts, and an unrelated working directory.
- The worker-copy check confirmed that moving the original executable does not remove the session worker. The tester's exact Windows `Process.Start` failure was not reproduced on the test host; the revised Windows executable still needs a native interface check. This change adds no plan schema version or runtime arguments. Existing GPU measurements below belong to the earlier Linux candidate.

### Linux NVIDIA field test

The final Linux amd64 candidate completed two start/test/status/down cycles on an RTX 4080 SUPER with Qwen3 0.6B Q8_0, context 4096 and one sequence. Both cycles reused the saved plan, runtime image and GGUF file. The report's binary SHA-256 matches the candidate built from `e108a65`.

| Measurement | First cycle | Restart after `down` |
| --- | --- | --- |
| Complete `up` | 59.493 s | 57.217 s |
| Server listening after container start | 39.940 s | 39.049 s |
| Separate chat smoke request | `OK`, 733 ms | `OK`, 487 ms |
| GPU layer offload | 29/29 | 29/29 |
| API status | Reachable, selected model loaded | Reachable, selected model loaded |

Neither cycle attempted a runtime image download or reported a credential-helper error. Both stopped successfully and retained the weights. Logged CUDA buffers were 604.15 MiB for weights, 448.00 MiB for KV cache and 30.01 MiB for compute, about 1.06 GiB in total. Host-wide GPU readings include other activity. These are measurements of this small-model smoke test, not a general throughput benchmark. Docker health was still `starting` in both status snapshots; readiness was independently established by model API checks and nonempty chat responses.

- The second NVIDIA field test confirmed llama.cpp b9935 on an RTX 4080 SUPER: 29/29 layers offloaded, Qwen3 0.6B Q8_0 at 4096 context, first cached-weight `up` completed in 63.671 seconds, and a separate chat request returned `OK` in 499 ms. Model loading reached the listening state at 42.48 seconds. Logged device buffers were 604.15 MiB for weights, 448.00 MiB for KV cache, and 30.01 MiB for compute. Total host GPU memory also included other activity and is not attributed to this model.
- That test's restart stopped before container creation because Compose 2.38.2 attempted a pull of the cached digest and its Docker credential helper failed. The launcher now inspects the exact local image first and starts Compose with `--pull never`. Mock Docker plus a local HTTP server cover start/down/restart with broken registry credentials; separate cases cover cached tags and digests, downloads, credential errors, unresolved images, and cancellation. The final field test above confirmed the revised restart path on NVIDIA.
- The follow-up report contained only 14 runtime log lines at verbosity 3. The server became ready in about 40 seconds and generated 117 tokens in 636 ms, but hid the offload message. Source inspection confirmed that b9935 maps library INFO to level 4.
- Reproduced the logging difference with the official b9935 macOS ARM64 binary and the same Qwen3 GGUF on CPU. Both runs reached the health endpoint. Level 3 produced 7 log lines and no loader messages, while level 4 produced 208 lines including loader details. The NVIDIA field test above subsequently confirmed GPU offload with level 4.
- A user report from Ubuntu 24.04.2 under WSL2 with an RTX 4080 SUPER and NVIDIA driver 610.88 confirmed two successful Ollama Qwen3 0.6B Q4_K_M launch/relaunch cycles. The runtime log showed 29/29 layers on GPU. Separate smoke requests took 488–606 ms in the first session and 1482–1646 ms in the second.
- The first llama.cpp attempt spent its timeout downloading the runtime image. A later attempt downloaded the GGUF and answered the model request, then failed GPU verification. Its complete startup log was unavailable, so GPU offload for that run remains unconfirmed.
- Fixed a verification path that searched only the final 300 log lines. The new check reads the owned container's current start using Docker `logs --since` and matches both output streams while reading them. Regression checks cover early messages, split writes, large stderr output, CPU-only starts, and previous-container evidence.
- Added `logs --all` to collect retained startup messages.
- Go tests, the race detector, vet, and the release build passed on macOS ARM64. A process fixture exercised large stderr output. The Linux amd64 llama.cpp NVIDIA lifecycle passed the field test above. Windows GUI and other runtime/model combinations were not exercised by that test.

## v0.3.3

- The desktop journal regression used mock controls to replay the Russian Docker summary and disk warning in French, German, Chinese, and English. It also checked a nested GPU warning, streamed API details, and separation of original diagnostic output.
- Go tests, vet, and the release build ran on macOS ARM64. The Windows executable was cross-compiled.
- The release workflow passed static validation. The Authenticode signing path requires a validated publisher identity. Visual verification in Windows WinForms remains open.

## v0.3.2

- `go test ./...`, `go vet ./...`, and `bash scripts/build.sh` passed on macOS ARM64. All five binaries passed SHA-256 verification.
- The general help and 19 command help screens were checked in the macOS ARM64 binary and in Linux ARM64 and x64 containers.
- PowerShell parsed the embedded desktop script and translation table. With mock controls, all five languages updated the labels, retained the selected model, GPU and prepared plan, and persisted across preference reloads. A fresh or invalid preference selected English.
- The Windows executable was cross-compiled. Visual and interactive verification in Windows WinForms remains open.

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
