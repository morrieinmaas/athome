#!/usr/bin/env bash
# mise — the single cross-platform installer for portable tooling (D2).
# Decoupled from Homebrew: installed via the official curl bootstrap so it
# doesn't live in /opt/homebrew/Cellar and survives the nanobrew cutover.
#
# PLAIN run_ (every apply), NOT run_once_, on purpose. A run_once_ that failed —
# e.g. a GitHub API rate limit during bootstrap, with `mise install || true`
# swallowing the error — gets recorded "done" in chezmoi's scriptState and is
# NEVER retried, leaving the machine with ZERO mise tools and no way to recover
# via `czu`. (That exact double-bug shipped once; this is the fix.) Running every
# apply + an honest exit makes it idempotent AND self-healing: it no-ops fast
# once every tool is present, and a later `czu` (after `gh auth login` clears the
# rate limit) finishes the install on its own.
set -euo pipefail

if ! command -v mise >/dev/null 2>&1; then
  echo "==> installing mise via mise.run"
  curl -fsSL https://mise.run | sh
fi

# mise.run installs to ~/.local/bin/mise — reachable for the rest of THIS script
# even before a login shell sources path.zsh.
export PATH="$HOME/.local/bin:$PATH"

if ! command -v mise >/dev/null 2>&1; then
  echo "!! mise still not on PATH after install; skipping tool install" >&2
  exit 0
fi

mise trust "$HOME/.config/mise/config.toml" 2>/dev/null || true

# Fast path: nothing missing → done. Keeps every routine `czu` cheap (a local
# state check, no network) once the machine is settled.
if [ -z "$(mise ls --missing 2>/dev/null)" ]; then
  exit 0
fi

echo "==> installing missing mise tools"
if mise install; then
  # Regenerate shims so ~/.local/share/mise/shims (on PATH via path.zsh) carries
  # every tool — including ones the shell calls before `mise activate` runs.
  mise reshim 2>/dev/null || true
else
  # Do NOT swallow this. A silent failure here is the precise bug being fixed:
  # surface it so the user can act, and let the next apply retry.
  echo "" >&2
  echo "!! mise install FAILED — some tools are still missing." >&2
  echo "   Most common cause: a GitHub API rate limit (mise had no token)." >&2
  echo "   Fix:  gh auth login   then re-run:  czu   (or: mise install)" >&2
  echo "   This script re-runs every apply, so it self-heals once that's done." >&2
fi
