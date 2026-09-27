#!/usr/bin/env bash
# Rewrite slk's [appearance] theme to match the active `theme` pick + the current
# system dark/light, so the Slack TUI follows the picker. slk reads its config
# fresh on every launch, so the `prefix M` popup runs this just before slk.
# Called by: tmux.conf's `prefix M` popup, and ~/.local/bin/theme on a pick.
set -euo pipefail

dir="${HOME}/.config/themes"
# $1 = file to recolour instead (the chezmoi modify_ script passes a temp copy).
cfg="${1:-${HOME}/.config/slk/config.toml}"
[ -f "$cfg" ] || exit 0   # slk not configured yet — nothing to do

# shellcheck source=/dev/null
. "$(dirname "${BASH_SOURCE[0]}")/registry.sh"   # own dir: works from ~/.config/themes and the source repo

family="$(cat "$dir/active" 2>/dev/null || echo gruvbox)"

# Current appearance → mode (default light; the pills + palettes are light-first).
mode=light
case "$(uname -s)" in
  Darwin) [ "$(defaults read -g AppleInterfaceStyle 2>/dev/null)" = Dark ] && mode=dark ;;
  *)      case "$(gsettings get org.gnome.desktop.interface color-scheme 2>/dev/null)" in
            *dark*) mode=dark ;;
          esac ;;
esac

name="$(theme_slk "$family" "$mode")"
[ -z "$name" ] && name="$(theme_slk gruvbox "$mode")"   # unknown family → safe default

# Rewrite EVERY `theme = ` line — the [appearance] one plus any per-workspace
# override slk's in-app switcher wrote — so nothing beats the picker.
sed -i.bak -E "s|^theme[[:space:]]*=.*|theme = \"$name\"|" "$cfg" && rm -f "$cfg.bak"
