#!/usr/bin/env bats
# Tests for scripts/teardown.sh. Focus: the SAFETY properties — dry-run never
# acts, tiers are cumulative, bad input is rejected, execute aborts without the
# typed confirmation. Every test stays in dry-run or aborts before any deletion,
# so running the suite is itself harmless.

setup() {
  TEARDOWN="${BATS_TEST_DIRNAME}/../scripts/teardown.sh"
}

@test "no tier flag exits 2 with usage" {
  run "$TEARDOWN"
  [ "$status" -eq 2 ]
  [[ "$output" == *"pick a tier"* ]]
}

@test "unknown flag exits 2" {
  run "$TEARDOWN" --bogus
  [ "$status" -eq 2 ]
  [[ "$output" == *"unknown argument"* ]]
}

@test "--help exits 0 and lists all three tiers" {
  run "$TEARDOWN" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"--state"* ]]
  [[ "$output" == *"--dotfiles"* ]]
  [[ "$output" == *"--all"* ]]
}

@test "--state is dry-run by default and only prints would-do lines" {
  run "$TEARDOWN" --state
  [ "$status" -eq 0 ]
  [[ "$output" == *"DRY-RUN"* ]]
  [[ "$output" == *"would:"* ]]
  # no executed-action marker ("+ ") must appear in a dry run
  [[ "$output" != *$'\n  + '* ]]
}

@test "--all dry-run shows the cumulative plan (state + dotfiles + tools)" {
  run "$TEARDOWN" --all
  [ "$status" -eq 0 ]
  [[ "$output" == *"chezmoi persistent state"* ]]
  [[ "$output" == *"chezmoi-managed files"* ]]
  [[ "$output" == *"mise"* ]]
  # Native package layer is OS-specific: nanobrew on macOS, pacman/yay on Linux.
  if [[ "$(uname -s)" == Darwin ]]; then
    [[ "$output" == *"nanobrew"* ]]
  else
    [[ "$output" == *"pacman"* ]]
  fi
}

@test "--all documents user-data preservation (~/.secrets + project dirs)" {
  run "$TEARDOWN" --all
  [ "$status" -eq 0 ]
  # The safety contract must be stated: teardown wipes the tool layer but never
  # the user's secrets/env files or repo dirs. Guards against a future edit that
  # drops the note (and, by proxy, the intent).
  [[ "$output" == *".secrets"* ]]
  [[ "$output" == *"LEFT ALONE"* ]]
}

@test "execute without the typed confirmation aborts and deletes nothing" {
  run bash -c "printf '\n' | '$TEARDOWN' --dotfiles --execute"
  [ "$status" -eq 2 ]
  [[ "$output" == *"aborted"* ]]
}
