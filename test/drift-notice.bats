#!/usr/bin/env bats
# home/dot_zsh/drift-notice.zsh: on shell start, one line from the cached drift
# check; the check (athome status --quiet) runs at most once a day, in the
# background. A stub athome records each call; HOME and XDG_CACHE_HOME are temp.

bats_require_minimum_version 1.5.0

setup() {
  command -v zsh >/dev/null || skip "zsh not installed"
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  HOOK="$ROOT/home/dot_zsh/drift-notice.zsh"
  ZSH_BIN="$(command -v zsh)"
  export HOME="$BATS_TEST_TMPDIR/home" XDG_CACHE_HOME="$BATS_TEST_TMPDIR/cache"
  mkdir -p "$HOME"
  CACHE="$XDG_CACHE_HOME/athome/drift"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  CALLS="$BATS_TEST_TMPDIR/calls"
  mkdir -p "$STUBS"
  # athome status --quiet: prints $STUB_OUT, exits $STUB_RC after $STUB_SLEEP.
  cat >"$STUBS/athome" <<STUB
#!/bin/sh
echo "\$*" >>"$CALLS"
sleep "\${STUB_SLEEP:-0}"
[ -n "\${STUB_OUT:-}" ] && printf '%b\n' "\$STUB_OUT"
exit "\${STUB_RC:-0}"
STUB
  chmod 0755 "$STUBS/athome"
  LINE="athome: canopy, grofe not current; run chezmoi apply (athome status for details)"
}

# notice: start a shell that sources the hook, with only the stubs and the
# system dirs on PATH (so a real athome is never found). Its stdout is $output.
# fd 3 is closed so a background check never holds bats open.
notice() {
  run --separate-stderr env PATH="$STUBS:/usr/bin:/bin" "$ZSH_BIN" -f -c 'exec 3>&-; source "$1"' _ "$HOOK"
}

calls() { [ -f "$CALLS" ] && wc -l <"$CALLS" | tr -d ' ' || echo 0; }

# wait_for <want>: wait up to 5s until the cache holds exactly <want>.
wait_for() {
  for _ in $(seq 50); do
    [ "$(calls)" -ge 1 ] && [ -f "$CACHE" ] && [ "$(cat "$CACHE")" = "$1" ] && return 0
    sleep 0.1
  done
  echo "cache: $(cat "$CACHE" 2>/dev/null)" >&2
  return 1
}

seed() {
  mkdir -p "${CACHE%/*}"
  printf '%s' "$1" >"$CACHE"
}

@test "a fresh cache with drift: prints the line, runs no check" {
  seed "$LINE"
  notice
  [ "$status" -eq 0 ]
  [ "$output" = "$LINE" ]
  sleep 0.3
  [ "$(calls)" -eq 0 ]
}

@test "a fresh empty cache (all current): prints nothing, runs no check" {
  seed ""
  notice
  [ "$status" -eq 0 ]
  [ -z "$output" ]
  [ -z "$stderr" ]
  sleep 0.3
  [ "$(calls)" -eq 0 ]
}

@test "no cache: prints nothing now, the background check writes the line for the next shell" {
  STUB_OUT='canopy\ngrofe' STUB_RC=3 notice
  [ -z "$output" ]
  wait_for "$LINE"
  [ "$(cat "$CALLS")" = "status --quiet" ]
  notice
  [ "$output" = "$LINE" ]
}

@test "all current or a failed check: the cache stays empty" {
  STUB_RC=0 notice
  wait_for ""
  sleep 0.3
  [ -z "$(cat "$CACHE")" ]
  rm -f "$CACHE" "$CALLS"
  STUB_OUT='athome status: repo not found' STUB_RC=1 notice
  wait_for ""
  sleep 0.3
  [ -z "$(cat "$CACHE")" ]
}

@test "runs the check at most once a day" {
  export STUB_OUT='canopy\ngrofe' STUB_RC=3
  notice
  notice
  wait_for "$LINE"
  notice
  sleep 0.3
  [ "$(calls)" -eq 1 ]
  touch -t 202001010000 "$CACHE"
  notice
  [ "$output" = "$LINE" ]
  for _ in $(seq 50); do [ "$(calls)" -ge 2 ] && break; sleep 0.1; done
  [ "$(calls)" -eq 2 ]
}

@test "never blocks: a slow check runs in the background" {
  start="$(date +%s)"
  STUB_SLEEP=4 STUB_OUT='canopy' STUB_RC=3 notice
  end="$(date +%s)"
  [ "$status" -eq 0 ]
  [ $((end - start)) -lt 2 ]
}

@test "athome not installed: says so, without a check" {
  rm "$STUBS/athome"
  notice
  [ "$output" = "athome: not installed; run chezmoi apply" ]
  [ ! -e "$CACHE" ]
}
