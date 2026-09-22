#!/usr/bin/env bash
# Sourceable fzf palette for tmux popups. Exports COLORS for `fzf --color=...`.
#
#   . "$HOME/.config/tmux/fzf-theme.sh"
#
# Why this does NOT just read tmux's @theme_* options, which was the first
# attempt and was wrong: the status bar only ever uses @theme_bg as a FOREGROUND
# on coloured pills, and the bar's own background is `default` (transparent), so
# it looks right in dark mode while holding a light palette. A popup is the one
# place that paints @theme_bg as a real background, so reading those options
# gives a light popup floating in a dark terminal.
#
# Instead ask registry.sh for the palette in the CURRENT appearance, which is
# the same source the ghostty and nvim themes use, so the popup matches the
# terminal rather than the status bar's foreground-only palette.
#
# bg AND gutter are set together on purpose: fzf's default transparent gutter
# renders as a dark bar down the left inside a tmux popup.

_ft_dir="${HOME}/.config/themes"

_ft_is_dark() {  # same detection as pet.sh / battery.sh
  if [ "$(uname)" = Darwin ]; then
    [ "$(defaults read -g AppleInterfaceStyle 2>/dev/null)" = Dark ]
  else
    local s
    s=$(gdbus call --session --dest org.freedesktop.portal.Desktop \
          --object-path /org/freedesktop/portal/desktop \
          --method org.freedesktop.portal.Settings.ReadOne \
          org.freedesktop.appearance color-scheme 2>/dev/null)
    case "$s" in *'uint32 1'*) return 0 ;; *'uint32 2'*) return 1 ;; esac
    [ "$(gsettings get org.gnome.desktop.interface color-scheme 2>/dev/null)" = "'prefer-dark'" ]
  fi
}

if _ft_is_dark; then _ft_mode=dark; else _ft_mode=light; fi
_ft_name="$(cat "$_ft_dir/active" 2>/dev/null || echo gruvbox)"

_ft_pal=""
if [ -r "$_ft_dir/registry.sh" ]; then
  # shellcheck source=/dev/null
  . "$_ft_dir/registry.sh"
  _ft_pal="$(theme_palette "$_ft_name" "$_ft_mode" 2>/dev/null || true)"
  [ -z "$_ft_pal" ] && _ft_pal="$(theme_palette gruvbox "$_ft_mode" 2>/dev/null || true)"
fi
# Last resort if the registry is missing entirely: everforest, matched to mode.
if [ -z "$_ft_pal" ]; then
  if [ "$_ft_mode" = dark ]; then
    _ft_pal='BG=#2b3339 FG=#d3c6aa BLUE=#7fbbb3 YELLOW=#dbbc7f GREEN=#a7c080 GREY=#859289 MUTED=#9da9a0'
  else
    _ft_pal='BG=#fdf6e3 FG=#5c6a72 BLUE=#3a94c5 YELLOW=#dfa000 GREEN=#8da101 GREY=#a6b0a0 MUTED=#829181'
  fi
fi
eval "$_ft_pal"   # sets BG FG BLUE YELLOW GREEN GREY MUTED

# Selected row: accent background with the page background as its text colour,
# which keeps contrast correct in both modes.
COLORS="bg:${BG},gutter:${BG},fg:${FG},bg+:${GREEN},fg+:${BG},hl:${YELLOW},hl+:${BG},pointer:${BLUE},marker:${BLUE},prompt:${GREEN},info:${MUTED},header:${MUTED},border:${MUTED},label:${GREEN}"
export COLORS
