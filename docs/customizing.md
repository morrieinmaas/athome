# Customizing & day-to-day

How to add packages, add config values, and keep your live machine in sync with
the repo without losing edits.

## The two phases (bootstrap vs apply)

- **`mise run bootstrap`** = *baseline*: chezmoi, mise, ssh keys, the package
  **manager**, `gh` + `rbw`, project dirs, and your config **files**. It does NOT
  install the full package set (it runs `chezmoi init --apply --exclude=scripts`).
- **`mise run apply`** = *convergence*: installs the full package set and runs
  the desktop/setup scripts. **Run it once after bootstrap, and any time after.**
  It's idempotent — re-running converges and is a fast no-op when nothing changed.

So a fresh machine is: `./scripts/install-mise.sh` → `mise run bootstrap` →
`mise run apply`.

## Interactive vs non-interactive bootstrap

On a terminal `athome init` shows a full-screen form first; pass `--plain` (or set
`ATHOME_PLAIN=1`) for the line prompts described below. Your answers are stored in the
`[data]` table of `~/.config/chezmoi/chezmoi.toml` (private, never in the repo). To change
them, rerun `athome init`: the form starts from what is stored there, and the rest of the
run skips what is already done.

`mise run bootstrap` is **interactive by default** — it asks about the choices
below. Add **`--non-interactive`** (or `-y`), or just run without a TTY (CI,
pipes), to skip all prompts and take the defaults — which a `bootstrap.toml`
(`--config`) or `ATHOME_*` env var can override:

| Prompt | `bootstrap.toml` key | Default |
|---|---|---|
| Use Bitwarden for secrets? | `useBitwarden` | `true` (`false` = local-only) |
| SSH key found — use / generate / reupload? | `sshKeyAction` | `use` |
| No SSH key — create one? | `generateSshKey` | `true` |

So the same script runs three ways: `./scripts/bootstrap.sh` (interactive),
`--non-interactive` (defaults), or `--non-interactive --config bootstrap.toml`
(custom, hands-off — what the e2e uses).

**Bitwarden is never a gate.** Declining it (or `useBitwarden=false`) gives a
*complete* local setup: `~/.secrets/` is created, ready for hand-populated
`<repo>/.env` files; run `bw-setup` anytime later to sync from Bitwarden.

## Adding a package

Packages live in `home/.chezmoidata/packages.yaml`, split by OS. The dividing
line (see `AGENTS.md`): **portable single-binary CLIs go in mise**
(`home/dot_config/mise/config.toml` `[tools]`); **GUI apps, fonts, libraries,
system/desktop packages, and anything needed outside an interactive shell go in
`packages.yaml`**.

1. Pick the right section in `packages.yaml`:
   - `common` — same package name on Homebrew **and** pacman/AUR (installed on
     macOS via `nb`, on Arch via `yay`).
   - `darwin.brew_formulae` / `darwin.brew_casks` — macOS-only (CLI / GUI app).
   - `linux.pacman` (official repo) / `linux.aur` — Arch. The split is an
     organizational hint; both install through `yay`.
   - `fedora.dnf` / `fedora.copr` — Fedora is self-contained (does **not** layer
     `common`), so add Fedora names here explicitly.
2. Add the canonical package name (e.g. `gh` on brew/dnf is `github-cli` on Arch).
3. `mise run apply`. chezmoi re-hashes `packages.yaml`, so
   `run_onchange_02-install-packages.sh.tmpl` re-runs and installs it.

> Portable tool instead? Add it to `home/dot_config/mise/config.toml` `[tools]`
> and `mise run apply` (or `mise install`).

### Extra packages without editing `packages.yaml` (overlay / BYO)

To add packages on a single machine, or in a fork that keeps these dotfiles as
the default, use either overlay — both install on every `mise run apply` via the
OS's PM, idempotently (`run_after_05-install-extra-packages.sh`):

