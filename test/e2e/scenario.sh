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

cd "$(dirname "$0")/../.." # repo root (so ./scripts/* and test/e2e/* resolve)

# chezmoi + mise install into ~/.local/bin; mise shims live under ~/.local/share.
# Non-existent entries (e.g. shims before mise is installed) are simply ignored.
export PATH="$HOME/.local/bin:$HOME/.local/share/mise/shims:/opt/nanobrew/prefix/bin:$PATH"

phase() { printf '\n\033[1;35m========== %s ==========\033[0m\n' "$1"; }

# The image puts athome in /usr/local/bin; the installer copies it into
# ~/.local/bin, which is first on PATH, so later `athome` calls run the installed
# copy. Pin the source now so the second pass never installs a file onto itself.
athome_src="$(command -v athome || true)"

# actions/checkout leaves a pull_request build on a detached merge commit, and
# bootstrap's last step moves a detached repo to main, so phase 3 would run
# main's scripts instead of the commit under test. Name the commit with a local
# branch so the whole scenario stays on it.
if [ "$(git rev-parse --abbrev-ref HEAD)" = HEAD ]; then
  git checkout -q -B e2e-under-test
fi
under_test="$(git rev-parse HEAD)"

assert_same_commit() {
  local now
  now="$(git rev-parse HEAD)"
  if [ "$now" != "$under_test" ]; then
    echo "FAIL: the repo moved from $under_test to $now during the scenario" >&2
    exit 1
  fi
}

run_bootstrap() {
  ./scripts/bootstrap.sh --install-from "$athome_src" --config test/e2e/bootstrap.toml --machine personal
}

# Second `athome init` must be a no-op: last stdout line "athome: applied 0, skipped N".
assert_init_idempotent() {
  local last
  last="$(athome init --config test/e2e/bootstrap.toml --machine personal | tail -n 1)"
  printf '%s\n' "$last"
  if ! [[ "$last" =~ ^athome:\ applied\ 0,\ skipped\ [0-9]+$ ]]; then
    echo "FAIL: second athome init was not a no-op: '$last'" >&2
    exit 1
  fi
}

# After apply, nothing in the home may differ from the source: `athome status --json`
# exits 0 and reports dotfiles as current.
assert_status_dotfiles_current() {
  local js
  js="$(athome status --json)" || {
    echo "FAIL: athome status --json exited non-zero" >&2
    exit 1
  }
  if ! printf '%s' "$js" | tr -d ' \n' | grep -q '"name":"dotfiles","status":"current"'; then
    echo "FAIL: athome status does not report dotfiles as current: $js" >&2
    exit 1
  fi
}

# Baseline bootstrap (a second init right after must be a no-op,
# before apply changes the home), then converge with apply, asserting each half.
bootstrap_and_verify() {
  run_bootstrap
  assert_same_commit
  assert_init_idempotent
  bash test/e2e/verify-baseline.sh
  chezmoi apply
  bash test/e2e/verify.sh
  assert_status_dotfiles_current
}

phase "1/3 bootstrap -> apply -> verify"
bootstrap_and_verify

phase "2/3 seed user-data canaries -> teardown --all -> verify-teardown"
# Canaries the teardown MUST NOT touch — the safety contract under test.
mkdir -p "$HOME/.secrets/canary" "$HOME/personal/canary-repo"
printf 'SECRET=keepme\n' >"$HOME/.secrets/canary/.env"
printf 'keepme\n' >"$HOME/personal/canary-repo/keepme"
athome teardown --all --execute --yes
assert_same_commit
bash test/e2e/verify-teardown.sh

phase "3/3 re-bootstrap -> re-apply -> re-verify (round-trip)"
bootstrap_and_verify

printf '\n\033[32m✓ FULL E2E PASSED — bootstrap, teardown (user data safe), re-bootstrap\033[0m\n'
