# athome

My dotfiles and machine setup. It takes a fresh macOS, Arch or Fedora machine
to a ready-to-use development workstation, and keeps several machines in sync
afterwards. Built on [chezmoi](https://www.chezmoi.io/) and
[mise](https://mise.jdx.dev/), and open-source wherever there's a choice.

## What you get

- **Shell:** zsh with starship, zoxide, fzf, direnv, and a modern CLI set
  (ripgrep, fd, bat, eza, lazygit and friends) installed through mise.
- **Terminal and editor:** Ghostty, tmux, Neovim, with Zed as the GUI editor.
  One `theme` command recolours all of them at once.
- **Git that knows who you are:** repos under `~/personal`, `~/work` and
  `~/sidebiz` commit with the right name and email automatically, signed with
  your SSH key.
- **Secrets from Bitwarden:** pulled into your environment when you `cd` into
  a project, never written to disk.
- **On Linux, a tiling desktop:** [niri](https://github.com/niri-wm/niri) with
  the [Noctalia](https://github.com/noctalia-dev/noctalia-shell) shell, with
  plain GNOME kept as a fallback session.
- **Guard rails:** a global git hook runs gitleaks on every commit in every
  repo.

## Before you start

- You need a GitHub account. Setup uploads an SSH key to it so you can push
  and sign commits.
- Secrets are meant to live in [Bitwarden](https://bitwarden.com/) (or a
  self-hosted Vaultwarden). That's optional: say no when asked, or see
  [docs/secrets.md](docs/secrets.md).
- Setup uses `sudo`, creates SSH keys, and installs software from the AUR,
  Fedora COPRs, nanobrew and the mise registry. Read [SECURITY.md](SECURITY.md)
  if that matters to you.

If you want to make it your own, fork it first and clone your fork below.

## Install

Starting from a blank disk? Install the OS first:
[Arch](docs/install.md#fresh-arch-install) or
[Fedora](docs/install.md#fresh-fedora-install). macOS needs nothing extra.

**1. Install the GitHub CLI and log in**

```bash
# macOS
curl -fsSL https://nanobrew.trilok.ai/install | bash && nb install gh
# Arch
sudo pacman -S --needed github-cli
# Fedora
sudo dnf install -y gh

gh auth login
```

**2. Clone athome and run the installer**

```bash
git clone https://github.com/morrieinmaas/athome ~/.local/share/chezmoi
cd ~/.local/share/chezmoi
./scripts/bootstrap.sh
```

This takes a few minutes. It asks a few questions, each with a sensible
default. For a work machine, add `--machine work`. To answer everything in
advance and run hands-off, see [unattended installs](docs/install.md#unattended-installs).

**3. Install everything else**

```bash
mise run apply
```

This is the long step: all packages, tools and the desktop setup. It's safe to
re-run whenever you like.

**4. Log out and back in**

Your shell is now zsh, and on Linux the login screen starts niri (GNOME is
under the gear icon). Then, if you use them:

```bash
bw-setup             # log in to Bitwarden (see docs/secrets.md for the one-time API key)
sudo netbird up      # or: nordvpn login — mesh VPN
```

## Everyday use

The repo lives in `~/.local/share/chezmoi`. `czc` takes you there.

| Command | What it does |
| --- | --- |
| `czs` | Show what differs between the repo and your machine |
| `czd` | Show the actual differences |
| `cza` | Apply the repo to your machine |
| `czu` | Pull the latest changes, then apply |
| `cze ~/.zshrc` | Edit a file in the repo instead of in place |
| `mise run apply` | Apply, including package installs (run from the repo) |
| `mise upgrade` | Upgrade the mise-managed tools |
| `theme` | Pick a colour theme for terminal, tmux, editor and desktop |

Edited a config file directly and want to keep the change? `chezmoi add <file>`
copies it back into the repo; then commit and push. Run `czd` before `cza` if
you're not sure, because applying overwrites local edits to managed files.

## Making it yours

- **Add a CLI tool:** put it in
  [`home/dot_config/mise/config.toml`](home/dot_config/mise/config.toml).
- **Add an app, font or system package:** put it in
  [`home/.chezmoidata/packages.yaml`](home/.chezmoidata/packages.yaml).
- **Change a config:** edit it under [`home/`](home), which mirrors your home
  folder (`dot_zshrc.tmpl` becomes `~/.zshrc`).

[docs/customizing.md](docs/customizing.md) covers the details, including
per-machine extra packages that never touch the repo.

## Starting over

`scripts/teardown.sh` removes what athome installed. It's a dry run unless you
pass `--execute`, and it never touches your project folders or `~/.secrets`.

```bash
./scripts/teardown.sh --all             # preview
./scripts/teardown.sh --all --execute   # do it, then run bootstrap again
```

## More docs

- [Installing](docs/install.md): Arch and Fedora from scratch, netboot, every
  bootstrap flag and answer
- [How it works](docs/how-it-works.md): identities, SSH and signing, secrets,
  the desktop, repo layout, why these tools
- [Secrets](docs/secrets.md): Bitwarden and rbw setup
- [Customizing](docs/customizing.md): adding packages and settings, handling drift
- [Themes](docs/theme-switching.md), [tmux and Neovim for VS Code users](docs/spatial-model.md),
  [NetBird](docs/netbird-cloud.md), [Neovim plugin pins](docs/nvim-pack-lock-workflow.md)
- [AGENTS.md](AGENTS.md): rules for AI agents (and people) changing this repo

## Contributing, security, licence

This is one person's setup, shared because the wiring might be useful to read.
Forking is the intended way to use it. Fixes that would help anyone are
welcome; see [CONTRIBUTING.md](CONTRIBUTING.md) and the
[code of conduct](CODE_OF_CONDUCT.md). To report a vulnerability, see
[SECURITY.md](SECURITY.md).

Licensed under [Apache-2.0](LICENSE).
