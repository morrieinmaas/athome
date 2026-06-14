#!/usr/bin/env bash
# Non-interactive Bitwarden nudge. If rbw isn't set up on this machine, remind
# the user to run `bw-setup`. NEVER prompts — safe inside `chezmoi apply`.
#
# Suppressed during bootstrap (ATHOME_BOOTSTRAP=1), which runs bw-setup itself
# in the TTY. A plain `chezmoi apply` (e.g. onboarding a machine without
# bootstrap.sh) has no ATHOME_BOOTSTRAP, so it DOES get this one-time nudge.
set -uo pipefail

[[ -n "${ATHOME_BOOTSTRAP:-}" ]] && exit 0

# rbw may be in nanobrew's prefix (macOS); make it findable without activating mise.
export PATH="/opt/nanobrew/prefix/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
command -v rbw >/dev/null 2>&1 || exit 0

# `rbw unlocked` is read-only and does not start the agent or prompt.
if ! rbw unlocked >/dev/null 2>&1; then
  echo "==> Bitwarden (rbw) isn't set up on this machine yet."
  echo "    Run \`bw-setup\` to log in (register once on official cloud, then unlock)."
  echo "    Details: docs/secrets.md"
  if [[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]]; then
    printf '  • %s\n' "Bitwarden: run \`bw-setup\` to access secrets. See docs/secrets.md." >> "$ATHOME_FOLLOWUP_LOG"
  fi
fi
