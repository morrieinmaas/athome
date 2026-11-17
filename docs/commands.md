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
