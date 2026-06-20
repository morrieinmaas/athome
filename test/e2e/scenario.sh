#!/usr/bin/env bash
# scenario.sh — the full e2e lifecycle, shared by Dockerfile (arch) and
# Dockerfile.fedora so both distros run the exact same flow (no duplicated CMD).
#
# Three phases, all in one container run:
#   1. bootstrap (baseline) → chezmoi apply (converge) → verify
#   2. seed user-data canaries → teardown --all → verify-teardown
#      (proves teardown wipes the tool layer but NEVER user data)
#   3. re-bootstrap → re-apply → re-verify
#      (proves teardown leaves a genuinely re-bootstrappable machine)
#
# Package skipping under emulation is handled upstream via ATHOME_SKIP_PACKAGES
# (set by test/e2e/run.sh); it flows through both bootstrap passes automatically.
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root (so ./scripts/* and test/e2e/* resolve)

# chezmoi + mise install into ~/.local/bin; mise shims live under ~/.local/share.
# Non-existent entries (e.g. shims before mise is installed) are simply ignored.
export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:$PATH"

phase() { printf '\n\033[1;35m========== %s ==========\033[0m\n' "$1"; }

# Baseline bootstrap, then converge with apply, asserting each half.
bootstrap_and_verify() {
  ./scripts/bootstrap.sh --config test/e2e/bootstrap.toml --machine personal
  bash test/e2e/verify-baseline.sh
  chezmoi apply
  bash test/e2e/verify.sh
}

phase "1/3 bootstrap -> apply -> verify"
bootstrap_and_verify

phase "2/3 seed user-data canaries -> teardown --all -> verify-teardown"
# Canaries the teardown MUST NOT touch — the safety contract under test.
mkdir -p "$HOME/.secrets/canary" "$HOME/personal/canary-repo"
printf 'SECRET=keepme\n' > "$HOME/.secrets/canary/.env"
printf 'keepme\n'        > "$HOME/personal/canary-repo/keepme"
./scripts/teardown.sh --all --execute --yes
bash test/e2e/verify-teardown.sh

phase "3/3 re-bootstrap -> re-apply -> re-verify (round-trip)"
bootstrap_and_verify

printf '\n\033[32m✓ FULL E2E PASSED — bootstrap, teardown (user data safe), re-bootstrap\033[0m\n'
