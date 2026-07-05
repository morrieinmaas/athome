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
THEME_LIST="gruvbox everforest catppuccin tokyonight rose-pine kanagawa flexoki melange zenbones selenized"

theme_ghostty() {
  case "$1" in
    gruvbox)    echo "dark:Gruvbox Dark,light:Gruvbox Light" ;;
    everforest) echo "dark:Everforest Dark Hard,light:Everforest Light Med" ;;
    catppuccin) echo "dark:Catppuccin Mocha,light:Catppuccin Latte" ;;
    tokyonight) echo "dark:TokyoNight Moon,light:TokyoNight Day" ;;
    rose-pine)  echo "dark:Rose Pine Moon,light:Rose Pine Dawn" ;;
    kanagawa)   echo "dark:Kanagawa Wave,light:Kanagawa Lotus" ;;
    flexoki)    echo "dark:Flexoki Dark,light:Flexoki Light" ;;
    melange)    echo "dark:Melange Dark,light:Melange Light" ;;
    zenbones)   echo "dark:Zenbones Dark,light:Zenbones Light" ;;
    selenized)  echo "dark:Selenized Dark,light:Selenized Light" ;;
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
    flexoki)    echo "flexoki-light/dark" ;;   # per-mode names (see nvim config/theme.lua)
    melange)    echo melange ;;
    zenbones)   echo zenbones ;;
    selenized)  echo "base16-selenized-*" ;;   # per-mode names (see nvim config/theme.lua)
  esac
}

# slk (Slack TUI) — maps a family to slk's own theme name per light/dark mode.
# Unlike ghostty (one `dark:…,light:…` value), slk's [appearance] theme is a
# SINGLE name, so apply-slk.sh detects the current mode and picks the variant.
# Names are slk's built-ins (internal/ui/styles/themes.go). Sourced by apply-slk.sh.
theme_slk() {  # $1 = family, $2 = mode (dark|light)
  case "$1:$2" in
    gruvbox:dark)     echo "Gruvbox Dark" ;;     gruvbox:light)     echo "Gruvbox Light" ;;
    everforest:dark)  echo "Everforest Dark" ;;  everforest:light)  echo "Everforest Light" ;;
    catppuccin:dark)  echo "Catppuccin Mocha" ;; catppuccin:light)  echo "Catppuccin Latte" ;;
    tokyonight:dark)  echo "Tokyo Night Storm" ;; tokyonight:light) echo "Tokyo Night Light" ;;
    rose-pine:dark)   echo "Rosé Pine Moon" ;;   rose-pine:light)   echo "Rosé Pine Dawn" ;;
    kanagawa:dark)    echo "Kanagawa Dragon" ;;  kanagawa:light)    echo "Kanagawa Lotus" ;;
    # flexoki/melange/zenbones/selenized: slk ships no matching NAMED theme, so
    # they use slk's ANSI theme — it renders from the terminal's own 16-colour
    # palette, which ghostty has already repainted to the picked family. So Slack
    # follows the active theme (like btm does) instead of the gruvbox fallback.
    flexoki:dark|melange:dark|zenbones:dark|selenized:dark)      echo "ANSI Dark" ;;
    flexoki:light|melange:light|zenbones:light|selenized:light)  echo "ANSI Light" ;;
  esac
}

# Full hex palette per family AND mode — the dark counterpart to theme_tmux's
# light-only pills. apply-ghdash.sh evals this to recolour gh-dash (which has no
# ANSI-following option: it needs explicit hex/256 for every slot, and ANSI slots
# can't flip fg/bg between light & dark). Dark values track the ghostty dark
# variants (Gruvbox Dark / Everforest Dark Hard / Catppuccin Mocha / TokyoNight
# Moon / Rosé Pine Moon / Kanagawa Wave); light delegates to theme_tmux.
theme_palette() {  # $1 = family, $2 = mode (dark|light) -> BG FG BLUE YELLOW GREEN GREY MUTED
  if [ "${2:-light}" = light ]; then theme_tmux "$1"; return; fi
  case "$1" in
    gruvbox)    echo 'BG=#282828 FG=#ebdbb2 BLUE=#83a598 YELLOW=#d8a657 GREEN=#b8bb26 GREY=#928374 MUTED=#a89984' ;;
    everforest) echo 'BG=#2b3339 FG=#d3c6aa BLUE=#7fbbb3 YELLOW=#dbbc7f GREEN=#a7c080 GREY=#859289 MUTED=#9da9a0' ;;
    catppuccin) echo 'BG=#1e1e2e FG=#cdd6f4 BLUE=#89b4fa YELLOW=#f9e2af GREEN=#a6e3a1 GREY=#9399b2 MUTED=#7f849c' ;;
    tokyonight) echo 'BG=#222436 FG=#c8d3f5 BLUE=#82aaff YELLOW=#ffc777 GREEN=#c3e88d GREY=#828bb8 MUTED=#636da6' ;;
    rose-pine)  echo 'BG=#232136 FG=#e0def4 BLUE=#9ccfd8 YELLOW=#f6c177 GREEN=#3e8fb0 GREY=#6e6a86 MUTED=#908caa' ;;
    kanagawa)   echo 'BG=#1f1f28 FG=#dcd7ba BLUE=#7e9cd8 YELLOW=#e6c384 GREEN=#98bb6c GREY=#727169 MUTED=#957fb8' ;;
    flexoki)    echo 'BG=#100f0f FG=#cecdc3 BLUE=#4385be YELLOW=#d0a215 GREEN=#879a39 GREY=#575653 MUTED=#878580' ;;
    melange)    echo 'BG=#292522 FG=#ece1d7 BLUE=#7f91b2 YELLOW=#ebc06d GREEN=#85b695 GREY=#867462 MUTED=#c1a78e' ;;
    zenbones)   echo 'BG=#1c1917 FG=#b4bdc3 BLUE=#6099c0 YELLOW=#b77e64 GREEN=#819b69 GREY=#777d81 MUTED=#4d5154' ;;
    selenized)  echo 'BG=#103c48 FG=#adbcbc BLUE=#4695f7 YELLOW=#dbb32d GREEN=#75b938 GREY=#2d5b69 MUTED=#72898f' ;;
  esac
}

# btm (bottom) is NOT switched here: its config uses ANSI "Reset" + named accent
# colours, so it follows the terminal's active palette automatically (light or
# dark, any family) — see dot_config/bottom/bottom.toml. Same hands-off rule as
# slk's "ANSI Dark". (Replaced btop, which had no native dual-theme and needed a
# per-launch color_theme rewrite here.)

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
    flexoki)    echo 'BG=#fffcf0 FG=#100f0f BLUE=#205ea6 YELLOW=#d0a215 GREEN=#879a39 GREY=#b7b5ac MUTED=#6f6e69' ;;
    melange)    echo 'BG=#f1f1f1 FG=#54433a BLUE=#465aa4 YELLOW=#a06d00 GREEN=#3a684a GREY=#a98a78 MUTED=#7d6658' ;;
    zenbones)   echo 'BG=#f0edec FG=#2c363c BLUE=#286486 YELLOW=#944927 GREEN=#4f6c31 GREY=#596a75 MUTED=#859fae' ;;
    selenized)  echo 'BG=#fbf3db FG=#3a4d53 BLUE=#0072d4 YELLOW=#ad8900 GREEN=#489100 GREY=#d5cdb6 MUTED=#909995' ;;
  esac
}
