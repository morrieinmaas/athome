#!/usr/bin/env bash
# Post-TPM: make tmux-which-key's "+Keys ?" entry (prefix Space → ?) open our
# searchable fzf popup (keys-popup.sh) instead of the default non-searchable
# `list-keys -N` pager — so `prefix Space ?` matches `prefix ?`.
#
# The plugin builds its menu into the @wk_menu_root option; we patch just that one
# command. Done here (not by editing the plugin's generated init.tmux, which lives
# in the untracked, git-cloned plugin dir) so it survives plugin updates.
#
# POLL: on `source-file`, TPM re-sources which-key AFTER this runs (run-shell
# ordering), and which-key REBUILDS @wk_menu_root back to `list-keys -N`. So we
# can't patch once up-front — we poll until the menu shows up (or is already
# patched), which is why tmux.conf calls us backgrounded (`run-shell -b`).
set -uo pipefail

for _ in $(seq 1 50); do   # ~10s max (which-key usually builds within ~1s)
  cur="$(tmux show -gv @wk_menu_root 2>/dev/null || true)"
  case "$cur" in
    *keys-popup.sh*) exit 0 ;;                                  # already patched
    *"list-keys -N"*)
      tmux set -g @wk_menu_root \
        "${cur/list-keys -N/run-shell $HOME/.config/tmux/keys-popup.sh}"
      exit 0 ;;
  esac
  sleep 0.2
done
