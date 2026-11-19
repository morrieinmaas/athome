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

# block prints the lines under the given header up to the next line at the
# header's indent, without comments. "job test" is the test job's block.
block() {
  awk -v h="$1" '
    $0 == h { p = 1; ind = match(h, /[^ ]/); next }
    p && /^ *#/ { next }
    p && /[^ ]/ && match($0, /[^ ]/) <= ind { exit }
    p { print }
  ' "$2"
}

@test "a newer run of the same pull request cancels the older one" {
  want="  group: \${{ github.workflow }}-\${{ github.event.pull_request.number || github.sha }}
  cancel-in-progress: \${{ github.event_name == 'pull_request' }}"
  for f in $(pr_workflows); do
    [ "$(block 'concurrency:' "$f")" = "$want" ] || { echo "$f: concurrency is not exactly:"; echo "$want"; return 1; }
  done
}

# go test runs about GOMAXPROCS packages at a time, so two waves of hung
# packages must each reach the 5m test timeout before the 20m job limit.
@test "go test jobs have a 20m job limit and a 5m test timeout" {
  for f in "$WF/go.yml" "$WF/release.yml"; do
    run block '  test:' "$f"
    grep -qx '    timeout-minutes: 20' <<<"$output" || { echo "$f: job limit"; return 1; }
    grep -qx '      - run: go test -timeout 5m ./...' <<<"$output" || { echo "$f: test timeout"; return 1; }
  done
}

@test "secrets-scan still runs on a push to any branch" {
  grep -qx '  push:' "$WF/secrets-scan.yml"
  [ -z "$(block '  push:' "$WF/secrets-scan.yml")" ]
}

# A job-level name: would rename the check; e2e's is the name it always had.
@test "the job names other checks refer to are unchanged" {
  for job in go.yml:test go.yml:release-check lint.yml:shellcheck lint.yml:bats sshd.yml:drill \
    secrets-scan.yml:gitleaks secrets-scan.yml:chezmoi-verify e2e.yml:bootstrap; do
    f="$WF/${job%%:*}"
    grep -qx "  ${job#*:}:" "$f" || { echo "$job: missing"; return 1; }
    names=$(block "  ${job#*:}:" "$f" | grep -E '^    name:' || true)
    if [ "$job" = e2e.yml:bootstrap ]; then
      [ "$names" = '    name: bootstrap-${{ matrix.distro }}' ] || { echo "$job: name $names"; return 1; }
    else
      [ -z "$names" ] || { echo "$job: name $names"; return 1; }
    fi
  done
  grep -qx '        os: \[ubuntu-latest, macos-latest\]' "$WF/go.yml"
}
