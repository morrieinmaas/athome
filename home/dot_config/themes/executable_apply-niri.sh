#!/usr/bin/env bash
# Recolour niri's focus ring, border and insert hint from the active theme.
# Called by the `theme` picker; a no-op on machines without a niri config.
#
# niri has no include directive and no runtime colour API, so the only way to
# theme it is to rewrite the literal colours in config.kdl. That is safe here
# because niri WATCHES its config and hot-reloads on write, so the change is
# visible immediately without restarting the compositor or logging out.
#
# Only the five colour VALUES are touched; the surrounding KDL is left alone.
# The sed patterns anchor on the key name inside the block, so reordering or
# adding keys in config.kdl will not break this.
set -euo pipefail

dir="${HOME}/.config/themes"
# $1 = file to recolour instead (the chezmoi modify_ script passes a temp copy).
cfg="${1:-${HOME}/.config/niri/config.kdl}"
[ -f "$cfg" ] || { echo "apply-niri: no $cfg, skipping"; exit 0; }

name="$(cat "$dir/active" 2>/dev/null || echo gruvbox)"

is_dark() {  # same detection as pet.sh / battery.sh / fzf-theme.sh
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
if is_dark; then mode=dark; else mode=light; fi

# shellcheck source=/dev/null
. "$(dirname "${BASH_SOURCE[0]}")/registry.sh"   # own dir: works from ~/.config/themes and the source repo
pal="$(theme_palette "$name" "$mode" 2>/dev/null || true)"
[ -z "$pal" ] && pal="$(theme_palette gruvbox "$mode")"
eval "$pal"   # sets BG FG BLUE YELLOW GREEN GREY MUTED

# Roles:
#   active-color     the focused window. BLUE, so it matches the tmux session
#                    pill and ghostty's accent under the same theme.
#   inactive-color   MUTED, deliberately low contrast: unfocused windows should
#                    recede rather than compete with the focused one.
#   insert-hint      YELLOW, deliberately NOT the same as active-color. It marks
#                    where a new window will land, which is a different question
#                    from which window has focus, so it should read differently.
# active-/inactive-color are unambiguous keys, so a plain substitution is safe.
# A BARE `color` key is not: insert-hint has one, but so does niri's `shadow`
# block in newer versions, and a blanket rule would repaint shadows yellow. So
# track the enclosing block and only touch the one inside insert-hint.
tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
awk -v blue="$BLUE" -v muted="$MUTED" -v yellow="$YELLOW" '
  /insert-hint[[:space:]]*\{/ { inhint = 1 }
  inhint && /^[[:space:]]*\}/  { inhint = 0 }
  {
    if ($0 ~ /^[[:space:]]*active-color[[:space:]]+"#[0-9a-fA-F]{6}"/) {
      sub(/"#[0-9a-fA-F]{6}"/, "\"" blue "\"")
    } else if ($0 ~ /^[[:space:]]*inactive-color[[:space:]]+"#[0-9a-fA-F]{6}"/) {
      sub(/"#[0-9a-fA-F]{6}"/, "\"" muted "\"")
    } else if (inhint && $0 ~ /^[[:space:]]*color[[:space:]]+"#[0-9a-fA-F]{6}"/) {
      sub(/"#[0-9a-fA-F]{6}"/, "\"" yellow "\"")
    }
    print
  }
' "$cfg" > "$tmp"

if cmp -s "$cfg" "$tmp"; then
  echo "apply-niri: already ${name}/${mode}" >&2
else
  cat "$tmp" > "$cfg"   # write in place so niri's watcher sees one modification
  echo "apply-niri: niri recoloured to ${name}/${mode}" >&2
fi
