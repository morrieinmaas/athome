#!/usr/bin/env bash
# Actions behind the sessionx picker's kill and rename keys (see sessionx.sh).
#
#   list          the picker's window list, one "session:index name" per line
#   kill TARGET   kill a window ("work:2") or a whole session ("work")
#   rename TARGET rename the session TARGET belongs to
#
# Killing the session you are in is safe because detach-on-destroy is
# `previous` (user.conf): the client moves to the session you were in before
# instead of tmux exiting. The one kill that WOULD exit tmux, the last window
# of the last session, is refused with a message instead.
set -uo pipefail

list() { tmux list-windows -a -F '#{session_name}:#{window_index} #{window_name}'; }

kill_target() {
  local target="$1" session
  session="${target%%:*}"
  tmux has-session -t "=$session" 2>/dev/null || return 0

  if [[ "$(tmux list-sessions | wc -l)" -eq 1 ]] &&
     [[ "$target" != *:* || "$(tmux display -p -t "=$session:" '#{session_windows}')" -eq 1 ]]; then
    tmux display-message "sessionx: not killing the last session, that would exit tmux"
  elif [[ "$target" == *:* ]]; then
    tmux kill-window -t "=$session:${target#*:}"
  else
    tmux kill-session -t "=$session"
  fi
}

rename_target() {
  local session="${1%%:*}" name
  tmux has-session -t "=$session" 2>/dev/null || return 0
  printf 'Rename session "%s" to: ' "$session" >/dev/tty
  read -r name </dev/tty
  [[ -n "$name" ]] && tmux rename-session -t "=$session" "$name"
}

case "${1:-}" in
  list)   list ;;
  kill)   kill_target "${2:-}" ;;
  rename) rename_target "${2:-}" ;;
  *)      echo "usage: $0 list|kill TARGET|rename TARGET" >&2; exit 2 ;;
esac