- **Per-machine file** (recommended): `~/.config/athome/extra-packages`, one
  package per line (`#` comments allowed). Never tracked; edits take effect on
  the next `mise run apply`.
  ```
  # ~/.config/athome/extra-packages
  htop
  lazygit
  ```
- **Bootstrap config / data key**: set `extraPackages = "htop lazygit"` in your
  `bootstrap.toml` (see `examples/bootstrap.toml.example`) or answer the
  `extraPackages` prompt — stored in chezmoi `[data].extra_packages`.

Names are **your responsibility per-OS** (the curated cross-OS set stays in
`packages.yaml`).

## Adding a config value (chezmoi data key)

Per-machine values (names, emails, URLs, flags) live in chezmoi `[data]`.

1. Add a prompt in `home/.chezmoi.toml.tmpl` (the `promptStringOnce` block) and a
   line in `[data]`, e.g. `my_key = {{ $myKey | quote }}`.
2. Reference it in any template as `{{ .my_key }}`.
3. Optionally add it to `examples/bootstrap.toml.example` so it can be
   pre-answered non-interactively.

Values are cached in `~/.config/chezmoi/chezmoi.toml` after first run. To change
one later, edit that file directly (it's your private cache, not tracked).

## Context colours and the remote prompt

Each context has one colour, an ANSI colour name, in `home/.chezmoidata/contextColors.toml`:

```toml
[contextColors]
    work     = "yellow"
    personal = "green"
    sidebiz  = "magenta"
```

ANSI colours come from your terminal's palette, so they follow your terminal theme. On a
[remote machine](remote-machines.md) the starship prompt's host segment becomes
`<machine> · <scope>` in the scope's context colour; on the laptop the prompt is unchanged.

## Headless packages

A [remote machine](remote-machines.md) skips GUI packages. In `packages.yaml`, a GUI app or a
piece of the desktop stack in a shared list is written as a map instead of a plain name:

```yaml
  - {name: ghostty, gui: true}
```

Remote machines skip those entries and every macOS cask; everything else installs as usual.

## Bumping grofe or canopy

One edit: change `grofe` (a release tag such as `v0.3.1`) or `canopy` (a tag or commit) in
`home/.chezmoidata/pins.toml`. The install scripts render the pins, so the next `chezmoi apply`
(or `athome update`) reinstalls the tool; `athome status` compares what is installed with the pin.

`pins.athome` is different: it is a floor, not a version to bump with every release.
`run_14-install-athome` installs it only where athome is missing, older, or its `athome version`
fails, so raise it
only when every machine must have at least that athome; `athome update` does the rest.

## What athome owns vs what you own

Some files in your home folder are yours, and tools append to them too (an
installer adds its completion line to `~/.zshrc`, `git config --global` writes
`~/.gitconfig`). athome never replaces those. It keeps its own settings in files
it owns, and either keeps one line in yours that pulls them in (a `modify_`
script) or writes your file once, when it is missing (a `create_` file).

| Your file | athome's file (managed, apply replaces edits) | How chezmoi treats yours |
| --- | --- | --- |
| `~/.zshrc` | `~/.zsh/athome.zsh` | keeps one `source "$HOME/.zsh/athome.zsh"` line, at the top when it adds it |
| `~/.ssh/config` | `~/.ssh/athome/base.conf` (and the generated `~/.ssh/athome/config`) | keeps one `Include athome/*.conf` line above every `Host`/`Match` |
| `~/.gitconfig` | `~/.config/git/config` | every line kept; chezmoi only appends the per-directory `includeIf` blocks (and an empty `[user]`) when no `[user]` section has them after it, so `git config --global user.email` cannot beat them |
| `~/.config/containers/registries.conf` | `~/.config/containers/registries.conf.d/athome.conf` | not managed |
| `~/.zprofile` | none | not managed |
| `~/.config/gh-dash/config.yml`, `~/.config/slk/config.toml`, `~/.config/niri/config.kdl` | none (the `theme` command recolours them) | written once when missing, then never touched |

Every other line in a file of yours passes through `apply` verbatim. Two
consequences worth knowing:

- **Precedence.** git reads `~/.config/git/config` first and `~/.gitconfig` after,
  so your value wins. For `user.*` that would beat the per-directory
  identities for `~/personal`, `~/work` and `~/sidebiz`, so chezmoi keeps their
  `includeIf` blocks after your last `[user]` section (`git config --global
  user.email` writes into that section, above them). ssh
  takes the first value it finds, and athome's `Host *` defaults come first,
  so override them per host in `~/.ssh/config.local` (read before them), not
  in a later block.
- **Upgrades.** A file written once never gets later repo changes. To take the
  repo's current version of one, move yours aside and apply. For a themed
  config, run `theme <name>` afterwards: the fresh copy has the repo's colours.

Migrating from an older athome, where it owned these files whole: the first
apply recognises the old body (by its "managed by chezmoi" header) and swaps it
for the one line, dropping only lines some athome version wrote and keeping
whatever you or a tool added in or around it. An existing
`~/.gitconfig` is left exactly as it is, old athome settings included; they
match `~/.config/git/config`, and you can delete them from `~/.gitconfig` so
later repo changes are not shadowed. Run `chezmoi init` before that apply when
chezmoi warns that the config template changed.

Every other file under `home/` is athome's, whole, which the next section is about.
Some of those are files a tool can write too, so an apply would undo such a
write: `~/.zshenv` (rustup adds `. "$HOME/.cargo/env"`), `~/.config/mise/config.toml`
(`mise use -g`), `~/.config/gh/config.yml` (`gh config set`),
`~/.config/zed/settings.json` (Zed's settings UI), `~/.config/opencode/opencode.json`
and `~/.config/containers/containers.conf`. Capture such a change with `mise run
save`, or move it somewhere the tool also reads that athome does not manage.

## Managing drift (don't lose live edits!)

chezmoi's source of truth is the **repo** (`home/…`); `apply` renders source →
`$HOME`. If you edit a **managed** file live (e.g. `~/.config/nvim/init.lua` — it
*is* managed), the repo does NOT update, and the next `mise run apply` / `czu`
will **overwrite your live edit with the repo's version**. Two safe workflows:

- **Edit the source** (preferred): `cze ~/.config/nvim/init.lua` (= `chezmoi
  edit`) opens the *source* file and auto-applies on save → then commit + push.
- **Capture a live edit back**: after editing live, run **`mise run save`** (=
  `chezmoi re-add` + diff) to pull it into the source → commit + push.

Useful checks:

```
mise run status   # what's drifted (M = live edit not in the repo)
mise run diff     # exactly what `apply` would change (and would clobber)
```

Always `mise run diff` before `apply` if you suspect uncaptured live edits.

## Conventions (for editing scripts)

- **Scripts that run *from the repo*** (`scripts/bootstrap.sh`)
  share helpers from **`scripts/lib/`** (`ssh.sh`) — source
  them; don't re-inline.
- **chezmoi `run_*` scripts are islands.** They run rendered-standalone in
  chezmoi's apply context and **cannot `source` `scripts/lib/`**. So each one
  that needs them carries its *own* small copy of two patterns — by design, not
  oversight:
  - **nanobrew on PATH:** `[[ -d /opt/nanobrew/prefix/bin ]] && export PATH="/opt/nanobrew/prefix/bin:$PATH"`
  - **follow-up note:** `[[ -n "${ATHOME_FOLLOWUP_LOG:-}" ]] && printf '  • %s\n' "…" >> "$ATHOME_FOLLOWUP_LOG"`
  - and the `ATHOME_CI` skip guard: `if [ -n "${ATHOME_CI:-}" ]; then echo "…skip…"; exit 0; fi`
  Copy these verbatim into new `run_*` scripts. The github ssh-seed in
  `run_before_00` likewise **mirrors** `scripts/lib/ssh.sh` — keep the two in sync.
