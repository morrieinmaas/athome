#!/usr/bin/env bats
# The secrets-backup and secrets-restore wrappers pass their arguments and the
# exit code through to athome. A stub athome on PATH records the call.

setup() {
  unset TMUX GROFE
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  CALLS="$BATS_TEST_TMPDIR/calls"
  mkdir -p "$STUBS"
  cat >"$STUBS/athome" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >>"$CALLS"
exit \${STUB_EXIT:-0}
STUB
  chmod 0755 "$STUBS/athome"
  export PATH="$STUBS:$PATH"
}

@test "secrets-restore passes --dry-run and the exit code through" {
  STUB_EXIT=3 run "$ROOT/home/dot_local/bin/executable_secrets-restore" --dry-run
  [ "$status" -eq 3 ]
  [ "$(cat "$CALLS")" = "secrets restore --dry-run" ]
}

@test "secrets-backup passes its arguments and the exit code through" {
  STUB_EXIT=2 run "$ROOT/home/dot_local/bin/executable_secrets-backup" --foo bar
  [ "$status" -eq 2 ]
  [ "$(cat "$CALLS")" = "secrets backup --foo bar" ]
}

@test "the wrappers pass exit 0 through with no arguments" {
  run "$ROOT/home/dot_local/bin/executable_secrets-restore"
  [ "$status" -eq 0 ]
  [ "$(cat "$CALLS")" = "secrets restore" ]
}
