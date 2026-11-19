# How athome works

Background for when you want to change something or understand why it's built
this way. For installing, see [install.md](install.md).

- [The interactive screens](#the-interactive-screens)
- [chezmoi, mise and the package list](#chezmoi-mise-and-the-package-list)
- [One folder per identity](#one-folder-per-identity)
- [SSH keys and signing](#ssh-keys-and-signing)
- [Per-project secrets](#per-project-secrets)
- [Guarding against leaked secrets](#guarding-against-leaked-secrets)
- [The desktop (Linux)](#the-desktop-linux)
- [Shared AI-agent config](#shared-ai-agent-config)
- [Repo layout](#repo-layout)
- [Why these tools](#why-these-tools)
- [Releases and rolling back](#releases-and-rolling-back)
- [Tests and CI](#tests-and-ci)
- [More than one machine: pyinfra](#more-than-one-machine-pyinfra)

## The interactive screens

On a terminal, `athome init` runs one form and then one progress view over the very same
step list the plain run uses; the screens only collect answers and show state. Questions a
step still asks, and every command that needs the terminal (sudo, `bw-setup`, the `gh` device
flow), run with the full-screen view released and restored afterwards, so sudo asks only when
a step needs it.

## chezmoi, mise and the package list

Three places decide what ends up on a machine:

| What | Where | Examples |
| --- | --- | --- |
| Config files | [`home/`](../home), deployed by chezmoi | `~/.zshrc`, `~/.config/tmux/…`, `~/.gitconfig` |
| Portable CLI tools and language runtimes | [`home/dot_config/mise/config.toml`](../home/dot_config/mise/config.toml) | ripgrep, fd, bat, lazygit, node, go, rust |
| Everything native | [`home/.chezmoidata/packages.yaml`](../home/.chezmoidata/packages.yaml) | apps, fonts, the niri desktop, podman, system services |

The rule for a new tool: a single-binary CLI goes in mise, so it installs the
same way on every OS. A GUI app, font, library, service, or anything needing
root goes in `packages.yaml`. One exception: tools that git hooks or system
services call (`gitleaks`, `rbw`) must be native, because mise's shims aren't
on the PATH outside an interactive shell.

On macOS native packages come from nanobrew (`nb`), on Arch from pacman and
the AUR via yay, on Fedora from dnf. mise itself is installed from
[mise.run](https://mise.run), not a package manager.

chezmoi keeps a source (this repo's `home/` folder) and a target (your real
files) and renders one onto the other. `.chezmoiroot` points it at `home/` so
the repo root stays free for docs and scripts.

## One folder per identity

Where a repo lives decides who you commit as:

| Folder | Commits as |
| --- | --- |
| `~/personal/` | your handle and GitHub noreply email |
| `~/work/` | `workEmail` / `workName` |
| `~/sidebiz/` | `sidebizEmail` / `sidebizName` |

This is plain git `includeIf` in [`dot_gitconfig.tmpl`](../home/dot_gitconfig.tmpl).
There are no per-repo overrides to forget. A side-business repo cloned into
`~/personal/` looks wrong when you `ls`, which is the point. Emails come from
your answers at install time and are never written to the repo. Repos outside
those three folders commit with your noreply email and aren't signed.

## Remote machines

A third machine type, `machineType = "remote"`, is for [remote dev
machines](remote-machines.md). It is never asked in the form: `athome machine add` on the
laptop writes a `bootstrap.toml` with it, the box's `scope` (e.g. `work/acme`) and its
`machineName`, and runs `athome init` on the box with that file. On a remote machine:

- `run_03` creates only `~/<scope>`, with the context's `use ctx` envrc and git identity.
- The machine is headless. `packages.yaml` entries tagged `gui: true` and every macOS cask
  are skipped, the desktop scripts render empty, and the desktop configs are ignored.
- init never runs `bw-setup`, never asks to restore the vault and never generates an ssh
  key; the box's own key comes from `machine add`.
- `~/.zshenv` and `~/.config/environment.d/athome.conf` set `ATHOME_MACHINE`,
  `ATHOME_SCOPE` and `ATHOME_CONTEXT_COLOR`; the starship prompt shows
  `<machine> · <scope>` in the context's colour.
- `run_onchange_after_30` hardens sshd (keys only, no root login), turns on lingering, sets
  the laptop's timezone and locale, and enables `grofe-restore.service`, which starts
  `grofe start-server --restore` at boot.

On the laptop, `~/.ssh/config` includes `~/.ssh/athome/config`, which athome writes from the
inventory (`~/.config/athome/machines.toml`, mirrored into the secrets vault): one `Host`
block per machine, with the host key pinned in `~/.ssh/athome/known_hosts`.

## SSH keys and signing

Each machine gets one passphrase-less ed25519 key, `~/.ssh/<hostname>_ed25519`,
named after the host so GitHub's key list shows which machine is which.
`~/.ssh/config` points GitHub at it, so no ssh-agent is needed.

Commits in the three identity folders are signed with SSH
(`gpg.format = ssh`), using `~/.ssh/id_ed25519.pub` if you have one (it's
what `gh auth login` creates), otherwise the per-host key. There's no GPG.

Clones stay on plain HTTPS; only pushes go over SSH
(`url."git@github.com:".pushInsteadOf`). A catch-all `insteadOf` would push
clones through SSH too, and GitHub refuses SSH clones, even of public repos,
from a key it doesn't know, which breaks a fresh machine mid-install.

## Per-project secrets

Secrets live in Bitwarden and are read with [rbw](https://github.com/doy/rbw).
Setup, including the one-time API key a new device needs, is in
[secrets.md](secrets.md).

The usual route is direnv. Any `.envrc` inside `~/personal`, `~/work` or
`~/sidebiz` loads automatically when you `cd` in (those folders are
whitelisted), and the helpers in
[`direnvrc`](../home/dot_config/direnv/direnvrc) pull values from Bitwarden:

```bash
# .envrc
use ctx personal                                  # a bundle: github, aws, openrouter…
use_rbw OPENAI_API_KEY personal/openai-api-key    # or one value by item name
```

Nothing lands on disk or in your shell startup files. Starter `.envrc` files
are in [`examples/`](../examples). To add a secret:

```bash
rbw unlock
rbw add work/some-api-key
echo 'use_rbw SOME_API_KEY work/some-api-key' >> .envrc
```

If a tool insists on a real file (`~/.aws/config`, say), chezmoi manages it
with non-secret settings only, and credentials still come from Bitwarden.

## Guarding against leaked secrets

The main protection is that secrets never go in tracked files. Behind that:

| Check | Catches | Lives in |
| --- | --- | --- |
| Global gitignore | `.env*`, keys, certificates, tfvars | `~/.gitignore_global` |
| Global pre-commit hook | private-key filenames, and gitleaks on staged changes | `~/.config/git/hooks/pre-commit` |
| gitleaks GitHub Action | anything that got past the above, on push, PR and weekly | [`secrets-scan.yml`](../.github/workflows/secrets-scan.yml) |

The hooks apply to every repo on the machine (`core.hooksPath`). Skipping them
takes an explicit `SKIP_GIT_HOOKS=1 git commit`.

## The desktop (Linux)

GNOME and GDM come from the OS install. athome adds:

- **niri**, a scrolling tiling Wayland compositor, set as your default session
  ([config](../home/.chezmoitemplates/niri/config.kdl)).
- **Noctalia**, the shell running inside niri: bar, launcher, notifications,
  lock screen, and clipboard history on `Ctrl+Alt+V`. athome's defaults are in
  `~/.config/noctalia/athome.toml`: a built-in dark scheme that follows
  `theme`, the Inter font, and templates that recolour GTK and Qt apps to
  match. Anything you change in Noctalia's own settings is saved to
  `~/.local/state/noctalia/settings.toml`, which wins.
- **Icons and cursor:** Papirus-Dark and Bibata, with adw-gtk3 for GTK apps and
  qt6ct for Qt apps.
- **GPaste** for clipboard history when you're in the GNOME session.
- power-profiles-daemon and Bluetooth. Wi-Fi is NetworkManager on its default
  wpa_supplicant backend: manage it from Noctalia, GNOME, `nmtui` or `nmcli`.

The `theme` command recolours Ghostty, tmux, Neovim and niri together; see
[theme-switching.md](theme-switching.md).

## Shared AI-agent config

Set `agentsRepo` to your agent skills repo (`owner/repo`) and athome clones it
into `~/.claude` itself: global instructions, rules, agent prompts and a
vendored tree of skills under `agent-skills/skills/`. The repo's `.gitignore`
keeps Claude Code's own state (sessions, projects, settings) out of git.

Not every vendored skill is switched on. The ones listed in
`agent-skills/.skill-lock.json` (maintained by the `skills` CLI, which finds
the tree through `~/.agents`) are linked into `~/.config/agents/skills/`, next
to your own skills, which live there as real folders. Both `~/.claude/skills`
and opencode's skills folder point at that one set. Every `chezmoi apply`
brings the links in line with the lock; pulling the repo
(`git -C ~/.claude pull`) is up to you. `--no-agents` skips all of it.

## Updating

`athome update` runs five steps in a fixed order: `self` (replace the binary from a release,
then continue in the new one, so the rest runs with matching code), `source` (move the repo to
the release), `config` (regenerate `~/.config/chezmoi/chezmoi.toml` with
`chezmoi init --no-tty --promptDefaults` when `.chezmoi.toml.tmpl` changed, keeping every
stored answer and the `sourceDir` pin), `apply` (`chezmoi apply`) and `mise` (`mise install`). athome's own version is
the release tag; grofe and canopy are pinned in `home/.chezmoidata/pins.toml`, and
`run_onchange_17` and `run_onchange_18` render those pins, so a bump reruns them on the next
apply. grofe installs from its release tarball after a sha256 check.

## Repo layout

```text
athome/
├── scripts/bootstrap.sh       the installer
├── mise.toml                  repo tasks: mise run apply / diff / test …
├── docs/                      you are here
├── examples/                  bootstrap answers, starter .envrc files
├── infra/                     pyinfra deploy for remote machines
├── test/                      bats tests and the container end-to-end test
└── home/                      everything chezmoi puts in your home folder
    ├── .chezmoi.toml.tmpl     install-time questions
    ├── .chezmoidata/packages.yaml
    ├── .chezmoidata/pins.toml     the grofe and canopy versions
    ├── .chezmoiexternal.toml.tmpl   things cloned from elsewhere (zinit, tmux plugins…)
    ├── .chezmoiscripts/       numbered setup scripts
    ├── dot_zshrc.tmpl, dot_zsh/     the shell
    ├── dot_gitconfig*.tmpl    git, identities, signing
    ├── private_dot_ssh/       SSH config
    └── dot_config/            ghostty, tmux, canopy, nvim, niri, mise, direnv, …
```

## Why these tools

| Tool | Why |
| --- | --- |
| **mise** | One installer for portable tools on every OS, with versions and per-project overrides. Python is left to `uv`. |
| **nanobrew** | Homebrew-compatible on macOS but much faster, and doesn't touch `/opt/homebrew`. |
| **Ghostty** | Fast, native-feeling on macOS and Linux, simple config, follows the system's light/dark mode. |
| **tmux** | Works everywhere, including over SSH. Its config is layered on [canopy](https://github.com/morrieinmaas/canopy). |
| **Neovim** | Same Lua config everywhere, with the built-in plugin manager and LSP. Zed is installed as the GUI editor. |
| **Podman** | Rootless and daemonless, and `docker` commands still work. |
| **rbw, NetBird, Noctalia** | Open-source picks, each with a self-hosting option if the hosted service goes away. |

## Releases and rolling back

`main` is the working branch. Tags (`vX.Y.Z`) mark known-good states;
`bootstrap.sh --ref` can build from one. To roll a machine back:

```bash
chezmoi cd
git fetch --tags && git checkout v0.3.3
exit
chezmoi apply
# later: chezmoi cd && git checkout main && exit && chezmoi apply
```

## Tests and CI

| What | Run locally | In CI |
| --- | --- | --- |
| shellcheck | `mise run lint` | on every PR and every push to main |
| bats unit tests | `mise run test` | on every PR and every push to main |
| Full install in a container, Arch and Fedora | `mise run e2e`, `mise run e2e-fedora` | on every PR and every push to main |
| gitleaks | every commit | on push, PR and weekly |

The end-to-end test installs athome in a clean container with dummy answers,
checks the result, tears it down with `athome teardown --all`, checks that user
data survived, and installs again.

## More than one machine: pyinfra

[`infra/`](../infra) has a [pyinfra](https://pyinfra.com/) deploy you run from
your laptop. It does the root-level setup chezmoi can't (users, base packages,
authorized keys, hostname) on remote machines over SSH; then you log in and
run bootstrap as normal. Details in [infra/README.md](../infra/README.md).
