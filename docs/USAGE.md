# User guide

Install Docker with Compose v2 before starting a deployment. The launcher uses the local Docker engine and publishes model APIs on `127.0.0.1`.

## Choose and run a model

On Linux x64, use `./llm-launcher-linux-amd64 scan` in a terminal for the shortest path. Choose chat, code, RAG, or embeddings, then select a model. To preview a launch, use `scan --interactive --dry-run`. For hardware details alone, use `scan --info` or `scan --json`.

Use `wizard --dir ./my-model` to review a plan before starting it. For scripts, choose a model ID from `recommend`, save a plan, and run it. Examples below use `llm-launcher` for your platform's CLI binary.

```bash
llm-launcher recommend --task chat --runtime llama.cpp --cpu
llm-launcher plan --task chat --runtime llama.cpp --model llamacpp-qwen3-0.6b \
  --cpu --context 1024 --dir ./my-model
llm-launcher inspect --dir ./my-model
llm-launcher up --dir ./my-model
```

Use `--gpu 0` or a GPU UUID to select a specific card. Use `inspect --compose --dir ./my-model` to see the generated Compose file. To change launch settings, write a plan in a new directory.

## Windows desktop

1. Start Docker Desktop with the WSL2 backend and Linux containers.
2. Open `LLM-Launcher-Windows-x64.exe` and select the folder for deployments.
3. Click **Сканировать компьютер** (Scan), choose a task and engine, then click **Подобрать модели** (Find models).
4. Pick a model. **Проф. настройки…** opens advanced settings. **Подготовить план** saves the plan, **Посмотреть детали…** opens the Compose file, and **Запустить подготовленный план** starts the deployment.
5. Use **Мои развёртывания** (My deployments) to inspect status, view logs, test, stop, archive, or restore a deployment.

The Windows CLI supports the same planning and management commands. In PowerShell, invoke it as `.\llm-launcher-windows-amd64.exe`.

## Import from Hugging Face

| Runtime | Input | Execution |
| --- | --- | --- |
| llama.cpp | HTTPS link to a single `.gguf` file | CPU |
| vLLM | HTTPS link to a compatible text model repository with Safetensors and a tokenizer | NVIDIA |

```bash
llm-launcher hf-plan \
  --hf-url 'https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/main/Qwen3-0.6B-Q8_0.gguf' \
  --context 1024 --dir ./my-gguf
llm-launcher up --dir ./my-gguf
```

For vLLM, add `--runtime vllm` and use a repository URL such as `https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct`. The desktop app has a **Свой HF…** button when you select either engine. For gated or private weights, provide a Hugging Face read token with access. The CLI reads `LLM_LAUNCHER_HF_TOKEN` for planning and downloading. The app keeps it in the current session.

The import pins a commit and checks file sizes and hashes during download. The engine reads the verified files from `hf-model/` in the deployment directory.

## Advanced settings

Pass JSON to `plan` or `hf-plan`. The desktop app exposes the same settings under **Проф. настройки…**.

```bash
llm-launcher plan --task code --runtime ollama --model ollama-qwen25-coder-1.5b \
  --dir ./coding-model --settings-json '{"temperature":0.2,"max_tokens":256,"top_p":0.9}'
```

| Engine | Settings |
| --- | --- |
| Ollama | Sampling, repetition controls, seed, stop strings, and selected `OLLAMA_*` variables. An Ollama plan with generation overrides creates an API alias. Use the name printed after launch. |
| vLLM | Sampling, penalties, `dtype`, `enforce_eager`, `gpu_memory_utilization`, and `VLLM_LOGGING_LEVEL`. Use `--image` to select a supported official tag or digest. |
| llama.cpp | Sampling, repetition controls, and seed. |

Put runtime variables inside the JSON `environment` object. Ollama supports `OLLAMA_FLASH_ATTENTION`, `OLLAMA_KV_CACHE_TYPE`, `OLLAMA_KEEP_ALIVE`, `OLLAMA_LOAD_TIMEOUT`, and `OLLAMA_MAX_QUEUE`. Use the `LLM_USER_` prefix for non-secret custom variables. Generation defaults apply to text tasks.

## Manage deployments

| Command | Result |
| --- | --- |
| `status` | Show running and restarting launcher projects in local Docker. Use `--dir` to inspect one project. |
| `list --dir ./runs` | List saved deployment states under a directory. |
| `logs --dir ./my-model --follow` | Stream runtime logs. |
| `test --dir ./my-model` | Send a chat or embedding request. |
| `down --dir ./my-model` | Stop and remove the project's containers while keeping weights. |
| `delete --dir ./my-model` | Stop and archive the deployment while keeping the plan and weights. |
| `restore --dir ./my-model` | Restore the archived plan for a later `up`. |

`run` is an alias for `up`. Use `help` or `COMMAND --help` for all flags.

## API and storage

Use the base URL and model name printed at launch. The default ports are 11434 for Ollama, 8000 for vLLM, and 8080 for llama.cpp. Set different ports when running several projects.

```bash
curl --noproxy '*' http://127.0.0.1:8080/v1/models
```

All three engines expose `/v1/chat/completions` for chat. Ollama embedding profiles also expose `/v1/embeddings`. Use the engine's API reference for request options. [Ollama](https://docs.ollama.com/api/openai-compatibility) · [vLLM](https://docs.vllm.ai/en/stable/serving/openai_compatible_server/) · [llama.cpp](https://github.com/ggml-org/llama.cpp/blob/b9935/tools/server/README.md)

Catalog weights for Ollama and vLLM live in the deployment's Docker volume. Docker Desktop keeps those volumes in its virtual disk. Catalog GGUF files and manual Hugging Face imports live in `hf-model/` next to the plan. Both `down` and `delete` retain the downloaded files.
