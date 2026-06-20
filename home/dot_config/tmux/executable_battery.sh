#!/usr/bin/env bash
# tmux status-bar battery segment — a colour-emoji battery icon (same visual
# size as pet.sh's emoji: ⚡ charging, 🪫 low, 🔋 otherwise) + percentage. The
# emoji conveys state at a glance; the % is colour-coded from the live theme —
# green while charging/charged, amber when low, red when critical, normal
# otherwise. Self-hides on machines with no battery (desktops), so it's safe to
# leave enabled everywhere.
#
# Icons render from codepoints (no literal multibyte bytes in tracked files),
# matching pet.sh — needs a colour-emoji font (macOS built-in; Linux via
# noto-fonts-emoji). macOS reads `pmset`; Linux reads /sys/class/power_supply.
# Emits #[fg=…]…#[default] so tmux re-parses the percentage colour.

# Emoji from codepoint + VS16 (U+FE0F) to FORCE colour-emoji (2-cell)
# presentation. Without VS16, default-text-presentation glyphs like ⚡ (U+26A1)
# render as a narrow 1-cell monochrome symbol — which both breaks the alignment
# while charging and looks dull. VS16 makes every state a uniform 2 cells AND
# the full-colour emoji.
emoji() { printf -v h '%08X' "0x$1"; printf "\\U$h\\U0000FE0F"; }

# Theme colours from tmux user options, falling back to gruvbox if unset.
opt() { tmux show -gv "$1" 2>/dev/null; }
blue=$(opt @theme_blue); blue=${blue:-#458588}
bg=$(opt @theme_bg);     bg=${bg:-#fbf1c7}
pl=$(opt @pill_l); pr=$(opt @pill_r)

pct=""; charging=0

if [ "$(uname)" = Darwin ]; then
  batt=$(pmset -g batt 2>/dev/null)
  case "$batt" in *InternalBattery*) ;; *) exit 0 ;; esac   # no battery present
  pct=$(printf '%s\n' "$batt" | grep -o '[0-9]\{1,3\}%' | head -1 | tr -d '%')
  case "$batt" in
    *" charging"*|*"AC Power"*|*charged*|*"finishing charge"*) charging=1 ;;
  esac
else
  for b in /sys/class/power_supply/BAT*; do
    [ -r "$b/capacity" ] || continue
    pct=$(cat "$b/capacity")
    case "$(cat "$b/status" 2>/dev/null)" in
      Charging|Full|"Not charging") charging=1 ;;
    esac
    break
  done
fi

[ -n "$pct" ] || exit 0   # no battery → render nothing (pill disappears)

# Icon by state: ⚡️ charging · 🪫 low (≤15%) · 🔋 otherwise. The pill background
# is a FIXED palette colour (blue) for EVERY state, so it never clashes with the
# green/yellow/red of the emoji sitting inside it.
if   [ "$charging" -eq 1 ]; then icon=26A1    # ⚡️
elif [ "$pct" -le 15 ];     then icon=1FAAB   # 🪫
else                             icon=1F50B   # 🔋
fi

# Icon AND "<pct>%" both live inside the pill: " <emoji> <pct>% ". A space each
# side keeps them off the rounded caps; width changes only on a rare digit-count
# change (9→10, 99→100), not every tick.
printf '#[fg=%s,bg=default]%s#[fg=%s,bg=%s,bold] %s %s%% #[fg=%s,bg=default,nobold]%s' \
  "$blue" "$pl" "$bg" "$blue" "$(emoji "$icon")" "$pct" "$blue" "$pr"
