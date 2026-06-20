#!/usr/bin/env bash
# prefix G launcher — open ENHANCE (gh-enhance, the GitHub Actions TUI) for the
# CURRENT branch's PR.
#
# gh-enhance REQUIRES a PR/run target; a bare `gh-enhance` prints "No PR passed"
# and exits immediately, so with display-popup's -E the popup snapped shut before
# you saw anything. Here we resolve the open PR for the current repo+branch first
# (the popup is opened with -d "#{pane_current_path}", so we're already in it),
# and only fall back to a readable hint when there's nothing to show.
#
# PATH is set explicitly: display-popup runs in the tmux *server* environment,
# which never sources ~/.zshenv, so the mise shims dir isn't on PATH there.
set -uo pipefail
export PATH="$HOME/.local/share/mise/shims:/opt/homebrew/bin:/opt/nanobrew/prefix/bin:$HOME/.local/bin:/usr/bin:/bin:$PATH"

num="$(gh pr view --json number -q .number 2>/dev/null || true)"
if [ -n "$num" ]; then
  exec gh-enhance "$num"
fi

branch="$(git branch --show-current 2>/dev/null || echo '(not a git repo)')"
printf '\n  ENHANCE: no open PR for the current branch (%s).\n' "$branch"
printf '  Enter a PR #/URL or a run URL (empty = cancel): '
read -r r
[ -n "$r" ] && exec gh-enhance "$r"
