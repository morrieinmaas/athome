#!/usr/bin/env bats
# home/dot_zsh/functions.zsh repo(): with no local project it falls back to the
# remote-projects file and attaches via grofe (on PATH, else ~/.local/lib/grofe/grofe).

bats_require_minimum_version 1.5.0

setup() {
  unset TMUX GROFE
  command -v zsh >/dev/null || skip "zsh not installed"
  ROOT="$(cd "$BATS_TEST_DIRNAME/.." && pwd)"
  H="$BATS_TEST_TMPDIR/home"
  mkdir -p "$H/.local/state/athome" "$H/.local/lib/grofe"
  printf 'widget\tacme-dev\twork\t/srv/widget\n' >"$H/.local/state/athome/remote-projects"
  printf '#!/bin/sh\nprintf "%%s\\n" "$@"\n' >"$H/.local/lib/grofe/grofe"
  chmod 0755 "$H/.local/lib/grofe/grofe"
}

@test "repo falls back to ~/.local/lib/grofe/grofe for a remote project" {
  HOME="$H" PATH="/usr/bin:/bin" run zsh -c 'source "$1"; repo widget' _ "$ROOT/home/dot_zsh/functions.zsh"
  [ "$status" -eq 0 ]
  output=$(printf "%s\n" "$output" | tail -n 8)
  [ "$output" = "$(printf -- '-r\nacme-dev\nnew-session\n-A\n-s\nwidget\n-c\n/srv/widget')" ]
}
