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

## Display the model list

Choose the command for the list you need:

| List | Command | Contents |
| --- | --- | --- |
| Launcher's model catalog | `catalog` | Every catalog profile, including profiles that exceed this computer's resources. Docker is not required. |
| Suitable models | `recommend --task chat` | Profiles filtered using current hardware observations, memory estimates, task and engine. |
| Running deployments | `status` | Models managed by the launcher in the current Docker daemon. |
| Saved deployment plans | `list --dir ./runs` | Plans under that folder, including stopped and archived deployments. |

For example, in the folder containing the Linux x64 binary:

```bash
./llm-launcher-linux-amd64 catalog
./llm-launcher-linux-amd64 recommend --task chat
./llm-launcher-linux-amd64 recommend --task code --runtime vllm
```

In Windows PowerShell, use the CLI executable:

```powershell
.\llm-launcher-windows-amd64.exe catalog
.\llm-launcher-windows-amd64.exe recommend --task chat
.\llm-launcher-windows-amd64.exe recommend --task code --runtime vllm
```

For an interactive task-and-model picker, run `./llm-launcher-linux-amd64 scan` in a Linux terminal or `.\llm-launcher-windows-amd64.exe scan --interactive` in Windows PowerShell. The `catalog` commands above print the full catalog directly.

The catalog's columns are profile ID, engine, supported tasks, exact weight reference and license. Copy the profile ID from a recommendation into `plan --model ID`. Use `catalog --json` or `recommend --task chat --json` for structured output. Tasks are `chat`, `code`, `rag` and `embeddings`. Omit `--runtime` for automatic engine selection; supported filters are `ollama`, `vllm` and `llama.cpp`.

`catalog` displays the launcher's curated profiles. A compatible model outside that catalog can be selected through `hf-plan`. Use `update` to refresh catalog metadata; model files are downloaded when a deployment starts or copied with `import-weights`.

### Display models in the Windows app

1. Open **New deployment** (**Новое развёртывание**). Start Docker Desktop, then click **Scan computer** (**Сканировать компьютер**).
2. In **Task** (**Задача**), choose chat, code, answers over supplied passages, or embeddings.
3. Leave the engine and acceleration selectors on automatic for the widest choice. Selecting a particular engine or GPU narrows the results. Context 4096 is the usual starting point.
4. Click **Find models** (**Подобрать модели**). Wait for the command to finish. The journal reports the number of suitable profiles and the dropdown immediately to the right of that button opens.
5. Click a row in the dropdown. Each row shows the model name, engine, acceleration and license. If the list closes, click its down arrow to reopen it. Scroll within the list to see more results.
6. Read the selected profile's source and memory notes in the journal. **Prepare plan** saves the selected model and settings. **Launch plan** starts it. **View details…** opens the generated Compose configuration.

The catalog row at the top shows all catalog profiles, while this dropdown shows only suitable recommendations. **Update catalog** checks for signed metadata and reloads recommendations. Changing the task, engine, acceleration or context clears the old selection; click **Find models** again.

### If the model dropdown is empty

Wait for the running command to finish, then read the journal. Start Docker Desktop and scan again if it is unavailable. Select automatic engine/acceleration, reduce the context, or free memory and repeat **Find models**. vLLM requires a compatible NVIDIA GPU and a verified Linux Docker backend on Windows. CPU profiles are available through other engines when their memory requirements fit. **Technical details…** contains the original error if a command failed.

## Windows desktop

1. Start Docker Desktop with the WSL2 backend and Linux containers.
2. Open `LLM-Launcher-Windows-x64.exe` and select the folder for deployments. English is the default. The **Language** menu in the top right switches the interface immediately and remembers your choice for next time.
3. The catalog row shows the profile count and update date. **Update catalog** checks for a signed update and refreshes the recommended models. Click **Scan computer**, choose a task and runtime, then click **Find models**.
4. Pick a model, or use **Model from disk…** for downloaded weights. Leave **Port** at `0` to select a free port. **Advanced settings…** opens advanced settings. **Prepare plan** saves the plan, **View details…** opens the Compose file, and **Launch plan** starts the deployment.
5. Use **My deployments** to inspect status, view logs, test, stop, archive, or restore a deployment.

The desktop interface supports English, German, Russian, Simplified Chinese, and French. Changing languages also translates existing activity entries, including the hardware scan and model selection notes. **Technical details…** shows the original output from Docker, the model, and any diagnostics that are specific to the environment.

The Windows CLI supports the same planning and management commands. In PowerShell, invoke it as `.\llm-launcher-windows-amd64.exe`.

If Docker Desktop is stopped, start it and scan again. The Linux-container message applies when the daemon actually reports Windows containers. After changing a prepared plan, **Prepare plan** selects a new folder when the previous one already exists. Earlier deployments remain available in **My deployments**.

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

For vLLM, add `--runtime vllm` and use a repository URL such as `https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct`. The desktop app has an **Import from HF…** button when you select either engine. For gated or private weights, provide a Hugging Face read token with access. The CLI reads `LLM_LAUNCHER_HF_TOKEN` for planning and downloading. The app keeps it in the current session.

The import pins a commit and checks file sizes and hashes during download. The engine reads the verified files from `hf-model/` in the deployment directory.

## Use downloaded weights

