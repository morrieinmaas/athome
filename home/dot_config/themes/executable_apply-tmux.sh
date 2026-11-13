#!/usr/bin/env bash
# Push the active theme's pill palette into the running tmux server as @theme_*
# user options. tmux re-evaluates #{@theme_*} in the status format on each draw,
# so the bar recolours immediately. Called two ways:
#   • by tmux.conf on server start  (honours a theme picked in a previous session)
#   • by the `theme` picker          (live switch in the current session)
# With a name argument it applies that theme instead of the active one: the
# picker's live preview, which leaves `active` alone.
set -euo pipefail

dir="${HOME}/.config/themes"
name="${1:-$(cat "$dir/active" 2>/dev/null || echo gruvbox)}"

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

# ── tmux 3.8's own colours ───────────────────────────────────────────────────
# tmux 3.8 restyled its UI (popups, menus, copy mode, messages, pane borders)
# with named colours (themeblack … themeyellow) that it fills from built-in
# greys and goldenrods, picking the light or dark set from what the terminal
# reports. Point both sets at this theme's palettes instead. Older tmux has no
# such options: every set here is allowed to fail.
opt() { tmux set -g "$1" "$2" 2>/dev/null || true; }
for mode in light dark; do
  eval "$(theme_palette "$name" "$mode" 2>/dev/null || theme_palette gruvbox "$mode")"
  if [ "$mode" = light ]; then ink=$FG paper=$BG light=$GREY darkg=$MUTED
  else ink=$BG paper=$FG light=$MUTED darkg=$GREY; fi
  opt "$mode-theme-black"      "$ink"
  opt "$mode-theme-white"      "$paper"
  opt "$mode-theme-light-grey" "$light"
  opt "$mode-theme-dark-grey"  "$darkg"
  opt "$mode-theme-green"      "$GREEN"
  opt "$mode-theme-yellow"     "$YELLOW"
  opt "$mode-theme-blue"       "$BLUE"
done
eval "$pal"   # back to the pill palette for the literal colours below

# Popups and menus on the terminal's own background (tmux 3.7 and older did
# this; 3.8 paints them a grey slab), framed by a rounded border in the theme's
# grey, the same shape as the status-bar pills.
opt popup-style        default
opt popup-border-style "fg=$GREY"
opt popup-border-lines rounded
opt menu-style         default
opt menu-border-style  "fg=$GREY"
opt menu-border-lines  rounded
# 3.8 also underlines the current pane and session labels by default.
opt pane-status-current-style    default
opt session-status-current-style default

tmux refresh-client -S 2>/dev/null || true
