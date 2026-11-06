#!/usr/bin/env bash
set -euo pipefail

project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
go_bin="${GO_BIN:-go}"
build_version="${BUILD_VERSION:-0.3.2}"

if [[ ! "$build_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9.-]+)?$ ]]; then
  echo 'BUILD_VERSION must be a semantic version, for example 0.3.0.' >&2
  exit 1
fi

cd -- "$project_dir"
"$go_bin" version
native_os="$("$go_bin" env GOHOSTOS)"
native_arch="$("$go_bin" env GOHOSTARCH)"
CGO_ENABLED=0 GOOS="$native_os" GOARCH="$native_arch" "$go_bin" test ./...
CGO_ENABLED=0 GOOS="$native_os" GOARCH="$native_arch" "$go_bin" vet ./...
mkdir -p dist

for build_target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
  target_os="${build_target%/*}"
  target_arch="${build_target#*/}"
  suffix=''
  if [[ "$target_os" == windows ]]; then suffix='.exe'; fi
  output_path="dist/llm-launcher-${target_os}-${target_arch}${suffix}"
  echo "Building $output_path"
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    "$go_bin" build -trimpath -buildvcs=false \
      -ldflags "-s -w -X main.version=${build_version}" \
      -o "$output_path" ./cmd/llm-launcher
done

desktop_path="dist/LLM-Launcher-Windows-x64.exe"
echo "Building $desktop_path"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
  "$go_bin" build -trimpath -buildvcs=false \
    -ldflags "-s -w -H=windowsgui -X main.version=${build_version} -X main.desktopMode=1" \
    -o "$desktop_path" ./cmd/llm-launcher

cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum llm-launcher-linux-amd64 llm-launcher-linux-arm64 \
    llm-launcher-windows-amd64.exe llm-launcher-darwin-arm64 LLM-Launcher-Windows-x64.exe > SHA256SUMS.txt
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 llm-launcher-linux-amd64 llm-launcher-linux-arm64 \
    llm-launcher-windows-amd64.exe llm-launcher-darwin-arm64 LLM-Launcher-Windows-x64.exe > SHA256SUMS.txt
else
  echo 'Built binaries. Install sha256sum or shasum to create checksums.' >&2
fi
