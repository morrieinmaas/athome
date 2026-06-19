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

## Adding a config value (chezmoi data key)

Per-machine values (names, emails, URLs, flags) live in chezmoi `[data]`.

1. Add a prompt in `home/.chezmoi.toml.tmpl` (the `promptStringOnce` block) and a
   line in `[data]`, e.g. `my_key = {{ $myKey | quote }}`.
2. Reference it in any template as `{{ .my_key }}`.
3. Optionally add it to `examples/bootstrap.toml.example` so it can be
   pre-answered non-interactively.

Values are cached in `~/.config/chezmoi/chezmoi.toml` after first run. To change
one later, edit that file directly (it's your private cache, not tracked).

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
