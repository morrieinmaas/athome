#!/usr/bin/env bash
# Rewrite gh-dash's `theme:` block to match the active `theme` pick + the current
# system dark/light, so the GitHub dashboard follows the picker. gh-dash reads its
# config fresh on every launch, so the `prefix g` popup runs this just before it.
# Called by: tmux.conf's `prefix g` popup, and ~/.local/bin/theme on a pick.
#
# gh-dash has no ANSI-following mode (every colour must be an explicit hex/256),
# so we generate a full hex block from registry.sh::theme_palette. The block lives
# between two markers in config.yml; we replace everything between them in place.
set -euo pipefail

dir="${HOME}/.config/themes"
cfg="${HOME}/.config/gh-dash/config.yml"
[ -f "$cfg" ] || exit 0

# shellcheck source=/dev/null
. "$dir/registry.sh"

family="$(cat "$dir/active" 2>/dev/null || echo gruvbox)"

# Current appearance → mode (default light, matching the light-first palettes).
mode=light
case "$(uname -s)" in
  Darwin) [ "$(defaults read -g AppleInterfaceStyle 2>/dev/null)" = Dark ] && mode=dark ;;
  *)      case "$(gsettings get org.gnome.desktop.interface color-scheme 2>/dev/null)" in
            *dark*) mode=dark ;;
          esac ;;
esac

pal="$(theme_palette "$family" "$mode")"
[ -z "$pal" ] && pal="$(theme_palette gruvbox "$mode")"   # unknown family → safe default
eval "$pal"   # BG FG BLUE YELLOW GREEN GREY MUTED

beg='# >>> theme (managed by apply-ghdash.sh — do not edit) >>>'
end='# <<< theme (managed by apply-ghdash.sh) <<<'

block="$(cat <<EOF
$beg
theme:
  colors:
    text:
      primary: "$FG"
      secondary: "$BLUE"
      inverted: "$BG"
      faint: "$GREY"
      warning: "$YELLOW"
      success: "$GREEN"
    background:
      selected: "$GREY"
    border:
      primary: "$BLUE"
      secondary: "$MUTED"
      faint: "$GREY"
$end
EOF
)"

tmp="$(mktemp)"
# Drop any existing managed block, then append the fresh one.
awk -v b="$beg" -v e="$end" '
  $0 == b {skip=1}
  skip && $0 == e {skip=0; next}
  !skip {print}
' "$cfg" > "$tmp"
# Ensure a trailing newline before appending.
[ -n "$(tail -c1 "$tmp")" ] && printf '\n' >> "$tmp"
printf '%s\n' "$block" >> "$tmp"
mv "$tmp" "$cfg"
