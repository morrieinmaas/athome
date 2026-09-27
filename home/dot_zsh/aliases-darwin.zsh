# ~/.zsh/aliases-darwin.zsh — managed by chezmoi
# Sourced from ~/.zshrc only when $OSTYPE says darwin, right after
# aliases.zsh, so anything here wins over the common set.
#
# What belongs here: an alias whose MEANING is macOS, not merely one whose
# tool happens to be installed on macOS. A cross-platform tool behind a
# `command -v` guard belongs in aliases.zsh, which is where most of them are.

# ─── Package management ──────────────────────────────────────────────────────
# nanobrew is the canonical package manager on macOS and `nb` is its command,
# so muscle-memory `brew` resolves to it. Interactive only: an alias does not
# fire in a script, and scripts in this repo call `nb` directly.
#
# Legacy Homebrew is still installed at /opt/homebrew with everything that
# predates the cutover, and `command brew` still reaches it when you need it.
command -v nb >/dev/null && alias brew='nb'

# ─── Clipboard ───────────────────────────────────────────────────────────────
# Nothing to do here: pbcopy and pbpaste are native. The Linux file aliases
# them onto wl-copy and wl-paste so the same muscle memory works there, and
# this comment exists so that asymmetry does not read as an omission.
