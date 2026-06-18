#!/usr/bin/env bash
# install-mise.sh — the ONE prerequisite the mise-task front door can't install
# itself: mise. After this, `mise run bootstrap` provisions the machine and
# `mise run <task>` runs everything else.
#
# This is bootstrapping the bootstrapper — the same irreducible first step as
# `curl https://sh.rustup.rs | sh` before you can use cargo, or nvm before npm.
#
# Standalone alternative (no mise needed, single command): ./scripts/bootstrap.sh
set -euo pipefail

if command -v mise >/dev/null 2>&1; then
  echo "✓ mise already installed: $(mise --version)"
else
  echo "==> installing mise via mise.run"
  curl -fsSL https://mise.run | sh
fi

# Trust this repo's mise.toml so `mise run` works without a prompt. Use the
# resolved binary path — a freshly-installed mise (~/.local/bin/mise) isn't on
# the CURRENT shell's PATH yet (mise.run wires that into your shell rc).
mise_bin="$(command -v mise || echo "$HOME/.local/bin/mise")"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
"$mise_bin" trust "$repo_root/mise.toml" >/dev/null 2>&1 || true

echo
echo "✓ mise ready. Next:"
if command -v mise >/dev/null 2>&1; then
  echo "    mise run bootstrap"
else
  echo "    open a new shell (or run: export PATH=\"\$HOME/.local/bin:\$PATH\"), then:"
  echo "    mise run bootstrap"
fi
