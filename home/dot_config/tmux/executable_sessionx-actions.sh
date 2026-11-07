#!/usr/bin/env bash
# Actions behind the sessionx picker's keys (see sessionx.sh).
#
#   list                the picker's window list, "session:index name" per line
#   kill TARGET         kill a window ("work:2") or a whole session ("work")
#   rename-mode TARGET  fzf actions that turn the prompt into a rename prompt
#   enter, esc          fzf actions for those keys: rename/cancel while renaming,
#                       the plugin's usual open/abort otherwise
#
# The last three run under fzf's transform(): whatever they print, fzf runs as
# actions. fzf exports its prompt and query as FZF_PROMPT and FZF_QUERY, and
# the "rename <session> › " prompt is the whole of rename mode's state.
#
# Killing the session you are in is safe because detach-on-destroy is
# `previous` (user.conf): the client moves to the session you were in before
# instead of tmux exiting. The one kill that WOULD exit tmux, the last window
# of the last session, is refused with a message instead.
set -uo pipefail

self="$HOME/.config/tmux/sessionx-actions.sh"

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

renaming() { [[ "${FZF_PROMPT:-}" == "rename "*" › " ]]; }

# Back to the picker as it opened: prompt, header, search, fresh list.
normal_mode() {
  printf 'change-prompt(%s)+change-header(%s)+enable-search+clear-query+reload(%s list)\n' \
    "$(tmux show -gqv @sessionx-_prompt)" "$(tmux show -gqv @sessionx-_header)" "$self"
}

rename_mode() {
  local session="${1%%:*}"
  tmux has-session -t "=$session" 2>/dev/null || return 0
  printf 'change-prompt(rename %s › )+change-header(enter rename · esc cancel)+disable-search+change-query(%s)\n' \
    "$session" "$session"
}

on_enter() {
  renaming || { echo 'replace-query+print-query'; return; }
  local old new err
  old="${FZF_PROMPT#rename }" old="${old% › }"
  new="${FZF_QUERY:-}"
  if [[ -z "$new" || "$new" == "$old" ]]; then
    normal_mode
  elif err="$(tmux rename-session -t "=$old" -- "$new" 2>&1)"; then
    normal_mode
  else
    printf 'change-header(%s · esc cancel)\n' "${err//[()]/}"
  fi
}

on_esc() { if renaming; then normal_mode; else echo abort; fi; }

case "${1:-}" in
  list)        list ;;
  kill)        kill_target "${2:-}" ;;
  rename-mode) rename_mode "${2:-}" ;;
  enter)       on_enter ;;
  esc)         on_esc ;;
  *)           echo "usage: $0 list|kill TARGET|rename-mode TARGET|enter|esc" >&2; exit 2 ;;
esac
