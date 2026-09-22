#!/usr/bin/env bash
# Sourceable fzf palette derived from the LIVE tmux theme.
# Exports COLORS for `fzf --color="$COLORS"`.
#
#   . "$HOME/.config/tmux/fzf-theme.sh"
#
# The theme picker (~/.local/bin/theme) already writes @theme_* options into
# tmux and records the choice in ~/.config/themes/active, so reading them back
# is the only way a popup can track the picker instead of drifting from it.
# An earlier version hardcoded everforest, copied from pet-pick.sh, and went
# visibly out of step the moment a different theme was selected.
#
# bg AND gutter are set to the same colour on purpose: fzf's default
# transparent gutter renders as a dark left bar inside a tmux popup.
#
# Falls back to everforest-light when tmux is not running or the options are
# unset, so this stays usable outside a tmux session.
#
# NOTE: pet-pick.sh still carries its own hardcoded copy. Left alone
# deliberately rather than refactored as a side effect; fold it in next time
# that script is edited for its own reasons.

_t() { # $1=option, $2=fallback
  local v
  v=$(tmux show -gv "$1" 2>/dev/null)
  printf '%s' "${v:-$2}"
}

BG=$(_t @theme_bg    '#fdf6e3')
FG=$(_t @theme_fg    '#5c6a72')
ACC=$(_t @theme_green '#8da101')
HL=$(_t @theme_yellow '#dfa000')
PTR=$(_t @theme_red   '#e66868')
HDR=$(_t @theme_muted '#829181')

# Selected row: accent background with the page background as its text colour,
# which keeps contrast correct in both light and dark themes.
COLORS="bg:${BG},gutter:${BG},fg:${FG},bg+:${ACC},fg+:${BG},hl:${HL},hl+:${BG},pointer:${PTR},marker:${PTR},prompt:${ACC},info:${HDR},header:${HDR},border:${HDR},label:${ACC}"
export COLORS
