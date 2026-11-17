# athome commands

`athome` is the Go binary that `./scripts/bootstrap.sh` installs into
`~/.local/bin`. See [installing](install.md) for the installer itself.

## athome init

Sets up this machine: the same baseline steps as the old bash bootstrap. A
second run skips whatever is already done and, with `-y` or non-interactively,
ends with `athome: applied 0, skipped N`. An interactive rerun asks some questions
again (ssh key choice, Bitwarden, an empty handle or URL), so those steps count as
applied.

```bash
athome init [flags]
```

| Flag | What it does |
| --- | --- |
| `--machine personal\|work` | Machine type (default `personal`). |
| `--config <file>` | A TOML file of answers (`bootstrap.toml`). |
| `--ref <ref>` | Git ref of the athome repo to apply. |
| `--no-agents` | Don't set up agent tooling. |
| `--import-from <dir>` | Directory to import files from. |
| `--import-ssh` | Import the SSH key from `--import-from`. |
| `--import-ssh-bw <item>` | Bitwarden item holding the SSH key. |
| `--non-interactive`, `-y`, `--yes` | Never ask; use defaults, the config file and `ATHOME_*` variables. |

Questions are asked only on a terminal, and never for a value already given by
a flag, an `ATHOME_*` variable or the config file. Details on the answers are
in [unattended installs](install.md#unattended-installs).

`athome init` finds the repo through `ATHOME_REPO` (a path that must hold
`.chezmoiroot`; the installer sets it), the current directory, the `sourceDir`
pinned in `~/.config/chezmoi/chezmoi.toml`, or `~/.local/share/chezmoi`.

## athome update

```
athome update [--to <version>] [--yes|-y]
```

Brings this machine up to a release. It first runs `git fetch --tags` on the repo and picks
the target: `--to` (`vX.Y.Z` or `X.Y.Z`), else the highest local `vX.Y.Z` tag. Then four steps:

1. `self`: downloads `athome_<os>_<arch>` and `checksums.txt` from the release (the installer's
   `ATHOME_RELEASE_BASE`), checks the sha256, prints the commit subjects between the two
   tags, asks `update athome from <current> to <target>?` on a terminal (not with `--yes`),
   replaces `~/.local/bin/athome` and runs `update --to <target>` in the new binary. Skipped
   when already at the target, or newer without `--to` (no downgrade); `--to` installs an
   older version too. A `dev` or snapshot build skips it unless `--to` is given.
2. `source`: `git pull --ff-only` on a branch; on a detached HEAD at a tag, checks out the
   target tag; at an untagged detached commit changes nothing and says so.
3. `apply`: `chezmoi apply`, which installs new packages and moves grofe and canopy when
   their pins changed.
4. `mise`: `mise install`, skipped when `mise ls --missing` lists nothing.

It ends with `athome: applied N, skipped M`. Exit codes: 0 done or cancelled; 1 a step
failed (named), the fetch failed, or `--to` names a tag `origin` lacks; 2 a usage error
(`--to v0.5`); 130 Ctrl-C at the prompt. A checksum mismatch, a missing checksums line or a
missing asset leaves the installed binary untouched.

## athome teardown

Undoes athome on this machine for a from-scratch retry. A dry run unless you pass
`--execute`.

```bash
athome teardown (--state | --dotfiles | --all) [--execute] [--yes]
```

| Tier | What it removes (each includes the ones above it) |
| --- | --- |
| `--state` | chezmoi's persistent state and cache, and athome's marker files. Nothing leaves `$HOME`. |
| `--dotfiles` | Every chezmoi-managed file in `$HOME` and `~/.config/chezmoi`. The source repo stays. |
| `--all` | mise and its tools, chezmoi externals and app data, the stale `~/.claude` links into `~/.config/agents`, nanobrew with its casks (macOS, needs sudo; on Linux the system packages stay, with a hint), rbw data, this machine's SSH key, and `~/.local/bin/athome`. |

| Flag | What it does |
| --- | --- |
| `--execute`, `--run` | Do it. Prints the plan, asks you to type the tier name, then runs it. |
| `--dry-run` | Preview only (the default). The last of `--execute` and `--dry-run` wins, as does the last tier. |
| `--yes`, `-y` | Skip the typed confirmation. |
| `-h`, `--help` | Print the usage. |

Dry run prints `  would: <command>` lines and changes nothing. `~/.secrets`, `~/personal`,
`~/sidebiz` and `~/work` are never removed. Without `chezmoi` on PATH its two calls print
`skip: chezmoi not found`, so a rerun after a half-finished `--all` completes. A binary
elsewhere on PATH is left alone.

Exit codes: 0 done (or dry run); 1 an action failed (the output names it; the actions before
it ran); 2 no tier, an unknown argument, or the confirmation not given.

## athome version

Prints the version and commit, e.g. `athome 0.5.0 (abc1234)`.

## athome status

```
athome status [--json]
```

Reads, never writes, and reports five components: `athome` (the running version
against the highest `vX.Y.Z` tag on `origin`), `dotfiles` (`chezmoi status
--exclude=scripts` prints nothing), `mise` (`mise ls --missing` lists nothing),
`grofe` (its version against `pins.grofe`) and `canopy` (its checkout against
`pins.canopy`). The pins live in `home/.chezmoidata/pins.toml`.

Each is `current`, `not current`, `not installed` or `unknown` (offline, no
`pins.toml`, a version that is not a release). The text output starts with
`athome <version> (<commit>), repo <repo> at <ref>`, then one line per component.
`--json` prints `{"version", "commit", "repo", "ref", "current", "components": [{"name",
"status", "installed", "wanted", "detail"}]}`, where `current` is true only when all
five are current.

Exit codes: 0 whenever the report printed, whatever it says; 1 when the repo is not
found; 2 for a usage error.
