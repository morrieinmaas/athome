#!/usr/bin/env bash
# Push the active theme's pill palette into the running tmux server as @theme_*
# user options. tmux re-evaluates #{@theme_*} in the status format on each draw,
# so the bar recolours immediately. Called two ways:
#   • by tmux.conf on server start  (honours a theme picked in a previous session)
#   • by the `theme` picker          (live switch in the current session)
set -euo pipefail

dir="${HOME}/.config/themes"
name="$(cat "$dir/active" 2>/dev/null || echo gruvbox)"

# shellcheck source=/dev/null
. "$dir/registry.sh"

pal="$(theme_tmux "$name")"
[ -z "$pal" ] && pal="$(theme_tmux gruvbox)"   # unknown name → safe default
eval "$pal"   # sets BG FG BLUE YELLOW GREEN GREY MUTED

tmux set -g @theme_bg     "$BG"
tmux set -g @theme_fg     "$FG"
tmux set -g @theme_blue   "$BLUE"
tmux set -g @theme_yellow "$YELLOW"
tmux set -g @theme_green  "$GREEN"
tmux set -g @theme_grey   "$GREY"
tmux set -g @theme_muted  "$MUTED"
tmux refresh-client -S 2>/dev/null || true
