#!/usr/bin/env bash
# Post-load patches to tmux-which-key's menus (prefix Space):
#
#   1. Centre them (-x C -y C). which-key anchors at the pane (-x R -y P), so the
#      menu grows up from the pane's bottom edge and its top ran off-screen in any
#      short pane (e.g. a Claude session split).
#   2. "+Keys ?" opens our searchable fzf popup (keys-popup.sh) instead of the
#      non-searchable `list-keys -N` pager, so `prefix Space ?` matches `prefix ?`.
#
# which-key bakes the menu into the show-wk-menu / show-wk-menu-root command
# aliases plus an inline copy in the `prefix Space` binding — NOT into
# @wk_menu_root, which is only its build input. (Patching @wk_menu_root, as this
# script used to, changed nothing.)
# Done here rather than in the plugin's generated files, which live in the
# untracked, git-cloned plugin dir, so it survives plugin updates.
#
# POLL: on `source-file`, canopy's plugin loader re-sources which-key AFTER
# this starts (user.conf runs before the loader) and which-key rebuilds all of
# the above. So poll until it has built them fresh, which is why user.conf
# calls us backgrounded (`run-shell -b`).
set -uo pipefail

popup="run-shell $HOME/.config/tmux/keys-popup.sh"
patch() {
  sed -e "s/-x 'R' -y 'P'/-x 'C' -y 'C'/g" -e 's/-x R -y P/-x C -y C/g' \
      -e "s|list-keys -N|$popup|g"
}

aliases() { tmux show -gv command-alias 2>/dev/null; }
# Wait for which-key's own build: its root alias still at -x R -y P. On a
# reload the alias patched last time is already there, and which-key
# replaces it only when the plugin loader reaches it, after this started.
fresh() { aliases | grep '^show-wk-menu-root=' | grep -q -e "-x 'R' -y 'P'" -e '-x R -y P'; }
for _ in $(seq 1 50); do   # ~10s max (which-key usually builds within ~1s)
  fresh && break
  sleep 0.2
done
fresh || exit 0

# Pair each command-alias index with its raw value, patch the which-key ones.
mapfile -t idx < <(tmux show -g command-alias | sed -E 's/^command-alias\[([0-9]+)\].*/\1/')
mapfile -t val < <(aliases)
for i in "${!idx[@]}"; do
  v="${val[$i]}"
  [[ "$v" == show-wk-menu* ]] || continue
  new="$(printf '%s' "$v" | patch)"
  [[ "$new" != "$v" ]] && tmux set -g "command-alias[${idx[$i]}]" "$new"
done

# The prefix Space binding is an inline copy of the root menu; point it at the
# (now patched) alias instead of re-parsing `list-keys` output.
tmux bind-key -T prefix Space show-wk-menu-root
exit 0
