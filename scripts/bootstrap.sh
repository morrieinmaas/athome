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
MACHINE_FROM_FLAG="false"  # true once --machine is passed (so a config file can't override an explicit flag)
INCLUDE_AGENTS="true"   # set to "false" by --no-agents
AGENTS_FROM_FLAG="false"   # true once --no-agents is passed (config can't override an explicit flag)
CONFIG_FILE_ARG=""      # set by --config: a TOML file of answers for a non-interactive run
IMPORT_FROM=""          # set to a backup-dir path by --import-from
IMPORT_SSH="false"      # set to "true" by --import-ssh (opt-in: SSH keys are usually per-machine)
IMPORT_SSH_BW=""        # set to a Bitwarden item name by --import-ssh-bw (restore the key via rbw)
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref)           REF="$2"; shift 2 ;;
    --machine)       MACHINE="$2"; MACHINE_FROM_FLAG="true"; shift 2 ;;
    --config)        CONFIG_FILE_ARG="$2"; shift 2 ;;   # TOML answers file (see examples/bootstrap.toml.example)
    --no-agents)     INCLUDE_AGENTS="false"; AGENTS_FROM_FLAG="true"; shift ;;  # skip ~/.config/agents skills sync
    --import-from)   IMPORT_FROM="$2"; shift 2 ;;      # restore SSH key from a prior key-backup dir (with --import-ssh)
    --import-ssh)    IMPORT_SSH="true"; shift ;;       # opt in to restoring SSH from --import-from
    --import-ssh-bw) IMPORT_SSH_BW="$2"; shift 2 ;;    # restore the SSH key from a Bitwarden secure note (rbw get)
    -h|--help)
      sed -n 's/^# \{0,1\}//p' "$0" | head -30
      exit 0 ;;
    *) c_red "unknown arg: $1"; exit 2 ;;
  esac
done

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

# ── 0.2 load a non-interactive config file (optional) ───────────────────────
# Everything the first-run prompts ask for can live in a TOML answers file so a
# fresh box can be provisioned hands-off. Resolution order (first readable wins):
#   1. --config <path>
#   2. $repo_root/bootstrap.local.toml   (gitignored — your filled-in copy)
#   3. ~/.config/athome/bootstrap.toml
# Values here only fill BLANKS: an explicit env var (ATHOME_*) or flag always
# wins. Nothing from this file is committed — it seeds chezmoi's own private
# ~/.config/chezmoi/chezmoi.toml at apply time, exactly where chezmoi already
# caches answers. Template: examples/bootstrap.toml.example.
#
# Parser is deliberately minimal: `key = value` or `key = "value"`, one per
# line, comments on their OWN line (no inline `#` after a value).
cfg_get() {
  sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$CONFIG_FILE" 2>/dev/null \
    | head -1 | sed 's/[[:space:]]*$//; s/^"\(.*\)"$/\1/'
}
load_config() {
  CONFIG_FILE="$1"
  c_blue "==> non-interactive config: $CONFIG_FILE"
  : "${ATHOME_GITHUB_HANDLE:=$(cfg_get githubHandle)}"
  : "${ATHOME_GITHUB_ID:=$(cfg_get githubId)}"
  : "${ATHOME_PERSONAL_NAME:=$(cfg_get personalName)}"
  : "${ATHOME_WORK_EMAIL:=$(cfg_get workEmail)}"
  : "${ATHOME_WORK_NAME:=$(cfg_get workName)}"
  : "${ATHOME_SIDEBIZ_EMAIL:=$(cfg_get sidebizEmail)}"
  : "${ATHOME_SIDEBIZ_NAME:=$(cfg_get sidebizName)}"
  : "${ATHOME_AGENTS_REPO:=$(cfg_get agentsRepo)}"
  : "${ATHOME_NETBIRD_MGMT_URL:=$(cfg_get netbirdManagementUrl)}"
  : "${ATHOME_NORDVPN_COUNTRY:=$(cfg_get nordvpnCountry)}"
  if [[ "$MACHINE_FROM_FLAG" == false ]]; then
    local m; m="$(cfg_get machine)"; [[ -n "$m" ]] && MACHINE="$m"
  fi
  if [[ "$AGENTS_FROM_FLAG" == false ]]; then
    local a; a="$(cfg_get includeAgents)"; [[ -n "$a" ]] && INCLUDE_AGENTS="$a"
  fi
}
if [[ -n "$CONFIG_FILE_ARG" ]]; then
  [[ -r "$CONFIG_FILE_ARG" ]] || { c_red "--config: file not readable: $CONFIG_FILE_ARG"; exit 2; }
  load_config "$CONFIG_FILE_ARG"
elif [[ -r "$repo_root/bootstrap.local.toml" ]]; then
  load_config "$repo_root/bootstrap.local.toml"
elif [[ -r "$HOME/.config/athome/bootstrap.toml" ]]; then
  load_config "$HOME/.config/athome/bootstrap.toml"
fi

# Validate machine context now that flags AND the config file have had their say.
case "$MACHINE" in
  personal|work) ;;
  *) c_red "machine must be personal|work (got: $MACHINE)"; exit 2 ;;
