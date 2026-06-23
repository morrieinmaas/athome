# ~/.zsh/functions.zsh — managed by chezmoi
# Sourced from ~/.zshrc after aliases.zsh.

# superfile (spf): cd into the dir you quit from. With cd_on_quit=true in the
# config, ANY quit (q or Q) writes the last dir to a state file — as a `cd '…'`
# shell command (superfile single-quote-escapes the path), so we SOURCE it rather
# than read it as a path. -c points at the chezmoi-managed config (superfile
# defaults to ~/Library/… on macOS). The state-file path is OS-specific → ask spf.
spf() {
  # Sync superfile's theme to the active `theme` pick + OS dark/light first
  # (superfile reads its theme fresh per launch; the generated 'athome' theme
  # tracks ghostty/nvim — see ~/.config/themes/apply-superfile.sh).
  [ -x "$HOME/.config/themes/apply-superfile.sh" ] && "$HOME/.config/themes/apply-superfile.sh" 2>/dev/null
  command spf -c "$HOME/.config/superfile/config.toml" "$@"
  local ldfile
  ldfile="$(command spf path-list --lastdir-file 2>/dev/null)"
  if [[ -n "$ldfile" && -r "$ldfile" ]]; then
    builtin source "$ldfile"          # contains: cd '/path/you/quit/from'
    command rm -f -- "$ldfile"
  fi
}
alias y='spf'   # muscle memory from the old yazi y() wrapper

# lazydocker talks to the podman socket directly (it doesn't go through the
# docker→podman shim), so lazy-start the podman VM first on macOS. podman-up is a
# fast no-op when the socket is already up, and a no-op entirely on Linux.
lazydocker() {
  command -v podman-up >/dev/null 2>&1 && podman-up
  command lazydocker "$@"
}

# mkcd: make a dir and cd into it
mkcd() {
  mkdir -p "$1" && cd "$1"
}

# extract: dispatch any archive type to the right tool
extract() {
  if [[ -f "$1" ]]; then
    case "$1" in
      *.tar.bz2|*.tbz2) tar xjf "$1"   ;;
      *.tar.gz|*.tgz)   tar xzf "$1"   ;;
      *.tar.xz|*.txz)   tar xJf "$1"   ;;
      *.tar.zst)        tar --zstd -xf "$1" ;;
      *.tar)            tar xf "$1"    ;;
      *.bz2)            bunzip2 "$1"   ;;
      *.gz)             gunzip "$1"    ;;
      *.zip)            unzip "$1"     ;;
      *.Z)              uncompress "$1";;
      *.7z)             7z x "$1"      ;;
      *.rar)            unrar x "$1"   ;;
      *.zst)            unzstd "$1"    ;;
      *)                echo "extract: unknown archive '$1'" >&2; return 1 ;;
    esac
  else
    echo "extract: '$1' is not a file" >&2
    return 1
  fi
}

# pyproject: scaffold a new uv-managed Python project in cwd
pyproject() {
  local name="${1:-${PWD##*/}}"
  local pyver="${2:-3.13}"
  uv init --name "$name" --python "$pyver"
  uv add --dev pytest ruff ty
  echo "$pyver" > .python-version
  echo "==> uv project '$name' ready (python $pyver)"
}

# repo: cd to a project — exits at the first existing match
# Searches the per-context roots only; ~/Code is gone (see commit notes
# in dot_gitconfig.tmpl). Personal → ~/personal, work → ~/work, sidebiz
# → ~/sidebiz. Single tree per context.
repo() {
  for base in ~/personal ~/work ~/sidebiz; do
    if [[ -d "$base/$1" ]]; then
      cd "$base/$1"; return
    fi
    # Fuzzy: find dir matching the name 2 levels deep
    local match
    match="$(fd --type d --max-depth 2 "^${1}$" "$base" 2>/dev/null | head -1)"
    if [[ -n "$match" ]]; then
      cd "$match"; return
    fi
  done
  echo "repo: no project '$1' under ~/personal, ~/work, ~/sidebiz" >&2
  return 1
}

# weather: quick wttr.in lookup (default = Amsterdam)
weather() {
  curl -s "wttr.in/${1:-Amsterdam}?format=v2"
}

# ff: fuzzy-find file under cwd with bat preview, open the pick in nvim
ff() {
  local file
  file="$(fd --type f --hidden --follow --exclude .git \
    | fzf --reverse --height=80% --preview='bat --color=always --style=numbers --line-range=:300 {}')"
  [[ -n "$file" ]] && command nvim "$file"
}

# fcd: same but cd to a directory
fcd() {
  local dir
  dir="$(fd --type d --hidden --follow --exclude .git | fzf --reverse --preview='eza -la --git --icons {}')"
  [[ -n "$dir" ]] && builtin cd -- "$dir"
}

# rg-fzf: ripgrep | fzf, open match at line in nvim
rgf() {
  local file line
  IFS=: read -r file line _ < <(rg --line-number --column --no-heading --smart-case "${1:-.}" \
    | fzf --reverse --delimiter=: --preview='bat --color=always --highlight-line={2} --style=numbers {1}' \
          --preview-window='right,60%,+{2}/2')
  [[ -n "$file" && -n "$line" ]] && command nvim "+${line}" "$file"
}

# ── tmux cockpit: on a fresh interactive shell with NO tmux sessions, offer
# ONCE EVER to bootstrap the 4 context sessions. Two guards keep it from nagging:
#   1. If the context dirs already exist (~/work, ~/personal, ~/sidebiz), the
#      machine is already bootstrapped — there's no point asking, so stay quiet.
#   2. A persistent marker in ~/.local/state (survives reboot, unlike $TMPDIR)
#      means a "no" answer is remembered forever, not just for this boot.
# Re-offer with `rm ${XDG_STATE_HOME:-~/.local/state}/athome/cockpit-asked`.
# Always available manually: the `cockpit` command or tmux `prefix B`.
cockpit_offer() {
  [[ -o interactive ]] || return
  [[ -n "$TMUX" ]] && return
  command -v tmux cockpit >/dev/null 2>&1 || return
  tmux ls >/dev/null 2>&1 && return        # sessions exist → quiet
  # Already bootstrapped (context dirs present) → nothing to offer.
  [[ -d "$HOME/work" || -d "$HOME/personal" || -d "$HOME/sidebiz" ]] && return
  local marker="${XDG_STATE_HOME:-$HOME/.local/state}/athome/cockpit-asked"
  [[ -f "$marker" ]] && return             # already offered (ever) → quiet
  mkdir -p "${marker:h}" 2>/dev/null
  : > "$marker" 2>/dev/null                 # remember, so we never re-ask
  printf "bootstrap cockpit (work/personal/sidebiz/misc)? [y/N] "
  local ans; read -r ans
  [[ "$ans" == [yY]* ]] && { cockpit; tmux attach -t work; }
}
cockpit_offer
