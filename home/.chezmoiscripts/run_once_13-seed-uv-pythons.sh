#!/usr/bin/env bash
# Seed common Python versions via uv so `uv run` / `uv venv` work
# out-of-the-box without first downloading an interpreter.
# uv installs to ~/.local/share/uv/python/... (managed by uv itself).
set -euo pipefail

command -v uv >/dev/null 2>&1 || { echo "uv not installed; skipping"; exit 0; }

# Idempotent: uv skips already-installed versions
uv python install 3.13 3.12 3.11

# Pin the global default
uv python pin 3.13 --global 2>/dev/null || true

echo "==> uv managed Pythons:"
uv python list --only-installed 2>/dev/null || uv python list
