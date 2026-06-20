#!/usr/bin/env bash
# tmux battery pill content: a Nerd Font (Font Awesome) battery glyph picked by
# charge level + the percentage, with a bolt when charging / plugged in. Prints
# NOTHING when there's no battery (desktops / CI) so the pill can be hidden with
# `tmux set -g @bat ''`. Glyphs render from codepoints (no multibyte bytes in
# tracked files) — needs a Nerd Font. macOS: pmset; Linux: /sys/class/power_supply.
set -u

pct=""; charging=0
if [ "$(uname)" = Darwin ]; then
  batt="$(pmset -g batt 2>/dev/null)"
  pct="$(printf '%s\n' "$batt" | grep -oE '[0-9]+%' | head -1 | tr -d '%')"
  # "discharging" = on battery; anything else (charging / charged / AC) = plugged.
  printf '%s\n' "$batt" | grep -qi 'discharging' || charging=1
else
  for b in /sys/class/power_supply/BAT*; do
    [ -r "$b/capacity" ] || continue
    pct="$(cat "$b/capacity" 2>/dev/null)"
    case "$(cat "$b/status" 2>/dev/null)" in Charging|Full|"Not charging") charging=1 ;; esac
    break
  done
fi

case "$pct" in '' | *[!0-9]*) exit 0 ;; esac   # no / non-numeric battery → no pill

# Font Awesome battery glyphs: F240 full · F241 ¾ · F242 ½ · F243 ¼ · F244 empty.
if   [ "$pct" -ge 90 ]; then g=F240
elif [ "$pct" -ge 65 ]; then g=F241
elif [ "$pct" -ge 40 ]; then g=F242
elif [ "$pct" -ge 15 ]; then g=F243
else                         g=F244
fi
glyph="$(printf "\\U$(printf '%08X' "0x$g")")"
bolt=""
[ "$charging" = 1 ] && bolt="$(printf "\\U$(printf '%08X' 0xF0E7)") "   # nf-fa-bolt

printf '%s%s %s%%' "$bolt" "$glyph" "$pct"
