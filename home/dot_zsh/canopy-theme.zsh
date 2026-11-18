# canopy-theme.zsh: export the active theme's name (what `theme` last set) as
# CANOPY_THEME before every prompt, so a grofe or tmux client started from this
# shell carries it. canopy on a remote box reads it on attach and runs
# `theme <name>` there, so the box follows the laptop's theme.
#
# An ssh login that arrived with CANOPY_THEME already set got it from the
# laptop (SendEnv), so it is kept: a tmux attach from this shell then carries
# the laptop's theme, not this machine's.
if [[ -z ${SSH_CONNECTION-} || -z ${CANOPY_THEME-} ]]; then
  _athome_canopy_theme() {
    if [[ -r ~/.config/themes/active ]]; then
      export CANOPY_THEME="$(<~/.config/themes/active)"
    fi
  }
  autoload -Uz add-zsh-hook
  add-zsh-hook precmd _athome_canopy_theme
fi