esac
case "$INCLUDE_AGENTS" in
  true|false) ;;
  *) c_red "includeAgents must be true|false (got: $INCLUDE_AGENTS)"; exit 2 ;;
esac
c_blue "==> machine context: $MACHINE"

# Prune any leaked worktrees from previous hard-killed bootstrap runs.
# (EXIT trap normally removes them on Ctrl-C, but SIGKILL bypasses it.)
# Safe to run unconditionally — no-op if nothing to prune.
git -C "$repo_root" worktree prune 2>/dev/null || true

# ── 0.5 normalize to ~/.local/share/chezmoi if we're elsewhere ──────────────
expected="$HOME/.local/share/chezmoi"
if [[ "$repo_root" != "$expected" ]]; then
  # Idempotent re-run: a prior bootstrap already created the symlink. Detect the
  # correct link and no-op instead of erroring "resolve manually" (which made a
  # second run from the same repo dir fail outright).
  if [[ -L "$expected" && "$(readlink "$expected")" == "$repo_root" ]]; then
    c_green "✓ $expected already symlinked → $repo_root"
  elif confirm "==> repo at $repo_root, chezmoi expects $expected. symlink it?" y; then
    # Only bail if something OTHER than our correct link is in the way.
    if [[ -e "$expected" || -L "$expected" ]]; then
      c_red "$expected exists and isn't a link to $repo_root — resolve manually"; exit 1
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

# ── 1.6 (optional) restore the SSH key from Bitwarden (rbw) ─────────────────
# The painless cross-machine path: pull a key you previously stored as a
# Bitwarden secure note. Needs rbw (it's in packages.yaml; if not on PATH yet,
# install it then re-run). Item name = whatever you stored, e.g.
# ssh/<otherhost>_ed25519.
if [[ -n "$IMPORT_SSH_BW" ]]; then
  if ! command -v rbw >/dev/null 2>&1; then
    c_yellow "==> --import-ssh-bw: rbw not installed yet — install it, then re-run:"
    c_yellow "    ./scripts/bootstrap.sh --import-ssh-bw $IMPORT_SSH_BW"
  else
    c_blue "==> restoring SSH key from Bitwarden '$IMPORT_SSH_BW' → ${ssh_host}_ed25519"
    mkdir -p "$HOME/.ssh"; chmod 700 "$HOME/.ssh"
    rbw unlock >/dev/null 2>&1 || true
    if rbw get --field notes "$IMPORT_SSH_BW" > "$ssh_key" 2>/dev/null && [[ -s "$ssh_key" ]]; then
      chmod 600 "$ssh_key"
      ssh-keygen -y -f "$ssh_key" > "${ssh_key}.pub"
      c_green "  ✓ ssh key restored from Bitwarden as ${ssh_host}_ed25519"
    else
      rm -f "$ssh_key"
      c_red "  ✗ couldn't read '$IMPORT_SSH_BW' (run \`rbw unlock\`, check the item name)"
    fi
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

# ── 4.5 SSH key backup → Bitwarden ──────────────────────────────────────────
# Fires when a key was freshly generated. We store the private key as a
# Bitwarden SECURE NOTE (ssh/<host>_ed25519) via the `bw` CLI — synced,
# encrypted at rest, recoverable on any machine with `--import-ssh-bw`. No more
# key-transport dance (wormhole/wush/scp). Per-machine keys are still the
# default; this just makes recovery painless. Re-bootstrap (existing key) skips.
#
# `bw` isn't installed until chezmoi apply lands packages, so if it's not on
# PATH yet we print the exact one-liner to run once it is.
if (( ssh_keys_were_generated )); then
  bw_item="ssh/${ssh_host}_ed25519"
  store_ok=false
  if command -v bw >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
    c_blue "==> storing SSH key in Bitwarden as secure note '$bw_item'"
    bw_sess="$(bw unlock --raw 2>/dev/null)" \
      || { bw login >/dev/null 2>&1 && bw_sess="$(bw unlock --raw 2>/dev/null)"; } \
      || bw_sess=""
    if [[ -n "$bw_sess" ]] \
       && jq -n --arg name "$bw_item" --arg notes "$(cat "$ssh_key")" \
              '{type:2,name:$name,notes:$notes,secureNote:{type:0}}' \
            | bw encode | bw create item --session "$bw_sess" >/dev/null 2>&1; then
      c_green "  ✓ stored in Bitwarden: $bw_item"
      store_ok=true
    fi
  fi
  if ! $store_ok; then
    c_yellow ""
    c_yellow "  ── back up this machine's SSH key to Bitwarden (recommended) ──"
    c_yellow "  Once \`bw\` is installed + unlocked, store the private key as a secure note:"
    c_yellow ""
    c_yellow "    jq -n --arg name '$bw_item' --arg notes \"\$(cat $ssh_key)\" \\"
    c_yellow "      '{type:2,name:\$name,notes:\$notes,secureNote:{type:0}}' | bw encode | bw create item"
    c_yellow ""
    c_yellow "  Restore it on another machine:  ./scripts/bootstrap.sh --import-ssh-bw $bw_item"
    c_yellow ""
  fi
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
    # But wait: ~/.gitconfig's `url."git@github.com:" insteadOf
    # https://github.com/` catch-all makes every github.com op use SSH, and our
    # key (~/.ssh/<host>_ed25519) is NOT a default ssh identity name — so ssh
    # only offers it if ~/.ssh/config pins it via IdentityFile. If chezmoi
    # hasn't deployed the ssh config yet on this run, `git pull` would fail.
    #
    # So before rewriting the remote, make sure ~/.ssh/config pins github.com to
    # our key. If chezmoi already deployed the full config, the grep below sees
    # the marker and we skip; otherwise append the minimum block. chezmoi's later
    # apply overwrites this file with its full version idempotently.
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
# Work / sidebiz emails (and every other answer) ride in via, in priority order:
# an ATHOME_* env var → the --config TOML answers file → interactive prompt.
# None are hardcoded in the template or committed anywhere. Empty value = "use
# personal everywhere". See examples/bootstrap.toml.example for the file form.
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

