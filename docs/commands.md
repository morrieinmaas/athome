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
with `init`, `update` and `quit`. `update` runs `athome update`. Without a terminal it prints usage.

Questions are asked only on a terminal. A value from a flag or `ATHOME_*`
variable is read-only. A value from the config file is prefilled and editable
only in the form; line prompts (`--plain`, or a `--config` run on a terminal)
never ask for it and use it as is. `--config` is unattended only with `-y` or
without a terminal; on a terminal it uses line prompts, not the TUI. Details on the answers are
in [unattended installs](install.md#unattended-installs).

`athome init` finds the repo through `ATHOME_REPO` (a path that must hold
`.chezmoiroot`; the installer sets it), the current directory, the `sourceDir`
pinned in `~/.config/chezmoi/chezmoi.toml`, or `~/.local/share/chezmoi`.

## athome update

```
athome update [--to <version>] [--yes|-y]
```

Brings this machine up to a release. It first runs `git fetch --tags` on the repo and picks
the target: `--to` (`vX.Y.Z` or `X.Y.Z`, which must be a tag `git ls-remote` lists on
`origin`), else the highest local `vX.Y.Z` tag. Then four steps:

1. `self`: prints the commit subjects between the two tags, asks
   `update athome from <current> to <target>?` on a terminal (not with `--yes`), downloads
   `athome_<os>_<arch>` and `checksums.txt` from the release (the installer's
   `ATHOME_RELEASE_BASE`), checks the sha256, replaces `~/.local/bin/athome` and re-execs
   `update --to <target>` in the new binary. Skipped
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

## athome secrets

Backs the local secret files up to an encrypted git vault, and restores them on a new
machine. It replaces the `secrets-backup` and `secrets-restore` scripts; those names stay
one release as wrappers that pass their arguments and exit code through.

```bash
athome secrets backup
athome secrets restore [--dry-run]
```

`backup` mirrors `~/.secrets/<repo>/.env*` (not `.example`, `.sample`, `.template` or
`.dist`, and not a directory starting with a dot), `~/.aws/config`, `~/.aws/credentials`,
`~/.config/scw/config.yaml`, `~/.config/stripe/config.toml` and
`~/.codeium/windsurf/mcp_config.json` into the vault, commits, and pushes. It refuses to
commit unless every staged file is ciphertext. A failed push leaves the commit for the
next run.

`restore` clones the vault (or pulls it), unlocks it with the git-crypt key from
Bitwarden (`rbw`) when it is still encrypted, copies the files into `$HOME` at mode 600
and makes `~/.secrets` and its directories owner-only. With `--dry-run` it still clones or
pulls and unlocks, and only skips the copy.

If the git-crypt key cannot be read from Bitwarden, or `git-crypt unlock` fails, `restore`
exits 1 before copying anything. The old script carried on and copied the still-encrypted
files into `$HOME`.

| Variable | Default | What it sets |
| --- | --- | --- |
| `SECRETS_VAULT` | `~/.local/share/attic` for `backup`, `~/.local/share/<name>` for `restore` | The vault checkout. |
| `SECRETS_VAULT_NAME` | `attic` | The repo name and the Bitwarden item `git-crypt/<name>-key` (`restore`). |
| `SECRETS_VAULT_REPO` | `<owner>/<name>` | The GitHub repo to clone. |
| `ATHOME_GITHUB_HANDLE` | the `gh` user | The owner when `SECRETS_VAULT_REPO` is not set. |

Exit codes: 0 done (also for a vault with nothing tracked, or a push that failed after the
commit); 1 no vault (`backup`), a vault that cannot be cloned, listed or unlocked, or a
file that could not be copied; 2 plaintext found in the staged files (`backup`), or a
usage error.

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
