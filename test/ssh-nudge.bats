#!/usr/bin/env bats
# home/dot_zsh/ssh-nudge.zsh: in a grofe pane, `ssh <machine>` prints a hint and
# still runs ssh with the same arguments. Stub `ssh` and `grofe` are on PATH.

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE
  command -v zsh >/dev/null || skip "zsh not installed"
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  STUBS="$BATS_TEST_TMPDIR/stubs"
  CALLS="$BATS_TEST_TMPDIR/ssh-args"
  mkdir -p "$STUBS"
  # ssh: `-G <host>` answers like ssh would (alias "acme" resolves to the target
  # of acme-dev); anything else records one argument per line.
  cat >"$STUBS/ssh" <<STUB
#!/bin/sh
if [ "\$1" = "-G" ]; then
  case "\$2" in
    acme-alias) echo "hostname dev.acme.example" ;;
    *) echo "hostname \$2" ;;
  esac
  exit 0
fi
printf '%s\n' "\$@" >"$CALLS"
STUB
  cat >"$STUBS/grofe" <<'STUB'
#!/bin/sh
[ "$1 $2" = "machine ls" ] || exit 2
printf 'acme-dev  dev.acme.example  work  ssh  true\n'
printf 'lab       lab                -     ssh  true\n'
STUB
  chmod 0755 "$STUBS/ssh" "$STUBS/grofe"
  export PATH="$STUBS:$PATH"
  HINT="grofe: for reconnects and forwarding, use grofe -r acme-dev"
}

# nudge <args...>: run the ssh function under zsh; stderr goes to $output, the
# arguments the stub ssh received are in $CALLS.
nudge() {
  rm -f "$CALLS"
  run --separate-stderr zsh -c 'source "$1"; shift; ssh "$@"' _ "$ROOT/home/dot_zsh/ssh-nudge.zsh" "$@"
}

@test "GROFE set, a machine name: hint on stderr, ssh runs with the same arguments" {
  GROFE=1 nudge acme-dev
  [ "$status" -eq 0 ]
  [ "$stderr" = "$HINT" ]
  [ "$(cat "$CALLS")" = "acme-dev" ]
}

@test "without GROFE: no hint, ssh runs" {
  nudge acme-dev
  [ -z "$stderr" ]
  [ "$(cat "$CALLS")" = "acme-dev" ]
}

@test "with -T: no hint, ssh gets -T" {
  GROFE=1 nudge -T acme-dev
  [ -z "$stderr" ]
  [ "$(cat "$CALLS")" = "$(printf -- '-T\nacme-dev')" ]
}

@test "with a clustered -T: no hint" {
  GROFE=1 nudge -NT acme-dev
  [ -z "$stderr" ]
}

@test "with a remote command: no hint, arguments unchanged" {
  GROFE=1 nudge acme-dev 'ls -la'
  [ -z "$stderr" ]
  [ "$(cat "$CALLS")" = "$(printf 'acme-dev\nls -la')" ]
}

@test "a host not in grofe machine ls: no hint" {
  GROFE=1 nudge somewhere.example
  [ -z "$stderr" ]
  [ "$(cat "$CALLS")" = "somewhere.example" ]
}

@test "the target and user@name also match" {
  GROFE=1 nudge dev.acme.example
  [ "$stderr" = "$HINT" ]
  GROFE=1 nudge dev@acme-dev
  [ "$stderr" = "$HINT" ]
}

@test "an ssh alias whose hostname is the machine's target matches" {
  GROFE=1 nudge acme-alias
  [ "$stderr" = "$HINT" ]
}

@test "options that take a value do not count as the host" {
  GROFE=1 nudge -p 2222 -i /tmp/key -o StrictHostKeyChecking=no -l dev acme-dev
  [ "$stderr" = "$HINT" ]
  [ "$(cat "$CALLS")" = "$(printf -- '-p\n2222\n-i\n/tmp/key\n-o\nStrictHostKeyChecking=no\n-l\ndev\nacme-dev')" ]
  GROFE=1 nudge -p2222 -L 8080:localhost:80 acme-dev
  [ "$stderr" = "$HINT" ]
}

@test "two words after the options (host and command) never nudge, even with value options" {
  GROFE=1 nudge -p 2222 acme-dev uptime
  [ -z "$stderr" ]
}
