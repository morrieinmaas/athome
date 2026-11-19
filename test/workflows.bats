#!/usr/bin/env bats
# The CI workflows: one run per change, and a hang that reports instead of
# being cancelled.

setup() {
  WF="$(cd "$BATS_TEST_DIRNAME/.." && pwd)/.github/workflows"
}

# Workflows that run on pull requests; release.yml runs on tags only.
pr_workflows() {
  echo "$WF/e2e.yml" "$WF/go.yml" "$WF/lint.yml" "$WF/secrets-scan.yml" "$WF/sshd.yml"
}

@test "the workflow list above is every workflow that runs on pull requests" {
  run grep -l '^  pull_request:' "$WF"/*.yml
  [ "$status" -eq 0 ]
  [ "$(echo $output)" = "$(pr_workflows)" ]
}

@test "a pull request branch runs each workflow once, not again for its push" {
  for f in $(pr_workflows); do
    # gitleaks keeps scanning every pushed branch: a secret on a branch with no PR must still be flagged
    [ "$f" = "$WF/secrets-scan.yml" ] && continue
    # push is limited to main, so a PR branch's push does not start a second run
    run awk '/^  push:/ { p = 1; next } p && /^  [a-z_]+:/ { exit } p { print }' "$f"
    [ "$output" = '    branches: [main]' ] || { echo "$f: push not limited to main: $output"; return 1; }
  done
}

@test "a newer run of the same pull request cancels the older one" {
  for f in $(pr_workflows); do
    grep -q '^concurrency:' "$f" || { echo "$f: no concurrency"; return 1; }
    grep -q 'group: ${{ github.workflow }}-${{ github.event.pull_request.number || github.sha }}' "$f" || { echo "$f: group"; return 1; }
    grep -q "cancel-in-progress: \${{ github.event_name == 'pull_request' }}" "$f" || { echo "$f: cancel-in-progress"; return 1; }
  done
}

# go test runs about GOMAXPROCS packages at a time, so two waves of hung
# packages must each reach the 5m test timeout before the 20m job limit.
@test "go test jobs have a job limit and a test timeout that fits twice in it" {
  for f in "$WF/go.yml" "$WF/release.yml"; do
    run awk '/^  test:/ { p = 1; next } p && /^  [a-z-]+:/ { exit } p' "$f"
    [[ "$output" == *'timeout-minutes: 20'* ]] || { echo "$f: job limit"; return 1; }
    [[ "$output" == *'go test -timeout 5m ./...'* ]] || { echo "$f: test timeout"; return 1; }
  done
}

@test "secrets-scan still runs on a push to any branch" {
  grep -qx '  push:' "$WF/secrets-scan.yml"
  run awk '/^  push:/ { p = 1; next } p && /^  [a-z_]+:/ { exit } p && !/^ *#/ { print }' "$WF/secrets-scan.yml"
  [ -z "$output" ]
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
