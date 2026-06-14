# `infra/` — multi-machine provisioning via pyinfra

Companion to chezmoi for system-level work on **remote** boxes.

| Layer | Tool | Runs where | Scope |
| --- | --- | --- | --- |
| Cloud infra (VMs, networks) | Terraform / cloud console | your laptop | outside this repo |
| System provisioning (user, SSH, base pkgs) | **pyinfra** (this dir) | your laptop, over SSH | root-level remote |
| User dotfiles + packages | **chezmoi** (rest of repo) | the target, as user | `$HOME` |

You only need pyinfra when there's a **second machine** to manage or
you're treating a cloud VM as your next target. For a single
ThinkPad sitting next to you, just use [`scripts/bootstrap.sh`](../scripts/bootstrap.sh).

## Prerequisites

- `uv` on your laptop (already installed by the chezmoi setup)
- SSH access to the target as `root` or an existing sudo user
- Your personal SSH public key uploaded to GitHub (bootstrap.sh does
  this on the first machine automatically — pyinfra pulls authorized_keys
  from `https://github.com/<handle>.keys`)

```bash
uv tool install pyinfra
pyinfra --version   # should print 3.x
```

## Quick start

1. **Edit `inventory.py`** — add your host(s). Each entry needs:
   - SSH-reachable hostname/IP
   - `ssh_user` (`root` for a fresh cloud VM; whatever sudo user exists
     for an existing box)
   - `hostname` (the OS hostname pyinfra will set)

2. **Dry-run** to see what would change:
   ```bash
   pyinfra infra/inventory.py infra/deploy.py --dry
   ```

3. **Provision** a single host:
   ```bash
   pyinfra infra/inventory.py --limit <hostname> infra/deploy.py
   ```

4. **SSH in as the new user** and run the standard chezmoi bootstrap:
   ```bash
   ssh <your-user>@<hostname>
   # (git + github-cli were installed by the deploy — distro-agnostic)
   gh auth login --web
   gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
   cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh --machine personal
   ```

That's it — same end state as a fresh archinstall + bootstrap, just
driven from your mac instead of typed at the target.

## What deploy.py does

1. Detects the distro via the `LinuxName` fact (`Arch`, `Debian`,
   `Ubuntu`, …).
2. Installs base packages via the right package manager: `git`, `openssh`,
   `sudo`, `curl`, `bash`, `ca-certificates`, plus `github-cli`/`gh`.
3. Creates the user named by `$ATHOME_PYINFRA_USER` (or whatever
   `group_data/all.py` resolves to), adds them to `wheel` and `sudo`
   (whichever group exists).
4. Writes `/etc/sudoers.d/90-<user>` with passwordless sudo so the
   chezmoi bootstrap step can install packages without prompting.
5. Pulls authorized_keys from `https://github.com/<gh-handle>.keys`
   (handle from `$ATHOME_PYINFRA_GITHUB_USER`) into
   `~<user>/.ssh/authorized_keys`. Whatever you have on GitHub
   becomes the SSH gate for the new box.
6. Sets the OS hostname via `server.hostname`.
7. Prints a next-steps reminder so you know exactly what 4 commands
   to run after SSHing in as the new user.

## Examples

### Provision a fresh machine (physical or VM)

Works the same way for any SSH-reachable Linux. The most common cases:

- **Physical**: a ThinkPad you just installed Arch on through the
  upstream `archinstall` TUI (see the main [README](../README.md#from-bare-metal-no-os-yet)).
  Enable sshd manually after first boot (`sudo systemctl enable --now sshd`)
  and authorized_keys gets populated by [`scripts/bootstrap.sh`](../scripts/bootstrap.sh)
  once you've run it — from the next machine over, you can then pyinfra
  the box by hostname (`thinkpad.local`).
- **Cloud VM**: a Scaleway / DigitalOcean / Vultr / whatever image. Most
  default to root SSH on a public IP. The deploy.py detects the distro
  (Arch / Debian / Ubuntu) and uses the right package manager.

```bash
# 1. Add the host (edit inventory.py → uncomment a block, set ssh_user
#    to whatever the cloud provider hands you, usually root):
#
#    ("<hostname>.local", { "ssh_user": "root", "hostname": "<hostname>" })

# 2. Sanity-check connectivity (uses your local ~/.ssh/config aliases):
pyinfra infra/inventory.py --limit <hostname> exec -- whoami
# expected: root

# 3. Dry-run — see what would change, no execution:
pyinfra infra/inventory.py --limit <hostname> infra/deploy.py --dry

# 4. Real run:
pyinfra infra/inventory.py --limit <hostname> infra/deploy.py

# 5. Finish as the user (interactive, ~30s for gh auth + ~5 min for the
#    full chezmoi apply):
ssh <your-user>@<hostname>.local
# git + github-cli already installed by the deploy (pacman on Arch, apt on Debian)
gh auth login --web
gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh
```

### Apply config drift to an already-provisioned fleet

After the user has bootstrapped, day-to-day dotfile drift is chezmoi's
job — `czu` on each box pulls + applies. But if you want to push a
SYSTEM-level change (e.g. install a new base package across every box),
re-run deploy.py — it's idempotent, only the changed steps fire:

```bash
pyinfra infra/inventory.py infra/deploy.py
```

### Run an ad-hoc command across the fleet

```bash
# Show kernel version on every host
pyinfra infra/inventory.py exec -- uname -r

# Reboot a specific group
pyinfra infra/inventory.py --limit arch exec -- systemctl reboot --sudo
```

### One-off Python script against the inventory

```bash
# Run a Python file against the inventory, no deploy.py
pyinfra infra/inventory.py path/to/script.py
```

## When to use pyinfra vs chezmoi

| Question | Answer |
| --- | --- |
| "Install a user-level CLI tool on every machine" | chezmoi — add to `home/.chezmoidata/packages.yaml` |
| "Create a new system user" | pyinfra — root-level, can't do in chezmoi |
| "Change my zshrc" | chezmoi — `home/dot_zshrc.tmpl` |
| "Add a system-wide nginx config" | pyinfra — root file under `/etc/` |
| "Sync ~/.claude/skills to a fresh machine" | chezmoi external |
| "Provision a brand-new VM" | pyinfra (initial) + chezmoi (after first login) |

## Adding a new task

`deploy.py` is just a Python script — add operations inline or factor
out reusable bits into separate `*.py` files in this dir and import
them. pyinfra's
[operations docs](https://docs.pyinfra.com/en/3.x/operations.html) list
every primitive: `files.put`, `server.user`, `apt.packages`, etc.

Pattern for a new system-level concern:

```python
# infra/deploy.py
from pyinfra.operations import server

server.shell(
    name="enable + start netbird",
    commands=["systemctl enable --now netbird"],
    _sudo=True,
)
```

For anything user-level, keep it in chezmoi (`home/.chezmoiscripts/run_*`).
The dividing line: if it needs root and lives outside `$HOME`, it's
pyinfra's job; otherwise chezmoi.
