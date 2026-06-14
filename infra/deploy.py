"""
infra/deploy.py — pyinfra deploy script for system-level provisioning.

This runs FROM your laptop (controller), SSHs to one or more remote hosts,
and brings them to the state where the chezmoi bootstrap flow can take
over as a regular user. It does the things chezmoi cannot:

    * Install root-level base packages on a fresh box
    * Create the human user with passwordless sudo
    * Drop authorized_keys from your public GitHub key
    * Set the hostname

After this script finishes, SSH in as the human user and run three
commands to take over with chezmoi (git + gh are already installed by
this deploy, so there's no distro-specific package step):

    gh auth login --web
    gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
    cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh

Same end-state as a from-scratch archinstall, just with all the system
prep already done remotely.

Run from the repo root:

    pyinfra infra/inventory.py infra/deploy.py            # whole inventory
    pyinfra infra/inventory.py infra/deploy.py --dry      # see what would change
    pyinfra infra/inventory.py --limit <hostname> infra/deploy.py   # one host
"""

from pyinfra import host
from pyinfra.facts.server import LinuxName
from pyinfra.operations import apt, files, pacman, server

# ── Per-host config ─────────────────────────────────────────────────────
# Values resolve in this order (each `host.data.get` falls through):
#   1. host-specific data in inventory.py
#   2. group_data/<group>.py
#   3. group_data/all.py (which reads ATHOME_PYINFRA_* env vars)
#   4. literal fallback below — generic placeholders, not personal IDs
USERNAME = host.data.get("username", "user")
GITHUB_USER = host.data.get("github_user", "your-github-user")
NEW_HOSTNAME = host.data.get("hostname", host.name)
SSH_KEY_URL = f"https://github.com/{GITHUB_USER}.keys"

# Distro detection drives the package manager choice. LinuxName returns
# "Arch", "Debian", "Ubuntu", "Fedora", etc.
distro = host.get_fact(LinuxName)

# ──────────────────────────────────────────────────────────────────────────
# 1. Base packages
# ──────────────────────────────────────────────────────────────────────────
# Just enough for the user to gh-auth + clone + bootstrap. Anything else
# (zsh, niri, Noctalia, etc.) is chezmoi's job once the user logs in.
BASE_PKGS = ["git", "openssh", "sudo", "curl", "bash", "ca-certificates"]

if distro == "Arch":
    pacman.update(name="refresh pacman db", _sudo=True)
    pacman.packages(
        name="install base packages",
        packages=BASE_PKGS + ["github-cli"],
        _sudo=True,
    )
elif distro in {"Debian", "Ubuntu"}:
    apt.update(name="apt update", _sudo=True)
    apt.packages(
        name="install base packages",
        packages=BASE_PKGS + ["gh"],  # github-cli on Debian/Ubuntu
        _sudo=True,
    )
else:
    raise NotImplementedError(
        f"deploy.py doesn't know how to provision distro={distro!r} yet. "
        "Add a branch for it above."
    )

# ──────────────────────────────────────────────────────────────────────────
# 2. User account
# ──────────────────────────────────────────────────────────────────────────
# Add to both `wheel` (Arch) and `sudo` (Debian) — server.user silently
# skips groups that don't exist, so this is safe across distros.
server.user(
    name=f"ensure user {USERNAME} exists",
    user=USERNAME,
    groups=["wheel", "sudo"],
    shell="/bin/bash",
    create_home=True,
    _sudo=True,
)

# Passwordless sudo for the user — required so the post-deploy bootstrap
# step can install packages without prompting. Locked behind a per-user
# sudoers.d file so it's reversible (`rm /etc/sudoers.d/90-<user>`).
files.line(
    name=f"sudoers: passwordless sudo for {USERNAME}",
    path=f"/etc/sudoers.d/90-{USERNAME}",
    line=f"{USERNAME} ALL=(ALL) NOPASSWD: ALL",
    mode="440",
    _sudo=True,
)

# ──────────────────────────────────────────────────────────────────────────
# 3. SSH authorized_keys from GitHub
# ──────────────────────────────────────────────────────────────────────────
# https://github.com/<user>.keys returns the public keys associated with
# that GH account. Anything you upload to GH lands here automatically —
# bootstrap.sh handles that on the first machine, so re-provisioning from
# a second machine just inherits the same set.
files.directory(
    name=f"~{USERNAME}/.ssh dir",
    path=f"/home/{USERNAME}/.ssh",
    user=USERNAME,
    group=USERNAME,
    mode="700",
    _sudo=True,
)

server.shell(
    # Fetch to a temp file and VALIDATE before installing: curl -f guards HTTP
    # errors but NOT an empty/garbage 200, which would otherwise clobber a
    # working authorized_keys and lock you out. Require non-empty + a real key
    # prefix before moving into place.
    name=f"pull authorized_keys from {SSH_KEY_URL}",
    commands=[
        f"curl -fsSL --proto '=https' {SSH_KEY_URL} -o /tmp/authorized_keys.new",
        "test -s /tmp/authorized_keys.new && grep -qE '^(ssh-|ecdsa-|sk-)' /tmp/authorized_keys.new",
        f"install -D -m 600 -o {USERNAME} -g {USERNAME} /tmp/authorized_keys.new /home/{USERNAME}/.ssh/authorized_keys",
        "rm -f /tmp/authorized_keys.new",
    ],
    _sudo=True,
)

# ──────────────────────────────────────────────────────────────────────────
# 4. Hostname
# ──────────────────────────────────────────────────────────────────────────
server.hostname(
    name=f"set hostname to {NEW_HOSTNAME}",
    hostname=NEW_HOSTNAME,
    _sudo=True,
)

# ──────────────────────────────────────────────────────────────────────────
# 5. Next steps for the human — printed at deploy end
# ──────────────────────────────────────────────────────────────────────────
# After pyinfra finishes, SSH in as ${USERNAME} and run the standard
# chezmoi bootstrap sequence — same as the post-archinstall flow. No
# per-machine helper script needed; gh auth login --web works in any
# graphical session (or use the device flow if you're on a headless
# remote — gh prints the code/URL and you complete on your phone).
#
# Three commands (git + gh already installed above — distro-agnostic):
#   gh auth login --web
#   gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
#   cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh --machine <personal|work>
server.shell(
    name="print next-steps reminder",
    commands=[
        f"""echo '
═══════════════════════════════════════════════════════════════════
Deploy complete on {NEW_HOSTNAME}. Next: SSH in as {USERNAME} and run:

  gh auth login --web
  gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
  cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh

(--machine work if this is a work box; default is personal.)
═══════════════════════════════════════════════════════════════════
'"""
    ],
)
