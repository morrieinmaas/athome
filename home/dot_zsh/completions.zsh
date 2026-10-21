# ~/.zsh/completions.zsh — managed by chezmoi
# Sourced from ~/.zshrc after functions.zsh.
#
# Tool integrations + completions, CACHED. Each `tool init/completion zsh` is a
# fork+exec that cost ~10–80ms every startup (≈300ms total); instead we generate
# each once to a file and `source` that. Caches rebuild when missing, when a mise
# tool changes (the installs dir bumps), or weekly — run `zcache-clear` to force.

# _zcache <name> <generator-cmd...> — cache a shell-init/completion and source it.
typeset -g _ZCACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/zsh-init"
typeset -g _ZCACHE_STAMP="${XDG_DATA_HOME:-$HOME/.local/share}/mise/installs"
_zcache() {
  local name="$1"; shift
  command -v "$1" >/dev/null 2>&1 || return 0        # tool absent → skip cleanly
  local f="$_ZCACHE_DIR/$name.zsh"
  # Regenerate if: missing/empty · a mise tool changed · or >7 days old.
  if [[ ! -s $f || $_ZCACHE_STAMP -nt $f || -n $f(#qN.md+7) ]]; then
    [[ -d $_ZCACHE_DIR ]] || mkdir -p $_ZCACHE_DIR
    "$@" >| $f 2>/dev/null
  fi
  [[ -s $f ]] && source $f
}
zcache-clear() { command rm -rf "$_ZCACHE_DIR"; print "zsh-init cache cleared — new shells rebuild it."; }

# ─── Completion styles (vendored from oh-my-zsh lib/completion.zsh) ──────────
# Kept when we purged OMZ: case-insensitive + partial-word matching, menu
# select, and caching. compinit already ran in .zshrc; these zstyles apply on
# the next completion. (Dropped OMZ's niche bits: the ignored-users list, the
# Solaris ps branch, and COMPLETION_WAITING_DOTS.)
zmodload -i zsh/complist
WORDCHARS=''
unsetopt menu_complete flowcontrol
setopt auto_menu complete_in_word always_to_end
bindkey -M menuselect '^o' accept-and-infer-next-history
zstyle ':completion:*:*:*:*:*' menu select
zstyle ':completion:*' matcher-list 'm:{[:lower:][:upper:]}={[:upper:][:lower:]}' 'r:|=*' 'l:|=* r:|=*'
zstyle ':completion:*' special-dirs true
zstyle ':completion:*' list-colors ''
zstyle ':completion:*:*:kill:*:processes' list-colors '=(#b) #([0-9]#) ([0-9a-z-]#)*=01;34=0=01'
zstyle ':completion:*:*:*:*:processes' command "ps -u $USERNAME -o pid,user,comm -w -w"
zstyle ':completion:*:cd:*' tag-order local-directories directory-stack path-directories
zstyle ':completion:*' use-cache yes
zstyle ':completion:*' cache-path "${XDG_CACHE_HOME:-$HOME/.cache}/zsh/zcompcache"
zstyle '*' single-ignored show
autoload -U +X bashcompinit && bashcompinit   # bash-style completion functions

# ─── Tool integrations (define functions / hooks / keybindings) ──────────────
_zcache zoxide    zoxide init zsh        # z / zi + cd hook
_zcache mise-act  mise activate zsh      # per-dir tool/env hook
_zcache direnv    direnv hook zsh        # per-dir .envrc
_zcache fzf       fzf --zsh              # Ctrl-R / Ctrl-T / Alt-C + completion
_zcache worktrunk wt config shell init zsh   # wt() — switch/cd hook for git worktrees
_zcache starship  starship init zsh          # prompt (replaced oh-my-zsh/dogenpunk)

# ─── Explicit completions (tools that don't auto-register) ───────────────────
# uv's completion is ~540KB — even cached, sourcing it costs ~28ms every startup.
# Register a stub instead and load the real thing on first `uv <tab>`.
if command -v uv >/dev/null 2>&1; then
  _uv() { unfunction _uv; _zcache uv uv generate-shell-completion zsh; _uv "$@"; }
  compdef _uv uv
fi
_zcache uvx       uvx --generate-shell-completion zsh
_zcache bun       bun completions          # mise-provided everywhere → not machine-local
_zcache op        op completion zsh        # 1Password CLI
_zcache chezmoi   chezmoi completion zsh
# `mise activate` does NOT register completions — cache them so `mise run <tab>`
# etc. complete (task-name completion also needs the `usage` CLI, via mise config).
_zcache mise-comp mise completion zsh
_zcache gh        gh completion -s zsh
# just: deliberately NOT cached. Since 1.58, `just --completions zsh` emits a clap DYNAMIC stub
# whose body is `source <(JUST_COMPLETE=zsh just)`. _zcache SOURCES its cache at startup, so that
# runs `just` right there, and any repo pinning an older just gets the wrong binary: 1.46 ignores
# JUST_COMPLETE and RUNS THE DEFAULT RECIPE. In ~/work/ErasmusAI/erasmusAI (mise.toml pins
# just 1.46.0) `zfresh` therefore executed `bash scripts/bootstrap-setup.sh`, printed its output
# through a process substitution, and died with `/dev/fd/16:1: =^[[0m not found` (zsh EQUALS
# expansion hitting an ANSI escape) plus SIGPIPE.
#
# Homebrew already ships the STATIC completion at
# /opt/homebrew/share/zsh/site-functions/_just (7550 bytes, no top-level source/eval), and that
# directory is on fpath, so dropping this line leaves `just <TAB>` working via normal autoload
# with nothing executed at startup. Re-add only if that file goes away AND just emits a static
# script again.
_zcache task      task --completion zsh
_zcache scw       scw autocomplete script shell=zsh   # Scaleway CLI
_zcache leaf      leaf --auto-complete zsh:dump       # markdown previewer (dump = stdout, no install side effect)

# compinit runs in dot_zshrc.tmpl (before zinit plugins); cdreplay there replays
# deferred compdef calls. The cached files above register via compdef, which is
# safe post-compinit.
