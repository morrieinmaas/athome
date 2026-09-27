# ~/.zsh/aliases-linux.zsh — managed by chezmoi
# Sourced from ~/.zshrc only when $OSTYPE says linux, right after aliases.zsh,
# so anything here wins over the common set.
#
# Almost everything about this setup is deliberately identical on both
# systems: the Rust CLI replacements, git, chezmoi, tmux and Python aliases all
# live in aliases.zsh and behave the same. What is left here is the small set
# of names that exist natively on macOS and have a different spelling on Linux.
# Aliasing the macOS name to the Linux tool means one set of habits works on
# both machines rather than two.

# ─── Clipboard ───────────────────────────────────────────────────────────────
# pbcopy and pbpaste are macOS commands. On Linux under niri the clipboard is
# Wayland's, so wl-clipboard provides the same thing under other names.
#
# Guarded, because a Linux machine without a Wayland session has neither, and
# an alias to a missing command is worse than no alias: it reports failure from
# the wrong name.
if command -v wl-copy >/dev/null; then
  alias pbcopy='wl-copy'
  alias pbpaste='wl-paste'
elif command -v xclip >/dev/null; then
  # X11 fallback. -selection clipboard, because xclip's default is the primary
  # selection, which is the middle-click buffer and not what Ctrl+V pastes.
  alias pbcopy='xclip -selection clipboard'
  alias pbpaste='xclip -selection clipboard -o'
fi

# ─── Opening a file or URL in its desktop application ────────────────────────
# macOS has `open`; the freedesktop equivalent is xdg-open. Not aliased to the
# bare word `open` blindly: zsh has no builtin called open, but some scripts
# expect the macOS one, so this only defines it when xdg-open is actually
# present.
command -v xdg-open >/dev/null && alias open='xdg-open'
