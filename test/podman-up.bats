#!/usr/bin/env bats
# podman-up starts the podman machine only when it is really stopped, and only
# one of many parallel callers does it. Stub uname, curl-less socket (none exists,
# so every ping fails), and a podman that keeps its state in a file.

setup() {
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  export STATE="$BATS_TEST_TMPDIR/state" STARTS="$BATS_TEST_TMPDIR/starts"
  export TMPDIR="$BATS_TEST_TMPDIR" DOCKER_HOST="unix://$BATS_TEST_TMPDIR/none.sock"
  export PODMAN_UP_WAIT=2
  mkdir -p "$STUBS"
  printf '#!/bin/sh\necho Darwin\n' >"$STUBS/uname"
  cat >"$STUBS/podman" <<'STUB'
#!/bin/sh
case "$*" in
  "machine inspect"*) cat "$STATE" ;;
  "machine start") echo x >>"$STARTS"; sleep 1; echo running >"$STATE" ;;
esac
STUB
  chmod 0755 "$STUBS/uname" "$STUBS/podman"
  export PATH="$STUBS:$PATH"
  : >"$STARTS"
}

@test "a running machine with a dead-looking socket is not started again" {
  echo running >"$STATE"
  run sh "$ROOT/home/dot_local/bin/executable_podman-up"
  [ "$status" -eq 0 ]
  [ ! -s "$STARTS" ]
}

@test "a stopped machine is started" {
  echo stopped >"$STATE"
  run sh "$ROOT/home/dot_local/bin/executable_podman-up"
  [ "$(wc -l <"$STARTS")" -eq 1 ]
}

@test "parallel callers start a stopped machine once" {
  echo stopped >"$STATE"
  for _ in 1 2 3 4 5; do sh "$ROOT/home/dot_local/bin/executable_podman-up" 2>/dev/null & done
  wait
  [ "$(wc -l <"$STARTS")" -eq 1 ]
}

@test "a stale lock does not block the start" {
  echo stopped >"$STATE"
  mkdir "$TMPDIR/podman-up.lock"
  touch -t 202001010000 "$TMPDIR/podman-up.lock"
  run sh "$ROOT/home/dot_local/bin/executable_podman-up"
  [ "$(wc -l <"$STARTS")" -eq 1 ]
}
