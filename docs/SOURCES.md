# Technical references

## Docker and GPUs

- [Docker Engine installation](https://docs.docker.com/engine/install/) and [Compose GPU access](https://docs.docker.com/compose/how-tos/gpu-support/).
- [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) for Linux.
- [Docker Desktop GPU support](https://docs.docker.com/desktop/features/gpu/) and [NVIDIA CUDA on WSL](https://docs.nvidia.com/cuda/wsl-user-guide/index.html) for Windows.
- [Docker container logs](https://docs.docker.com/reference/cli/docker/container/logs/) documents reading both output streams and filtering by the current container start with `--since`. [Compose logs](https://docs.docker.com/reference/cli/docker/compose/logs/) documents `--tail all` for full startup diagnostics.
- Runtime image acquisition first uses [image inspect](https://docs.docker.com/reference/cli/docker/image/inspect/) for the exact saved reference, then downloads only if that inspection fails. Startup uses [Compose up `--pull never`](https://docs.docker.com/reference/cli/docker/compose/up/) after inspection. Compose 2.38.2's [`imageAlreadyPresent`](https://github.com/docker/compose/blob/v2.38.2/pkg/compose/pull.go#L147-L158) requires a tagged reference, so `pull --policy missing` can contact the registry for an already cached digest. [Docker credential helpers](https://docs.docker.com/reference/cli/docker/login/#credential-helpers)

## Runtime behavior

Windows command startup uses a session-owned executable path and an explicit [ProcessStartInfo.WorkingDirectory](https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.processstartinfo.workingdirectory?view=netframework-4.8.1). Docker diagnostics distinguish a failed [docker info](https://docs.docker.com/reference/cli/docker/system/info/) probe from a reachable server reporting Windows containers. Local weight imports use the same pinned file hashes as the Hugging Face download path; no runtime arguments or model-data formats are added.

| Runtime | Reference | Used for |
| --- | --- | --- |
| Ollama | [Docker](https://docs.ollama.com/docker) · [GPU support](https://docs.ollama.com/gpu) · [FAQ](https://docs.ollama.com/faq) | Container startup, NVIDIA admission, memory and context behavior |
| Ollama | [API compatibility](https://docs.ollama.com/api/openai-compatibility) · [Create](https://docs.ollama.com/api/create) · [Embedding](https://docs.ollama.com/api/embed) · [Environment options](https://github.com/ollama/ollama/blob/v0.40.14/envconfig/config.go) | Inference checks, aliases, advanced settings |
| vLLM | [v0.32.0 GPU setup](https://docs.vllm.ai/en/v0.32.0/getting_started/installation/gpu/) · [Docker](https://docs.vllm.ai/en/stable/deployment/docker/) · [Serve options](https://docs.vllm.ai/en/latest/cli/serve/) | Driver compatibility, image selection, and generation defaults |
| vLLM | [API server](https://docs.vllm.ai/en/stable/serving/openai_compatible_server/) · [gpt-oss recipe](https://docs.vllm.ai/projects/recipes/en/latest/OpenAI/GPT-OSS.html) | Chat endpoint and GPU requirements for MXFP4 |
| llama.cpp | [b9935 Docker images](https://github.com/ggml-org/llama.cpp/blob/b9935/docs/docker.md) · [b9935 server](https://github.com/ggml-org/llama.cpp/blob/b9935/tools/server/README.md) · [Model loader](https://github.com/ggml-org/llama.cpp/blob/b9935/src/llama-model.cpp) | CPU/CUDA images, GGUF launch options, and GPU offload check |

Release image references are recorded in [catalog.json](../catalog.json) and the [llama.cpp image constants](../internal/catalog/catalog.go). The pinned vLLM image uses CUDA 13.0. The earlier CUDA 12.9 image had a [torchcodec dependency failure](https://github.com/vllm-project/vllm/issues/63121).

llama.cpp b9935 routes library INFO messages, including layer offload details, through the trace threshold (4). New GPU plans set `LLAMA_ARG_LOG_VERBOSITY=4` so the launcher can verify offload. [Log callback](https://github.com/ggml-org/llama.cpp/blob/b9935/common/log.cpp) · [Logging levels](https://github.com/ggml-org/llama.cpp/blob/b9935/common/log.h) · [Supported environment option](https://github.com/ggml-org/llama.cpp/blob/b9935/common/arg.cpp)

## Model provenance

Use the `url` and `profiles.source` fields in [catalog.json](../catalog.json) for the weight artifact and upstream model card. Revisions and hashes are recorded where supported. [Catalog provenance](../internal/catalog/SOURCES.md) · [Hugging Face Hub API](https://huggingface.co/docs/hub/api)

## Development and roadmap

Release checkout uses the selected tag through the pinned [actions/checkout `ref` input](https://github.com/actions/checkout/tree/v4#usage), including manually dispatched releases. The same tag supplies source, desktop checks and release notes.

- [Go command reference](https://pkg.go.dev/cmd/go) and [Windows Forms](https://learn.microsoft.com/en-us/dotnet/desktop/winforms/overview/).
- [vLLM parallelism](https://docs.vllm.ai/en/stable/serving/parallelism_scaling/) and [llama.cpp multi-GPU](https://github.com/ggml-org/llama.cpp/blob/master/docs/multi-gpu.md) for future multi-GPU work.
- [SGLang](https://github.com/sgl-project/sglang) and [Docker Model Runner inference engines](https://docs.docker.com/ai/model-runner/inference-engines/) for future adapters.
- [rsrc](https://github.com/akavel/rsrc) for updating the embedded Windows icon. The checked-in resource is used by ordinary builds.

```bash
go run github.com/akavel/rsrc@v0.10.2 -arch amd64 \
  -ico assets/llm-launcher.ico -o cmd/llm-launcher/icon_windows_amd64.syso
```
