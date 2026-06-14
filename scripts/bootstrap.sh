#!/usr/bin/env bash
# Bootstrap a fresh machine: install chezmoi, generate any missing keys,
# pin to a tagged release, apply.
#
# Usage:
#
#   ~/.local/share/chezmoi/scripts/bootstrap.sh             # default: latest tag
#   ~/.local/share/chezmoi/scripts/bootstrap.sh --ref main  # bootstrap from tip
#   ~/.local/share/chezmoi/scripts/bootstrap.sh --ref v1.x  # explicit tag/branch
#
# Prerequisite: this script must live inside a cloned `athome` repo. If you
# haven't cloned yet:
#
#   gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
#
# Idempotent: re-run any time, skips what's already done.

set -euo pipefail

# ── helpers ─────────────────────────────────────────────────────────────────
c_red()    { printf '\033[31m%s\033[0m\n' "$*"; }
c_green()  { printf '\033[32m%s\033[0m\n' "$*"; }
c_blue()   { printf '\033[34m%s\033[0m\n' "$*"; }
c_yellow() { printf '\033[33m%s\033[0m\n' "$*"; }

confirm() {
  local prompt="$1" default="${2:-n}" answer
  if [[ "$default" == "y" ]]; then prompt="$prompt [Y/n] "; else prompt="$prompt [y/N] "; fi
  read -r -p "$prompt" answer
  answer="${answer:-$default}"
  [[ "$answer" =~ ^[Yy]$ ]]
}

# Short hostname without depending on the `hostname` command (which lives
# in `inetutils` — NOT a member of Arch's `base` meta-package, so a fresh
# `archinstall` install doesn't have it). Falls back through:
#   $HOSTNAME  → bash's auto-populated builtin (gethostname(2))
#   /etc/hostname
#   hostnamectl --static  (always present — comes with systemd)
#   "machine" as a last resort
short_hostname() {
  local h=""
  if [[ -n "${HOSTNAME:-}" ]]; then h="$HOSTNAME"
  elif [[ -r /etc/hostname ]];  then h="$(cat /etc/hostname)"
  elif command -v hostnamectl >/dev/null 2>&1; then h="$(hostnamectl --static)"
  fi
  h="${h%%.*}"
  printf '%s\n' "${h:-machine}"
}

# ── arg parsing ─────────────────────────────────────────────────────────────
REF=""
MACHINE="personal"
INCLUDE_AGENTS="true"   # set to "false" by --no-agents
IMPORT_FROM=""          # set to a backup-dir path by --import-from
IMPORT_SSH="false"      # set to "true" by --import-ssh (opt-in: SSH keys are usually per-machine)
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref)         REF="$2"; shift 2 ;;
    --machine)     MACHINE="$2"; shift 2 ;;
    --no-agents)   INCLUDE_AGENTS="false"; shift ;;   # skip ~/.claude skills sync
    --import-from) IMPORT_FROM="$2"; shift 2 ;;       # restore SSH keys from a prior key-backup dir (with --import-ssh)
    --import-ssh)  IMPORT_SSH="true"; shift ;;        # opt in to restoring SSH from --import-from (SSH is per-machine by default)
    -h|--help)
      sed -n 's/^# \{0,1\}//p' "$0" | head -30
      exit 0 ;;
    *) c_red "unknown arg: $1"; exit 2 ;;
  esac
done

case "$MACHINE" in
  personal|work) ;;
  *) c_red "--machine must be personal|work (got: $MACHINE)"; exit 2 ;;
esac
# personal covers everything that isn't 9-to-5 work — including sidehustle.
# Per-directory git identity routing handles sidehustle separately.

# Which identity keypairs to materialize on this machine, scoped by
# --machine to prevent the wrong-credential leak:
#   personal → personal + sidebiz   (no work key ever lands here)
#   work     → work only            (no personal/sidebiz on a corp box)
#
# We use this list for keygen, GitHub upload, and the SSH-config Host
# alias seeding. Single source of truth — change here once, everything
# downstream follows.
# One SSH key per machine (single GitHub account) does auth + commit signing —
# see the keygen section below. --machine still selects which context *dirs* get
# created (run_once_03); the per-tree git email routing is independent of keys.
c_blue "==> machine context: $MACHINE"

# ── 0. resolve the repo location from script path ───────────────────────────
# The chezmoi source dir lives at $repo_root/home (pointed at by .chezmoiroot).
# $repo_root is the git working tree root — where .git lives, where this script
# lives via scripts/, where infra/ lives, etc.
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
chezmoi_source="$repo_root/home"

