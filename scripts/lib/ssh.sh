# shellcheck shell=bash
# Shared SSH helper — `source` me.
#
# seed_github_ssh_config <hostkey-basename>: append a minimal github.com Host
# block to ~/.ssh/config that pins id_ed25519 (preferred) then this host's
# per-host key — IF our marker isn't already present. The full chezmoi-managed
# ~/.ssh/config replaces it idempotently on the next apply; this is just the
# pre-deploy safety net so the HTTPS→SSH remote flip can't deadlock.
#
# MIRRORS home/.chezmoiscripts/run_before_00-seed-ssh-aliases.sh.tmpl, which runs
# in chezmoi's apply context and therefore CANNOT source this file. KEEP THE TWO
# IN SYNC (the dual-IdentityFile block must match).
seed_github_ssh_config() {
  local hostkey="$1"
  mkdir -p "$HOME/.ssh"; chmod 700 "$HOME/.ssh"
  touch "$HOME/.ssh/config"; chmod 600 "$HOME/.ssh/config"
  grep -q "$hostkey" "$HOME/.ssh/config" 2>/dev/null && return 0
  cat >> "$HOME/.ssh/config" <<SSHSEED

# === seeded by scripts/lib/ssh.sh — the chezmoi-managed ~/.ssh/config replaces this ===
Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519
    IdentityFile ~/.ssh/$hostkey
SSHSEED
}
