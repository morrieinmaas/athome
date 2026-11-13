#!/usr/bin/env bash
# prefix s: tmux-sessionx, with some of its keys replaced.
#
# Upstream's kill key runs `kill-session -t {1}`. With @sessionx-window-mode on
# every line is a WINDOW ("work:2 nvim"), so killing one window line killed its
# whole session, and it reloaded a list of SESSIONS into a picker that had been
# showing windows. Its rename read a line inside the popup, drawn over the old
# screen, where Esc did not cancel. And its header is a row of bare nerd-font
# glyphs nobody can read.
#
# Rename now happens in fzf itself: ctrl-r turns the prompt into
# "rename <session> ›" with the name as the query. enter and esc go through
# sessionx-actions.sh so they can mean rename/cancel in that mode and keep
# their usual open/abort meaning otherwise.
#
# sessionx.tmux stores its finished fzf argument list in @sessionx-_built-args
# and sessionx.sh evals it at launch, so this patches that list and then hands
# over to the plugin's own script. Patching on every launch, rather than once
# after TPM loads, means a plugin reload can never leave the old keys behind.
# The plugin's git checkout itself is never touched.
set -uo pipefail

# canopy vendors the plugins and loads them itself, so the plugin lives in
# canopy's store rather than in TPM's ~/.config/tmux/plugins, which is gone.
plugin="$HOME/.local/share/canopy/plugins/tmux-sessionx/scripts/sessionx.sh"
if [[ ! -x "$plugin" ]]; then
  tmux display-message "sessionx: not in canopy's store ($plugin)"
  exit 0
fi
act="$HOME/.config/tmux/sessionx-actions.sh"
kill_key="$(tmux show -gqv @sessionx-bind-kill-session)"
rename_key="$(tmux show -gqv @sessionx-bind-rename-session)"
: "${kill_key:=alt-bspace}" "${rename_key:=ctrl-r}"
header="enter open/new · $kill_key kill · $rename_key rename · ctrl-w windows · ctrl-t tree · ctrl-e dirs · ctrl-f zoxide · ctrl-b back · ? preview"

built="$(tmux show -gqv @sessionx-_built-args)"
if [[ -n "$built" ]]; then
  eval "$built"
  for i in "${!args[@]}"; do
    case "${args[$i]}" in
      "$kill_key":*)   args[$i]="$kill_key:execute-silent($act kill {1})+reload($act list)" ;;
      "$rename_key":*) args[$i]="$rename_key:transform($act rename-mode {1})" ;;
      enter:*)         args[$i]="enter:transform($act enter)" ;;
      esc:*)           args[$i]="esc:transform($act esc)" ;;
      --header=*)      args[$i]="--header=$header" ;;
      --prompt)        tmux set -g @sessionx-_prompt "${args[$((i + 1))]}" ;;
    esac
  done
  # What leaving rename mode restores.
  tmux set -g @sessionx-_header "$header"
  tmux set -g @sessionx-_built-args "$(declare -p args)"
fi

exec "$plugin"
