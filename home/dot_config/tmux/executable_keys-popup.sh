#!/usr/bin/env bash
# prefix ?  (and which-key's prefix Space → ?): a SEARCHABLE list of every tmux
# key binding in an fzf popup. fzf fuzzy-matches the WHOLE line, so you search by
# what a binding DOES ("resize", "split", "session") just as well as by its key.
# Enter just closes — it's a reference, not a launcher.
#
# Runs its OWN `fzf --tmux` popup (auto-sized + fully filled), exactly like
# ~/.local/bin/tsess — so bind it with `run-shell`, NOT inside a display-popup
# (that double-popup + FZF_DEFAULT_OPTS --height is what left the big empty gap).
#
# `sed` only REFORMATS each `list-keys` line into "[table]  key  → command" (a
# capture-group substitution — sed's job, not ripgrep's); the interactive fuzzy
# SEARCH is fzf. PATH is set because the tmux server env has no mise shims (fzf).
set -uo pipefail
export PATH="$HOME/.local/share/mise/shims:/opt/homebrew/bin:/opt/nanobrew/prefix/bin:$HOME/.local/bin:/usr/bin:/bin:$PATH"

tmux list-keys \
  | sed -E 's/^bind-key[[:space:]]+(-r[[:space:]]+)?-T[[:space:]]+([^[:space:]]+)[[:space:]]+("[^"]*"|[^[:space:]]+)[[:space:]]+(.*)$/[\2]\t\3\t→ \4/' \
  | column -t -s "$(printf '\t')" \
  | fzf --tmux center,80%,80% --reverse --no-wrap --prompt 'keys> ' \
        --header 'tmux keybindings — type to search (action or key) · esc/enter closes' \
        --no-multi >/dev/null || true
