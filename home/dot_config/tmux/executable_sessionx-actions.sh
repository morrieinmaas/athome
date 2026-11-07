#!/usr/bin/env bash
# Actions behind the sessionx picker's kill and rename keys (see sessionx.sh).
#
#   list          the picker's window list, one "session:index name" per line
#   kill TARGET   kill a window ("work:2") or a whole session ("work")
#   rename TARGET rename the session TARGET belongs to, in tmux's prompt
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

# Runs after the picker closes (the key binding aborts fzf) and opens tmux's
# own prompt in the status line, prefilled with the current name: Esc cancels,
# Enter renames and reopens the picker.
rename_target() {
  local session="${1%%:*}" client
  tmux has-session -t "=$session" 2>/dev/null || return 0
  client="$(tmux show -gqv @sessionx-client)"
  # -b: without it command-prompt blocks until answered, so fzf (whose popup
  # covers the status line) would never get to close.
  tmux command-prompt -b ${client:+-t "$client"} -I "$session" -p "rename session:" \
    "rename-session -t '=$session' -- '%%' ; run-shell '$HOME/.config/tmux/sessionx.sh'"
}

case "${1:-}" in
  list)   list ;;
  kill)   kill_target "${2:-}" ;;
  rename) rename_target "${2:-}" ;;
  *)      echo "usage: $0 list|kill TARGET|rename TARGET" >&2; exit 2 ;;
esac
