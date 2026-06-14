#!/usr/bin/env bash
# Tail-end cleanup: swap chezmoi remote HTTPS -> SSH once SSH keys are deployed.
#
# NB: never shell out to `chezmoi` from inside a chezmoi-run script — it tries
# to acquire the same persistent state lock and times out. Use the env vars
# chezmoi exports to scripts instead.
set -euo pipefail

src="${CHEZMOI_WORKING_TREE:-${CHEZMOI_SOURCE_DIR:-}}"
if [[ -z "$src" || ! -d "$src/.git" ]]; then
  echo "!! cleanup: no chezmoi source/working tree env var set; skipping remote swap"
  exit 0
fi
cd "$src"

remote="$(git remote get-url origin 2>/dev/null || true)"
if [[ "$remote" == https://github.com/* ]]; then
  ssh_url="git@github.com:${remote#https://github.com/}"
  ssh_url="${ssh_url%.git}.git"
  echo "==> switching chezmoi remote: $remote -> $ssh_url"
  git remote set-url origin "$ssh_url"
fi

echo "==> chezmoi apply complete. Reload your shell: exec zsh"
