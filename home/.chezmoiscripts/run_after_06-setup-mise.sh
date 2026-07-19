#!/usr/bin/env bash
# mise — the single cross-platform installer for portable tooling (D2).
# Decoupled from Homebrew: installed via the official curl bootstrap so it
# doesn't live in /opt/homebrew/Cellar and survives the nanobrew cutover.
#
# run_AFTER_ (not a plain run_06), on purpose. chezmoi applies plain run_ scripts
# BEFORE it deploys the dotfiles + externals, so a plain run_ here fires before
# ~/.config/mise/config.toml exists → mise sees zero tools and installs NOTHING.
# That left a fresh machine's FIRST apply with an empty mise toolchain (tools
# only appeared on a later `czu`, once the config was on disk). run_after_ runs
# after files + externals are in place, so the config is guaranteed present.
#
# run_after_ (every apply), NOT run_once_after_, also on purpose. A run_once_ that
# failed — e.g. a GitHub API rate limit during bootstrap, with `mise install ||
# true` swallowing the error — gets recorded "done" in chezmoi's scriptState and
# is NEVER retried, leaving ZERO mise tools and no way to recover via `czu`. (That
# exact double-bug shipped once; this is the fix.) Running every apply + an honest
# exit makes it idempotent AND self-healing: it no-ops fast once every tool is
# present, and a later `czu` (after `gh auth login` clears a rate limit) finishes
# the install on its own.
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

cfg="$HOME/.config/mise/config.toml"
if [ ! -f "$cfg" ]; then
  # Defensive: run_after_ guarantees the config is deployed by now, so this
  # shouldn't fire. If it ever does, bail LOUDLY (not a silent success) so the
  # next `chezmoi apply` re-runs this and installs the tools.
  echo "!! mise config not deployed yet ($cfg missing); tools install on next apply" >&2
  exit 0
fi

mise trust "$cfg" 2>/dev/null || true

# Always attempt the install — it's idempotent (already-installed tools are
# skipped in seconds, keeping routine `czu` cheap). We deliberately do NOT gate
# on `mise ls --missing`: with unresolved "latest" specs that can return empty
# BEFORE version resolution, which silently skipped the whole install and left
# the toolchain (direnv, …) uninstalled — the bug this script exists to prevent.
echo "==> installing mise tools (idempotent — already-present tools are skipped)"
# Install cargo-binstall FIRST. mise auto-uses it for cargo-backend tools (the
# explicit cargo: entries + bare names that resolve to cargo, e.g. tokei) to
# fetch PREBUILT binaries instead of compiling from source — a tokei source build
# alone took ~5min under emulation and slows every fresh bootstrap. Non-fatal: if
# it can't install, the main `mise install` below just falls back to compiling.
mise install cargo-binstall 2>/dev/null || true
# Retry on transient network failure before declaring defeat. Tool downloads hit
# GitHub/Google over TLS and a single truncated transfer ("peer closed connection
# without sending TLS close_notify") or a 503 shouldn't fail the whole apply —
# especially now that a failure here is fatal (exit 1 below). Already-installed
# tools are skipped, so a retry only re-attempts what's actually missing.
mise_install_with_retry() {
  local attempt
  for attempt in 1 2 3; do
    if mise install; then return 0; fi
    [ "$attempt" -lt 3 ] || return 1
    echo "   mise install attempt $attempt failed (often transient network) — retrying in $((attempt * 10))s" >&2
    sleep "$((attempt * 10))"
  done
}
if mise_install_with_retry; then
  # Regenerate shims so ~/.local/share/mise/shims (on PATH via path.zsh) carries
  # every tool — including ones the shell calls before `mise activate` runs.
  mise reshim 2>/dev/null || true
  echo "✓ mise tools installed"
else
  # Do NOT swallow this. A silent failure here is the precise bug being fixed:
  # surface it so the user can act, and let the next apply retry.
  echo "" >&2
  echo "!! mise install FAILED — some tools are still missing." >&2
  echo "   Most common cause: a GitHub API rate limit (mise had no token)." >&2
  echo "   Fix:  gh auth login   then re-run:  czu   (or: mise install)" >&2
  echo "   This script re-runs every apply, so it self-heals once that's done." >&2
  # Exit non-zero so `chezmoi apply` (and `mise run apply`) report failure
  # honestly instead of printing "✓ apply complete" over a broken toolchain.
  exit 1
fi
