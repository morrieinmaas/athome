#!/usr/bin/env bash
# fzf icon picker for the status-bar icon — bound to `prefix P`. Generates the
# emoji catalog ON THE FLY from Python's stdlib unicodedata (via `uv run
# python3` — no third-party packages, no image rendering, no committed data
# file). Pick a LIGHT-mode icon then a DARK-mode one; pet.sh shows whichever
# matches the system appearance. Writes two codepoints "LIGHTCP DARKCP" to
# ~/.config/tmux/pet-glyph. Type to search by name.
export PATH="$HOME/.local/share/mise/shims:$PATH"   # fzf + uv may be mise-managed

# everforest fzf palette matched to the system appearance (like pet.sh/ghostty).
# bg AND gutter are set explicitly to the same colour so the popup has no dark
# left bar (the default -1/transparent gutter rendered black inside the popup).
is_dark() {  # same detection as pet.sh — portal first, gsettings fallback
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
if is_dark; then
  BG=2d353b FG=d3c6aa SEL=a7c080 SELFG=2d353b HL=dbbc7f PTR=e67e80 ACC=a7c080 HDR=859289
else
  BG=fdf6e3 FG=5c6a72 SEL=8da101 SELFG=fdf6e3 HL=dfa000 PTR=e66868 ACC=8da101 HDR=829181
fi
COLORS="bg:#$BG,gutter:#$BG,fg:#$FG,bg+:#$SEL,fg+:#$SELFG,hl:#$HL,hl+:#$SELFG,pointer:#$PTR,marker:#$PTR,prompt:#$ACC,info:#$HDR,header:#$HDR,border:#$HDR"

# Catalog: "<glyph> <name>\t<CP>" — glyph + name are ONE display field separated
# by a single space (NOT a tab; a tab would pad out to the next tab stop and
# leave a big gap). The codepoint sits in field 2 (after the tab). Runtime glyph
# bytes are fine — the no-literal-glyphs rule is about *tracked* files.
list=$(uv run python3 - <<'PY'
import unicodedata
blocks = [(0x1F300, 0x1F5FF), (0x1F600, 0x1F64F), (0x1F680, 0x1F6FF),
          (0x1F900, 0x1F9FF), (0x1FA70, 0x1FAFF), (0x2600, 0x26FF), (0x2700, 0x27BF)]
for a, b in blocks:
    for cp in range(a, b + 1):
        try:
            name = unicodedata.name(chr(cp))
        except ValueError:
            continue
        print(f"{chr(cp)} {name.lower()}\t{cp:X}")
PY
)

pick() {  # $1 prompt, $2 header → echoes the chosen codepoint (hex)
  printf '%s\n' "$list" | fzf --prompt="$1 ▸ " --with-nth=1 --delimiter='\t' \
    --reverse --height=100% --header="$2" --no-separator --no-scrollbar --color="$COLORS" \
    | cut -f2
}

lightcp=$(pick "light mode" "LIGHT-mode icon (1/2) — type to search, enter to choose") || exit 0
[ -z "$lightcp" ] && exit 0
darkcp=$(pick "dark mode" "DARK-mode icon (2/2) — type to search, enter to choose")     || exit 0
[ -z "$darkcp" ] && exit 0

printf '%s %s\n' "$lightcp" "$darkcp" > "$HOME/.config/tmux/pet-glyph"
command -v tmux >/dev/null && tmux refresh-client -S
