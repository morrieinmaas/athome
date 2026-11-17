# athome commands

`athome` is the Go binary that `./scripts/bootstrap.sh` installs into
`~/.local/bin`. See [installing](install.md) for the installer itself.

## athome init

Sets up this machine: the same baseline steps as the old bash bootstrap. A
second run skips whatever is already done and ends with
`athome: applied 0, skipped N`.

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

`athome init` finds the repo through the current directory, the `sourceDir`
pinned in `~/.config/chezmoi/chezmoi.toml`, or `~/.local/share/chezmoi`.

## athome version

Prints the version and commit, e.g. `athome 0.4.0 (abc1234)`.
