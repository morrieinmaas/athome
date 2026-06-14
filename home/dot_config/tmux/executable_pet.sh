#!/usr/bin/env bash
# tmux status-bar icon — a LIGHT-mode glyph and a DARK-mode glyph, switched to
# follow the system appearance (the signal ghostty/nvim/bat use). NO animation,
# NO status readout, NO pill background — the emoji just floats on the
# transparent status bar. The two glyphs are chosen via the fzf picker
# (`prefix P`) and stored as two codepoints in ~/.config/tmux/pet-glyph
# ("LIGHTCP DARKCP"). Defaults: 🐢 (light) / 🦉 (dark).
#
# Glyphs render from codepoints (no literal multibyte bytes in tracked files);
# needs a color-emoji font (macOS built-in; Linux via noto-fonts-emoji).

# New pet-glyph is "LIGHTCP DARKCP"; tolerate the old 4-field
# "LIGHTCP COLOR DARKCP COLOR" by detecting the extra fields.
if [ -r "$HOME/.config/tmux/pet-glyph" ]; then
  read -r f1 f2 f3 _ < "$HOME/.config/tmux/pet-glyph"
  if [ -n "$f3" ]; then lightcp=$f1; darkcp=$f3; else lightcp=$f1; darkcp=$f2; fi
fi
[[ "$lightcp" =~ ^[0-9A-Fa-f]+$ ]] || lightcp=1F422   # 🐢
[[ "$darkcp"  =~ ^[0-9A-Fa-f]+$ ]] || darkcp=1F989    # 🦉

# Follow the OS appearance. macOS: AppleInterfaceStyle is "Dark" only in dark
# mode (the key is absent in light mode). Linux: the XDG desktop-portal
# colour-scheme (1=dark, 2=light), falling back to GNOME's gsettings key.
is_dark() {
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
if is_dark; then cp=$darkcp; else cp=$lightcp; fi

printf -v hex '%08X' "0x$cp"
printf "\\U$hex"
