#!/usr/bin/env bash
# The agentic skills/instructions live canonically (agent-agnostic) in
# ~/.config/agents — cloned by .chezmoiexternal. opencode reads them there
# directly. Claude Code still expects them under ~/.claude, so symlink the
# SHARED items across; Claude's own runtime (settings.json, projects/,
# sessions/, history.jsonl) is left untouched in ~/.claude.
#
# Idempotent: re-running just refreshes the symlinks. If an old real copy of an
# item is sitting in ~/.claude (e.g. from when the external deployed there
# directly), it's the same synced content now living in ~/.config/agents, so we
# replace it with a symlink.
set -euo pipefail

src="$HOME/.config/agents"
[ -d "$src" ] || exit 0   # agentsRepo empty / external skipped → nothing to link

mkdir -p "$HOME/.claude"
linked=()
for item in CLAUDE.md AGENTS.md AGENTIC-SYSTEMS.md RTK.md skills agents; do
  [ -e "$src/$item" ] || continue
  target="$HOME/.claude/$item"
  # A real (non-symlink) file/dir in the way is the old synced copy → drop it.
  if [ -e "$target" ] && [ ! -L "$target" ]; then rm -rf "$target"; fi
  ln -sfn "$src/$item" "$target"
  linked+=("$item")
done

[ ${#linked[@]} -gt 0 ] && \
  echo "==> linked ~/.config/agents/{${linked[*]// /,}} → ~/.claude (Claude Code compat)"
