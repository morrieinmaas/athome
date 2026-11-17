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
| `--plain` | On a terminal, ask line by line instead of showing the full-screen form. `ATHOME_PLAIN=1` does the same. |

## The form and the home screen

On a terminal (stdin and stdout), without `-y`, `--yes`, `--non-interactive` or `--plain`,
`athome init` opens a full-screen form with five pages (Machine, Identity, Keys and secrets,
Network, Extras) and a review page: enter applies, `b` goes back to edit, `q` or Ctrl-C quits
with exit status 130 and writes nothing. Each field starts from, in order, a flag or `ATHOME_*`
variable (shown read-only), the config file, the answers already in `~/.config/chezmoi/chezmoi.toml`,
then a default. After the review, one progress view lists every step; `l` shows the run log.
A failing step stays on screen until a key is pressed, then `athome init` exits 1 with the
same error line as the plain run.

Run `athome` with no arguments on a terminal to open the home screen: version, repo,
and the machine type, hostname and GitHub handle from `chezmoi.toml` (or "not set up"),
with `init` and `quit`. Without a terminal it prints usage.

Questions are asked only on a terminal. A value from a flag or `ATHOME_*`
variable is read-only; a value from the config file is prefilled and editable.
`--config` is unattended only with `-y` or without a terminal; on a terminal it
uses line prompts, not the TUI. Details on the answers are
in [unattended installs](install.md#unattended-installs).

`athome init` finds the repo through `ATHOME_REPO` (a path that must hold
`.chezmoiroot`; the installer sets it), the current directory, the `sourceDir`
pinned in `~/.config/chezmoi/chezmoi.toml`, or `~/.local/share/chezmoi`.

## athome version

Prints the version and commit, e.g. `athome 0.5.0 (abc1234)`.