In Windows, select **Model from disk…**, choose a Safetensors folder for vLLM or a GGUF file for llama.cpp, and provide the original Hugging Face repository or file URL. **Prepare plan** resolves the revision and verifies and copies the matching files. Planning still needs access to Hugging Face metadata. For a snapshot downloaded at an older revision, use `https://huggingface.co/OWNER/MODEL/tree/FULL_COMMIT_SHA`.

For an existing compatible plan, **Attach weights…** in **My deployments** opens a file or folder picker. The CLI equivalent is:

```bash
llm-launcher import-weights --dir ./my-model --from /path/to/downloaded-snapshot
# For a llama.cpp plan, --from points to its GGUF file instead.
llm-launcher up --dir ./my-model
```

The launcher copies only the files pinned by the plan and checks their sizes and hashes. Original downloads remain in place. Existing cache files are verified and kept; mismatches are rejected. Ollama uses its own project Docker volume. To use a downloaded Safetensors folder, choose the vLLM import path. Putting unrelated weights next to the executable or plan does not select a model.

For a downloaded Safetensors folder on a supported NVIDIA system, the complete CLI sequence is:

```bash
llm-launcher hf-plan --runtime vllm \
  --hf-url https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct \
  --context 2048 --port 0 --dir ./runs/local-qwen
llm-launcher import-weights --dir ./runs/local-qwen --from /path/to/Qwen2.5-1.5B-Instruct
llm-launcher run --dir ./runs/local-qwen
```

Replace the source URL and local path with the model you downloaded. In Windows PowerShell, replace `llm-launcher` with `.\llm-launcher-windows-amd64.exe` and invoke each command on one line. `import-weights` is a binary command shared with the desktop app. Automatic port selection and Docker diagnostics also use the same backend in both interfaces.

## Advanced settings

Pass JSON to `plan` or `hf-plan`. The desktop app exposes the same settings under **Advanced settings…**.

```bash
llm-launcher plan --task code --runtime ollama --model ollama-qwen25-coder-1.5b \
  --dir ./coding-model --settings-json '{"temperature":0.2,"max_tokens":256,"top_p":0.9}'
```

| Engine | Settings |
| --- | --- |
| Ollama | Sampling, repetition controls, seed, stop strings, and selected `OLLAMA_*` variables. An Ollama plan with generation overrides creates an API alias. Use the name printed after launch. |
| vLLM | Sampling, penalties, `dtype`, `enforce_eager`, `gpu_memory_utilization`, and `VLLM_LOGGING_LEVEL`. Use `--image` to select a supported official tag or digest. |
| llama.cpp | Sampling, repetition controls, seed, and `LLAMA_ARG_LOG_VERBOSITY`. New NVIDIA plans use level 4 to expose GPU offload evidence. |

Put runtime variables inside the JSON `environment` object. Ollama supports `OLLAMA_FLASH_ATTENTION`, `OLLAMA_KV_CACHE_TYPE`, `OLLAMA_KEEP_ALIVE`, `OLLAMA_LOAD_TIMEOUT`, and `OLLAMA_MAX_QUEUE`. Use the `LLM_USER_` prefix for non-secret custom variables. Generation defaults apply to text tasks.

## Manage deployments

| Command | Result |
| --- | --- |
| `status` | Show running and restarting launcher projects in local Docker. Use `--dir` to inspect one project. |
| `list --dir ./runs` | List saved deployment states under a directory. |
| `import-weights --dir ./my-model --from PATH` | Verify and copy downloaded GGUF or snapshot files into a compatible saved plan. |
| `logs --dir ./my-model --follow` | Stream runtime logs. |
| `logs --dir ./my-model --all` | Include the complete retained startup log. |
| `test --dir ./my-model` | Send a chat or embedding request. |
| `down --dir ./my-model` | Stop and remove the project's containers while keeping weights. |
| `delete --dir ./my-model` | Stop and archive the deployment while keeping the plan and weights. |
| `restore --dir ./my-model` | Restore the archived plan for a later `up`. |

`run` is an alias for `up`. Use `help` or `COMMAND --help` for all flags.

## API and storage

Use the base URL and model name printed at launch. With `--port 0`, planning prefers 11434 for Ollama, 8000 for vLLM, or 8080 for llama.cpp and selects another free port if needed. An explicitly occupied port fails before saving the plan. Startup checks the saved port again because another application can claim it after planning.

```bash
curl --noproxy '*' http://127.0.0.1:8080/v1/models
```

All three engines expose `/v1/chat/completions` for chat. Ollama embedding profiles also expose `/v1/embeddings`. Use the engine's API reference for request options. [Ollama](https://docs.ollama.com/api/openai-compatibility) · [vLLM](https://docs.vllm.ai/en/stable/serving/openai_compatible_server/) · [llama.cpp](https://github.com/ggml-org/llama.cpp/blob/b9935/tools/server/README.md)

Catalog weights for Ollama and vLLM live in the deployment's Docker volume. Docker Desktop keeps those volumes in its virtual disk. Catalog GGUF files and manual Hugging Face imports live in `hf-model/` next to the plan. Both `down` and `delete` retain the downloaded files.

`up` reuses the exact runtime image from the local Docker cache when available. It downloads an image only when local inspection cannot resolve that reference, then starts Compose with image pulling disabled. The recorded image ID is still checked before startup. Missing model weights can require a separate download.
