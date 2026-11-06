#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd -- "$project_dir"

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) executable="$project_dir/dist/llm-launcher-linux-amd64" ;;
  Linux/aarch64|Linux/arm64) executable="$project_dir/dist/llm-launcher-linux-arm64" ;;
  Darwin/arm64) executable="$project_dir/dist/llm-launcher-darwin-arm64" ;;
  *) echo 'No bundled binary for this OS/architecture. Build it from source.' >&2; exit 1 ;;
esac

if [[ ! -x "$executable" ]]; then
  echo "Missing executable: $executable" >&2
  echo 'Run bash scripts/build.sh, or restore execute permission with chmod +x.' >&2
  exit 1
fi

exec "$executable" wizard --dir "$project_dir/my-model" "$@"