if [[ ! -f "$repo_root/.chezmoiroot" ]] || [[ ! -d "$chezmoi_source/.chezmoiscripts" ]]; then
  c_red "Error: $repo_root doesn't look like the athome repo"
  c_red "(expected .chezmoiroot at repo root + home/.chezmoiscripts/ subdir)"
  c_red "Clone first, then run the script from inside the clone:"
  c_red ""
  c_red "    gh repo clone morrieinmaas/athome ~/.local/share/chezmoi"
  c_red "    ~/.local/share/chezmoi/scripts/bootstrap.sh"
  exit 1
fi

# Prune any leaked worktrees from previous hard-killed bootstrap runs.
# (EXIT trap normally removes them on Ctrl-C, but SIGKILL bypasses it.)
# Safe to run unconditionally — no-op if nothing to prune.
git -C "$repo_root" worktree prune 2>/dev/null || true

# ── 0.5 normalize to ~/.local/share/chezmoi if we're elsewhere ──────────────
expected="$HOME/.local/share/chezmoi"
if [[ "$repo_root" != "$expected" ]]; then
  c_yellow "==> repo at $repo_root, chezmoi expects $expected"
  if confirm "symlink it?" y; then
    if [[ -e "$expected" ]]; then
      c_red "$expected already exists — resolve manually"; exit 1
    fi
    mkdir -p "$(dirname "$expected")"
    ln -s "$repo_root" "$expected"
    c_green "✓ symlinked $expected → $repo_root"
  else
    c_yellow "continuing with source=$repo_root"
  fi
fi

# ── 0.6 resolve --ref. Default = latest tag. Never touches the user's
#       working tree: a git worktree materializes the ref in a temp dir
#       and chezmoi reads from there. Repo stays on whatever branch the
#       user cloned (typically main).
ORIGINAL_BRANCH="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD)"
if [[ -z "$REF" ]]; then
  git -C "$repo_root" fetch --tags --quiet 2>/dev/null || true
  LATEST_TAG="$(git -C "$repo_root" describe --tags --abbrev=0 2>/dev/null || true)"
  if [[ -n "$LATEST_TAG" ]]; then
    REF="$LATEST_TAG"
    c_blue "==> defaulting --ref to latest tag: $REF"
    c_yellow "    (pass --ref $ORIGINAL_BRANCH to bootstrap from current tip instead)"
  else
    REF="$ORIGINAL_BRANCH"
    c_blue "==> no tags found; bootstrapping from $REF"
  fi
fi

# If pinning to a different ref than what's checked out, build a throwaway
# worktree at that ref so chezmoi reads from there without disturbing the
# main checkout. trap cleans it up even on failure.
SOURCE_FOR_CHEZMOI="$chezmoi_source"
worktree=""
KEEP_ALIVE_PID=""

# Single consolidated EXIT cleanup: kill sudo keep-alive + remove temp worktree.
cleanup_on_exit() {
  local rc=$?
  [[ -n "$KEEP_ALIVE_PID" ]] && kill "$KEEP_ALIVE_PID" 2>/dev/null
  if [[ -n "$worktree" ]]; then
    git -C "$repo_root" worktree remove --force "$worktree" 2>/dev/null
    rm -rf "$worktree" 2>/dev/null
  fi
  exit $rc
}
trap cleanup_on_exit EXIT

if [[ "$REF" != "$ORIGINAL_BRANCH" ]]; then
  worktree="$(mktemp -d -t athome-bootstrap-XXXXXX)"
  rmdir "$worktree"   # git worktree add needs the dir NOT to exist yet
  c_blue "==> materializing $REF in worktree: $worktree (repo stays on $ORIGINAL_BRANCH)"
  git -C "$repo_root" worktree add --detach "$worktree" "$REF"
  # chezmoi source is still the home/ subdir inside the worktree (.chezmoiroot
  # gets checked out alongside everything else)
  SOURCE_FOR_CHEZMOI="$worktree/home"

  # Guard: the chosen ref must carry the chezmoi sources under home/ (per
  # .chezmoiroot). If $worktree/home is missing, pointing chezmoi at it would
  # silently render nothing — fail loudly instead.
  if [[ ! -d "$SOURCE_FOR_CHEZMOI" ]]; then
    c_red ""
    c_red "Error: $SOURCE_FOR_CHEZMOI doesn't exist at ref $REF"
    c_red "This ref has no chezmoi sources under home/. Bootstrap can't use it."
    c_red ""
    c_red "Fix one of:"
    c_red "  ./scripts/bootstrap.sh --ref main      # use current main"
    c_red "  git fetch --tags && ./scripts/bootstrap.sh    # let default pick latest tag"
    exit 1
  fi
fi

