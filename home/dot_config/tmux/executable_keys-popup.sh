#!/usr/bin/env bash
# prefix ? — a SEARCHABLE list of every tmux key binding (fzf popup).
#
# tmux's default `prefix ?` dumps `list-keys` into a pager (not searchable), and
# the which-key popup (prefix Space) only shows a curated subset. This shows ALL
# bindings across every key-table, fuzzy-searchable. Pressing enter on a row just
# closes the popup (it's a reference, not a launcher).
#
# PATH is set explicitly because display-popup runs in the tmux server env, which
# has no mise shims on PATH (fzf comes from mise).
set -uo pipefail
export PATH="$HOME/.local/share/mise/shims:/opt/homebrew/bin:/opt/nanobrew/prefix/bin:$HOME/.local/bin:/usr/bin:/bin:$PATH"

# Format each binding as:  [table]  key  →  command
tmux list-keys \
  | sed -E 's/^bind-key[[:space:]]+(-r[[:space:]]+)?-T[[:space:]]+([^[:space:]]+)[[:space:]]+("[^"]*"|[^[:space:]]+)[[:space:]]+(.*)$/[\2]\t\3\t→ \4/' \
  | column -t -s $'\t' \
  | fzf --reverse --prompt 'keys> ' \
        --header 'tmux keybindings — type to search · esc/enter closes' \
        --no-multi >/dev/null || true
