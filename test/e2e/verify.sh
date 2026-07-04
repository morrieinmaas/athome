#!/usr/bin/env bash
# Assert the e2e bootstrap produced a working setup. Runs INSIDE the container
# right after bootstrap.sh. Any failed check makes the e2e (and CI) fail.
set -uo pipefail

export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:$PATH"

fail=0
check() {
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then
    printf '  \033[32m✓\033[0m %s\n' "$desc"
  else
    printf '  \033[31m✗\033[0m %s\n' "$desc"
    fail=1
  fi
}

echo "== e2e assertions =="

# Non-interactive config actually drove chezmoi (the dummy handle landed).
check "chezmoi config seeded from test toml" grep -q "athome-ci" "$HOME/.config/chezmoi/chezmoi.toml"

# Dotfiles deployed.
check "dotfile .zshrc deployed"              test -f "$HOME/.zshrc"
check "dotfile .gitconfig deployed"          test -f "$HOME/.gitconfig"
check "mise config.toml deployed"            test -f "$HOME/.config/mise/config.toml"

# run_03-setup-project-dirs ran (personal machine → personal + sidebiz).
check "project dir personal created"         test -d "$HOME/personal"
check "project dir sidebiz created"          test -d "$HOME/sidebiz"
check "personal/.envrc seeded"               test -f "$HOME/personal/.envrc"

# run_06-setup-mise installed the mise tool layer.
check "mise on PATH"                          command -v mise
check "direnv installed via mise"            command -v direnv

# Supply-chain guard: the AUR malware scanner is installed and this freshly
# provisioned box is NOT infected (scanner rc=2). Warnings (rc=1) don't fail the
# e2e — same policy as the bootstrap gate (run_once_after_21). Arch-only: skipped
# where there's no AUR (Fedora), so this verify.sh stays distro-shared.
if command -v pacman >/dev/null 2>&1; then
  aur-malware-check >/dev/null 2>&1; arc=$?
  if [ "$arc" -ne 2 ]; then
    printf '  \033[32m✓\033[0m %s\n' "aur-malware-check installed, box not infected (rc=$arc)"
  else
    printf '  \033[31m✗\033[0m %s\n' "aur-malware-check: INFECTED (rc=2)"
    fail=1
  fi
fi

# Headless guards fired (system scripts were skipped, not failed).
check "no ~/work on personal machine"        bash -c '! test -d "$HOME/work"'

echo
if [ "$fail" -eq 0 ]; then
  echo -e "\033[32m✓ ALL E2E CHECKS PASSED\033[0m"
else
  echo -e "\033[31m✗ E2E CHECKS FAILED\033[0m"
fi
exit "$fail"