# ── 0.7 cache sudo credentials once, keep them fresh in background ──────────
# Bootstrap fires many sudo calls (pacman install, systemctl enable, /etc/...
# deploys, brew cask installs). Default sudo cache is 15 min and not shared
# across child processes — so the user gets repeatedly prompted. Validate
# once now, then a background loop runs `sudo -n true` every 60s until the
# script exits (cleanup_on_exit kills the loop).
c_blue "==> caching sudo credentials (one prompt, kept warm for the whole bootstrap)"
if sudo -v; then
  (while true; do sudo -n true 2>/dev/null; sleep 60; kill -0 "$$" 2>/dev/null || exit; done) &
  KEEP_ALIVE_PID=$!
else
  c_yellow "sudo refused — package install + systemd-enable steps may prompt repeatedly"
fi

# ── 1. chezmoi installed? ───────────────────────────────────────────────────
if ! command -v chezmoi >/dev/null 2>&1; then
  c_blue "==> installing chezmoi"
  mkdir -p "$HOME/.local/bin"
  sh -c "$(curl -fsLS https://get.chezmoi.io)" -- -b "$HOME/.local/bin"
  export PATH="$HOME/.local/bin:$PATH"
fi
c_green "✓ chezmoi: $(chezmoi --version | head -n1)"

# Single per-machine SSH key, named after the host so it matches the chezmoi
# templates' {{ .chezmoi.hostname }}_ed25519 (ssh-config + signing line up). We
# derive it via chezmoi (installed just above) so the two never disagree.
ssh_host="$(chezmoi execute-template '{{ .chezmoi.hostname }}' 2>/dev/null)"
ssh_host="${ssh_host:-$(short_hostname)}"
ssh_key="$HOME/.ssh/${ssh_host}_ed25519"

# ── 1.5 (optional) restore SSH key from a prior key-backup dir ──────────────
# age + GPG are retired (D4/D5): secrets live in Bitwarden (rbw), and chezmoi
# no longer encrypts anything. The only thing a key-backup holds now is SSH
# keys — and those are per-machine by default (lose a laptop → revoke ONE
# GitHub key, the others keep working). So importing them is opt-in via
# --import-ssh. Without it, --import-from is a no-op and fresh SSH keys are
# generated below.
if [[ -n "$IMPORT_FROM" ]]; then
  if [[ ! -d "$IMPORT_FROM" ]]; then
    c_red "--import-from: directory not found: $IMPORT_FROM"; exit 2
  fi
  if [[ "$IMPORT_SSH" == "true" && -d "$IMPORT_FROM/ssh" ]]; then
    c_blue "==> importing SSH key from $IMPORT_FROM (installs as ${ssh_host}_ed25519)"
    mkdir -p "$HOME/.ssh"
    chmod 700 "$HOME/.ssh"
    # Take the first private key in the backup, whatever the source host named
    # it, and install it under THIS machine's name.
    src_priv="$(find "$IMPORT_FROM/ssh" -maxdepth 1 -name '*_ed25519' ! -name '*.pub' 2>/dev/null | head -1)"
    if [[ -n "$src_priv" && ! -f "$ssh_key" ]]; then
      install -m 0600 "$src_priv" "$ssh_key"
      if [[ -f "${src_priv}.pub" ]]; then
        install -m 0644 "${src_priv}.pub" "${ssh_key}.pub"
      else
        ssh-keygen -y -f "$ssh_key" > "${ssh_key}.pub"   # regenerate pub from priv
      fi
      c_green "  ✓ ssh key restored as ${ssh_host}_ed25519"
    fi
    c_green "✓ import done — continuing with keygen flow (an existing key is reused)"
  else
    c_yellow "==> --import-from given without --import-ssh (or no ssh/ dir present)."
    c_yellow "    Nothing to import — age/GPG are retired. Fresh SSH keys will be generated."
  fi
fi

# ── 2. (age + GPG keygen retired) ───────────────────────────────────────────
# age was dropped (D5): chezmoi no longer encrypts anything, so there's no
# decrypt key to generate. GPG was dropped (D4): `pass` is gone and commit
# signing is SSH-based. Secrets live in Bitwarden, accessed at runtime via rbw
# (`rbw login` once on a fresh machine — see README). The only keys bootstrap
# still manages are the SSH identity keys below.

# ── 4. SSH keys for each identity ───────────────────────────────────────────
mkdir -p "$HOME/.ssh" && chmod 700 "$HOME/.ssh"
ssh_keys_were_generated=0
if [[ -f "$ssh_key" ]]; then
  c_green "✓ ssh key already exists — using it: ${ssh_host}_ed25519"
else
  c_blue "==> generating ssh key: $ssh_key"
  ssh-keygen -t ed25519 -f "$ssh_key" -N "" -C "${ATHOME_GITHUB_HANDLE:-$(id -un)}@${ssh_host}"
  ssh_keys_were_generated=1
