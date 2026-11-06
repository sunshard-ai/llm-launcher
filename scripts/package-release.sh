#!/usr/bin/env bash
set -euo pipefail

repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tag="${1:-}"
if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo 'Usage: bash scripts/package-release.sh vMAJOR.MINOR.PATCH' >&2
  exit 1
fi

cd -- "$repo"
test -s "docs/releases/$tag.md"
commit="$(git rev-parse --verify "refs/tags/$tag^{commit}")"
staging="$(mktemp -d)"
trap 'rm -rf -- "$staging"' EXIT
name="llm-launcher-${tag#v}"
archive="$name-source.tar.gz"

git archive --format=tar.gz --prefix="$name/" --output="$staging/$archive" "$commit"
tar -xzf "$staging/$archive" -C "$staging"
cd -- "$staging/$name"
BUILD_VERSION="${tag#v}" bash scripts/build.sh
cp -- "$staging/$archive" dist/

cd dist
assets=(
  LLM-Launcher-Windows-x64.exe
  llm-launcher-windows-amd64.exe
  llm-launcher-linux-amd64
  llm-launcher-linux-arm64
  llm-launcher-darwin-arm64
  "$archive"
)
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "${assets[@]}" > SHA256SUMS.txt
else
  shasum -a 256 "${assets[@]}" > SHA256SUMS.txt
fi
destination="$repo/dist/release/$tag"
mkdir -p -- "$destination"
cp -- "${assets[@]}" SHA256SUMS.txt "$destination/"
printf 'Release files: %s\n' "$destination"
