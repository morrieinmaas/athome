#!/usr/bin/env bash
# Non-interactive nudge to restore the encrypted secrets vault. If rbw is set up
# but ~/.secrets env files haven't been restored yet, remind the user to run
# `secrets-restore`. NEVER prompts — safe inside `chezmoi apply`.
#
# Suppressed during bootstrap (ATHOME_BOOTSTRAP=1), which offers secrets-restore
# itself in the TTY (bootstrap step 7.6). A plain `chezmoi apply` gets this nudge.
set -uo pipefail

[[ -n "${ATHOME_BOOTSTRAP:-}" ]] && exit 0

export PATH="/opt/nanobrew/prefix/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
command -v rbw >/dev/null 2>&1 || exit 0
command -v secrets-restore >/dev/null 2>&1 || exit 0

# Only nudge once rbw is usable (unlocked) and the vault hasn't been restored yet.
rbw unlocked >/dev/null 2>&1 || exit 0
# Heuristic "already restored?": any per-project secrets env file exists.
compgen -G "$HOME/.secrets/*/.env" >/dev/null 2>&1 && exit 0

echo "==> Encrypted secrets vault not restored on this machine yet."
echo "    Run \`secrets-restore\` to pull + decrypt your ~/.secrets env files."
echo "    Details: docs/secrets.md"
if [[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]]; then
  printf '  • %s\n' "Secrets: run \`secrets-restore\` to recover ~/.secrets env files. See docs/secrets.md." >> "$ATHOME_FOLLOWUP_LOG"
fi
