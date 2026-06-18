#!/usr/bin/env bash
# run.sh — build + run the athome bootstrap e2e locally (docker or podman).
#
#   test/e2e/run.sh          # build image + run the full bootstrap + asserts
#   test/e2e/run.sh --build  # build the image only
#
# Passes a GITHUB_TOKEN into the run (from $GITHUB_TOKEN or `gh auth token`) so
# mise doesn't hit the anon rate limit. CI uses the same image via e2e.yml.
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root (Dockerfile COPYs the whole tree)

engine="$(command -v podman || command -v docker || true)"
[ -n "$engine" ] || { echo "need docker or podman on PATH" >&2; exit 1; }
img="athome-e2e:latest"

# Arch Linux ships x86_64 only — pin amd64 so this works on Apple Silicon too
# (emulated, slow) and matches CI's amd64 runners (native).
platform="${ATHOME_E2E_PLATFORM:-linux/amd64}"

echo "==> building $img with ${engine##*/} (platform=$platform)"
"$engine" build --platform "$platform" -f test/e2e/Dockerfile -t "$img" .

[ "${1:-}" = "--build" ] && { echo "built (skip run)"; exit 0; }

token="${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}"
[ -n "$token" ] || echo "warning: no GITHUB_TOKEN / gh token — mise may hit the GitHub rate limit" >&2

echo "==> running bootstrap e2e"
exec "$engine" run --rm --platform "$platform" \
  -e "GITHUB_TOKEN=$token" -e "MISE_GITHUB_TOKEN=$token" "$img"
