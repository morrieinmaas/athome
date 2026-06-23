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

# ─── Tool integrations (define functions / hooks / keybindings) ──────────────
_zcache zoxide    zoxide init zsh        # z / zi + cd hook
_zcache mise-act  mise activate zsh      # per-dir tool/env hook
_zcache direnv    direnv hook zsh        # per-dir .envrc
_zcache fzf       fzf --zsh              # Ctrl-R / Ctrl-T / Alt-C + completion

# ─── Explicit completions (tools that don't auto-register) ───────────────────
_zcache uv        uv generate-shell-completion zsh
_zcache uvx       uvx --generate-shell-completion zsh
_zcache chezmoi   chezmoi completion zsh
# `mise activate` does NOT register completions — cache them so `mise run <tab>`
# etc. complete (task-name completion also needs the `usage` CLI, via mise config).
_zcache mise-comp mise completion zsh
_zcache gh        gh completion -s zsh
_zcache just      just --completions zsh
_zcache task      task --completion zsh
_zcache scw       scw autocomplete script shell=zsh   # Scaleway CLI

# compinit runs in dot_zshrc.tmpl (before zinit plugins); cdreplay there replays
# deferred compdef calls. The cached files above register via compdef, which is
# safe post-compinit.
