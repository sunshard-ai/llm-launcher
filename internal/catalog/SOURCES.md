# Источники каталога

Ниже приведены сведения о стартовых профилях Qwen2.5 и Ollama. Для расширенного каталога 0.3.0 см. [источники моделей и движков](../../docs/SOURCES.md) и [матрицу выбора](../../docs/MODELS.md).

Primary references:

- Qwen2.5 quantized Ollama tags: https://ollama.com/library/qwen2.5/tags
- Exact 7B Q4_K_M tag and Apache 2.0 license: https://ollama.com/library/qwen2.5:7b-instruct-q4_K_M
- Qwen2.5 Coder tags: https://ollama.com/library/qwen2.5-coder/tags
- Exact Coder 7B Q4_K_M tag: https://ollama.com/library/qwen2.5-coder:7b-instruct-q4_K_M
- Multilingual embedding model: https://ollama.com/library/bge-m3:567m
- BGE-M3 original model and MIT license: https://huggingface.co/BAAI/bge-m3
- FP16 Qwen: https://huggingface.co/Qwen/Qwen2.5-1.5B-Instruct
- Qwen AWQ: https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-AWQ
- Qwen AWQ metadata: https://huggingface.co/api/models/Qwen/Qwen2.5-7B-Instruct-AWQ
- Qwen 14B AWQ: https://huggingface.co/Qwen/Qwen2.5-14B-Instruct-AWQ
- Coder AWQ: https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-AWQ
- Ollama runtime release: https://github.com/ollama/ollama/releases/tag/v0.40.14
- vLLM runtime release/images: https://github.com/vllm-project/vllm/releases/tag/v0.32.0
- NVIDIA hardware prerequisites: https://docs.ollama.com/gpu
- CUDA 12.x driver minimum and CUDA 12.9.1 toolkit-driver distinction: https://docs.nvidia.com/cuda/archive/12.9.1/cuda-toolkit-release-notes/index.html
- CUDA minor-compatibility limitations (including PTX): https://docs.nvidia.com/deploy/cuda-compatibility/minor-version-compatibility.html
- Docker support: https://docs.ollama.com/docker and https://docs.docker.com/desktop/features/gpu/
- vLLM GPU support: https://docs.vllm.ai/en/stable/getting_started/installation/gpu/

All included Qwen model sizes use Apache-2.0. Qwen2.5 3B and 72B have distinct licensing and are deliberately absent. Model licenses remain those of their publishers.

`MinRAMGiB`, `MinVRAMGiB`, and `DiskGiB` are engineering estimates for initial admission, not publisher guarantees or measurements from this machine. RAM requirements apply also when the GPU is selected, to leave capacity for model loading. VRAM estimates include weights, KV cache for 4096 tokens and one request, plus runtime headroom. Disk estimates include model staging; the planner adds 4 GiB for Ollama or 20 GiB for vLLM images and layers. CUDA image sizes can change between tagged rebuilds. The reported free disk belongs to the chosen host path; Docker may use a different disk or virtual disk.

Full HF revisions are pinned for Qwen2.5-7B-Instruct-AWQ and new vLLM entries. Entries without a pinned revision explicitly disclose that in recommendation notes. Ollama tags can be retagged. The launcher records the observed digest after download.

GPU filtering never sums VRAM across cards. Known free memory is required. The selected NVIDIA GPU must pass a later Docker GPU check and real inference before readiness is claimed. CPU and GPU availability can change after planning.


`Context` is the maximum context supported by the starter serving profile: 32768 tokens for the included Qwen checkpoints and 8192 for BGE-M3. The default is 4096. Resource estimates remain based on 4096 tokens; CLI planning and deployment preflight multiply their RAM/VRAM budgets conservatively by `max(1, requested_context/4096)`. This deliberately overestimates weight growth and is an admission heuristic, not an exact KV-cache formula.

The NVIDIA gate follows Ollama's documented compute capability >=5.0 and driver branch >=550. Capabilities 5.0 through 6.2 need >=570. The default vLLM CUDA 13 image requires R580 or newer. The CUDA 12.x alternative uses capability >=7.5 and Linux driver >=525.60.13 as a preliminary minimum. BF16 and MXFP4 profiles require capability >=8.0. PTX or newer features may require an upgraded driver. The launcher checks the container and makes an inference request before reporting readiness.

## Профили для быстрого выбора на Linux

- [Mistral 7B Instruct v0.3](https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3) и [точный Q4_K_M тег Ollama](https://ollama.com/library/mistral:7b-instruct-v0.3-q4_K_M). Apache-2.0. Используется шаблон из тега.
- [Meta Llama 3.2 3B Instruct](https://huggingface.co/meta-llama/Llama-3.2-3B-Instruct) и [точный Q4_K_M тег Ollama](https://ollama.com/library/llama3.2:3b-instruct-q4_K_M). Llama 3.2 Community License. Используется шаблон из тега.
- [DeepSeek R1 0528 Qwen3 8B](https://huggingface.co/deepseek-ai/DeepSeek-R1-0528-Qwen3-8B) и [точный Q4_K_M тег Ollama](https://ollama.com/library/deepseek-r1:8b-0528-qwen3-q4_K_M). MIT. Температура 0.6 указана для использования модели, Top P 0.95 взят из параметров семплирования, описанных авторами. Smoke-запрос получает до 512 токенов с учётом контекста.

Для трёх тегов размеры и лицензии проверены по источникам. Бюджеты памяти и диска являются стартовыми оценками при контексте 4096 и одной последовательности. Этот статус не подразумевает аппаратную квалификацию.
