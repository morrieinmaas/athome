#!/usr/bin/env bats
# The CI workflows: one run per change, and a hang that reports instead of
# being cancelled.

setup() {
  WF="$(cd "$BATS_TEST_DIRNAME/.." && pwd)/.github/workflows"
}

# Workflows that run on pull requests; release.yml runs on tags only.
pr_workflows() {
  grep -l '^  pull_request:' "$WF"/*.yml
}

@test "a pull request branch runs each workflow once, not again for its push" {
  for f in $(pr_workflows); do
    # push is limited to main, so a PR branch's push does not start a second run
    run awk '/^  push:/ { p = 1; next } p && /^  [a-z_]+:/ { exit } p { print }' "$f"
    [[ "$output" == *'branches: [main]'* ]] || { echo "$f: push not limited to main"; return 1; }
  done
}

@test "a newer run of the same pull request cancels the older one" {
  for f in $(pr_workflows); do
    grep -q '^concurrency:' "$f" || { echo "$f: no concurrency"; return 1; }
    grep -q 'group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.sha }}' "$f" || { echo "$f: group"; return 1; }
    grep -q "cancel-in-progress: \${{ github.event_name == 'pull_request' }}" "$f" || { echo "$f: cancel-in-progress"; return 1; }
  done
}

@test "go test has a job timeout and a test timeout below it" {
  f="$WF/go.yml"
  run awk '/^  test:/ { p = 1 } /^  release-check:/ { p = 0 } p' "$f"
  [[ "$output" == *'timeout-minutes: 20'* ]]
  [[ "$output" == *'go test -timeout 10m ./...'* ]]
}

@test "the job names other checks refer to are unchanged" {
  grep -q '^  test:' "$WF/go.yml"
  grep -q 'os: \[ubuntu-latest, macos-latest\]' "$WF/go.yml"
  grep -q '^  release-check:' "$WF/go.yml"
  grep -q '^  shellcheck:' "$WF/lint.yml"
  grep -q '^  bats:' "$WF/lint.yml"
  grep -q 'name: bootstrap-${{ matrix.distro }}' "$WF/e2e.yml"
  grep -q '^  drill:' "$WF/sshd.yml"
  grep -q '^  gitleaks:' "$WF/secrets-scan.yml"
  grep -q '^  chezmoi-verify:' "$WF/secrets-scan.yml"
}