fi
c_green "✓ ssh public key: $(cat "${ssh_key}.pub")"

# ── 4.5 SSH key backup (TTY-friendly) ───────────────────────────────────────
# Fires whenever SSH keys were freshly generated this run. age + GPG are
# retired, so SSH identity keys are the only thing to back up. Per-machine SSH
# is the default; this keeps the option to share them across personal boxes
# open (restore with `--import-from <dir> --import-ssh`).
#
# Existing keys (re-bootstrap) don't trigger a backup — they're presumably
# already saved.
if (( ssh_keys_were_generated )); then
  backup_dir="$HOME/key-backup-$(date +%Y%m%d-%H%M%S)"
  mkdir -p "$backup_dir"
  chmod 700 "$backup_dir"

  mkdir -p "$backup_dir/ssh"
  chmod 700 "$backup_dir/ssh"
  install -m 0600 "$ssh_key"       "$backup_dir/ssh/${ssh_host}_ed25519"
  install -m 0644 "${ssh_key}.pub" "$backup_dir/ssh/${ssh_host}_ed25519.pub"

  bk_host="$(short_hostname)"
  bk_user="$(whoami)"

  c_yellow ""
  c_yellow "  ────────────────────────────────────────────────────────────"
  c_yellow "  SSH KEY BACKUP WRITTEN: $backup_dir"
  c_yellow ""
  c_yellow "  Contents:"
  c_yellow "    ssh/<id>_ed25519{,.pub}  ← per-machine by default; --import-ssh to share them too"
  c_yellow ""
  c_yellow "  Copy it OFF this box if you want to reuse these keys elsewhere."
  c_yellow "  (Your actual secrets live in Bitwarden — recover those with \`rbw login\`,"
  c_yellow "   no key transport needed.)"
  c_yellow ""
  c_yellow "  Live transport options (when you're at the receiving machine):"
  c_yellow "    • magic-wormhole  → wormhole send $backup_dir"
  c_yellow "    • wush (coder)    → wush send $backup_dir"
  c_yellow "    • scp on LAN      → from your laptop, pull from this host:"
  c_yellow ""
  c_yellow "        sudo systemctl enable --now sshd     # one-shot on THIS box"
  c_yellow "        ip -4 -o addr show | awk '/inet /{print \$2, \$4}'"
  c_yellow "        # then on your laptop:"
  c_yellow "        scp -r ${bk_user}@${bk_host}.local:$backup_dir ~/${bk_host}-keys/"
  c_yellow ""
  c_yellow "  Restore on the next machine:"
  c_yellow "    ./bootstrap.sh --import-from /path/to/backup --import-ssh"
  c_yellow ""
  c_yellow "  Once safely copied off, shred the local:"
  c_yellow "    shred -u $backup_dir/ssh/* 2>/dev/null; rmdir $backup_dir/ssh $backup_dir"
  c_yellow "  ────────────────────────────────────────────────────────────"
  c_yellow ""
fi

# ── 4.5 upload SSH keys to GitHub (auth + signing in one shot) ──────────────
# This is what makes the HTTPS-at-gh-auth-login choice safe: we upload SSH
# keys NOW, before chezmoi apply switches the repo remote to SSH (via
# run_once_after_99-cleanup.sh). After this block, SSH is end-to-end.
#
# gh_has_scope: scan `gh auth status` output for a granted scope (e.g. "repo").
gh_has_scope() {
  local scope="$1"
  gh auth status 2>&1 | grep -oE "Token scopes:.*" | head -1 | grep -q "'$scope'"
}

# log_indented: prefix every line of stdin with `    gh said: ` for diagnostics.
log_indented() {
  while IFS= read -r line; do
    printf '    gh said: %s\n' "$line" >&2
  done
}

# Extract just the base64 key blob from a .pub file (skips type prefix + comment).
pub_key_blob() { awk '{print $2}' "$1"; }

# key_on_gh_auth / key_on_gh_signing: return 0 if this exact key blob is already
# present on GitHub under the corresponding endpoint, else 1. Avoids the
# upload-then-detect-error dance — GitHub stores the same blob only once.
key_on_gh_auth() {
  local blob; blob="$(pub_key_blob "$1")"
  gh api user/keys --jq '.[].key' 2>/dev/null | grep -qF "$blob"
}
key_on_gh_signing() {
  local blob; blob="$(pub_key_blob "$1")"
  gh api user/ssh_signing_keys --jq '.[].key' 2>/dev/null | grep -qF "$blob"
}

