# ~/.zsh/aliases.zsh — managed by chezmoi
# Sourced from ~/.zshrc after exports.zsh.
#
# All `command -v X >/dev/null && alias …` guards mean an alias only
# activates if the underlying tool is installed. Safe to source on any
# machine, no broken-alias errors.

# ─── Rust-tool replacements (identical behaviour macOS + Linux) ──────────────
command -v bat    >/dev/null && alias cat='bat --paging=never'
command -v eza    >/dev/null && {
  alias ls='eza'
  alias ll='eza -la --git --icons --group-directories-first'
  alias la='eza -la --git --icons'
  alias lt='eza --tree --level=2 --icons'
  alias tree='eza --tree --icons'
}
command -v dust   >/dev/null && alias du='dust'
command -v duf    >/dev/null && alias df='duf'
command -v procs  >/dev/null && alias ps='procs'
command -v sd     >/dev/null && alias sed='sd'
command -v rg     >/dev/null && alias grep='rg'
command -v fd     >/dev/null && alias find='fd'
command -v doggo  >/dev/null && alias dig='doggo'
command -v xh     >/dev/null && alias curl='xh'
command -v btm    >/dev/null && alias top='btm'
command -v viddy  >/dev/null && alias watch='viddy'
command -v choose >/dev/null && alias cut='choose'
command -v tldr   >/dev/null && alias man='tldr'   # `command man` still hits real man
# macOS: nanobrew is the canonical PM — muscle-memory `brew` resolves to `nb`.
# Interactive only; scripts call `nb` directly (an alias won't fire in scripts).
command -v nb     >/dev/null && alias brew='nb'

# ─── Python via uv (NOT mise) ────────────────────────────────────────────────
# Per CLAUDE.md global rule: never bare `python` / `python3` — always uv.
if command -v uv >/dev/null 2>&1; then
  alias py='uv run python'
  alias ipy='uv run ipython'
  alias pip='uv pip'
  alias venv='uv venv'
  alias pyx='uv tool run'                  # one-shot run of a tool, no install
  alias pyinstall='uv tool install'        # persistent tool install
  alias pyup='uv tool upgrade --all'
  alias pyver='uv python list'
fi

# ─── Git shortcuts (extends zinit's OMZ::git plugin) ─────────────────────────
alias gst='git status -sb'
alias gco='git checkout'
alias gsw='git switch'
alias gbr='git branch'
alias gcm='git commit -m'
alias gca='git commit --amend --no-edit'
alias gp='git push'
alias gpl='git pull --rebase --autostash'
alias gd='git diff'
alias gds='git diff --staged'
alias glg="git log --graph --pretty=format:'%C(yellow)%h%Creset %C(cyan)%an%Creset %s %C(green)(%cr)%Creset %C(magenta)%d%Creset' --abbrev-commit"
alias gunstage='git reset HEAD --'
alias gplease='git push --force-with-lease'

# ─── chezmoi shortcuts ───────────────────────────────────────────────────────
alias cz='chezmoi'
alias czu='chezmoi update'           # git pull + apply in one shot (the daily sync)
alias cza='chezmoi apply'            # NO -v: a paged diff would abort the apply if you quit the pager
alias czd='chezmoi diff'             # preview changes (paging fine here — nothing to abort)
alias cze='chezmoi edit'
alias czs='chezmoi status'
alias czc='chezmoi cd'
alias czdoc='chezmoi doctor'

# ─── Container shortcuts (podman as docker) ──────────────────────────────────
if command -v podman >/dev/null 2>&1; then
  alias docker='podman'
  alias dc='podman compose'
  alias dps='podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
fi

# ─── Just / Task ─────────────────────────────────────────────────────────────
alias j='just'
alias t='task'

# ─── Infra / IaC ─────────────────────────────────────────────────────────────
command -v tofu >/dev/null && alias tf='tofu'

# ─── Editor shortcuts ────────────────────────────────────────────────────────
alias v='nvim'
alias vim='nvim'

# ─── tmux daily-driver shortcuts ────────────────────────────────────────────
alias ta='tmux attach || tmux new-session'
alias ts='tmux new-session -A -s'   # named session, attach-or-create
alias tk='tmux kill-server'
alias tl='tmux list-sessions'

# ─── Ghostty ─────────────────────────────────────────────────────────────────
# Browse every built-in theme with live preview (read-only — pick a name, set
# `theme = …` in ~/.config/ghostty/config, reload with ⌘⇧, / Ctrl+Shift+,).
command -v ghostty >/dev/null && alias gtheme='ghostty +list-themes --preview'

# ─── jujutsu (jj) shortcuts ──────────────────────────────────────────────────
if command -v jj >/dev/null 2>&1; then
  alias jjl='jj log --limit 10'
  alias jjs='jj status'
  alias jjd='jj diff'
  alias jjn='jj new'
  alias jjc='jj commit -m'
fi

# ─── Quick navigation ────────────────────────────────────────────────────────
alias ..='cd ..'
alias ...='cd ../..'
alias ....='cd ../../..'
alias ~='cd ~'
alias pers='cd ~/personal'
alias work='cd ~/work'
alias side='cd ~/sidebiz'

# ─── Safety nets ─────────────────────────────────────────────────────────────
# No `rm` alias on purpose: -i/-I trains reflexive "y", fights -f, and the
# GNU/BSD --preserve-root split made it break at runtime. Use `trash` for
# recoverable deletes instead of shadowing rm.
alias mv='mv -i'
alias cp='cp -i'

# ─── ANSI / colors helpers ───────────────────────────────────────────────────
# `paths` not `path`: zsh's `path` is the special array tied to $PATH (used in
# path.zsh as `path=(...)`); aliasing the bare word `path` shadows it.
alias paths='print -l $path'
alias reload='exec zsh'
