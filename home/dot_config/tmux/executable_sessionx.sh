#!/usr/bin/env bash
# prefix s: tmux-sessionx, with three of its keys replaced.
#
# Upstream's kill key runs `kill-session -t {1}`. With @sessionx-window-mode on
# every line is a WINDOW ("work:2 nvim"), so killing one window line killed its
# whole session. Its rename and kill keys also reloaded a list
# of SESSIONS into a picker that had been showing windows. And its header is a
# row of bare nerd-font glyphs nobody can read.
#
# sessionx.tmux stores its finished fzf argument list in @sessionx-_built-args
# and sessionx.sh evals it at launch, so this patches that list and then hands
# over to the plugin's own script. Patching on every launch, rather than once
# after TPM loads, means a plugin reload can never leave the old keys behind.
# The plugin's git checkout itself is never touched.
set -uo pipefail

plugin="$HOME/.config/tmux/plugins/tmux-sessionx/scripts/sessionx.sh"
act="$HOME/.config/tmux/sessionx-actions.sh"
kill_key="$(tmux show -gqv @sessionx-bind-kill-session)"
rename_key="$(tmux show -gqv @sessionx-bind-rename-session)"
: "${kill_key:=alt-bspace}" "${rename_key:=ctrl-r}"

built="$(tmux show -gqv @sessionx-_built-args)"
if [[ -n "$built" ]]; then
  eval "$built"
  for i in "${!args[@]}"; do
    case "${args[$i]}" in
      "$kill_key":*)   args[$i]="$kill_key:execute-silent($act kill {1})+reload($act list)" ;;
      "$rename_key":*) args[$i]="$rename_key:execute($act rename {1})+reload($act list)" ;;
      --header=*)      args[$i]="--header=enter open/new · $kill_key kill · $rename_key rename · ctrl-w windows · ctrl-t tree · ctrl-e dirs · ctrl-f zoxide · ctrl-b back · ? preview" ;;
    esac
  done
  tmux set -g @sessionx-_built-args "$(declare -p args)"
fi

exec "$plugin"
