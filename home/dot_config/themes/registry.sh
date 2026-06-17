# shellcheck shell=bash
# Theme registry — the single source of truth for the `theme` picker.
#
# Each theme maps to a value per app:
#   theme_ghostty <name>  -> ghostty `theme =` value (dark:…,light:…)
#   theme_nvim    <name>  -> :colorscheme arg (the nvim plugin derives light/dark
#                            from vim.opt.background, which auto-dark-mode toggles)
#   theme_tmux    <name>  -> a LIGHT pill palette: BG FG BLUE YELLOW GREEN GREY MUTED
#
# bat/delta/zed are NOT switched here: bat only bundles gruvbox among these
# families (everforest/catppuccin/tokyonight/rose-pine/kanagawa aren't shipped),
# so they stay on gruvbox-light/dark — the one palette they can all render.
#
# Sourced by ~/.local/bin/theme and ~/.config/themes/apply-tmux.sh.

# shellcheck disable=SC2034  # consumed by sourcing scripts (theme, apply-tmux.sh)
THEME_LIST="gruvbox everforest catppuccin tokyonight rose-pine kanagawa"

theme_ghostty() {
  case "$1" in
    gruvbox)    echo "dark:Gruvbox Dark,light:Gruvbox Light" ;;
    everforest) echo "dark:Everforest Dark Hard,light:Everforest Light Med" ;;
    catppuccin) echo "dark:Catppuccin Mocha,light:Catppuccin Latte" ;;
    tokyonight) echo "dark:TokyoNight Moon,light:TokyoNight Day" ;;
    rose-pine)  echo "dark:Rose Pine Moon,light:Rose Pine Dawn" ;;
    kanagawa)   echo "dark:Kanagawa Wave,light:Kanagawa Lotus" ;;
  esac
}

theme_nvim() {
  case "$1" in
    gruvbox)    echo gruvbox ;;
    everforest) echo everforest ;;
    catppuccin) echo catppuccin ;;
    tokyonight) echo tokyonight ;;
    rose-pine)  echo rose-pine ;;
    kanagawa)   echo kanagawa ;;
  esac
}

# btop bundled .theme names as "LIGHT DARK" (btop has no native dual-theme, so the
# picker writes the one matching the current OS appearance). Families btop doesn't
# ship fall back to gruvbox — same "where possible" rule as bat/delta/zed.
theme_btop() {
  case "$1" in
    gruvbox)    echo "gruvbox_light gruvbox_dark" ;;
    everforest) echo "everforest-light-medium everforest-dark-medium" ;;
    kanagawa)   echo "kanagawa-lotus kanagawa-wave" ;;
    tokyonight) echo "gruvbox_light tokyo-night" ;;   # no bundled tokyo-light → gruvbox in light mode
    catppuccin) echo "gruvbox_light gruvbox_dark" ;;  # not bundled → gruvbox
    rose-pine)  echo "gruvbox_light gruvbox_dark" ;;  # not bundled → gruvbox
  esac
}

# Light pill palettes (the bar floats on the transparent terminal bg, so these
# are the LIGHT-mode accents; the terminal itself follows ghostty's dark/light).
theme_tmux() {
  case "$1" in
    gruvbox)    echo 'BG=#fbf1c7 FG=#3c3836 BLUE=#458588 YELLOW=#b57614 GREEN=#79740e GREY=#a89984 MUTED=#7c6f64' ;;
    everforest) echo 'BG=#fdf6e3 FG=#5c6a72 BLUE=#3a94c5 YELLOW=#dfa000 GREEN=#8da101 GREY=#a6b0a0 MUTED=#829181' ;;
    catppuccin) echo 'BG=#eff1f5 FG=#4c4f69 BLUE=#1e66f5 YELLOW=#df8e1d GREEN=#40a02b GREY=#9ca0b0 MUTED=#8c8fa1' ;;
    tokyonight) echo 'BG=#e1e2e7 FG=#3760bf BLUE=#2e7de9 YELLOW=#8c6c3e GREEN=#587539 GREY=#848cb5 MUTED=#9da3c2' ;;
    rose-pine)  echo 'BG=#faf4ed FG=#575279 BLUE=#56949f YELLOW=#ea9d34 GREEN=#286983 GREY=#9893a5 MUTED=#9893a5' ;;
    kanagawa)   echo 'BG=#f2ecbc FG=#545464 BLUE=#4d699b YELLOW=#cc6d00 GREEN=#6f894e GREY=#8a8980 MUTED=#a09cac' ;;
  esac
}
