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
  # ...but say so if it came from a package manager rather than from mise.run.
  #
  # This check accepts ANY mise on PATH, which is right for a bootstrap and is
  # also how a machine ends up with Homebrew's mise: brew installs it as a
  # dependency or by hand, this script finds it, and the curl install never
  # happens. That was survivable until it wasn't. Homebrew's mise pulls in its
  # own `usage` as a dependency, which then shadows the newer one mise manages,
  # and because athome upgrades through `nb` rather than `brew` these leftovers
  # never move: one machine sat four months behind without a hint.
  #
  # Not auto-replaced. Swapping the tool that manages every other tool is not
  # something a bootstrap should do behind someone's back, and the two-command
  # fix is easy to run deliberately.
  mise_found="$(command -v mise)"
  case "$mise_found" in
    "$HOME/.local/bin/mise" | "$HOME/.local/share/mise/"*) ;;
    *)
      echo "  note: that mise is at $mise_found, not the mise.run location"
      echo "        ($HOME/.local/bin/mise). athome expects the curl-installed"
      echo "        one; a package-managed mise drifts out of date and can"
      echo "        shadow tools it manages. To switch:"
      echo "            brew uninstall mise usage   # or your package manager"
      echo "            curl -fsSL https://mise.run | MISE_INSTALL_HELP=0 sh"
      echo "            rm -f \"\${XDG_CACHE_HOME:-\$HOME/.cache}\"/zsh-init/mise-*.zsh"
      ;;
  esac
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
