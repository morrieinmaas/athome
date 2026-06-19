#!/usr/bin/env bash
set -uo pipefail
# Asserts the BASELINE bootstrap installed ONLY the baseline — and crucially that
# the full package set / desktop scripts were DEFERRED (not run during bootstrap).
# Runs in the e2e CMD between `bootstrap` and `chezmoi apply`, so at this point the
# package set must NOT be present yet; the later verify.sh confirms it appears
# after `chezmoi apply`. Together they prove the baseline/apply split works.

export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:$PATH"

fail=0
check()  { local d="$1"; shift; if "$@" >/dev/null 2>&1; then printf '  \033[32m✓\033[0m %s\n' "$d"; else printf '  \033[31m✗\033[0m %s\n' "$d"; fail=1; fi; }
refute() { local d="$1"; shift; if "$@" >/dev/null 2>&1; then printf '  \033[31m✗\033[0m %s\n' "$d"; fail=1; else printf '  \033[32m✓\033[0m %s\n' "$d"; fi; }

echo "== baseline assertions (after bootstrap, BEFORE apply) =="

# Baseline deps bootstrap installs explicitly:
check  "gh installed (baseline dep)"                 command -v gh
check  "rbw installed (baseline dep)"                command -v rbw
check  "mise binary installed (baseline)"            command -v mise

# Config files + project dirs from the --exclude=scripts init + run_03:
check  ".zshrc deployed"                             test -f "$HOME/.zshrc"
check  "project dir personal created"                test -d "$HOME/personal"

# The DEFERRED half — must NOT be present yet (proves run_ scripts didn't fire):
#   direnv = a mise tool installed by run_after_06 (a script) → absent at baseline.
refute "mise tools deferred (direnv absent pre-apply)" command -v direnv

echo
if [ "$fail" -eq 0 ]; then
  echo -e "\033[32m✓ BASELINE OK — packages/scripts correctly deferred to apply\033[0m"
else
  echo -e "\033[31m✗ BASELINE CHECKS FAILED\033[0m"
fi
exit "$fail"
