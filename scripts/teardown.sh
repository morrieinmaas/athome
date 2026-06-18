#!/usr/bin/env bash
# teardown.sh — cleanly undo athome on THIS machine, for a from-scratch retry.
#
# Tiers (cumulative — each includes the ones above it):
#   --state      chezmoi persistent state + cache only. Nothing is deleted from
#                $HOME; this just makes every run_once_ script run again and the
#                config re-prompt. The lightest "let me re-test bootstrap" reset.
#   --dotfiles   --state + remove every chezmoi-managed FILE from $HOME and the
#                generated chezmoi config (~/.config/chezmoi). Leaves a bare
#                shell. The source repo (this checkout) is preserved so you can
#                re-bootstrap.
#   --all        --dotfiles + uninstall the tool layers: mise tools, nanobrew
#                (/opt/nanobrew, needs sudo), rbw/Bitwarden local data, and this
#                machine's SSH key (it's backed up in Bitwarden per the design).
#
# DRY-RUN IS THE DEFAULT. With only a tier flag, it prints exactly what it WOULD
# do and changes nothing. Add --execute to actually do it; destructive tiers then
# require typing the tier name to confirm (skip with --yes).
#
#   ./scripts/teardown.sh --state                 # preview (dry-run)
#   ./scripts/teardown.sh --dotfiles --execute    # really remove dotfiles
#   ./scripts/teardown.sh --all --execute --yes   # full wipe, no prompt
#
# SAFER ALTERNATIVE: rather than wiping your working machine, test a fresh
# bootstrap in a NEW macOS user account (System Settings → Users) or a VM. This
# script cannot un-delete anything.
set -euo pipefail

if [[ -t 1 ]]; then
  c_red=$'\033[31m'; c_grn=$'\033[32m'; c_ylw=$'\033[33m'; c_dim=$'\033[2m'; c_rst=$'\033[0m'
else
  c_red=''; c_grn=''; c_ylw=''; c_dim=''; c_rst=''
fi

DRY_RUN=1
ASSUME_YES=0
TIER=""

usage() {
  sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'
}

die() { printf '%sError:%s %s\n' "$c_red" "$c_rst" "$*" >&2; exit 2; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --state)    TIER="state" ;;
    --dotfiles) TIER="dotfiles" ;;
    --all)      TIER="all" ;;
    --execute|--run) DRY_RUN=0 ;;
    --dry-run)  DRY_RUN=1 ;;
    --yes|-y)   ASSUME_YES=1 ;;
    -h|--help)  usage; exit 0 ;;
    *)          die "unknown argument: $1  (try --help)" ;;
  esac
  shift
done

[[ -n "$TIER" ]] || { usage; echo; die "pick a tier: --state | --dotfiles | --all"; }

# run CMD... — execute, or just print in dry-run. Simple commands only (no shell
# operators); callers that need a pipe/loop do their own dry-run guarding.
run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    printf '  %swould:%s %s\n' "$c_dim" "$c_rst" "$*"
  else
    printf '  %s+%s %s\n' "$c_grn" "$c_rst" "$*"
    "$@"
  fi
}

section() { printf '\n%s== %s ==%s\n' "$c_ylw" "$1" "$c_rst"; }

confirm() {
  # Gate destructive execution behind typing the tier name.
  [[ "$DRY_RUN" -eq 1 ]] && return 0
  [[ "$ASSUME_YES" -eq 1 ]] && return 0
  printf '\n%sThis will PERMANENTLY remove the above for tier "%s".%s\n' "$c_red" "$TIER" "$c_rst"
  printf 'Type %s%s%s to proceed (anything else aborts): ' "$c_red" "$TIER" "$c_rst"
  local answer; read -r answer
  [[ "$answer" == "$TIER" ]] || die "aborted — nothing was changed"
}

tier_state() {
  section "chezmoi persistent state + cache"
  run chezmoi state reset
  run rm -rf "$HOME/.cache/chezmoi"
}

tier_dotfiles() {
  tier_state
  section "chezmoi-managed files in \$HOME"
  # List is read-only, so it's safe to compute even in dry-run.
  local f count=0
  while IFS= read -r f; do
    [[ -n "$f" && -f "$f" ]] || continue
    run rm -f "$f"
    count=$((count + 1))
  done < <(chezmoi managed --path-style=absolute --include=files 2>/dev/null)
  printf '  %s(%d managed files)%s\n' "$c_dim" "$count" "$c_rst"
  section "generated chezmoi config"
  run rm -rf "$HOME/.config/chezmoi"
  local src; src="$(chezmoi source-path 2>/dev/null || true)"
  printf '  %skept: source repo %s (needed to re-bootstrap)%s\n' "$c_dim" "${src:-?}" "$c_rst"
}

tier_all() {
  tier_dotfiles
  section "mise (tools + config)"
  run rm -rf "$HOME/.local/share/mise" "$HOME/.config/mise" "$HOME/.local/bin/mise"
  section "nanobrew casks (GUI apps in /Applications)"
  # Uninstall casks first — that removes the .app bundles. Nuking the prefix
  # below would leave them behind, so a re-bootstrap hits "refusing to overwrite
  # existing app". List is read-only, safe in dry-run.
  if command -v nb >/dev/null 2>&1; then
    local cask
    while IFS= read -r cask; do
      [[ -n "$cask" ]] && run nb uninstall "$cask"
    done < <(nb list 2>/dev/null | awk '/\(cask\)/{print $1}')
  fi
  section "nanobrew prefix (needs sudo)"
  run sudo rm -rf /opt/nanobrew
  section "rbw / Bitwarden local data"
  if command -v rbw >/dev/null 2>&1; then run rbw stop-agent || true; fi
  run rm -rf "$HOME/.config/rbw" "$HOME/.local/share/rbw" "$HOME/.cache/rbw"
  section "per-machine SSH key (backed up in Bitwarden)"
  local host; host="$(uname -n)"
  run rm -f "$HOME/.ssh/${host}_ed25519" "$HOME/.ssh/${host}_ed25519.pub"
  printf '  %snote: ~/personal, ~/sidebiz etc. are LEFT ALONE (may hold your repos)%s\n' "$c_dim" "$c_rst"
}

mode="$([[ "$DRY_RUN" -eq 1 ]] && echo "DRY-RUN (no changes)" || echo "EXECUTE")"
printf '%sathome teardown%s — tier=%s  mode=%s\n' "$c_ylw" "$c_rst" "$TIER" "$mode"

# In execute mode, preview first (dry-run pass), then confirm, then do it.
if [[ "$DRY_RUN" -eq 0 ]]; then
  DRY_RUN=1; "tier_${TIER}"; DRY_RUN=0
  confirm
fi
"tier_${TIER}"

if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '\n%sDry run only — nothing changed.%s Re-run with --execute to apply.\n' "$c_dim" "$c_rst"
else
  printf '\n%s✓ teardown (%s) complete.%s Re-run ./scripts/bootstrap.sh for a fresh start.\n' "$c_grn" "$TIER" "$c_rst"
fi
