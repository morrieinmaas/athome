#!/usr/bin/env bash
# session-created hook: give an unnamed session a useful name.
#
# tmux names an unnamed session after its session id, and ids only ever count
# up, so after closing eight sessions the next one was "11" with two open.
# Instead: the git repo the session starts in, else its directory ("chezmoi",
# then "chezmoi-2"). A session started in $HOME has no better name than a
# number, so it gets the lowest free one.
#
# An unnamed session is recognisable because its name IS its id ($11 -> "11");
# a session created with -s keeps its name.
#
#   $1  the new session's name (#{hook_session_name}; #{hook_session}, the
#       id, comes through empty on tmux 3.8, so the id is looked up here)
set -uo pipefail

name="$1"
info="$(tmux display -p -t "=$name:" '#{session_id} #{session_path}' 2>/dev/null)" || exit 0
id="${info%% *}" dir="${info#* }"
[[ "$name" == "${id#\$}" ]] || exit 0

taken="$(tmux list-sessions -F '#{session_name}' | grep -vxF "$name")"
free() { ! grep -qxF "$1" <<<"$taken"; }

root="$(git -C "$dir" rev-parse --show-toplevel 2>/dev/null)" || root="$dir"
if [[ -z "$root" || "$root" == "$HOME" || "$root" == / ]]; then
  n=0
  until free "$n"; do n=$((n + 1)); done
  new="$n"
else
  base="$(basename "$root" | tr ':. ' '___')"   # tmux forbids . and : in names
  new="$base" n=2
  until free "$new"; do new="$base-$n"; n=$((n + 1)); done
fi

[[ "$new" == "$name" ]] || tmux rename-session -t "=$name" "$new"