# add_gh_ssh_key: caller should have already checked key_on_gh_* and skipped
# if the key is present. This just uploads + reports.
add_gh_ssh_key() {
  local pub="$1" title="$2" extra="$3"  # extra: empty for auth, "--type signing" for signing
  local out
  # shellcheck disable=SC2086   # intentional word-splitting on $extra
  if out="$(gh ssh-key add "$pub" --title "$title" $extra 2>&1)"; then
    c_green "  ✓ uploaded: $title"
    return 0
  fi
  c_red "  ✗ $title — upload failed"
  printf '%s\n' "$out" | log_indented
  return 1
}

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  if confirm "Upload SSH public keys to GitHub now (auth + signing)?" y; then
    host="$(short_hostname)"

    # ── proactive scope check: ensure gh has admin:public_key + admin:ssh_signing_key
    #    BEFORE attempting uploads. gh auth login doesn't request these scopes
    #    by default; refreshing once upfront beats failing six times then asking.
    missing_scopes=()
    for scope in admin:public_key admin:ssh_signing_key; do
      gh_has_scope "$scope" || missing_scopes+=("$scope")
    done

    upload_via_web=false
    if (( ${#missing_scopes[@]} > 0 )); then
      c_yellow ""
      c_yellow "==> gh auth is missing scope(s): ${missing_scopes[*]}"
      c_yellow "    (default \`gh auth login\` doesn't request these — needed for ssh-key uploads)"
      c_yellow ""
      c_yellow "    Two ways forward:"
      c_yellow "    [Y] Refresh via gh — opens GitHub device flow."
      c_yellow "        Warning: if you originally authed with a Personal Access Token"
      c_yellow "        (not browser), gh will prompt for your username + a NEW PAT —"
      c_yellow "        that's a gh CLI behavior we can't bypass."
      c_yellow "    [n] Skip. Bootstrap will print pub keys + the manual upload URL;"
      c_yellow "        you paste them in the GitHub web UI, no gh interaction needed."
      c_yellow ""
      if confirm "refresh via gh?" y; then
        gh auth refresh -h github.com -s "$(IFS=,; echo "${missing_scopes[*]}")"
        c_green "✓ scopes refreshed"
      else
        upload_via_web=true
      fi
    fi

    if $upload_via_web; then
      c_yellow ""
      c_yellow "==> manual SSH key upload:"
      c_yellow "    Visit https://github.com/settings/ssh/new (auth tab)"
      c_yellow "    and https://github.com/settings/ssh/signing/new (signing tab)"
      c_yellow "    Paste each block below — once as auth key, once as signing key:"
      c_yellow ""
      c_blue "  ── ${host} ──"
      cat "${ssh_key}.pub"
      echo ""
      c_yellow "    Then re-run bootstrap if you want the gh git_protocol switch to ssh"
      c_yellow "    (or run it manually: gh config set git_protocol ssh --host github.com)"
    else
      # ── upload via gh API (scopes are present at this point) ──
      # Check-then-upload so re-runs don't spam GitHub with duplicate keys.
      pub="${ssh_key}.pub"
      title="${host}"

      if key_on_gh_auth "$pub"; then
        c_green "  ✓ auth key already on GitHub: matching $title"
      else
        add_gh_ssh_key "$pub" "$title" "" || true
      fi

      if key_on_gh_signing "$pub"; then
        c_green "  ✓ signing key already on GitHub: matching ${title}-sign"
      else
        add_gh_ssh_key "$pub" "${title}-sign" "--type signing" || true
      fi
    fi

    # Now that SSH keys exist on GitHub, flip gh's default protocol so any
    # future `gh repo clone foo/bar` resolves to git@github.com:foo/bar.
    if gh config set git_protocol ssh --host github.com >/dev/null 2>&1; then
      c_green "✓ gh: default protocol switched to ssh"
    fi

    # Switch THIS chezmoi repo's remote from HTTPS to SSH now (instead of
    # waiting for run_once_after_99-cleanup.sh during chezmoi apply). Doing
    # it here means subsequent `git pull` in this repo uses SSH — no
    # prompt for HTTPS password (which GitHub doesn't even accept anymore).
    #
    # But wait: if chezmoi has ALREADY deployed ~/.gitconfig (e.g. on a
    # re-run / partial-apply state), its `url."git@github-personal:..."
    # insteadOf` rule will transform any `git@github.com:<handle>/...`
    # we set here into `git@github-personal:<handle>/...`. That host
    # alias is also defined in ~/.ssh/config — which chezmoi hasn't
    # necessarily deployed yet on this run.
    #
    # So before rewriting the remote, make sure the three github-{ctx}
    # Host aliases exist in ~/.ssh/config. If chezmoi already deployed the
    # full config, the grep below will see the marker and we'll skip; if
    # not, append the minimum needed so SSH can resolve. chezmoi's later
    # apply will overwrite this file with its full version idempotently.
    mkdir -p "$HOME/.ssh"
    chmod 700 "$HOME/.ssh"
    touch "$HOME/.ssh/config"
    chmod 600 "$HOME/.ssh/config"
    if ! grep -q "${ssh_host}_ed25519" "$HOME/.ssh/config" 2>/dev/null; then
      c_blue "==> seeding github.com Host → ${ssh_host}_ed25519 in ~/.ssh/config"
      c_yellow "    (chezmoi-managed ~/.ssh/config overwrites this idempotently on first apply)"
      cat >> "$HOME/.ssh/config" <<SSHSEED

# === bootstrap.sh seed — chezmoi-managed ~/.ssh/config will replace this ===
Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/${ssh_host}_ed25519
SSHSEED
    fi

    current_remote="$(git -C "$repo_root" remote get-url origin 2>/dev/null || true)"
    if [[ "$current_remote" == https://github.com/* ]]; then
      ssh_remote="git@github.com:${current_remote#https://github.com/}"
      ssh_remote="${ssh_remote%.git}.git"
      git -C "$repo_root" remote set-url origin "$ssh_remote"
      c_green "✓ $repo_root remote: HTTPS → SSH ($ssh_remote)"
    fi
  else
    c_yellow "skipping ssh-key upload — run manually later:"
    c_yellow "  gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key"
    c_yellow "  gh ssh-key add ${ssh_key}.pub --title \"${ssh_host}\""
    c_yellow "  gh ssh-key add ${ssh_key}.pub --title \"${ssh_host}-sign\" --type signing"
  fi
else
  c_yellow "gh not authed — skipping ssh-key upload (run \`gh ssh-key add\` manually later)"
fi

# ── 5. chezmoi init/apply against the LOCAL source dir ──────────────────────
# (age dropped — there's no longer an ageRecipient/encryption block to sanity-
# check in chezmoi.toml, so the old stale-data detection is gone.)

# All known prompts pre-filled so first init is non-interactive.
# Future invocations: chezmoi.toml caches answers; --promptString is a no-op.
#
# ATHOME_FOLLOWUP_LOG: chezmoi run_once scripts append manual-step notes to
# this file when something needs the human to act (chsh, Noctalia first-run
# wizard, `rbw login`, etc.). We tail it at the end of bootstrap so the user
# sees ALL pending follow-ups in one place.
export ATHOME_FOLLOWUP_LOG="${TMPDIR:-/tmp}/athome-bootstrap-followup.log"
: > "$ATHOME_FOLLOWUP_LOG"   # truncate so old runs don't bleed in

# Tells run_once_16 (the Bitwarden nudge) to stay quiet — bootstrap runs
# bw-setup itself in the TTY after apply. Non-bootstrap `chezmoi apply` runs
# won't have this set, so they DO get the (non-interactive) nudge.
export ATHOME_BOOTSTRAP=1

c_blue "==> chezmoi init --apply --force (source: $SOURCE_FOR_CHEZMOI, ref: $REF)"
# Work / sidebiz emails ride in via env vars (so they aren't hardcoded in
# the template). On first run, export them in your shell before running
# bootstrap.sh, OR let chezmoi prompt interactively and remember the answer
# in ~/.config/chezmoi/chezmoi.toml. Empty value = "use personal everywhere".
#
# --force: on a re-run / partial-state machine, some managed files may
# already exist with local edits (e.g. ssh config got seeded by an earlier
# bootstrap.sh that died mid-flight). Default chezmoi behavior is to
# prompt interactively, which has known TTY-input quirks. We want bootstrap
# to converge to the canonical state in one shot — `--force` clobbers any
# local user edits to managed files. If you've been hand-editing dotfiles
# outside chezmoi, snapshot them before re-running bootstrap.
# ── Personal GitHub identity: DERIVE, never hardcode ─────────────────────────
# The only thing a user must supply is their GitHub handle — and even that is
# taken from the logged-in gh account when possible. The numeric id comes from
# the PUBLIC profile, and the two build the GitHub "noreply" commit email
# (<id>+<handle>@users.noreply.github.com). Nothing personal — no email, no id —
# is ever stored in the repo; it's resolved here and cached in the user's own
# ~/.config/chezmoi.
#
# No chicken-and-egg with gh: this runs BEFORE `chezmoi init`, and on a fresh
# box gh may not be installed yet (mise installs it during apply). So gh is only
# an *opportunistic* shortcut for auto-filling the handle; the id is fetched
# from the PUBLIC api.github.com/users/<handle> via curl — no gh, no auth, no
# login. Worst case (no curl either) the id stays empty and the email falls back
# to the plain <handle>@users.noreply.github.com form, still a valid identity.
gh_handle="${ATHOME_GITHUB_HANDLE:-}"
if [[ -z "$gh_handle" ]] && command -v gh >/dev/null 2>&1; then
  gh_handle="$(gh api user --jq .login 2>/dev/null || true)"   # opportunistic: the logged-in account
fi
# Prompt if we still don't have it; `|| break` so a headless run (no TTY → EOF)
# doesn't spin forever — it proceeds with an empty handle, which the user can
# supply via ATHOME_GITHUB_HANDLE or chezmoi's own prompt on the next apply.
while [[ -z "$gh_handle" ]]; do read -r -p "GitHub username: " gh_handle || break; done
gh_id="${ATHOME_GITHUB_ID:-}"
if [[ -z "$gh_id" && -n "$gh_handle" ]]; then
  command -v gh >/dev/null 2>&1 && \
    gh_id="$(gh api "users/$gh_handle" --jq .id 2>/dev/null || true)"      # opportunistic (if gh present)
  if [[ -z "$gh_id" ]] && command -v curl >/dev/null 2>&1; then           # primary, gh-less path (no auth)
    gh_id="$(curl -fsSL "https://api.github.com/users/$gh_handle" 2>/dev/null \
      | sed -n 's/.*"id"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' | head -1 || true)"
  fi
fi
c_green "✓ GitHub identity: $gh_handle${gh_id:+ (id $gh_id)} → commit email ${gh_id:+${gh_id}+}${gh_handle}@users.noreply.github.com"

chezmoi init --apply --force --source="$SOURCE_FOR_CHEZMOI" \
  --promptString machineType="$MACHINE" \
  --promptString hostname="$(short_hostname)" \
  --promptString githubHandle="$gh_handle" \
  --promptString githubId="$gh_id" \
  --promptString personalName="${ATHOME_PERSONAL_NAME:-}" \
  --promptString netbirdManagementUrl="" \
  --promptString nordvpnCountry="" \
  --promptString workEmail="${ATHOME_WORK_EMAIL:-}" \
  --promptString sidebizEmail="${ATHOME_SIDEBIZ_EMAIL:-}" \
  --promptString workName="${ATHOME_WORK_NAME:-}" \
  --promptString sidebizName="${ATHOME_SIDEBIZ_NAME:-}" \
  --promptString agentsRepo="${ATHOME_AGENTS_REPO:-}" \
  --promptBool   includeAgents="$INCLUDE_AGENTS"

# ── 6. set chezmoi's permanent sourceDir so future plain `chezmoi apply`
#       calls (without --source) use the real cloned repo's home/ subdir.
#       Pin to $chezmoi_source (= $repo_root/home) — chezmoi would also
#       follow .chezmoiroot if we omitted this and `chezmoi cd`'d to
#       $repo_root, but the explicit pin removes any ambiguity.
chezmoi_config="$HOME/.config/chezmoi/chezmoi.toml"
if [[ -f "$chezmoi_config" ]] && ! grep -q "^sourceDir" "$chezmoi_config"; then
  c_blue "==> pinning chezmoi sourceDir to $chezmoi_source for future calls"
  # Prepend before the [data] section
  awk -v src="\"$chezmoi_source\"" 'NR==1{print "sourceDir = " src} {print}' \
    "$chezmoi_config" > "$chezmoi_config.new" && mv "$chezmoi_config.new" "$chezmoi_config"
fi

# ── 7. install global hooks into the in-repo .git/hooks/ ───────────────────
# .git lives at $repo_root (the working tree). Hook source files live
# under the chezmoi source dir at home/dot_config/git/hooks/.
if [[ -d "$repo_root/.git" ]] && [[ ! -f "$repo_root/.git/hooks/pre-commit" ]]; then
  c_blue "==> installing global hook in this repo's .git/hooks/"
  cp "$chezmoi_source/dot_config/git/hooks/executable_pre-commit"  "$repo_root/.git/hooks/pre-commit"
  cp "$chezmoi_source/dot_config/git/hooks/executable_commit-msg"  "$repo_root/.git/hooks/commit-msg"
  chmod +x "$repo_root/.git/hooks/"{pre-commit,commit-msg}
fi

# ── 7.5 secrets: Bitwarden login (rbw) ──────────────────────────────────────
# The login logic lives in ~/.local/bin/bw-setup (deployed by chezmoi above,
# runnable anytime). We just offer to run it HERE in the TTY — not inside
# chezmoi apply, which is non-interactive (--force) and where a pinentry
# master-password prompt would hit the flaky-TTY-in-tmux failure mode.
# ATHOME_BOOTSTRAP=1 (exported before apply) already suppressed the
# run_once_16 nudge, since we handle it here.
bw_setup="$HOME/.local/bin/bw-setup"
export PATH="/opt/nanobrew/prefix/bin:/usr/local/bin:$HOME/.local/bin:$PATH"
if [[ -x "$bw_setup" ]]; then
  if confirm "log in to Bitwarden now (rbw)?" y; then
    "$bw_setup" || true   # bw-setup logs its own result; never fail bootstrap on it
  else
    c_yellow "  Skipped — run \`bw-setup\` anytime to log in to Bitwarden."
    [[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]] && printf '  • %s\n' "Bitwarden: run \`bw-setup\` (register once + unlock) to access secrets. See docs/secrets.md." >> "$ATHOME_FOLLOWUP_LOG"
  fi
else
  c_yellow "  bw-setup not deployed yet — run it after a successful chezmoi apply."
  [[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]] && printf '  • %s\n' "Bitwarden: run \`bw-setup\` to access secrets. See docs/secrets.md." >> "$ATHOME_FOLLOWUP_LOG"
fi

# ── 7.6 secrets: restore the encrypted vault (git-crypt) ─────────────────────
# Once Bitwarden is unlocked, pull the private secrets-vault repo, decrypt it
# with the git-crypt key from Bitwarden, and populate ~/.secrets/<repo>/.env etc.
# secrets-restore guards on `rbw unlocked` itself; we only offer it here when
# unlocked so a fresh machine recovers env files in one step.
secrets_restore="$HOME/.local/bin/secrets-restore"
if [[ -x "$secrets_restore" ]] && rbw unlocked >/dev/null 2>&1; then
  if confirm "restore your encrypted secrets now (secrets-restore)?" y; then
    "$secrets_restore" || c_yellow "  secrets-restore had issues — run it manually later."
  fi
else
  [[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]] && printf '  • %s\n' "Secrets: run \`secrets-restore\` (after \`rbw unlock\`) to recover ~/.config env files from the encrypted vault. See docs/secrets.md." >> "$ATHOME_FOLLOWUP_LOG"
fi

# ── 8. recover from detached HEAD in the chezmoi repo, if any ──────────────
# Earlier bootstrap versions used to `git checkout <tag>` directly on the
# source dir and left HEAD detached on failure. With the worktree approach
# this shouldn't happen any more, but be belt+braces — if HEAD is somehow
# off-branch, restore to main so subsequent `git pull` / `czu` works.
current_branch="$(git -C "$repo_root" rev-parse --abbrev-ref HEAD 2>/dev/null)"
if [[ "$current_branch" == "HEAD" ]]; then
  c_yellow "==> $repo_root is in detached HEAD; restoring to main"
  git -C "$repo_root" fetch origin --quiet 2>/dev/null || true
  if git -C "$repo_root" checkout main 2>/dev/null; then
    c_green "✓ restored to main"
  elif git -C "$repo_root" checkout -B main origin/main 2>/dev/null; then
    c_green "✓ created local main tracking origin/main"
  else
    c_red "could not restore to main; check \`git status\` in $repo_root"
  fi
fi

c_green ""
c_green "✓ bootstrap done."

# ── Print any follow-up actions chezmoi scripts logged ────────────────────
if [[ -s "$ATHOME_FOLLOWUP_LOG" ]]; then
  c_yellow ""
  c_yellow "═══════════════════════════════════════════════════════════════"
  c_yellow "  Follow-up actions you (or the system) need to do:"
  c_yellow "═══════════════════════════════════════════════════════════════"
  cat "$ATHOME_FOLLOWUP_LOG"
  c_yellow "═══════════════════════════════════════════════════════════════"
fi

cat <<EOF

Day-to-day:

  czu                          # chezmoi update -v (pull + apply)
  czd                          # chezmoi diff
  cze ~/.zshrc                 # edit a tracked file (auto-applies)
  czdoc                        # chezmoi doctor

Next manual steps:

  1. Secrets (Bitwarden): handled above if you said yes. To (re)run it anytime:
       bw-setup          # registers a new device (API key) if needed, then unlocks
     See docs/secrets.md for the API-key / EU-region / Vaultwarden details.

  2. (Optional) Reuse these SSH keys on another personal box. age is gone, so
     don't roam them through chezmoi — transport the key-backup dir instead:
       wush send ~/key-backup-*        # or: wormhole send ~/key-backup-*
       # then on the other machine: ./bootstrap.sh --import-from <dir> --import-ssh

  3. VPNs (when ready):
       sudo netbird up                                   # mesh (NetBird Cloud SSO)
       nordvpn login --username "..." --password "..."   # Linux; macOS uses the GUI

  4. Switch your current shell to zsh (default already changed; this just
     reloads THIS terminal):
       exec zsh
EOF
