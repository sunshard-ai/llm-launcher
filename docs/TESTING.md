# Build, test, and release

## Build from source

Use Go 1.22 or later. The release workflow uses Go 1.27.3. Docker is needed for inference checks.

```bash
go test ./...
go vet ./...
bash scripts/build.sh
(cd dist && shasum -a 256 -c SHA256SUMS.txt)
```

Use `powershell -File scripts/build.ps1` on Windows. The scripts produce four CLI binaries and the Windows desktop executable in `dist/`. Add `go test -race ./...` where the host toolchain supports it.

## Review a plan without downloading weights

```bash
./dist/llm-launcher-linux-amd64 scan --info
./dist/llm-launcher-linux-amd64 plan \
  --inventory examples/hardware/linux-rtx4080-16gb.json \
  --task chat --runtime llama.cpp --model llamacpp-qwen3-4b --gpu 0 --dir ./preview
./dist/llm-launcher-linux-amd64 inspect --dir ./preview --compose
```

The fixture is synthetic. A deployment intended for `up` must be planned from the machine's real inventory.

## Verify inference on hardware

Create a plan on the target machine and run `up`, `test`, and `status --dir`. The chat test requires text and the embeddings test requires a valid vector. For NVIDIA, also confirm that the engine used the selected GPU. Run `down`, then start the same plan and check that it reuses the downloaded weights.

| Setup | What to exercise |
| --- | --- |
| CPU in Docker | A small llama.cpp GGUF and Ollama chat or embeddings. |
| Linux x64 with NVIDIA | Ollama GPU, vLLM, and llama.cpp CUDA. Check a small card and a larger card where available. |
| Windows x64 with WSL2 | Open the GUI, scan, choose a profile, review the plan, run it, test the API, stop and restore. |
| Private Hugging Face model | Import with a scoped read token and confirm it is absent from saved plans and Compose. |
| Catalog updates | Accept a signed update and keep the cached copy if the next update fails. |

Record the OS, Docker backend, launcher version, engine image, model ID, context, GPU and driver, and result. Distinguish a successful compile, fixture or mock check, CPU inference, and GPU inference. [Recorded results](BUILD_REPORT.md)

## Package a release

Add notes at `docs/releases/vMAJOR.MINOR.PATCH.md`. A tag push runs the **Release** workflow. For an existing tag with no release, open **Actions → Release → Run workflow** on the default branch and enter the tag.

To produce the same set of assets locally:

```bash
bash scripts/package-release.sh v0.3.0
(cd dist/release/v0.3.0 && shasum -a 256 -c SHA256SUMS.txt)
```

The source archive and binaries are built from the tagged commit. The workflow attaches them, the signed catalog, and the checksum file to the GitHub Release. Manual runs take the release description from the selected workflow branch. [GitHub workflow dispatch](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch)
