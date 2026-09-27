# Installing athome

The [README](../README.md) has the short version. This page covers the rest:
installing Linux from scratch, what bootstrap does, and every flag and answer
it takes.

- [Fresh Arch install](#fresh-arch-install)
- [Fresh Fedora install](#fresh-fedora-install)
- [Netboot](#netboot)
- [Bootstrap flags](#bootstrap-flags)
- [Unattended installs](#unattended-installs)
- [What bootstrap does](#what-bootstrap-does)
- [Second machine: reusing an SSH key](#second-machine-reusing-an-ssh-key)

On macOS the OS is already there, so skip straight to the README.

Both Linux paths end in the same place: GNOME and GDM as a known-good base,
with niri and Noctalia installed on top and set as your default session.
GNOME stays one click away under the gear icon on the login screen.

## Fresh Arch install

Arch's own `archinstall` does the heavy lifting. Allow about half an hour.

### 1. Boot the ISO and get online

Boot the Arch ISO (or [netboot](#netboot)). Ethernet usually just works. For
Wi-Fi:

```bash
iwctl
[iwd]# device list                     # find your adapter, e.g. wlan0
[iwd]# station wlan0 connect "your-ssid"
[iwd]# exit
ping -c 1 archlinux.org
```

### 2. Run archinstall

```bash
archinstall
```

These are the choices athome expects:

| Setting | Choose |
| --- | --- |
| Disk | Best-effort default layout, **btrfs**, compression and subvolumes on |
| Encryption | **LUKS** on the root partition |
| Bootloader | **systemd-boot** |
| Swap | on (zram) |
| User | your user, **added to sudoers** (root password can stay blank) |
| Profile | **Desktop → GNOME**, greeter **GDM**, open-source GPU drivers |
| Audio | **pipewire** |
| Network | **NetworkManager** |
| Additional packages | `git github-cli` |

Locale, timezone, mirrors and hostname are up to you. There is no niri
profile in archinstall; athome installs niri later. Optionally save the
config to `/tmp/user_configuration.json` for a reproducible reinstall.

Install, then reboot.

### 3. First boot

Enter your LUKS passphrase, log into GNOME, and reconnect to Wi-Fi (the ISO's
Wi-Fi profile doesn't carry over):

```bash
nmcli device wifi connect "your-ssid" --ask     # or: nmtui-connect
```

### 4. Run athome

Follow the [README's install steps](../README.md#install). When it finishes,
log out: GDM now starts niri by default.

## Fresh Fedora install

1. Install **Fedora Workstation** (the GNOME edition) normally, log in, get
   online.
2. `sudo dnf install -y git gh`
3. Follow the [README's install steps](../README.md#install).

Bootstrap notices Fedora and uses dnf instead of pacman/yay. It enables RPM
Fusion and the COPRs that carry niri, Noctalia, Ghostty, Zen, Zed and the
Nerd Font, then installs the `fedora` package list. Portable CLI tools still
come from mise, same as everywhere else.

Fedora package names in
[`packages.yaml`](../home/.chezmoidata/packages.yaml) are best-effort. If one
is wrong on your release, fix it there and run `chezmoi apply` again.

## Netboot

Arch can boot its installer over the network, handy for several machines or
no USB stick. Serve this from a PXE server (an OpenWRT router with TFTP and
DHCP works):

```text
#!ipxe
set arch-net-url https://geo.mirror.pkgbuild.com/iso/latest
kernel ${arch-net-url}/arch/boot/x86_64/vmlinuz-linux \
       initrd=initramfs-linux.img \
       archisobasedir=arch \
       archiso_http_srv=${arch-net-url}/ \
       ip=dhcp \
       net.ifnames=0 \
       BOOTIF=01-${netX/mac:hexhyp}
initrd ${arch-net-url}/arch/boot/x86_64/initramfs-linux.img
boot
```

PXE-boot the target machine and carry on from [step 2](#2-run-archinstall).
See the [Arch wiki on netboot](https://wiki.archlinux.org/title/Netboot).

## Bootstrap flags

| Flag | What it does |
| --- | --- |
| `--machine personal\|work` | Machine type (default `personal`). A work machine gets `~/work/`; a personal one gets `~/personal/` and `~/sidebiz/`. |
| `--non-interactive`, `-y` | Never ask; use defaults, the config file, and `ATHOME_*` variables. Automatic when there's no terminal (CI). |
| `--config <file>` | A TOML file of answers. See [unattended installs](#unattended-installs). |
| `--ref <tag or branch>` | Build from a specific ref, e.g. `--ref v0.3.3`, in a temporary worktree. Default: whatever you cloned. |
| `--no-agents` | Don't sync `~/.config/agents` (shared AI-agent skills and instructions). |
| `--import-ssh-bw <item>` | Restore this machine's SSH key from a Bitwarden note instead of generating one. |
| `--import-from <dir> --import-ssh` | Same, from a backup folder you copied over yourself. |

## Unattended installs

Bootstrap asks a handful of questions when run in a terminal, each with a
sensible default. To skip them, copy
[`examples/bootstrap.toml.example`](../examples/bootstrap.toml.example) to
`bootstrap.local.toml` in the repo root (it's gitignored), fill in what you
care about, and run `./scripts/bootstrap.sh --non-interactive`. Bootstrap
also looks for `~/.config/athome/bootstrap.toml`.

Each answer is taken from, in order: an `ATHOME_*` environment variable, the
config file, what can be worked out automatically (your GitHub handle comes
from `gh`), and finally a question. Answers end up in your private
`~/.config/chezmoi/chezmoi.toml`, never in the repo.

| Answer | Default | Notes |
| --- | --- | --- |
| `machine` | `personal` | Same as `--machine`. |
| `githubHandle` | from `gh` | The only personal value athome really needs. |
| `githubId` | looked up | Builds your `id+handle@users.noreply.github.com` commit email. |
| `personalName` | your handle | Git display name. Deliberately not taken from GitHub, which is often your real name. |
| `workEmail`, `workName` | empty | Identity for repos under `~/work/`. Empty means use the personal one. |
| `sidebizEmail`, `sidebizName` | empty | Identity for repos under `~/sidebiz/`. |
| `useBitwarden` | `true` | `false` for local-only secrets. |
| `bitwardenUrl` | Bitwarden US cloud | EU cloud: `https://vault.bitwarden.eu`, or your Vaultwarden URL. |
| `meshProvider` | `meshnet` | Mesh VPN: `meshnet` (NordVPN), `netbird`, or `none`. |
| `netbirdManagementUrl` | NetBird Cloud | Set when self-hosting ([docs](netbird-cloud.md)). |
| `nordvpnCountry` | NordVPN's choice | e.g. `Netherlands`. |
| `includeAgents` | `true` | Same as `--no-agents` when false. |
| `agentsRepo` | empty | `owner/repo` to sync into `~/.config/agents`. |
| `extraPackages` | empty | Extra packages on top of the curated list ([details](customizing.md#extra-packages-without-editing-packagesyaml-overlay--byo)). |
| `sshKeyAction` | `use` | When a key exists: `use`, `generate` (fresh one) or `reupload`. |

## What bootstrap does

Bootstrap is safe to re-run; every step checks what's already done.

1. Caches your sudo password once and keeps it fresh, so you're asked once.
2. Installs chezmoi into `~/.local/bin`.
3. Generates one SSH key for this machine, `~/.ssh/<hostname>_ed25519`, unless
   you're restoring one. It offers to back it up to Bitwarden.
4. Uploads your public keys to GitHub for pushing and for commit signing,
   skipping any that are already there. This needs the `admin:public_key` and
   `admin:ssh_signing_key` scopes on `gh`; bootstrap offers to add them.
5. Switches the repo and `gh` over to SSH.
6. Writes your answers into `~/.config/chezmoi/chezmoi.toml` and runs
   `chezmoi init --apply`, which lays down every config file but runs none of
   the setup scripts yet.
7. Installs the few things it needs itself: the package manager (yay on
   Arch), `gh`, `rbw`, `gitleaks`, your project folders and mise.
8. Installs the global git hooks and offers to set up Bitwarden (`bw-setup`).

That's the quick baseline. `mise run apply` then does the slow part: the full
package list, all the mise tools, and the desktop setup. Keeping them apart
means the heavy step can be re-run on its own at any time.

## Second machine: reusing an SSH key

Keys are per machine by default: lose a laptop, delete that one key on
GitHub, done. If you'd rather reuse a key (reinstalling the same machine,
say), bootstrap stores new keys in Bitwarden as `ssh/<hostname>_ed25519` and
can restore them:

```bash
./scripts/bootstrap.sh --import-ssh-bw ssh/<hostname>_ed25519
```
