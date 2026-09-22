#!/usr/bin/env bash
# Sourceable everforest fzf palette, matched to the system appearance.
# Exports COLORS for `fzf --color="$COLORS"`.
#
#   . "$HOME/.config/tmux/fzf-theme.sh"
#
# bg AND gutter are set to the same colour on purpose: fzf's default
# transparent gutter renders as a dark left bar inside a tmux popup.
#
# NOTE: pet-pick.sh carries its own copy of this block; it predates this file
# and is left alone deliberately rather than refactored as a side effect. Fold
# it in next time that script is touched for its own reasons.

_fzf_theme_is_dark() {  # portal first, gsettings fallback, same as pet.sh
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

if _fzf_theme_is_dark; then
  BG=2d353b FG=d3c6aa SEL=a7c080 SELFG=2d353b HL=dbbc7f PTR=e67e80 ACC=a7c080 HDR=859289
else
  BG=fdf6e3 FG=5c6a72 SEL=8da101 SELFG=fdf6e3 HL=dfa000 PTR=e66868 ACC=8da101 HDR=829181
fi
COLORS="bg:#$BG,gutter:#$BG,fg:#$FG,bg+:#$SEL,fg+:#$SELFG,hl:#$HL,hl+:#$SELFG,pointer:#$PTR,marker:#$PTR,prompt:#$ACC,info:#$HDR,header:#$HDR,border:#$HDR,label:#$ACC"
export COLORS