# Seed chezmoi's own config [data] BEFORE init so first-run is non-interactive.
# Why not --promptString? chezmoi matches `--promptString K=V` on the prompt's
# *display text*, NOT the field name — so `--promptString machineType=…` is
# silently ignored and chezmoi falls back to interactive prompts (dropping every
# value we computed here). The robust, version-proof path is the documented
# "promptStringOnce reads pre-existing config [data]" behavior: we write the
# answers into ~/.config/chezmoi/chezmoi.toml first, and init reuses them without
# prompting, then re-renders the file from the template (so this seed is replaced
# by the full config). This is the user's OWN private file — the same place
# chezmoi caches interactive answers — never the repo.
chezmoi_config="$HOME/.config/chezmoi/chezmoi.toml"
mkdir -p "$(dirname "$chezmoi_config")"
toml_str() { printf '%s' "${1:-}" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
cat > "$chezmoi_config" <<TOMLSEED
[data]
    machineType          = "$(toml_str "$MACHINE")"
    hostname             = "$(toml_str "$(short_hostname)")"
    githubHandle         = "$(toml_str "$gh_handle")"
    githubId             = "$(toml_str "$gh_id")"
    personalName         = "$(toml_str "${ATHOME_PERSONAL_NAME:-}")"
    workEmail            = "$(toml_str "${ATHOME_WORK_EMAIL:-}")"
    workName             = "$(toml_str "${ATHOME_WORK_NAME:-}")"
    sidebizEmail         = "$(toml_str "${ATHOME_SIDEBIZ_EMAIL:-}")"
    sidebizName          = "$(toml_str "${ATHOME_SIDEBIZ_NAME:-}")"
    netbirdManagementUrl = "$(toml_str "${ATHOME_NETBIRD_MGMT_URL:-}")"
    nordvpnCountry       = "$(toml_str "${ATHOME_NORDVPN_COUNTRY:-}")"
    agentsRepo           = "$(toml_str "${ATHOME_AGENTS_REPO:-}")"
    includeAgents        = $INCLUDE_AGENTS
TOMLSEED

# Non-fatal: a single failing run_once/run_onchange script (or a transient
# external fetch) must NOT abort bootstrap and swallow the next-steps footer.
# chezmoi apply is idempotent — re-running converges — so we record the failure,
# surface it in the follow-up log, and carry on. Steps below already guard on
# whether their artifacts (bw-setup, secrets-restore, hooks) actually deployed.
chezmoi_rc=0
chezmoi init --apply --force --source="$SOURCE_FOR_CHEZMOI" || chezmoi_rc=$?
if (( chezmoi_rc != 0 )); then
  c_yellow ""
  c_yellow "⚠ chezmoi apply exited non-zero ($chezmoi_rc) — bootstrap will continue."
  c_yellow "  It's idempotent: re-run to finish the remaining steps —"
  c_yellow "      chezmoi apply        # or: ./scripts/bootstrap.sh"
  printf '  • %s\n' "chezmoi apply exited $chezmoi_rc during bootstrap — re-run \`chezmoi apply\` (idempotent) to finish remaining steps." >> "$ATHOME_FOLLOWUP_LOG"
fi

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
if (( chezmoi_rc == 0 )); then
  c_green "✓ bootstrap done."
else
  c_yellow "✓ bootstrap finished WITH WARNINGS (chezmoi apply rc=$chezmoi_rc — see follow-ups below)."
fi

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

  2. (Optional) Reuse this SSH key on another box. It's stored in Bitwarden as a
     secure note (ssh/<hostname>_ed25519); restore it elsewhere with:
       ./scripts/bootstrap.sh --import-ssh-bw ssh/<hostname>_ed25519

  3. VPNs (when ready):
       sudo netbird up                                   # mesh (NetBird Cloud SSO)
       nordvpn login --username "..." --password "..."   # Linux; macOS uses the GUI

  4. Switch your current shell to zsh (default already changed; this just
     reloads THIS terminal):
       exec zsh
EOF
