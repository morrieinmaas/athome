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

# Use system coreutils for the destructive ops. This script removes nanobrew
# (/opt/nanobrew), which on macOS puts GNU `rm` first on PATH — deleting it
# mid-run would break every subsequent `rm` ("No such file or directory").
# /bin + /usr/bin hold the stable system rm on both macOS and Linux.
export PATH="/bin:/usr/bin:$PATH"

# Colour vars ($c_red $c_grn $c_ylw $c_dim $c_rst, TTY-gated) from the shared lib.
# shellcheck source=scripts/lib/colors.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib/colors.sh"

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
  # --force skips chezmoi's own "Remove …chezmoistate.boltdb?" prompt. teardown
  # already gates execution behind its typed confirmation, and a headless run
  # (the e2e / CI, no TTY) can't answer it — without --force it dies with
  # "chezmoi: could not open a new TTY".
  run chezmoi state reset --force
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
  section "chezmoi externals + app data (not 'managed files')"
  # zinit/TPM/agents are git-repo externals; nvim plugins + bat cache are
  # app-generated. None show in `chezmoi managed`, so they'd survive otherwise
  # and a re-bootstrap would reuse stale state. NOT ~/.claude — that's your real
  # Claude Code data, never athome's.
  run rm -rf "$HOME/.local/share/zinit" "$HOME/.config/tmux/plugins" \
             "$HOME/.config/agents" \
             "$HOME/.local/share/nvim" "$HOME/.local/state/nvim" \
             "$HOME/.cache/bat"
  # run_once_after_22 symlinks items from ~/.config/agents into ~/.claude
  # (CLAUDE.md, skills, …). Removing the external above leaves those dangling,
  # so prune EXACTLY those links — only symlinks under ~/.claude that point into
  # ~/.config/agents. Claude's real data (settings.json, projects/, …) is never
  # a link into agents, so it's untouched.
  section "stale ~/.claude → agents symlinks"
  local item link
  for item in CLAUDE.md AGENTS.md AGENTIC-SYSTEMS.md RTK.md skills agents; do
    link="$HOME/.claude/$item"
    [[ -L "$link" ]] || continue
    case "$(readlink "$link" 2>/dev/null)" in
      "$HOME/.config/agents/"*) run rm -f "$link" ;;
    esac
  done
  # Native package layer is OS-specific: macOS = nanobrew; Linux = pacman/yay
  # (system-wide — NOT auto-removed, since yanking system packages can break the
  # box). Only the macOS path touches /opt/nanobrew + casks.
  case "$(uname -s)" in
    Darwin)
      section "nanobrew casks (GUI apps in /Applications)"
      # Uninstall casks first — that removes the .app bundles. Nuking the prefix
      # below would leave them behind, so a re-bootstrap hits "refusing to
      # overwrite existing app". List is read-only, safe in dry-run.
      if command -v nb >/dev/null 2>&1; then
        local cask
        while IFS= read -r cask; do
          [[ -n "$cask" ]] && run nb uninstall "$cask"
        done < <(nb list 2>/dev/null | awk '/\(cask\)/{print $1}')
      fi
      section "nanobrew prefix (needs sudo)"
      run sudo rm -rf /opt/nanobrew
      ;;
    *)
      # Distro-aware hint (Arch: pacman/yay; Fedora: dnf). Either way the system
      # packages are LEFT IN PLACE — yanking them can break the OS.
      local pm_name pm_list pm_remove
      if command -v dnf >/dev/null 2>&1 && ! command -v pacman >/dev/null 2>&1; then
        pm_name="dnf"; pm_list="dnf repoquery --userinstalled"; pm_remove="sudo dnf remove <pkg>"
      else
        pm_name="pacman/yay"; pm_list="pacman -Qqe"; pm_remove="sudo pacman -Rns <pkg>"
      fi
      section "Linux system packages ($pm_name) — left in place"
      printf '  %snote: packages were installed system-wide via %s and are%s\n' "$c_dim" "$pm_name" "$c_rst"
      printf '  %sNOT auto-removed (could break the OS). Remove by hand if needed:%s\n' "$c_dim" "$c_rst"
      printf '  %s  %s   # list explicitly-installed, then  %s%s\n' "$c_dim" "$pm_list" "$pm_remove" "$c_rst"
      ;;
  esac
  section "rbw / Bitwarden local data"
  if command -v rbw >/dev/null 2>&1; then run rbw stop-agent || true; fi
  run rm -rf "$HOME/.config/rbw" "$HOME/.local/share/rbw" "$HOME/.cache/rbw"
  section "per-machine SSH key (backed up in Bitwarden)"
  local host; host="$(uname -n)"
  run rm -f "$HOME/.ssh/${host}_ed25519" "$HOME/.ssh/${host}_ed25519.pub"

  # ── USER DATA — NEVER removed by ANY tier ──────────────────────────────────
  # The teardown wipes the athome-managed *tool layer* only. Your own data is
  # deliberately out of scope and must STAY OUT of scope — re-bootstrap rebuilds
  # the tooling AROUND it. If you ever add a removal above, never let it reach:
  #   ~/.secrets/**            decrypted .env files restored from the vault
  #   ~/personal ~/sidebiz ~/work   your repos + project files
  section "user data — LEFT ALONE (never removed)"
  printf '  %skept: ~/.secrets (decrypted .env files from the secrets vault)%s\n' "$c_dim" "$c_rst"
  printf '  %skept: ~/personal, ~/sidebiz, ~/work (your repos + project files)%s\n' "$c_dim" "$c_rst"
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
