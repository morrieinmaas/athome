#!/usr/bin/env bash
# install-mise.sh — the ONE prerequisite the mise front door can't self-install:
# mise. It installs mise, trusts this repo's mise.toml, and wires `mise activate`
# into ~/.zshrc IF it isn't already active — then STOPS. You run the rest:
#
#   ./scripts/install-mise.sh
#   source ~/.zshrc          # (or open a new shell)
#   mise run bootstrap
#
# Bootstrapping the bootstrapper, like `curl rustup | sh` before cargo.
# Standalone alternative (one command, no mise-routing): ./scripts/bootstrap.sh
set -euo pipefail

if command -v mise >/dev/null 2>&1; then
  echo "✓ mise already installed: $(mise --version)"
else
  echo "==> installing mise via mise.run"
  # MISE_INSTALL_HELP=0 silences mise.run's "echo … >> ~/.zshrc" hint — we wire
  # activation ourselves below (and athome's dotfiles own it after bootstrap).
  curl -fsSL https://mise.run | MISE_INSTALL_HELP=0 sh
fi

mise_bin="$(command -v mise || echo "$HOME/.local/bin/mise")"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
"$mise_bin" trust "$repo_root/mise.toml" >/dev/null 2>&1 || true

# Wire activation for new shells so the next `mise run bootstrap` finds mise.
# Skip if it's already active anywhere ~/.zshrc sources (athome's completions.zsh
# activates it once dotfiles are deployed) — avoids a duplicate line. Uses the
# absolute mise path so it works even before ~/.local/bin is on PATH. The
# bootstrap's `chezmoi apply --force` later replaces ~/.zshrc with the template,
# so anything added here is only a pre-bootstrap stopgap.
rc="$HOME/.zshrc"
if grep -rqs "mise activate zsh" "$rc" "$HOME/.zsh" 2>/dev/null; then
  echo "✓ mise activation already wired — nothing added to ~/.zshrc"
else
  printf 'eval "$(%s activate zsh)"\n' "$mise_bin" >> "$rc"
  echo "✓ wired mise activation into ~/.zshrc"
fi

echo
echo "Next:  source ~/.zshrc   (or open a new shell), then:  mise run bootstrap"
