#!/usr/bin/env bash
# Post-TPM: make tmux-which-key's "+Keys ?" entry (prefix Space → ?) open our
# searchable fzf popup (keys-popup.sh) instead of the default non-searchable
# `list-keys -N` pager — so `prefix Space ?` matches `prefix ?`.
#
# The plugin builds its menu into the @wk_menu_root option; we patch just that
# one command. Done here (not by editing the plugin's generated init.tmux, which
# lives in the untracked, git-cloned plugin dir) so it survives plugin updates.
set -uo pipefail

cur="$(tmux show -gv @wk_menu_root 2>/dev/null)" || exit 0
[ -n "$cur" ] || exit 0
case "$cur" in *"list-keys -N"*) ;; *) exit 0 ;; esac   # already patched / not present

tmux set -g @wk_menu_root \
  "${cur/list-keys -N/run-shell $HOME/.config/tmux/keys-popup.sh}"
