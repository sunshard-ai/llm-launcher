# Первичные технические источники

Проверено 6 ноября 2026 года. Ссылки описывают возможности и ограничения сторонних компонентов; конкретная поддержка launcher определяется его кодом, каталогом и результатами тестов. Версии в источниках могут измениться после этой даты.

## Платформы и GPU

| ID | Источник | Для какого решения использован |
| --- | --- | --- |
| S1 | [Docker: GPU support in Docker Desktop for Windows](https://docs.docker.com/desktop/features/gpu/) | NVIDIA GPU passthrough в Linux-контейнеры Windows через WSL2 backend; нужна отдельная проверка GPU в контейнере |
| S2 | [vLLM: GPU installation](https://docs.vllm.ai/en/stable/getting_started/installation/gpu/) | Linux-путь vLLM, ограничения нативной Windows-установки, compute capability NVIDIA от 7.5; другие ускорители требуют отдельных backends |
| S3 | [Ollama: Docker](https://docs.ollama.com/docker) | Официальный Docker-образ, CPU/NVIDIA и отдельные способы доступа AMD/Vulkan |
| S4 | [Ollama: FAQ](https://docs.ollama.com/faq) | Контекст и параллелизм, проверка CPU/GPU загрузки, ограничения контейнерного пути на Mac, proxy и local-only режим |
| S5 | [Docker Compose: GPU access](https://docs.docker.com/compose/how-tos/gpu-support/) | GPU reservations и выбор устройств в Compose |
| S6 | [NVIDIA: Installing Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) | Подготовка GPU runtime для Docker на Linux |
| S7 | [NVIDIA: CUDA on WSL guide](https://docs.nvidia.com/cuda/wsl-user-guide/index.html) | CUDA в WSL использует Windows-драйвер; Linux display driver внутри WSL не устанавливается |
| S8 | [Docker Engine: installation](https://docs.docker.com/engine/install/) | Подготовка Docker по дистрибутиву |

Наличие более широких возможностей у Ollama или vLLM не означает, что каждый путь включён в MVP. ARM GPU, AMD, Intel и Apple Metal требуют самостоятельной аппаратной квалификации.

## API и запуск

| ID | Источник | Использование |
| --- | --- | --- |
| S9 | [Ollama: Generate response](https://docs.ollama.com/api/generate) | Запрос генерации и форма ответа |
| S10 | [Ollama: Generate embeddings](https://docs.ollama.com/api/embed) | Embedding smoke test и форма вектора |
| S11 | [Ollama: OpenAI compatibility](https://docs.ollama.com/api/openai-compatibility) | Локальный OpenAI-compatible API; поддерживается подмножество OpenAI API |
| S12 | [vLLM: Using Docker](https://docs.vllm.ai/en/stable/deployment/docker/) | Контейнерный запуск и параметры инфраструктуры |
| S13 | [vLLM: OpenAI-compatible server](https://docs.vllm.ai/en/stable/serving/openai_compatible_server/) | Контракт API модели |

## Версии движков

| Компонент | Профиль по умолчанию | Проверенный первичный источник |
| --- | --- | --- |
| Ollama | `ollama/ollama:0.40.14` | [Официальный release v0.40.14](https://github.com/ollama/ollama/releases/tag/v0.40.14) |
| vLLM | `vllm/vllm-openai@sha256:befccf0e7e4089aa23926bb6f887a85c0d46c2a99e6de5c5ffdf5cb7957c5b34` | [v0.32.0, CUDA 13.0](https://github.com/vllm-project/vllm/releases/tag/v0.32.0). Прежний CUDA 12.9 образ затронут [дефектом torchcodec](https://github.com/vllm-project/vllm/issues/63121) |

Эти версии выбраны как конкретная стартовая конфигурация, а не как обещание универсальной совместимости или самых новых релизов. Доступность образа в registry и его запуск проверяются при `up`. Явный tag не равен неизменяемому digest.

## Стартовые модели

| Источник | Использование |
| --- | --- |
| [Ollama Qwen2.5 tags](https://ollama.com/library/qwen2.5/tags) | Точные идентификаторы вариантов весов для текстового профиля |
| [Ollama Qwen2.5-Coder tags](https://ollama.com/library/qwen2.5-coder/tags) | Варианты для задач программирования |
| [Ollama BGE-M3](https://ollama.com/library/bge-m3) | Поддержка embedding-профиля `bge-m3:567m` |
| [BAAI/BGE-M3 model card](https://huggingface.co/BAAI/bge-m3) | Происхождение и описание embedding-модели |
| [Qwen2.5-7B-Instruct-AWQ model card](https://huggingface.co/Qwen/Qwen2.5-7B-Instruct-AWQ) | Официальный квантованный вариант для vLLM |

Точные ссылки каждой записи находятся в поле `url` встроенного каталога. Дополнительные сведения о проверке тегов и лицензий — в [internal/catalog/SOURCES.md](../internal/catalog/SOURCES.md).

Оценки `min_ram_gib`, `min_vram_gib` и `disk_gib` — решения этого проекта, а не гарантированные производителем минимумы. Они требуют калибровки на реальных машинах. Источники не используются как доказательство конкретной скорости инференса в launcher.

## Go и сборка

- [Установка Go](https://go.dev/doc/install).
- [Документация команды go](https://pkg.go.dev/cmd/go): `build`, `test`, `vet`, целевая ОС/архитектура и параметры сборки.
- [Windows Forms](https://learn.microsoft.com/en-us/dotnet/desktop/winforms/overview/): системная оболочка предварительного Windows EXE.
- [rsrc](https://github.com/akavel/rsrc): инструмент сборки ресурса иконки из исходного ICO. Для обновления ресурса из корня репозитория используйте `go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico assets/llm-launcher.ico -o cmd/llm-launcher/icon_windows_amd64.syso`. Обычная сборка использует готовый ресурс из репозитория.

Номер версии SDK, которым реально собрана поставка, указывается в отчёте проверки этой поставки. Минимальная языковая версия задаётся в `go.mod`.

## Несколько GPU и дополнительные движки

| Источник | Для какого решения используется |
| --- | --- |
| [vLLM: Parallelism and Scaling](https://docs.vllm.ai/en/stable/serving/parallelism_scaling/) | Разделить tensor/pipeline parallel и распределение запросов по репликам |
| [Ollama: FAQ](https://docs.ollama.com/faq) | Учитывать стратегию размещения модели и проверять CPU/GPU после загрузки |
| [llama.cpp: Multi-GPU](https://github.com/ggml-org/llama.cpp/blob/master/docs/multi-gpu.md) | Выбирать split mode, пропорции, допустимые форматы и конкретные GPU |
| [SGLang: репозиторий движка](https://github.com/sgl-project/sglang) | Квалифицировать отдельный контейнерный backend и профиль TP |
| [Docker Model Runner: Inference engines](https://docs.docker.com/ai/model-runner/inference-engines/) | Описывать возможности Docker Model Runner отдельно от обычных Compose-сервисов |

## Проф. настройки и импорт моделей

| Источник | Для чего использован |
| --- | --- |
| [Ollama: Create a model](https://docs.ollama.com/api/create) | Отдельное имя модели с параметрами генерации через `/api/create` |
| [Ollama: переменные версии 0.40.14](https://github.com/ollama/ollama/blob/v0.40.14/envconfig/config.go) | Допустимые серверные настройки и значения KV-cache |
| [vLLM: serve](https://docs.vllm.ai/en/latest/cli/serve/) и [реализация версии 0.32.0](https://github.com/vllm-project/vllm/blob/v0.32.0/vllm/config/model.py) | Параметры по умолчанию, лимит токенов, dtype и запуск локального снимка |
| [Hugging Face Hub API](https://huggingface.co/docs/hub/api) | Метаданные репозитория, commit и контрольные суммы файлов |
| [llama.cpp b9935: сервер](https://github.com/ggml-org/llama.cpp/blob/b9935/tools/server/README.md) | Аргументы CPU-сервера, GGUF, имя API и проверка ответа |

Образ llama.cpp `server-b9935` закреплён на OCI index digest `sha256:295dc9897fa8a643e4a513fbcaada51d3b8db4b0afa4fda7aeae2386757de58b`. В реестре подтверждены варианты Linux amd64 и arm64.

[llama.cpp b9935 в Docker](https://github.com/ggml-org/llama.cpp/blob/b9935/docs/docker.md) описывает образ `server-cuda`. Для новых планов NVIDIA закреплён digest `sha256:502fde462776339020cec39425525e9ce78f17cd9f7b14123f55f5197b1da00a`. Проверка выгрузки слоёв использует сообщение [загрузчика этой версии](https://github.com/ggml-org/llama.cpp/blob/b9935/src/llama-model.cpp).

Точные карточки и источники конкретных файлов приведены в [catalog.json](../catalog.json). Для новых семейств использованы [Qwen3](https://huggingface.co/collections/Qwen/qwen3), [Qwen3 Coder](https://huggingface.co/Qwen/Qwen3-Coder-30B-A3B-Instruct), [DeepSeek R1 0528](https://huggingface.co/deepseek-ai/DeepSeek-R1-0528-Qwen3-8B), [Mistral 7B v0.3](https://huggingface.co/mistralai/Mistral-7B-Instruct-v0.3), [Gemma 3 1B](https://huggingface.co/google/gemma-3-1b-it), [Phi-4-mini](https://huggingface.co/microsoft/Phi-4-mini-instruct), [Granite 3.3 2B](https://huggingface.co/ibm-granite/granite-3.3-2b-instruct), [Nomic Embed Text](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5) и [gpt-oss-120b](https://huggingface.co/openai/gpt-oss-120b). Профили MXFP4 учитывают [ограничения GPU из рецепта vLLM для gpt-oss](https://docs.vllm.ai/projects/recipes/en/latest/OpenAI/GPT-OSS.html).

[CUDA 13 и требования драйвера vLLM](https://docs.vllm.ai/en/v0.32.0/getting_started/installation/gpu/) и [текущий канал Docker Hub](https://hub.docker.com/r/vllm/vllm-openai/tags) используются для выбора образа.
