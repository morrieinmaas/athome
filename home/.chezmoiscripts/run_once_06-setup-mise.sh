#!/usr/bin/env bash
# mise — the single cross-platform installer for portable tooling (D2).
# Decoupled from Homebrew: installed via the official curl bootstrap so it
# doesn't live in /opt/homebrew/Cellar and survives the nanobrew cutover.
set -euo pipefail

if ! command -v mise >/dev/null 2>&1; then
  echo "==> installing mise via mise.run"
  curl -fsSL https://mise.run | sh
fi

# mise.run installs to ~/.local/bin/mise — make sure it's reachable for the
# rest of THIS script even before a login shell sources path.zsh.
export PATH="$HOME/.local/bin:$PATH"

if ! command -v mise >/dev/null 2>&1; then
  echo "!! mise still not on PATH after install; skipping tool install"
  exit 0
fi

# Trust the global config and install every tool in [tools].
mise trust "$HOME/.config/mise/config.toml" 2>/dev/null || true
mise install || true
# Regenerate shims so ~/.local/share/mise/shims (on PATH via path.zsh) carries
# every tool — including ones the shell calls before `mise activate` runs.
mise reshim 2>/dev/null || true
