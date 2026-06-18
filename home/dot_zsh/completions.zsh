# ~/.zsh/completions.zsh — managed by chezmoi
# Sourced from ~/.zshrc after functions.zsh.
#
# Per-tool completion setup. Each block is guarded so the file is safe
# on machines where the tool isn't installed.

# ─── Tool integrations that also install completions ────────────────────────
command -v zoxide  >/dev/null && eval "$(zoxide init zsh)"
command -v mise    >/dev/null && eval "$(mise activate zsh)"
command -v direnv  >/dev/null && eval "$(direnv hook zsh)"

# fzf — owns Ctrl-R (fuzzy history), Ctrl-T (fuzzy file), Alt-C (fuzzy cd)
command -v fzf     >/dev/null && source <(fzf --zsh)

# ─── Explicit completions (tools that don't auto-register) ──────────────────
command -v uv       >/dev/null && eval "$(uv generate-shell-completion zsh 2>/dev/null)"
command -v uvx      >/dev/null && eval "$(uvx --generate-shell-completion zsh 2>/dev/null)"
command -v chezmoi  >/dev/null && eval "$(chezmoi completion zsh)"
# `mise activate` (above) does NOT register completions — source them explicitly
# so `mise run <tab>`, `mise use <tab>` etc. complete. Task-name completion needs
# the `usage` CLI (installed via mise config); without it the rest still works.
command -v mise     >/dev/null && eval "$(mise completion zsh)"
command -v gh       >/dev/null && eval "$(gh completion -s zsh)"
command -v just     >/dev/null && eval "$(just --completions zsh 2>/dev/null)"
command -v task     >/dev/null && eval "$(task --completion zsh 2>/dev/null)"
command -v scw      >/dev/null && eval "$(scw autocomplete script shell=zsh 2>/dev/null)"   # Scaleway CLI

# compinit moved to dot_zshrc.tmpl (runs BEFORE zinit plugins so they
# don't hit "compdef: command not found"). zinit cdreplay there replays
# any deferred compdef calls. This file just needs to source completion
# data from binaries via their `<cmd> completion zsh` eval — which is
# safe to do post-compinit because compinit is already done by here.
