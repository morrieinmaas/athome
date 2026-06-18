# athome

> chezmoi-based, FOSS-first provisioning for macOS and Arch Linux dev machines.
> One command goes from bare OS to fully-configured workstation.

See [AGENTS.md](AGENTS.md) for the operational runbook — the non-obvious
"capture state before apply --force / tag after every shell-stack
change / Noctalia replaces X+Y+Z" rules that future-you (or another
LLM agent) needs to know before touching anything. Component rationale
("why this tool") lives in [Component choices](#component-choices-the-why) below.

## Quick start (fresh machine)

Clone it (no auth needed for a public repo) and run the bootstrap — it does all
the lifting: SSH keygen + upload, `chezmoi apply`, packages, desktop, the lot.
Secrets live in Bitwarden (log in with `bw-setup` at the end).

`gh` is still worth authenticating **on your own account**: bootstrap uploads
your freshly-generated SSH keys to *your* GitHub (so SSH push + commit signing
work). If you'd rather fork-and-personalize first, fork it, then clone your fork.

```bash
# ── one-time prep (fresh box, ~30 seconds) ──
sudo pacman -S --needed --noconfirm github-cli   # Arch
# or (macOS): install nanobrew (the canonical PM), then gh:
#   curl -fsSL https://nanobrew.trilok.ai/install | bash && nb install gh
gh auth login                                     # device flow in browser (your account)

# ── bootstrap (clone, cd, run) ──
git clone https://github.com/morrieinmaas/athome ~/.local/share/chezmoi
cd ~/.local/share/chezmoi
./scripts/bootstrap.sh                          # personal machine (ThinkPad / personal mac)
# or:  ./scripts/bootstrap.sh --machine work    # 9-to-5 employer mac
```

Want a fully hands-off run (no prompts)? Copy
[`examples/bootstrap.toml.example`](examples/bootstrap.toml.example) →
`bootstrap.local.toml` (gitignored), fill it in, and bootstrap auto-loads it
(or pass `--config <path>`). Everything else is still automatic.

> **At the `gh auth login` "HTTPS or SSH?" prompt, choose HTTPS.** A fresh
> machine has no SSH key on GitHub yet — picking SSH would deadlock you.
> Bootstrap generates SSH keys + uploads them + flips you to SSH everywhere
> automatically (see [How HTTPS becomes SSH](#how-https-becomes-ssh-automatically) below).

### From bare metal (no OS yet)

We don't ship a custom installer any more. Arch's own `archinstall` TUI
is well-maintained, supports netboot out of the box, and one boot
through it gets you to a working GNOME desktop + GDM; chezmoi then layers
niri + Noctalia on top and sets niri as your default login session. Less
code to maintain on our side, less schema drift to chase.

**Whole path, end to end:**

| Step | Where | Time | What you do |
| --- | --- | --- | --- |
| 1 | ISO (live) | ~2 min | boot the Arch ISO, connect to network |
| 2 | ISO (live) | ~10–15 min | run `archinstall`, pick options (see recipe below), let it install |
| 3 | installed box | ~5 min | reboot, login to the graphical session, get on wifi |
| 4 | installed box | ~10 min | clone this repo + run `scripts/bootstrap.sh` |

#### Step 1 — boot the ISO + network

Plain Arch ISO (or netboot — see [Netboot variant](#netboot-variant)
below). Connect WiFi (the ISO ships `iwd`):

```bash
iwctl
[iwd]# device list                       # find your wifi adapter name
[iwd]# station wlan0 connect "your-ssid"
[iwd]# exit
ping -c 1 archlinux.org                  # sanity check
```

For ethernet — usually plug-and-play (`systemd-networkd` autostarts +
grabs DHCP). If not: `dhcpcd <interface>`.

#### Step 2 — `archinstall` TUI (the recipe)

```bash
archinstall
```

The TUI is keyboard-driven; arrow keys + Enter. The settings we want:

| Setting | Pick |
| --- | --- |
| Locale | language `en_US`, keyboard `us` (or whatever you type on), encoding `UTF-8` |
| Timezone | `Europe/Amsterdam` (or yours) |
| Mirrors | default |
| Disk configuration | **Best-effort default partition layout** → pick your target disk → **btrfs** → use compression + subvolumes (yes) |
| Disk encryption | **LUKS** → set a passphrase, encrypt the root partition |
| Bootloader | **systemd-boot** |
| Swap | **on** (ZRAM) |
| Hostname | whatever you want (e.g. `thinkpad`) |
| Root password | optional — we use sudo via the regular user, so blank is fine |
| User account | username + password, **add to sudoers** (wheel) |
| Profile | **Desktop** → **GNOME** → its default greeter **GDM** → all open-source GPU drivers. archinstall has **no Niri profile** — niri/Noctalia come from chezmoi, which sets niri as your **default** session; GNOME stays selectable at the GDM gear menu. (`run_once_11-setup-niri-noctalia.sh.tmpl`. Prefer `greetd`+`tuigreet`? It's only auto-enabled if no DM exists — disable GDM first.) |
| Audio | **pipewire** |
| Kernels | `linux` |
| Network configuration | **Use NetworkManager** (so `nmtui-connect` works after reboot) |
| Additional packages | `git github-cli` (GNOME + GDM come from the profile above; niri/Noctalia + everything else land via chezmoi) |
| Timezone | already set above |
| Save config (optional) | export to `/tmp/user_configuration.json` if you want a reproducible re-install later |

Then **Install**. ~10 minutes of pacstrap output. When done: reboot.

#### Step 3 — first boot

At boot, type your LUKS passphrase. **GDM** loads — log into **GNOME**
for now (niri isn't installed until bootstrap runs). You're in a working
Wayland desktop with clipboard, browser, and terminal.

Open a terminal and reconnect WiFi (NetworkManager doesn't carry the
ISO's iwd profiles across the reboot — LUKS-encrypted disk can't read
the ISO's `/var/lib/iwd/`):

```bash
nmtui-connect                      # single-screen wifi picker
# or one-liner:
nmcli device wifi connect "your-ssid" --ask
ping -c 1 archlinux.org
```

#### Step 4 — clone + run bootstrap

```bash
gh auth login --web                # browser-based (you have a GUI now)
gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
cd ~/.local/share/chezmoi
./scripts/bootstrap.sh             # or:  ./scripts/bootstrap.sh --machine work
```

If you're setting up a SECOND machine and want to reuse your existing SSH
identity keys from a prior backup dir (see
[SSH keys](#ssh-keys-auth--commit-signing) for backup contents):

```bash
./scripts/bootstrap.sh --import-ssh-bw ssh/<hostname>_ed25519   # restore from Bitwarden
# without it, a fresh per-machine key is generated + backed up to Bitwarden (default)
```

The whole run is non-interactive — SSH key gen + upload, `chezmoi apply`, all
the chezmoi run-once scripts, package install via pacman (official repos) +
paru (AUR), niri + Noctalia shell setup. Secrets aren't part of bootstrap: log
in to Bitwarden once afterwards with `bw-setup` (registers the device + unlocks; the agent caches your key).

> **Heads-up on that first login:** Bitwarden's cloud blocks plain-password
> logins from CLIs, so a new device must register once with a **personal API
> key** — `bw-setup`/`rbw login` will fail (you'll see an HTTP **400**) until
> you do. Grab the key from the web vault → **Settings → Security → Keys → "View
> API Key"** (`client_id` + `client_secret`), then `rbw register`. After that
> it's master-password-only on this machine. Full walkthrough (incl. EU /
> self-hosted Vaultwarden): [docs/secrets.md](docs/secrets.md).

After bootstrap finishes, **log out (or reboot)** — GDM now defaults to the
**niri** session (set by `run_once_11`); GNOME stays on the GDM gear menu
whenever you want the "it just works" fallback (external displays, floating).

#### Netboot variant

For multi-machine setups or "fresh install over the network", Arch
supports iPXE netboot. The official netboot ISO is at
`https://archlinux.org/releng/netboot/` — drop one of these in a PXE
server (e.g. an OpenWRT router with TFTP + DHCP):

```text
#!ipxe
# Hand-rolled chainload entry for the Arch netboot ISO
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

Boot the target machine into PXE (F12 / BIOS PXE option). It pulls the
ISO over HTTP, lands you at the same Arch ISO shell — then continue
with **Step 2** above. No USB stick needed.

Authoritative reference: <https://wiki.archlinux.org/title/Netboot>.

### From bare metal — Fedora variant

Prefer Fedora? Same end-state, less to think about. **Fedora Workstation** (the
GNOME spin) installs GNOME + GDM that already "just work" (external displays,
floating windows), and chezmoi layers niri + Noctalia on top exactly like the
Arch path.

| Step | What you do |
| --- | --- |
| 1 | Install **Fedora Workstation** the normal way (Anaconda installer, GNOME spin). Reboot, log into GNOME, get on WiFi. |
| 2 | `sudo dnf install -y git github-cli` |
| 3 | `gh auth login` (choose **HTTPS**), then clone + bootstrap exactly as in [Quick start](#quick-start-fresh-machine): `git clone https://github.com/morrieinmaas/athome ~/.local/share/chezmoi && cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh` |

Bootstrap detects Fedora (`dnf` present, no `pacman`) and:

- `run_once_before_01` enables **RPM Fusion** (free + nonfree) + the COPR plugin — **no `paru`** (dnf is native)
- `run_onchange_02` `dnf copr enable`s the niri/Noctalia/ghostty/zen/zed/nerd-font/netbird COPRs, then installs the `fedora.dnf` set; **portable CLI tooling still comes from mise** (unchanged across OSes — it pulls release binaries via ubi/github/cargo/go)
- `run_once_11` installs niri + Noctalia, **respects GDM**, and sets niri as your default login session — GNOME stays one click away at the GDM gear

> Fedora package names in `packages.yaml` (`fedora.copr` + `fedora.dnf`) are best-effort — if a name is off on your release, fix it there and re-run `chezmoi apply`. The portable tools (matugen, et, impala, bandwhich, …) come from mise, not dnf.

### Bootstrap flags

| Flag | Default | Effect |
| --- | --- | --- |
| `--machine personal\|work` | `personal` | Sets chezmoi `machineType`. `personal` covers sidehustle too — per-directory git identity routing handles the distinction. |
| `--config <file.toml>` | auto-discovered | A TOML answers file for a fully hands-off run (see [First-run prompts](#first-run-prompts-all-pre-filled-by-bootstrap-no-manual-entry-needed)). If omitted, bootstrap auto-loads `./bootstrap.local.toml` (gitignored) or `~/.config/athome/bootstrap.toml` when present. Template: [`examples/bootstrap.toml.example`](examples/bootstrap.toml.example). |
| `--ref <tag\|branch>` | the ref you cloned (current `HEAD`, usually `main`) | Builds from what you cloned — "clone the repo and run bootstrap" just works. Pass `--ref vX.Y.Z` to pin a tagged release (it materializes in a throwaway worktree, leaving your checkout untouched); bootstrap also prints the latest tag as an FYI. Refuses a ref that has no chezmoi sources under `home/`. |
| `--no-agents` | off (i.e. include agents) | Skips the `~/.config/agents` skills+instructions sync. Use on machines that don't need agentic tooling. |
| `--import-from <dir>` | none | Restore from a key-backup directory you transported yourself (secondary path; only with `--import-ssh`). The primary cross-machine path is `--import-ssh-bw` (Bitwarden). |
| `--import-ssh` | off | With `--import-from`: install the key found in that backup dir as this machine's `~/.ssh/<hostname>_ed25519`. The GitHub upload step dedupes by fingerprint. |
| `--import-ssh-bw <item>` | none | Restore the SSH key from a Bitwarden secure note (e.g. `ssh/<host>_ed25519`) via `rbw` — the primary cross-machine path. |

### What the bootstrap actually does

1. **Prune leaked worktrees** from any prior killed run (idempotent no-op otherwise).
2. **Pin to the resolved ref** in a temp `git worktree` — your `~/.local/share/chezmoi` stays on `main`.
3. **Cache sudo credentials** once + background keep-alive every 60s, so sudo never re-prompts mid-run.
4. **Install chezmoi** if missing.
5. **Generate the SSH key** (idempotent — skips if it exists): **one ed25519
   keypair** named after the host, `~/.ssh/<hostname>_ed25519` (passphrase-less;
   see [SSH keys](#ssh-keys-auth--commit-signing)). A freshly generated key is
   backed up to **Bitwarden** as a secure note. (age + GPG keygen are retired —
   secrets live in Bitwarden, accessed via `rbw`.)
6. **Proactive `gh` scope check**: if `gh` lacks `admin:public_key` / `admin:ssh_signing_key`, you're offered either a `gh auth refresh` (browser flow) or the manual "print pub-keys + GH settings URL" opt-out — no surprise interactive prompts.
7. **Query-then-upload** SSH keys: queries `gh api user/keys` and `gh api user/ssh_signing_keys` before uploading, so re-runs don't spam duplicates on your GitHub account.
8. **Switch chezmoi repo remote HTTPS → SSH** as soon as keys are on GH, so subsequent `git pull` uses SSH (no HTTPS-password prompt — GitHub doesn't accept those anymore).
9. **Flip `gh config git_protocol` to `ssh`** so future `gh repo clone foo/bar` defaults to SSH.
10. **`chezmoi init --apply`** non-interactively: bootstrap resolves every first-run answer (env var → `--config` TOML → `gh`/public-API derivation) and writes them into your private `~/.config/chezmoi/chezmoi.toml` `[data]` before init, so chezmoi reuses them instead of prompting. (It seeds `[data]` rather than passing `--promptString`, because chezmoi matches `--promptString` on the prompt's display text, not the field name.)
11. **Pin `sourceDir`** so future plain `chezmoi apply` uses the canonical repo location.
12. **Install global git hooks** into the in-repo `.git/hooks/` (matches what chezmoi deploys to `~/.config/git/hooks/`).
13. **Recover from detached HEAD** in the chezmoi repo if any prior weirdness left it that way.
14. **Offer to run `bw-setup`** (Bitwarden) — secrets aren't part of `chezmoi apply`; bootstrap runs the interactive login at the end, and the rbw agent caches your key. A **new device** needs a one-time **personal API key** (`client_id`/`client_secret` from the web vault → Settings → Security → Keys) for `rbw register` — plain-password login returns a **400** by design. After that it's master-password-only. See [docs/secrets.md](docs/secrets.md).

The script is **idempotent** — re-run any time. Each step detects existing state and skips or proceeds accordingly. The per-package install loop means a single bad AUR package logs and continues instead of cascade-failing.

## Day-to-day flow after bootstrap

**Mental model:** chezmoi has two sides — the **source** (this repo's `home/`
dir, the source of truth) and the **target** (your live files in `$HOME`). Every
command compares the two or renders source → target.

```bash
czs                      # = chezmoi status     (what differs — START HERE)
czd                      # = chezmoi diff       (the actual line-by-line diffs)
cza                      # = chezmoi apply -v    (render source → $HOME, no pull)
czu                      # = chezmoi update -v   (git pull + apply, the daily sync)
cze ~/.zshrc             # = chezmoi edit        (edit a tracked file's SOURCE, then cza)
czc                      # = chezmoi cd          (jump into the source repo)
czdoc                    # = chezmoi doctor      (health check)
chezmoi init             # re-render the config from .chezmoi.toml.tmpl (clears the
                         #   "config file template has changed" warning after edits)
```

**The loop you'll actually use:** `czs` (what changed?) → `czd` (show me) →
`cza` (apply). Reach for `czu` when you also want to pull remote changes.

**Reading `czs` output** — two columns per line. Col 1 = "did *you* change the
live file?"; col 2 = "what would `apply` do?". Codes: `A`dd, `M`odify, `D`elete,
`R`un (script), space = nothing. Examples:

- `MM .config/nvim/lazy-lock.json` — modified on both sides: `lazy.nvim` bumped
  plugins locally **and** the source pins different versions. Routine churn (see
  [docs/lazy-lock-workflow.md](docs/lazy-lock-workflow.md)), not a problem.
- ` R .chezmoiscripts/00-…` — a script `apply` would **R**un; expected, scripts
  re-run by design (most are idempotent / no-op on a settled machine).

> If `czu`/`czd` ever looks frozen, you're probably inside the `less` pager —
> press `q`. Diffs render stock (no delta); `git difftool` gives difftastic
> side-by-side on demand.

## Reset / start over

To re-test a clean bootstrap, use [`scripts/teardown.sh`](scripts/teardown.sh)
— **dry-run by default**, tiered, and destructive runs need a typed confirm:

```bash
./scripts/teardown.sh --state                 # preview: reset chezmoi state only
./scripts/teardown.sh --dotfiles --execute    # + remove managed files (type to confirm)
./scripts/teardown.sh --all --execute         # + uninstall mise/nanobrew/rbw/SSH key
```

`--state` < `--dotfiles` < `--all` (cumulative). The source repo is always
preserved so you can re-bootstrap. Safer still: run `bootstrap.sh` in a fresh
macOS user account or VM rather than wiping your working machine.

## What's already wired up after bootstrap

`run_03-setup-project-dirs.sh.tmpl` pre-creates the per-context roots based on
`machineType` (macOS + Linux):

- **Personal machine** (`--machine personal`): `~/personal/` + `~/sidebiz/`
  (each with a seeded `.envrc` containing `use ctx <name>`)
- **Work machine** (`--machine work`): `~/work/` (with seeded `.envrc`)

**One tree per context, no `~/Code` anchor.** Single rule: the directory
IS the context. Cloning a sidebiz repo under `~/personal/` is now a
visible, hard-to-miss mistake instead of a silent wrong-email commit.

### Per-project secrets via `direnv` + `rbw` (Bitwarden)

[`home/dot_config/direnv/direnv.toml`](home/dot_config/direnv/direnv.toml) ships
with a `[whitelist]` block covering `~/personal`, `~/work`, `~/sidebiz`.
Any `.envrc` placed inside those trees auto-loads on `cd`
without needing `direnv allow` per repo — direnv just trusts the
prefix. (Outside those trees, `direnv allow` still applies as a safety
net.)

[`home/dot_config/direnv/direnvrc`](home/dot_config/direnv/direnvrc) ships custom
layouts that resolve secrets from **Bitwarden via `rbw`** at `cd`-time
(nothing on disk, nothing in `~/.zshenv`). Run `rbw unlock` once per session;
the rbw agent caches the key (it's what replaced gpg-agent):

```bash
# usage in any .envrc:
use ctx work         # bundle: aws + github + openrouter (work context)
use ctx personal     # bundle: aws + github + openrouter (personal context)
use ctx sidebiz      # bundle: aws + github + openrouter + stripe (sidebiz)

# or one-off pulls (entry = Bitwarden item name):
use_rbw OPENAI_API_KEY personal/openai-api-key
use_rbw DATABASE_URL   database uri            # → rbw get --field uri database
use_aws  work
use_openrouter personal
```

Starter templates live in [`examples/envrc-{personal,work,sidebiz}.example`](examples/) —
copy whichever fits, drop into your repo as `.envrc`:

```bash
cp ~/.local/share/chezmoi/examples/envrc-personal.example ~/personal/some-new-repo/.envrc
cd ~/personal/some-new-repo            # direnv loads it automatically (~/personal whitelisted)
echo "$GITHUB_TOKEN"                   # → fetched from Bitwarden (rbw get personal/github-token)
```

The example files are exactly one line: `use ctx <context>`. The
identity-aware git routing ([Identity model](#identity-model) below) is
already in `~/.gitconfig`'s `includeIf` blocks — no extra config
needed per repo.

### Identity-aware shells, git, and SSH

| What | How | Where |
| --- | --- | --- |
| Git author email + signing key per directory | `includeIf "gitdir:~/work/"` etc. in `~/.gitconfig` | [`home/dot_gitconfig.tmpl`](home/dot_gitconfig.tmpl) |
| SSH key | one `~/.ssh/<hostname>_ed25519` (auth + signing); per-tree *email* still routes via `includeIf` | [`home/private_dot_ssh/private_config.tmpl`](home/private_dot_ssh/private_config.tmpl) |
| HTTPS GitHub URLs → SSH | `url."git@github.com:" insteadOf https://github.com/` catch-all in `~/.gitconfig` | `home/dot_gitconfig.tmpl` |
| SSH commit signing | `gpg.format = ssh` reuses each identity's ed25519 key; verified via `allowed_signers` | [`home/dot_gitconfig-*.tmpl`](home/dot_gitconfig-personal.tmpl) |
| rbw agent caches the Bitwarden key | so direnv's per-cd `rbw get` calls don't re-prompt — `rbw unlock` once per session | [`home/dot_config/direnv/direnvrc`](home/dot_config/direnv/direnvrc) |

### Wayland desktop session (Linux)

archinstall's GNOME spin installs GNOME + GDM at install time (see Step 2 in
[From bare metal](#from-bare-metal-no-os-yet)). chezmoi then layers:

- [`home/dot_config/niri/`](home/dot_config/niri/) — keybindings, output config, autostart
- [`home/dot_config/greetd/config.toml`](home/dot_config/greetd/config.toml) —
  tuigreet config (only used if you swap GDM → greetd; GDM stays by default)
- [`home/.chezmoiscripts/run_once_11-setup-niri-noctalia.sh.tmpl`](home/.chezmoiscripts/run_once_11-setup-niri-noctalia.sh.tmpl)
  installs niri + Noctalia, **respects your DM (GDM)** and sets niri as the
  default login session, enables power-profiles-daemon + bluetooth
- The GNOME alt session is installed as additional packages during
  archinstall and surfaced via tuigreet's session picker

### CLI environment

| Tool | Source | Notes |
| --- | --- | --- |
| zsh + zinit | [`home/dot_zshrc.tmpl`](home/dot_zshrc.tmpl) | direnv hook auto-loaded via `zinit snippet OMZP::direnv` |
| `dogenpunk` zsh theme | `zinit snippet OMZT::dogenpunk` in `home/dot_zshrc.tmpl` | git-status-aware prompt |
| zoxide + shell completions | [`home/dot_zsh/completions.zsh`](home/dot_zsh/completions.zsh) | `z <dir>` instead of `cd ~/long/path` |
| themes | no external fetches | bat, bottom, and zed use **built-in** gruvbox; ghostty (`Everforest Light/Dark - Medium`), tmux (native everforest pill bar), and nvim (`sainnhe/everforest`) are themed inline — all OS dark/light-aware |
| `mise` (toolchain) | curl-bootstrapped via `mise.run` in [`run_once_06-setup-mise.sh`](home/.chezmoiscripts/run_once_06-setup-mise.sh) | **owns ALL portable CLI tooling** on both OSes — runtimes (node/go/rust/deno/bun) + the modern-CLI set (rg, fd, bat, eza, fzf, jq, lazygit, delta, ruff, …). Declared in [`home/dot_config/mise/config.toml`](home/dot_config/mise/config.toml); per-project `.mise.toml` / `.tool-versions` override |
| portable CLI tools | [`home/dot_config/mise/config.toml`](home/dot_config/mise/config.toml) `[tools]` | bare names resolve via mise's registry — no more per-OS name translation (choose vs choose-rust, jj vs jujutsu, taplo vs taplo-cli) |
| native / GUI / system packages | [`home/.chezmoidata/packages.yaml`](home/.chezmoidata/packages.yaml) | only the non-portable residue: casks, the niri/Noctalia desktop stack, fonts, libraries, podman/syncthing, and `uv` (Python) |

### Terminal TUIs, launchers & apps

One keystroke from tmux to the things you'd otherwise alt-tab for. The
launchers are `display-popup`s wired in [`tmux.conf`](home/dot_config/tmux/tmux.conf);
their binaries come from mise.

| What | Key / command | Notes |
| --- | --- | --- |
| **gh-dash** — GitHub PR/issue dashboard | `prefix g` | `T` inside it spawns ENHANCE in a new window |
| **ENHANCE** — GitHub Actions TUI | `prefix G` | both read `gh`'s token — no API keys in config |
| **slk** — Slack TUI | `prefix M` | browser-session auth; tokens never tracked |
| **status-bar icon picker** | `prefix P` | pick a light-mode + dark-mode glyph from ~all emoji; the icon follows the OS appearance. File-free (built from stdlib `unicodedata`); two static pill colours editable in [`pet.sh`](home/dot_config/tmux/executable_pet.sh) |
| **Eternal Terminal** (`et`) | `et <host>` | reconnecting remote shell; server on every machine over the NetBird mesh ([`run_once_18`](home/.chezmoiscripts/run_once_18-setup-eternal-terminal.sh.tmpl)) |
| **impala** — Wi-Fi TUI (Linux) | `impala` | talks to `iwd`; backend wired by [`run_once_15`](home/.chezmoiscripts/run_once_15-setup-wifi-backend.sh.tmpl) |
| **Zen** — default browser | — | extensions reinstalled declaratively + set as default by [`run_once_after_20`](home/.chezmoiscripts/run_once_after_20-setup-zen.sh.tmpl) |

## Multi-machine provisioning with pyinfra

Once you have **more than one** machine, the bootstrap-per-box pattern starts to
chafe. The [`infra/`](infra/) directory contains a [pyinfra](https://pyinfra.com/)
deploy that runs FROM your laptop, SSHes to remote hosts, and does the
root-level setup (user creation, base packages, authorized_keys, hostname)
that chezmoi can't do. After it finishes you SSH in as the new user and
run the standard chezmoi bootstrap chain — same end-state as a fresh
archinstall, just driven remotely.

```bash
# one-time on your laptop
uv tool install pyinfra

# 1. edit infra/inventory.py — add a host tuple
#    ("<hostname>.local", { "ssh_user": "root", "hostname": "<hostname>" })

# 2. dry-run to preview
pyinfra infra/inventory.py infra/deploy.py --dry

# 3. real run, single host
pyinfra infra/inventory.py --limit <hostname> infra/deploy.py

# 4. SSH in as the new user, finish with chezmoi
ssh <your-user>@<hostname>.local
sudo pacman -S --needed github-cli git
gh auth login --web
gh repo clone morrieinmaas/athome ~/.local/share/chezmoi
cd ~/.local/share/chezmoi && ./scripts/bootstrap.sh
```

| Question | Tool |
| --- | --- |
| "install a portable CLI on every machine" | mise (`dot_config/mise/config.toml`) |
| "install a GUI app / font / system package" | chezmoi (`.chezmoidata/packages.yaml`) |
| "create a system user / set sudoers" | pyinfra |
| "edit my zshrc" | chezmoi (`dot_zshrc.tmpl`) |
| "push a system-wide nginx config" | pyinfra |
| "provision a brand-new VM" | pyinfra (initial) → chezmoi (after first login) |

See [`infra/README.md`](infra/README.md) for the full task reference, more
examples, and patterns for extending `deploy.py`.

## Release / branch model

- `main` — development tip; CI must be green, daily work pushes here.
- Tags (`vX.Y.Z`) — known-good snapshots, used by `bootstrap.sh --ref` for fresh-machine pinning and as rollback targets.
- No `stable` branch. For a two-machine personal setup it's overkill; if you ever grow to ≥3 machines, promote a tag to `stable` and set `chezmoi.toml [git] branch = "stable"`.

Emergency rollback to a known-good tag:

```bash
chezmoi cd
git fetch --tags
git checkout v0.1.0        # or any tag you've cut since
exit
chezmoi apply
# back to current:
chezmoi cd && git checkout main && exit && chezmoi apply
```

## How HTTPS becomes SSH automatically

A fresh machine clone uses HTTPS because no SSH key is on GitHub yet. Bootstrap upgrades you to SSH end-to-end as soon as the keys are uploaded:

1. `gh repo clone` (HTTPS, via gh's stored token)
2. Bootstrap generates one SSH keypair, `~/.ssh/<hostname>_ed25519`
3. Bootstrap uploads the pub key to GitHub (auth + signing variants)
4. Bootstrap rewrites the chezmoi repo's `origin` URL: `https://github.com/...` → `git@github.com:...`
5. `gh config set git_protocol ssh` — future `gh repo clone` defaults to SSH
6. The chezmoi-deployed `~/.gitconfig` adds a `url."git@github.com:" insteadOf https://github.com/` catch-all so every clone is SSH. Per-directory git *email* still varies via `includeIf`; the single key signs all of them.

Net result: HTTPS for one clone, SSH end-to-end for everything after — and the right SSH identity per directory.

## SSH keys (auth + commit signing)

GPG is gone (D4): there's no GPG key to generate, no `pass` store to unlock.
`bootstrap.sh` generates **one ed25519 SSH key per machine**, named after the
host (`~/.ssh/<hostname>_ed25519`) — passphrase-less (`ssh-keygen -N ""`) — and
uploads it to GitHub as both an **auth** and a **signing** key. One GitHub
account → one key; naming it after the host means GitHub's key list shows which
box each key is from, so revoking a single machine is trivial. Because
`~/.ssh/config` pins it (`IdentityFile` + `IdentitiesOnly`), ssh, git push, and
SSH commit signing read it directly — **no ssh-agent or gpg-agent needed**.

Commit signing uses `gpg.format = ssh`; the per-directory `includeIf` blocks
still set the right **email** per tree (`~/work`·`~/personal`·`~/sidebiz`), and
all three emails map to the one key in `~/.config/git/allowed_signers`. Add a
second GitHub account later → add a second key + host alias; until then, one key
is the real-world default.

### SSH key backup + cross-machine recovery (Bitwarden)

Keys are **per-machine by default** — lose a laptop, revoke that one GitHub key,
done. When bootstrap generates a new key it stores the **private key in
Bitwarden** as a secure note named `ssh/<hostname>_ed25519` (via the `bw` CLI) —
encrypted at rest, synced, recoverable anywhere. No key-transport dance
(wormhole/wush/scp retired).

> If `bw` isn't installed yet at keygen time (it lands later via `chezmoi
> apply`), bootstrap prints the exact `jq … | bw encode | bw create item`
> one-liner to run once it is.

Restore that key on another machine (reusing a key, or reinstalling the same
box):

```bash
./scripts/bootstrap.sh --import-ssh-bw ssh/<hostname>_ed25519
```

It pulls the note via `rbw get`, installs it as this machine's
`~/.ssh/<hostname>_ed25519`, and regenerates the `.pub`. (The older
`--import-from <dir> --import-ssh` path still works if you'd rather restore from
a key-backup directory you transported yourself.)

## Agentic skills sync (`~/.config/agents`)

Bootstrap clones the agent-agnostic skills repo you name in the `agentsRepo` prompt (`owner/repo`, e.g. `<handle>/skills`) into **`~/.config/agents`** via a chezmoi external — gated by `includeAgents` (on by default; `--no-agents` to skip). Leave `agentsRepo` empty to sync nothing. Sensitive bits (`settings.json`, `projects/`, `sessions/`, `history.jsonl`) are excluded by that repo's own `.gitignore`.

`~/.config/agents` is the **canonical, tool-neutral home** — nothing is Claude-locked. What lands there:

- `~/.config/agents/CLAUDE.md` (and/or `AGENTS.md`) — global instructions
- `~/.config/agents/AGENTIC-SYSTEMS.md`, `RTK.md` — referenced from the instructions
- `~/.config/agents/skills/*/SKILL.md` — skills following the SKILL.md spec
- `~/.config/agents/agents/` — agent prompts

Who reads it:

- **opencode** — directly; `dot_config/opencode/config.json` points `instructions` at `~/.config/agents/CLAUDE.md`
- **Claude Code** — expects `~/.claude`, so `run_once_after_22-link-agents.sh` **symlinks** the shared items (`CLAUDE.md`, `skills`, `agents`, …) from `~/.config/agents` into `~/.claude`. Claude's own runtime (`settings.json`, `projects/`, `sessions/`, `history.jsonl`) stays in `~/.claude`, untouched.
- **nvim / aider / cursor / Windsurf** — point them at `~/.config/agents` per their own conventions

**Nothing IDE-specific is installed.** This is purely a sync of the agent-agnostic configuration.

To skip: `./scripts/bootstrap.sh --no-agents`.

## First-run prompts (all pre-filled by bootstrap, no manual entry needed)

Each answer is resolved in priority order: **`ATHOME_*` env var → `--config` TOML
file → derivation (`gh`/public API) → one interactive prompt**. For a fully
hands-off run, copy [`examples/bootstrap.toml.example`](examples/bootstrap.toml.example)
to a gitignored `bootstrap.local.toml`, fill it in, and bootstrap picks it up
automatically (or pass `--config <path>`). The work/sidebiz identity fields
(`workEmail`, `workName`, `sidebizEmail`, `sidebizName`) live there too — kept
out of the repo, written only into your private `~/.config/chezmoi/chezmoi.toml`.

| Prompt | Default | Notes |
| --- | --- | --- |
| `machineType` | `personal` (or `work` via `--machine work`) | `personal` covers sidehustle too |
| `hostname` | system hostname | Used in shell prompt, SSH-key comment, etc. |
| `githubHandle` | **auto** | Your GitHub username — bootstrap takes it from your logged-in `gh` account (`gh api user`), else `ATHOME_GITHUB_HANDLE` env, else one prompt. The only personal value athome needs. Drives the noreply email + the `url.insteadOf` namespace rewrite |
| `githubId` | **auto** | Derived by bootstrap from the handle's **public** GitHub profile (`gh api users/<handle>`, with a no-`gh` curl fallback) — you never type it. Builds `<id>+<handle>@users.noreply.github.com` (plain `<handle>@…` if it can't be resolved) |
| `personalName` | empty | Personal git display name; `ATHOME_PERSONAL_NAME` env (empty = your handle). Not derived — GitHub's display name is often a real name, which we deliberately keep out of commits |
| `netbirdManagementUrl` | empty (NetBird Cloud SaaS) | Set to `https://netbird.example.com` when self-hosting; see [docs/netbird-cloud.md](docs/netbird-cloud.md) |
| `nordvpnCountry` | empty (NordVPN picks best) | E.g. `Netherlands` to pin |
| `includeAgents` | `true` (or `false` via `--no-agents`) | Master on/off for the `~/.config/agents` skills sync |
| `agentsRepo` | empty | Repo synced into `~/.config/agents` as `owner/repo`; `ATHOME_AGENTS_REPO` env. Empty = nothing synced (a fork points it at its own, or skips) |

## Security model (defense in depth, primary-control-first)

The primary control is that **secrets never enter a tracked file** — they're
pulled from Bitwarden at runtime via `rbw`. The layers below are backstops:

| Layer | What it catches | Where it lives |
| --- | --- | --- |
| `.gitignore_global` covers `.env*` / keys / certs / tfvars | secret-bearing files staged by accident | chezmoi-managed `~/.gitignore_global` |
| filename guard in pre-commit hook | obvious private-key filenames staged by accident | chezmoi-managed `~/.config/git/hooks/pre-commit` |
| `gitleaks` on every commit (global hook) | embedded secret material in any tracked file | same hook, runs `gitleaks protect --staged` |
| `gitleaks` GH Action on push + PR + weekly cron | anything that slipped past local guards | `.github/workflows/secrets-scan.yml` |

Plus: chezmoi sets `core.hooksPath = ~/.config/git/hooks` so the above runs in **every** repo on the machine. Bypass requires explicit `SKIP_GIT_HOOKS=1 git commit` — easy to grep for in shell history.

### How secrets actually flow

```text
Bitwarden (source of truth)  ← cloud vault; rbw agent caches the key after `rbw unlock`
        │
        │  rbw get <item>   (rbw replaces pass + gpg-agent)
        ▼
~/.config/direnv/direnvrc    ← `use_rbw`, `use_aws`, `use_ctx` helpers
        │
        │  on `cd` into project
        ▼
$ENV_VARS                    ← live only in shell; never on disk
```

If a tool *insists* on a real file (e.g. `~/.aws/config`), chezmoi manages it
mode-0600 via the `private_` prefix and it carries **region/non-secret config
only** — credentials still come from Bitwarden at runtime via direnv + rbw.
Example: `private_dot_aws/config.tmpl`. If you ever need a `.env` on disk,
generate it locally from Bitwarden, keep it gitignored (Action A covers
`.env*`), and regenerate rather than persist.

> **age was dropped (D5):** there are no `encrypted_*` files in this repo and,
> with `pass` gone, no driver for encrypted-files-in-repo. To reuse an SSH key
> across machines it's stored in Bitwarden — restore with
> `./scripts/bootstrap.sh --import-ssh-bw ssh/<hostname>_ed25519` — not chezmoi encryption.

## Identity model

Three git identities, picked automatically by directory. Actual email
values are prompted at `chezmoi init` and cached in
`~/.config/chezmoi/chezmoi.toml` — never hardcoded in the template, so
the repo stays publishable.

| Context | Dir prefix | Name | Email source |
| --- | --- | --- | --- |
| personal | `~/personal/` | _(your handle, or `personalName`)_ | derived from handle + `githubId`: `<id>+<handle>@users.noreply.github.com` |
| work | `~/work/` | _(your name)_ | `workEmail` chezmoi prompt (set via `ATHOME_WORK_EMAIL` env or interactive) |
| sidehustle | `~/sidebiz/` | _(your name)_ | `sidebizEmail` chezmoi prompt (set via `ATHOME_SIDEBIZ_EMAIL` env or interactive) |

Single tree per context — the directory IS the identity, no per-org
overrides to maintain. Cloning a sidebiz repo under `~/personal/` will
*look wrong* on `ls`, which is the whole point: structural prevention
of the "I committed under the wrong email" incident.

SSH host alias per identity: clone as `git@github-{personal,work,sidebiz}:<owner>/<repo>.git` and the correct key + identity engage. The pre-rendered `~/.gitconfig` ships with `url.insteadOf` rewrites for the common case.

## Layout

The repo splits into two halves: the project root (docs, scripts,
infra, CI) and `home/` (the chezmoi source). `.chezmoiroot` at the root
points chezmoi at `home/`.

```text
athome/
├── README.md
├── AGENTS.md                         # operational runbook for LLM agents
├── LICENSE
├── .gitignore                        # paranoid: blocks *_ed25519, *.pem, .env, ...
├── .gitleaks.toml                    # secret-scan ruleset + repo-aware allowlist
├── .pre-commit-config.yaml           # pre-commit framework (gitleaks + actionlint + shellcheck …)
├── .chezmoiroot                      # contains "home" — points chezmoi at the source dir
├── .github/workflows/                # secrets-scan + chezmoi-verify CI
│
├── scripts/
│   └── bootstrap.sh                  # one-shot machine bootstrap (--machine, --ref, --no-agents, --import-from, --import-ssh)
├── infra/                            # pyinfra-based multi-machine deploy
├── examples/                         # sample .envrc files per identity (use ctx personal|work|sidebiz)
├── docs/                             # secrets, spatial-model, lazy-lock, netbird-cloud
│
└── home/                             # ← chezmoi source root (everything below applies to ~/)
    ├── .chezmoi.toml.tmpl            # first-run prompts → ~/.config/chezmoi/chezmoi.toml
    ├── .chezmoidata/packages.yaml    # explicit per-platform package lists
    ├── .chezmoiexternal.toml.tmpl    # declarative externals: zinit, TPM, ~/.config/agents
    ├── .chezmoiignore                # what NOT to deploy (OS-gated)
    ├── .chezmoiscripts/              # ordered setup scripts (sudo / podman / niri-noctalia / project-dirs / …)
    │
    ├── dot_zshrc.tmpl                # tiny: bootstraps zinit, sources ~/.zsh/*
    ├── dot_zsh/                      # path / exports / aliases / functions / completions
    ├── dot_zshenv, dot_zprofile      # minimal placeholders that nuke any stale legacy versions
    ├── dot_gitconfig.tmpl            # identity routing via includeIf + core.hooksPath
    ├── dot_gitconfig-{personal,work,sidebiz}.tmpl
    ├── dot_gitignore_global          # OS noise + per-machine state + secret-file guards (Action A)
    │
    ├── dot_config/
    │   ├── git/
    │   │   ├── allowed_signers.tmpl  # auto-populates from ~/.ssh/<id>_ed25519.pub
    │   │   └── hooks/{pre-commit,commit-msg}   # global hooks (gitleaks + convcommits)
    │   ├── direnv/{direnvrc,direnv.toml}        # use_rbw / use_aws / use_ctx helpers (Bitwarden via rbw)
    │   ├── ghostty/config.tmpl       # auto dark/light everforest (window-decoration gated to Linux)
    │   ├── tmux/tmux.conf            # TPM + sessionx + smart-manager + opensessions + everforest pill bar + icon picker (prefix P) + TUI launchers (gh-dash/slk)
    │   ├── nvim/                     # lazy.nvim + mason-org LSP + snacks (picker/explorer) + harpoon2 + bufferline + treesitter + which-key + noice (cmdline popup) + persistence (sessions) + render-markdown + conform + everforest
    │   ├── opencode/config.json      # wired to ~/.config/agents (CLAUDE.md + skills)
    │   ├── jj/config.toml.tmpl       # Jujutsu version control (templated identity)
    │   ├── gh/config.yml             # git_protocol: ssh; aliases co/cr/prv
    │   ├── niri/config.kdl           # Linux: scrollable-tiling Wayland compositor
    │   ├── (noctalia configs land in ~/.config/noctalia/ at runtime, not tracked here)
    │   ├── fastfetch/                # Linux: shows "Arch Linux"
    │   ├── greetd/                   # Linux: tuigreet greeter
    │   ├── mise/config.toml          # ALL portable tooling: runtimes + the modern-CLI set (NOT python — uv owns that)
    │   └── {bat,btm,lazygit,yazi}/
    │
    ├── private_dot_aws/config.tmpl   # region only — creds come from Bitwarden via direnv + rbw
    ├── private_dot_ssh/private_config.tmpl  # github host aliases (file 600, dir 700)
    │
    └── Library/LaunchAgents/com.local.caps-to-control.plist   # macOS only
```

> **Why the `home/` split?** chezmoi convention is to put template
> files at the repo root, which scales poorly: ~80 `dot_*` files start
> drowning the actual project files (README, scripts, infra, CI). The
> `.chezmoiroot` file ([chezmoi docs](https://www.chezmoi.io/reference/special-files-and-directories/chezmoiroot/))
> nests them under `home/` without changing chezmoi's behavior — the
> destination tree on disk is unchanged, only the source layout is
> tidier. Existing checkouts pick this up on the next `git pull +
> chezmoi apply` automatically.

## Daily verification commands

```bash
chezmoi doctor                          # config + binary sanity
gitleaks detect --redact --no-banner    # repo-wide secret sweep
chezmoi diff                            # what would change on next apply
pre-commit run --all-files              # the full lint suite

# end-to-end smoke
zsh -ic 'echo $0'   ; rbw get personal/test ; podman ps ; tmux -V ; nvim --headless +q
```

## Adding a new secret

```bash
rbw unlock                              # once per session (agent caches the key)

# 1. Store it in Bitwarden (the item name is what .envrc references)
rbw add work/some-api-key

# 2. Reference it in your project's .envrc
echo 'use_rbw SOME_API_KEY work/some-api-key' >> .envrc
direnv allow

# 3. NEVER do this:
echo 'SOME_API_KEY=sk-...' > .env      # .gitignore_global + pre-commit hook block this
```

## Updating dependencies

```bash
czu                         # = chezmoi update -v   (pull + apply, daily flow)
czdoc                       # = chezmoi doctor
chezmoi managed | head      # what chezmoi owns

# portable CLI tooling (the bulk of it) — mise owns versions/upgrades:
mise upgrade                # bump all [tools] to latest; `mise outdated` to preview
mise doctor                 # health-check the toolchain

# native/GUI/system packages — the OS PM:
nb upgrade                  # macOS (nanobrew)
paru -Syu                   # Arch

# force-refresh externals (zinit, TPM, ~/.config/agents) on next apply:
chezmoi state delete-bucket --bucket=entryState
```

## Component choices (the why)

Rationale for the headline picks (the *what* is in [CLI environment](#cli-environment)
and the [layout](#layout); this is the *why*):

| Component | Why this one |
| --- | --- |
| **mise** (toolchain) | One cross-platform installer for all portable tooling — kills the per-OS name-translation table. Versioned, upgradable, uninstallable; per-project `.mise.toml` overrides. `uv` keeps Python. |
| **nanobrew** (`nb`, macOS PM) | Homebrew-compatible (same formulae/casks/taps) but ~Zig-fast, installs into `/opt/nanobrew` without touching `/opt/homebrew`. One macOS PM instead of two. |
| **Ghostty** (terminal) | GPU-accelerated and native-feeling on both OSes; simple `key=value` config (no Lua/YAML); auto dark/light following the OS; Kitty image protocol for yazi previews; MIT. |
| **tmux** | Everywhere over SSH, infinitely scriptable, our muscle-memory default. (zellij was dropped — tmux won outright.) |
| **Neovim** (editor) | Lua config deployed identically everywhere via chezmoi: lazy.nvim + LSP (mason) + snacks (picker/explorer) + treesitter + conform + which-key + everforest. Kept native (not mise) because plugins compile against system lua/tree-sitter. **Zed** ships alongside as the GUI option. |
| **OpenCode** (AI agent) | Reads `~/.config/agents/CLAUDE.md` + `skills/*/SKILL.md` (the agent-agnostic dir, symlinked into `~/.claude` for Claude Code); model via OpenRouter, key pulled from Bitwarden through direnv + rbw. |
| **Podman** over Docker | Rootless, daemonless, drop-in `DOCKER_HOST` socket; `lazydocker`/compose work unchanged. |
| **NetBird** mesh / **rbw** secrets / **Noctalia** shell | Each is the FOSS-first pick with a no-penalty self-host or open-source escape hatch — see the relevant sections above. |

## Testing & CI

| What | Where | Runs |
| --- | --- | --- |
| **shellcheck** (`-S warning -x`) | `.pre-commit-config.yaml` hook | `lint-test` workflow + `mise run lint` |
| **bats** unit tests | [`test/*.bats`](test) | `lint-test` workflow + `mise run test` |
| **e2e bootstrap** (full Arch install) | [`test/e2e/`](test/e2e) | `e2e` workflow + `mise run e2e` |
| **secrets scan** (gitleaks) | global hook + workflow | every commit + `secrets-scan` workflow |

The **e2e** spins up a clean Arch container and runs the *real* bootstrap
unattended — no `gh` login, no Bitwarden, dummy answers from
[`test/e2e/bootstrap.toml`](test/e2e/bootstrap.toml), and `ATHOME_CI=1` so the
systemd/GUI/hardware scripts skip. CI feeds mise the auto-provided `GITHUB_TOKEN`
to dodge the anon rate limit. Run it locally with `mise run e2e` (`docker`/`podman`;
on Apple Silicon it builds the amd64 image under emulation). Fedora + a macOS
runner are the planned follow-on legs.

**Task wrapper:** common commands are mise tasks (in [`mise.toml`](mise.toml)) —
`mise run bootstrap`, `update`, `status`, `lint`, `test`, `e2e`, `teardown`
(`mise tasks` lists them). No extra tool: mise is already the dependency. Each
task just wraps a script you can still call directly.

## Further reading

- [docs/secrets.md](docs/secrets.md) — Bitwarden + rbw setup (register/login, EU/Vaultwarden, config knobs, daily use)
- [docs/spatial-model.md](docs/spatial-model.md) — tmux + nvim mental model for VSCode refugees (the seven everyday actions translated)
- [docs/lazy-lock-workflow.md](docs/lazy-lock-workflow.md) — committing `lazy-lock.json` for reproducible nvim plugin pins
- [docs/netbird-cloud.md](docs/netbird-cloud.md) — NetBird Cloud free-tier sign-in + the self-host exit (Scaleway + Caddy + Docker Compose)
