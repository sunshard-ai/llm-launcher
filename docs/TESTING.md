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

## Verify interface languages

Run `pwsh -File scripts/test-desktop-workflow.ps1` for the journal checks and desktop workflow fixtures. It covers catalog refresh, local-weight command paths, automatic folder selection, process-start failure recovery and command retries with a changed working directory. These checks use mock controls and a native process fixture; they do not replace Windows WinForms testing.

- Run `--help` and `hf-plan --help` on the Linux and macOS binaries and check that the command and flag descriptions are in English.
- On Windows, launch the desktop app with a fresh user profile and confirm it opens in English. Switch through all five languages and check both tabs, the GPU list, the Hugging Face and advanced settings dialogs, and the archive confirmation.
- Prepare a plan, change the language, and confirm that the selected options and **Launch plan** are still available. Close and reopen the app and verify it remembers the selected language.
- Scan in Russian, switch to French, and check that the existing Docker summary and disk note switch to French without a second scan. Verify that original container output is available under **Technical details…**.
- Run `pwsh -File scripts/test-desktop-journal.ps1` to check journal rendering in all five languages with mock controls.

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
| Catalog updates | Open the wizard with a fresh user config, confirm the background check, then reopen it within 24 hours. Force a check with `update` and verify offline startup uses the local catalog. |

Record the OS, Docker backend, launcher version, engine image, model ID, context, GPU and driver, and result. Distinguish a successful compile, fixture or mock check, CPU inference, and GPU inference. [Recorded results](BUILD_REPORT.md)

## Package a release

Add notes at `docs/releases/vMAJOR.MINOR.PATCH.md`. A tag push runs the **Release** workflow. For an existing tag with no release, open **Actions → Release → Run workflow** on the default branch and enter the tag.

To produce the same set of assets locally:

```bash
bash scripts/package-release.sh v0.3.4
(cd dist/release/v0.3.4 && shasum -a 256 -c SHA256SUMS.txt)
```

The source archive, binaries, desktop workflow checks and release description use the selected tag. The workflow attaches the assets and their checksums to the GitHub Release. The launcher fetches catalog updates separately from `main`. After publication, check the workflow conclusion, all seven assets, the source archive, checksums and the downloaded CLI's `version` output. [GitHub workflow dispatch](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch)

### Windows publisher signing

For signed Windows releases, provision an Azure Artifact Signing account with a validated publisher identity and a Public Trust certificate profile. Configure GitHub Actions as a federated identity and grant it the **Artifact Signing Certificate Profile Signer** role for the profile. [Microsoft setup guide](https://learn.microsoft.com/azure/artifact-signing/quickstart) · [GitHub Actions signing guide](https://github.com/Azure/artifact-signing-action#authenticating-with-openid-connect)

Set the repository secrets `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_SUBSCRIPTION_ID`. Set the repository variables `ARTIFACT_SIGNING_ENDPOINT`, `ARTIFACT_SIGNING_ACCOUNT`, and `ARTIFACT_SIGNING_PROFILE`. Then set `WINDOWS_SIGNING_ENABLED` to `true`. The federated identity must allow the release tag ref and any branch used to dispatch the workflow manually. [Microsoft Entra federated credentials](https://learn.microsoft.com/entra/workload-id/workload-identity-federation-create-trust)

With signing enabled, publication requires valid timestamped Authenticode signatures on both executables. The release workflow regenerates SHA-256 checksums after signing. With signing disabled, the release workflow builds unsigned Windows executables.
