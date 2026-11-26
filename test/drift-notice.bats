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
  # go build below keeps the real module and build caches; nothing else sees it.
  REAL_HOME="$HOME"
  export HOME="$BATS_TEST_TMPDIR/home" XDG_CACHE_HOME="$BATS_TEST_TMPDIR/cache"
  mkdir -p "$HOME"
  CACHE="$XDG_CACHE_HOME/athome/drift"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  CALLS="$BATS_TEST_TMPDIR/calls"
  PIDS="$BATS_TEST_TMPDIR/pids"
  mkdir -p "$STUBS"
  # athome status --quiet: records its pid, its parent (the command
  # substitution) and grandparent (the background check), then the call, then
  # prints $STUB_OUT and exits $STUB_RC after $STUB_SLEEP.
  cat >"$STUBS/athome" <<STUB
#!/bin/sh
echo "\$\$ \$PPID \$(ps -o ppid= -p \$PPID)" >"$PIDS"
echo "\$*" >>"$CALLS"
sleep "\${STUB_SLEEP:-0}"
[ -n "\${STUB_OUT:-}" ] && printf '%b\n' "\$STUB_OUT"
exit "\${STUB_RC:-0}"
STUB
  chmod 0755 "$STUBS/athome"
  DRIFT='grofe: chezmoi apply\ncanopy: chezmoi apply'
  LINE="athome: grofe, canopy not current; run chezmoi apply (athome status for details)"
}

# shell: start a shell that sources the hook, with only the stubs and the
# system dirs on PATH (so a real athome is never found). fd 3 is closed so a
# background check never holds bats open.
shell() {
  env PATH="$STUBS:/usr/bin:/bin" "$ZSH_BIN" -f -c 'exec 3>&-; source "$1"' _ "$HOOK"
}

# notice: shell, with its stdout in $output.
notice() {
  run --separate-stderr shell
}

calls() { [ -f "$CALLS" ] && wc -l <"$CALLS" | tr -d ' ' || echo 0; }

# wait_calls <n>: wait up to 5s until the stub has been called <n> times.
wait_calls() {
  for _ in $(seq 50); do
    [ "$(calls)" -ge "$1" ] && return 0
    sleep 0.1
  done
  return 1
}

# wait_for <want>: wait up to 10s until the check has written <want> to the cache.
# Only a finished check writes the cache, so an existing file means it is done.
wait_for() {
  for _ in $(seq 100); do
    [ -f "$CACHE" ] && [ "$(cat "$CACHE")" = "$1" ] && return 0
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
  STUB_OUT="$DRIFT" STUB_RC=3 notice
  [ -z "$output" ]
  wait_for "$LINE"
  [ "$(cat "$CALLS")" = "status --quiet" ]
  notice
  [ "$output" = "$LINE" ]
}

@test "each fix gets its own advice, in the order athome prints them" {
  STUB_OUT="athome: athome update\n$DRIFT" STUB_RC=3 notice
  wait_for "athome: athome not current; run athome update; grofe, canopy not current; run chezmoi apply (athome status for details)"
}

@test "all current: the finished check leaves an empty cache" {
  STUB_RC=0 notice
  wait_for ""
  [ "$(calls)" -eq 1 ]
}

@test "a failed check says so instead of reading as all current" {
  STUB_OUT='athome status: repo not found' STUB_RC=1 notice
  wait_for "athome: drift check failed (exit 1); run athome status"
  notice
  [ "$output" = "athome: drift check failed (exit 1); run athome status" ]
}

@test "a killed check leaves no cache, so the next shell checks again" {
  STUB_SLEEP=30 notice
  wait_calls 1
  read -r pid parent check <"$PIDS"
  kill -9 "$check" "$parent" "$pid"
  sleep 0.3
  [ ! -e "$CACHE" ]
  STUB_RC=0 notice
  wait_for ""
  [ "$(calls)" -eq 2 ]
}

@test "runs the check at most once a day" {
  export STUB_OUT="$DRIFT" STUB_RC=3
  notice
  wait_for "$LINE"
  notice
  sleep 0.3
  [ "$(calls)" -eq 1 ]
  touch -t 202001010000 "$CACHE"
  notice
  [ "$output" = "$LINE" ]
  wait_calls 2
  [ "$(calls)" -eq 2 ]
}

@test "a cache with a future mtime is stale" {
  seed "$LINE"
  touch -t 203001010000 "$CACHE"
  STUB_RC=0 notice
  [ "$output" = "$LINE" ]
  wait_for ""
  [ "$(calls)" -eq 1 ]
}

@test "shells started together run one check between them" {
  export STUB_SLEEP=1 STUB_OUT="$DRIFT" STUB_RC=3
  for _ in 1 2 3 4 5 6; do
    shell >/dev/null 3>&- &
  done
  wait
  wait_for "$LINE"
  sleep 0.5
  [ "$(calls)" -eq 1 ]
}

@test "never blocks: a slow check runs in the background" {
  start="$(date +%s)"
  STUB_SLEEP=6 STUB_OUT="$DRIFT" STUB_RC=3 notice
  end="$(date +%s)"
  [ "$status" -eq 0 ]
  [ $((end - start)) -lt 3 ]
}

@test "athome not installed: says so, without a check" {
  rm "$STUBS/athome"
  notice
  [ "$output" = "athome: not installed; run chezmoi apply" ]
  [ ! -e "$CACHE" ]
}

@test "the real athome: an old grofe and a missing canopy, origin offline" {
  if ! command -v go >/dev/null; then
    [ -z "${CI:-}" ] || { echo "go is required in CI" >&2; return 1; }
    skip "go not installed"
  fi
  rm "$STUBS/athome"
  (cd "$ROOT" && HOME="$REAL_HOME" go build -o "$STUBS/athome" ./cmd/athome)
  export ATHOME_REPO="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$ATHOME_REPO/home/.chezmoidata" "$HOME/.local/lib/grofe"
  echo home >"$ATHOME_REPO/.chezmoiroot"
  printf '[pins]\ngrofe = "v0.3.1"\ncanopy = "f445cd0f9f665d2ea3fb6e0694934ae45ef10942"\n' \
    >"$ATHOME_REPO/home/.chezmoidata/pins.toml"
  git -C "$ATHOME_REPO" init -q
  git -C "$ATHOME_REPO" remote add origin "$BATS_TEST_TMPDIR/no-such-origin"
  printf '#!/bin/sh\necho "grofe 0.3.0"\n' >"$HOME/.local/lib/grofe/grofe"
  chmod 0755 "$HOME/.local/lib/grofe/grofe"
  notice
  [ -z "$output" ]
  wait_for "$LINE"
}
