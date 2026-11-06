# Troubleshooting

## Windows SmartScreen

If Windows shows **Unknown publisher**, check that the executable came from the project’s GitHub release. Compare its hash with the matching entry in `SHA256SUMS.txt`.

```powershell
Get-FileHash -Algorithm SHA256 .\LLM-Launcher-Windows-x64.exe
Get-AuthenticodeSignature .\LLM-Launcher-Windows-x64.exe
```

A trusted Authenticode signature identifies the publisher. SmartScreen may still warn while the publisher or file builds reputation. Microsoft Store distribution is covered by Microsoft’s signing and trust. [SmartScreen reputation](https://learn.microsoft.com/windows/apps/package-and-deploy/smartscreen-reputation)

## Find the failing step

Run these commands from the directory where you normally use the launcher. Point `--dir` at your deployment.

```bash
llm-launcher doctor
llm-launcher status --dir ./my-model
llm-launcher logs --dir ./my-model
```

| Symptom | Check |
| --- | --- |
| Docker or Compose is unavailable | Run `docker info` and `docker compose version`. Start Docker and select its local context. On Windows, use Linux containers with WSL2. |
| Remote Docker context | Run `docker context show` and inspect the endpoint. Select the local daemon before planning. |
| GPU appears in `scan` but fails at launch | Run `nvidia-smi` on the host, then `llm-launcher gpu-check --dir ./my-model`. Check the NVIDIA Container Toolkit on Linux or the WSL2 GPU path on Windows. |
| Only CPU profiles appear | Check free VRAM, driver, and compute capability in `scan --info`. Try a smaller model or a shorter context. |
| A port is in use | Choose another `--port` when creating a plan, or stop the earlier launcher project with `down --dir ...`. |
| Compose or plan failed validation | Run `inspect --dir ./my-model` and create a fresh plan in a new directory. Managed files are checked against the saved plan. |

## vLLM fails before serving

Check `logs` for the first error. If a CUDA library cannot load, confirm the driver matches the image in `inspect`. Use `--image` with an official compatible vLLM tag or digest when creating a new plan. If the container runs out of memory, free VRAM, reduce `--context`, or choose smaller weights. Review the result with `gpu-check` and `test`.

On Windows, vLLM requires x64 Linux containers, working Compose, and a compatible NVIDIA device exposed to Docker Desktop. [Docker Desktop GPU support](https://docs.docker.com/desktop/features/gpu/) · [vLLM GPU setup](https://docs.vllm.ai/en/stable/getting_started/installation/gpu/)

## Download is slow or storage is full

The first `up` downloads the runtime image and model. Run `up --timeout 60m --dir ./my-model` if the connection needs more time. Docker volumes can live on a different disk from the deployment folder. On Docker Desktop they use its virtual disk. See [where weights are stored](USAGE.md#api-and-storage).

For a Hugging Face authentication error, check that your token has read access to the chosen repository. For `update` returning HTTP 404, check whether the catalog is publicly reachable. The built-in catalog remains available.

## The API responds but the test fails

Run `llm-launcher test --dir ./my-model` and check the container logs. A health response can arrive before the model can generate text or embeddings. Check the served model name and look for GPU, memory, or tokenizer errors in the log.

After a timeout or cancellation, inspect `status`. Use `down` to stop that deployment and `up` to retry with the saved weights. When reporting an issue, share the launcher version, engine, model ID, OS, and the first relevant error. Remove tokens, private paths, and GPU UUIDs from copied logs.
