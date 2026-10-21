#!/usr/bin/env bash
# nanobrew installs Homebrew bottles into its own Cellar, but the git bottle has
# Homebrew's prefix compiled in: `git --exec-path` reports
# /opt/homebrew/opt/git/libexec/git-core, which does not exist on a machine whose git
# came from nanobrew. git then finds ZERO of its 181 helpers, and every subcommand that
# dispatches through the exec path dies with "git: 'submodule' is not a git command".
#
# Measured 2026-10-01, before the fix:
#   git --exec-path   -> /opt/homebrew/opt/git/libexec/git-core   (absent)
#   helpers found     -> 0        (Apple's git: 170)
#   git submodule     -> not a git command
#   git init          -> warning: templates not found in /opt/homebrew/opt/git/share/...
#
# Two real consequences: `git submodule` was broken outright, which matters because
# infocolab carries 8 submodules, and `uv tool install git+<url>` failed for every
# repository, because uv's fetcher runs `git submodule update --recursive --init`
# unconditionally.
#
# The fix points the compiled-in path at nanobrew's own opt/ symlink rather than at a
# version directory, because nanobrew re-points opt/git on upgrade the same way Homebrew
# does. So this survives `nb upgrade git` instead of pinning 2.55.0.
#
# Idempotent: re-running just refreshes the symlink.
set -euo pipefail

[ "$(uname -s)" = Darwin ] || exit 0

nb_opt="/opt/nanobrew/prefix/opt/git"
hb_opt="/opt/homebrew/opt/git"

# No nanobrew git on this machine, or a layout we do not recognise: nothing to do.
[ -d "$nb_opt/libexec/git-core" ] || exit 0

# A real directory here means Homebrew's own git owns the path. Never clobber that.
if [ -e "$hb_opt" ] && [ ! -L "$hb_opt" ]; then
  echo "==> $hb_opt is a real directory (Homebrew git present?); leaving it alone"
  exit 0
fi

mkdir -p "$(dirname "$hb_opt")"
ln -sfn "$nb_opt" "$hb_opt"

# Verify against the thing that was actually broken, not just that the link exists.
if [ -x "$hb_opt/libexec/git-core/git-submodule" ]; then
  echo "==> linked $hb_opt -> $nb_opt (git exec-path resolves; git submodule works again)"
else
  echo "  ! $hb_opt linked but git-submodule is still unresolved; check nanobrew's git" >&2
  exit 1
fi
