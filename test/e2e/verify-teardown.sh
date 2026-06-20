#!/usr/bin/env bash
# Asserts `teardown.sh --all` wiped the athome TOOL LAYER while leaving USER DATA
# untouched. Runs in the e2e scenario right after teardown. The user-data checks
# are the SAFETY GUARD: a teardown that ever deleted ~/.secrets (env files) or a
# project/context dir would be destructive — these must catch that regression.
# Canaries are seeded by test/e2e/scenario.sh before teardown runs.
set -uo pipefail

export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:$PATH"

fail=0
check()  { local d="$1"; shift; if "$@" >/dev/null 2>&1; then printf '  \033[32m✓\033[0m %s\n' "$d"; else printf '  \033[31m✗\033[0m %s\n' "$d"; fail=1; fi; }
refute() { local d="$1"; shift; if "$@" >/dev/null 2>&1; then printf '  \033[31m✗\033[0m %s\n' "$d"; fail=1; else printf '  \033[32m✓\033[0m %s\n' "$d"; fi; }

echo "== teardown assertions (after teardown --all --execute) =="

# ── Tool layer + managed dotfiles must be GONE ──
refute "managed .zshrc removed"             test -f "$HOME/.zshrc"
refute "managed .gitconfig removed"         test -f "$HOME/.gitconfig"
refute "generated chezmoi config removed"   test -d "$HOME/.config/chezmoi"
refute "mise data removed"                  test -d "$HOME/.local/share/mise"
refute "mise tools gone (direnv off PATH)"  command -v direnv
refute "zinit external removed"             test -d "$HOME/.local/share/zinit"
refute "tmux plugins removed"               test -d "$HOME/.config/tmux/plugins"

# ── USER DATA must SURVIVE (the safety guard) ──
check  "secrets .env file preserved"        test -f "$HOME/.secrets/canary/.env"
check  "project-dir file preserved"         test -f "$HOME/personal/canary-repo/keepme"
check  "source repo preserved"              test -f "$HOME/athome/scripts/bootstrap.sh"

echo
if [ "$fail" -eq 0 ]; then
  echo -e "\033[32m✓ TEARDOWN OK — tool layer wiped, user data intact\033[0m"
else
  echo -e "\033[31m✗ TEARDOWN CHECKS FAILED\033[0m"
fi
exit "$fail"
