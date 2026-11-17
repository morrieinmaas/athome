# The athome binary: design

**Date:** 2026-11-16
**Status:** Approved 2026-11-16, pre-implementation
**Repo:** athome
**Build order:** 1 of 4. Then `2026-11-16-keys-and-recovery-design.md`, then
`2026-11-16-remote-machines-design.md`, then canopy's `2026-11-16-remote-awareness-design.md`.
The later specs add subcommands to this binary.

## Problem

athome's logic lives in bash: `scripts/bootstrap.sh` (1105 lines), `scripts/lib/*.sh`,
`scripts/teardown.sh`, and helpers in `home/dot_local/bin` (`secrets-backup`,
`secrets-restore`, `bw-setup`, `cockpit`). First run asks about 20 plain-text questions
from `home/.chezmoi.toml.tmpl`, plus bootstrap's own. Updating means running chezmoi, mise and the package
manager by hand.

The remote-machines and keys work adds an inventory with validation, bootstrap over ssh,
key rotation across GitHub and every box, and parallel sync. That is more than bash should
carry, and first run is the first thing a new termage user sees.

## Decision

One Go binary, `athome`, built with Bubble Tea, huh and lipgloss, released with
GoReleaser as checksummed macOS and Linux binaries.

- **Go over Rust:** the TUI is mostly forms and progress views, which huh and Bubble Tea
  cover directly; ratatui would need hand-built form widgets. The CLI is orchestration
  (ssh, `gh`, `rbw`, `chezmoi`, `mise`, `git`), which Go's standard library handles well.
  chezmoi, gh and the Charm tools are Go too. athome shares no code with grofe.
- **One binary, not two:** one install path, one version pin, one checksum.
- **External tools stay external.** athome runs `chezmoi`, `mise`, `gh`, `rbw` and `git` as
  programs and never imports them as libraries, so their versions stay pinned separately.
  The split of work between them does not change: chezmoi renders dotfiles and runs its
  scripts, mise installs every portable tool and each project's toolchain, and
  `packages.yaml` holds only native packages (GUI apps, the desktop stack, fonts,
  privileged tools).
- **It runs from a clone, as today.** The chezmoi source stays at `$repo_root/home`.
  `bootstrap.sh` passes the repo root to the binary; run by hand, the binary finds it from
  its working directory or from chezmoi's pinned `sourceDir`. Nothing is embedded.

## Three ways to run it

1. **TUI**, when stdout is a terminal and no subcommand is given, or on `athome init` and
   `athome update` run interactively.
2. **Config file**, for scripts and boxes: `athome init --config bootstrap.toml`. Same
   discovery order and precedence as today: the `--config` path, then
   `$repo_root/bootstrap.local.toml`, then `~/.config/athome/bootstrap.toml`; `ATHOME_*` env
   vars and flags win over the file. `test/e2e/bootstrap.toml` keeps working unchanged.
3. **Plain subcommands** for everything else, scriptable, with `--json` where output is data.

With no terminal and no config file, `init` behaves as `bootstrap.sh` does today: defaults
plus `ATHOME_*` variables, no prompts. Nothing requires the TUI.

## Commands

```
athome                       # TUI home: status, then init / update / machines
athome init [flags]          # first run: answers, ssh key, chezmoi init --apply, baseline
athome update                # self, chezmoi, mise, packages, grofe, canopy, then sync --all
athome status                # what athome manages here and whether it is current
athome teardown [--state|--dotfiles|--all] [--execute] [--yes]
athome secrets backup|restore|rekey
athome machine ...           # remote-machines spec
athome keys ...              # keys-and-recovery spec
athome project clone ...     # remote-machines spec
athome version
```

`init` takes every flag `bootstrap.sh` takes today, with the same meaning: `--ref`,
`--machine`, `--config`, `--no-agents`, `--import-from`, `--import-ssh`, `--import-ssh-bw`,
`--non-interactive` (`-y`, `--yes`). It adds `--restore` for a replacement laptop (keys spec),
which restores the ssh key and the vault before any key is generated. `teardown` keeps today's tiers and its dry run by
default. `test/e2e/scenario.sh` changes in two ways: both of its `bootstrap.sh` calls (before
and after the teardown) gain `--install-from <built binary>`, and its teardown call becomes
`athome teardown`. `teardown --all` also removes `~/.local/bin/athome`.

## The init form

The form replaces the prompts in `home/.chezmoi.toml.tmpl` and bootstrap's own prompts,
grouped into pages:

1. **Machine:** machine type (`personal` or `work`; `remote` is set only by `machine add`),
   hostname.
2. **Identity:** GitHub handle (prefilled from `gh`), names and emails per context.
3. **Keys and secrets:** Bitwarden server (US, EU or a Vaultwarden URL), ssh key (keep,
   generate, restore from Bitwarden), store the key in Bitwarden.
4. **Network:** mesh provider (meshnet, netbird, none) and its settings.
5. **Extras:** agents repo, extra packages, Slack and Matterhorn settings.
6. **Review:** every answer on one page, editable, then apply.

Fields validate as you type (email shape, a reachable Bitwarden URL, a known GitHub
handle). Answers are written to `~/.config/chezmoi/chezmoi.toml` `[data]`, as `bootstrap.sh`
does today, so chezmoi's `promptStringOnce` finds them and never prompts. The template's
prompts stay as a fallback for anyone running plain `chezmoi init`.

The apply step shows one progress view: each step with a spinner, a tick or a failure, and
the failing step's log on demand. The same view serves `update`.

## What happens to the bash

- **`scripts/bootstrap.sh` shrinks to an installer.** It detects OS and architecture,
  downloads the matching `athome` release, checks it against the release's checksum file,
  installs it to `~/.local/bin`, and runs `athome init` with its arguments passed through.
  `--install-from <path>` installs a local binary instead, for the e2e suite and offline
  machines. The README's install steps (clone, then `./scripts/bootstrap.sh`) stay the same.
- **Everything else in `bootstrap.sh` moves to Go:** config loading, sudo caching,
  installing chezmoi, ssh key import, generation and the Bitwarden copy, GitHub key and
  signing-key registration, the github.com ssh config seed, `chezmoi init --apply`, the
  baseline (package manager, gh, rbw, gitleaks, project dirs, mise), pinning chezmoi's
  source directory, and the global git hook.
- **`teardown.sh`** becomes `athome teardown`.
- **`secrets-backup` and `secrets-restore`** become `athome secrets backup|restore`; the old
  names stay as one-line wrappers for one release, then go.
- **chezmoi run scripts stay bash.** They are chezmoi's mechanism and render from templates.
- **`cockpit` and `bw-setup`** stay as they are.

## Versioning and self-update

- `athome version` prints the version and commit.
- `athome update` checks for a newer release, shows the changelog, and replaces itself after
  verifying the checksum. On a box, the laptop passes the version to update to, because the
  box cannot read the inventory (remote-machines spec).
- The binary and the chezmoi source are released together from one tag, so a binary always
  matches the dotfiles it applies.

## Updating the rest of termage

`athome update` also updates grofe and canopy, through their own updaters (grofe's release
install with checksum, canopy's pull), at the versions athome pins. Each tool keeps its own
updater, so it still works without athome.

## Release hosting

GitHub releases on the athome repo for now. The installer reads one base URL, so moving to
termage later changes one value.

## Layout

```
cmd/athome/            # main
internal/cli/          # subcommands
internal/tui/          # Bubble Tea models: home, init form, progress view
internal/steps/        # each bootstrap step, shared by TUI and non-interactive runs
internal/config/       # bootstrap.toml loading and precedence
internal/run/          # running external tools, logging, dry run
```

Each step in `internal/steps` is one function with a name, a check (already done?) and an
apply. The TUI and `--config` runs execute the same list, so they cannot drift, and a rerun
after a failure resumes at the first step whose check fails.

## Testing

- The e2e containers (`test/e2e/Dockerfile`, `Dockerfile.fedora`) run
  `scripts/bootstrap.sh --install-from <built binary> --config test/e2e/bootstrap.toml`.
  `verify.sh`, `verify-baseline.sh` and `verify-teardown.sh` pass unchanged: that is the
  migration's acceptance test.
- Go unit tests per step: check and apply against a temp home, external tools faked.
- Config precedence tests: flag over env over file over default, and the no-terminal,
  no-config defaults.
- Every `bootstrap.sh` flag has a test that `init` accepts it with the same effect.
- TUI tests with Bubble Tea's test helpers: the form fills, validates and produces the same
  `[data]` as the equivalent `bootstrap.toml`.
- Running `athome init` twice changes nothing the second time.

## Migration

1. Build the binary with `init --config` and the e2e suite green, while `bootstrap.sh` still
   holds the full logic. Run both in CI.
2. Switch `bootstrap.sh` to the installer. Keep the old script as `bootstrap-legacy.sh` for
   one release.
3. Add the TUI on top of the same steps.
4. Move `teardown` and `secrets`, then delete the legacy script.

## Docs

Each migration step updates the docs it changes, in the same PR:

- `README.md`: install still starts with `./scripts/bootstrap.sh`; add `athome` and the TUI.
- `docs/install.md`: the installer, `--install-from`, `--config`, non-interactive runs.
- `docs/how-it-works.md`: the binary, the step list, and how chezmoi, mise and
  `packages.yaml` split the work.
- `docs/secrets.md`: `athome secrets backup|restore` in place of the old script names.
- `docs/customizing.md`: where answers live and how to rerun `init`.
- A new `docs/commands.md`: every subcommand and flag.

## Open questions

None at this stage.

## Build 1: `init --config` beside the bash (migration step 1)

Goal: an `athome` binary whose `init` does everything `bootstrap.sh` does today, proven by
the unchanged e2e verify scripts, while `bootstrap.sh` stays the installed path.

Decisions:

- Go 1.27, module `github.com/morrieinmaas/athome`, `go test ./...`. Dependencies: stdlib
  plus `github.com/BurntSushi/toml`. Charm libraries arrive with the TUI (step 3).
- Build 1 has no prompts. Every question `bootstrap.sh` asks is answered from flags, env,
  file or default, as with `--non-interactive` today. Plain prompts arrive with step 2.
- Teardown stays `teardown.sh`; `scenario.sh` picks the bootstrap runner from
  `ATHOME_E2E_RUNNER` (`bash`, the default, or `binary`).
- sudo is requested only when a step first runs a sudo command, then kept warm for the rest
  of the run.
- `--import-ssh-bw` never overwrites an existing host key (bash did); it logs that the key
  was kept and how to restore over it.
- When the GitHub handle or id cannot be derived (offline, rate limit), athome keeps the
  value already in `[data]`; `bootstrap.sh` wrote an empty string.
- On Fedora, athome logs a note when RPM Fusion is still missing after the package-manager
  script (bootstrap.sh is silent); no warning.

Acceptance criteria:

1. `athome init` accepts `--ref`, `--machine`, `--config`, `--no-agents`, `--import-from`,
   `--import-ssh`, `--import-ssh-bw`, `--non-interactive`, `-y`, `--yes`, `-h`, `--help`;
   any other flag exits 2.
2. Config discovery and precedence match `bootstrap.sh`: `--config`, then
   `$repo_root/bootstrap.local.toml`, then `~/.config/athome/bootstrap.toml`; `ATHOME_*`
   env over file over default; `--machine` and `--no-agents` over the file.
3. An unreadable `--config`, `machine` not `personal|work`, or `includeAgents` not
   `true|false` exits 2 with a message naming the value.
4. `~/.config/chezmoi/chezmoi.toml` `[data]` written by `athome init` equals the one
   `bootstrap.sh` writes for the same inputs, key for key.
5. Each step is a name, a check and an apply; a step whose check passes is skipped.
6. A run that fails at step N, rerun, skips steps before N and resumes at N.
7. A second `athome init` with the same inputs, without `--ref`, applies no step. This
   excludes `sshKeyAction = "generate"`, which rotates the host's key on every run as
   `bootstrap.sh` does (the step prints a note saying so), `sshKeyAction = "reupload"`, or when a GitHub key
   upload failed, or while an unlocked `rbw` has no secrets restored and no `secrets-restore`
   script (the follow-up note is written again each run), or when the repo is on a detached HEAD
   that cannot be checked out to main (a failed `gh ssh-key add` is retried). The manual-upload
   fallbacks (an env token without key scopes, a failed `gh auth refresh`) count as done once
   a run has printed the instructions and written the marker; the first run still applies.
8. With `ATHOME_CI` set no GitHub key upload is attempted.
9. The e2e containers (arch, fedora) pass `verify-baseline.sh`, `verify.sh` and
   `verify-teardown.sh` unchanged with `ATHOME_E2E_RUNNER=binary`; CI runs both runners.
10. `athome version` prints the version and commit set at build time.

Edge cases: an empty config file; a config value with `=` or `#` inside quotes; an
existing ssh key with no `.pub`; `~/.local/share/chezmoi` already a different symlink or a
real directory; a detached HEAD; `gh` missing or not authenticated; a non-UTF-8 or
space-containing `$HOME` path; `--import-ssh` without `--import-from` (prints a note, imports nothing, the run continues with a
fresh key and exits 0, as bash does).

Test list (unit unless marked):

- config: flag over env over file over default, per key; discovery order with all three
  files present; each validation error above exits 2; the empty file gives defaults;
  `a = "x=y # z"` reads `x=y # z`; `test/e2e/bootstrap.toml` loads to its known values.
- chezmoi data: for `test/e2e/bootstrap.toml`, the written `[data]` equals a golden file
  captured from `bootstrap.sh`.
- steps core: a passing check skips apply; a failing apply stops the run and names the
  step; rerun resumes at it; two full runs, the second applies nothing.
- each step: check and apply against a temp home with external tools faked; recorded
  commands match the bash per OS (darwin `nb`, fedora `dnf`, arch `yay`, `gh` as
  `github-cli` on arch).
- github: `ATHOME_CI` set records no `gh ssh-key add`; an already-registered key records
  only the signing-key check.
- cli: every flag above parses with its effect; an unknown flag exits 2; `version` output.
- e2e: arch and fedora with `ATHOME_E2E_RUNNER=binary`, unchanged verify scripts.

Out of scope for build 1: the installer switch, release builds, prompts, the TUI,
`teardown`, `secrets`, `update`, `status`, `machine`, `keys`.

## Build 2: the installer, releases and plain prompts (migration step 2)

Goal: `scripts/bootstrap.sh` installs a checksummed `athome` release and runs
`athome init`, and an interactive `athome init` asks the questions `bootstrap.sh` asks
today, so the binary becomes the installed path.

Decisions:

- The current script moves to `scripts/bootstrap-legacy.sh`, unchanged except its usage
  text and the messages that name the script itself, for one release. The e2e `bash` runner calls it; the `binary` runner calls the
  installer with `--install-from`.
- The installer picks the release for the clone it runs from: the tag at `HEAD`, else the
  nearest tag (`git describe --tags --abbrev=0`). `ATHOME_VERSION` overrides it. With no
  tag it exits 1 and names `--install-from` and `go build`. If the asset download for the
  tag finds no release asset (HTTP 404, or a missing file under a `file://` base) and
  `--install-from` was not given and `ATHOME_VERSION` was not set (the tag came from git), it prints a note to stderr and execs
  `scripts/bootstrap-legacy.sh` with the same arguments. With `ATHOME_VERSION` set, a missing asset exits 1 naming the tag and URL instead. Any other download failure
  (network, 5xx) exits 1 naming the URL and curl's error, and a missing `curl` exits 1
  naming it. The fallback is for one
  release only: tags v0.4.0 to v0.4.3 have no athome assets, so a fresh clone would
  otherwise fail. It goes when `bootstrap-legacy.sh` is deleted (migration step 4).
- `ATHOME_REPO` names the repo to apply instead of searching from the working directory; a
  relative value is made absolute. `--install-from=PATH` is accepted as well as
  `--install-from PATH`.
- Base URL: `ATHOME_RELEASE_BASE`, default
  `https://github.com/morrieinmaas/athome/releases/download`. Assets are
  `<base>/<tag>/athome_<os>_<arch>` and `<base>/<tag>/checksums.txt`, os `darwin|linux`,
  arch `amd64|arm64`.
- An installed `~/.local/bin/athome` that already reports the wanted version is kept and
  nothing is downloaded, so a rerun works offline.
- GoReleaser builds plain binaries (no archives) with `CGO_ENABLED=0` and the same
  `-X ...cli.Version` and `-X ...cli.Commit` ldflags as the e2e image, except releases embed the short commit ({{.ShortCommit}}) while the e2e image embeds the full SHA. A tag `v*` push
  publishes the release; CI runs `goreleaser check` and a snapshot build on every PR.
- Prompts are plain lines on stderr, read from stdin. They appear only when stdin is a
  terminal and none of `--non-interactive`, `-y`, `--yes` is given. A value set by flag,
  env or file is never asked for (bash asked the ssh-key and Bitwarden questions with the
  env value as the default; athome takes the value).
- The nine prompts, with bash's wording and defaults: symlink the repo to chezmoi's path
  (y); existing key: use, generate or reupload (use); create a key when none exists (y);
  upload keys to GitHub (y); refresh `gh` scopes (y); GitHub username (asked again while
  empty); Bitwarden server URL (empty = US cloud); use Bitwarden (y); restore secrets
  now (y). An answer of empty takes the default; EOF on stdin takes the default.
- Ctrl-C at an open prompt prints "athome: interrupted", runs cleanup and exits 130, as
  bash dies at read.
- Dependencies are the standard library, BurntSushi/toml and golang.org/x/term (terminal
  detection).
- Deliberate differences from bash: Confirm accepts y, yes, n and no, case-insensitive,
  and asks again on anything else (bash: only `y` is yes, anything else is no); prompt
  texts use a colon where bash used a dash; the Bitwarden URL is not asked when one is
  already stored in `chezmoi.toml`, and not when `useBitwarden` is "false" (bash asked
  whenever the URL was empty); a stored Bitwarden URL is kept by later runs (bash
  overwrote it with empty); interactive reruns ask the upload, Bitwarden and
  restore questions again, as bash does (no remembered declines).

Acceptance criteria:

1. `bootstrap.sh` on darwin or linux, amd64 or arm64, downloads `athome_<os>_<arch>`, checks
   its sha256 against `checksums.txt`, installs it as `~/.local/bin/athome` with mode 0755,
   and execs `athome init` with every argument except `--install-from <path>`.
2. A missing asset (404, or a missing file over `file://`) falls back to
   `bootstrap-legacy.sh` with a note; a checksum mismatch, a missing checksums line, a
   missing sha tool or curl, any other download failure or an unsupported os or arch exits
   1, leaves
   no `~/.local/bin/athome` behind, and names the problem.
3. `--install-from <path>` installs that file and downloads nothing; a missing path exits 1.
4. An installed binary at the wanted version is reused without a download.
5. `bootstrap.sh --help` prints the installer usage; any other flag passes through to
   `athome init`, which keeps rejecting unknown ones with exit 2.
6. `scripts/bootstrap-legacy.sh` behaves as `bootstrap.sh` did before this build.
7. `goreleaser check` passes and a snapshot build yields the four binaries and
   `checksums.txt`; `athome version` on a snapshot binary prints the version and commit.
8. Interactive `athome init` asks each of the nine questions at the point bash asks it, and
   the answer has the same effect as the matching flag, env or file value.
9. With a value already set by flag, env or file, or with no terminal, or with `-y`, no
   prompt is printed and no stdin is read.
10. The e2e jobs (arch, fedora) pass unchanged verify scripts with both runners, the binary
    runner now going through `scripts/bootstrap.sh --install-from`.

Edge cases: `--install-from` as the last argument with no path; a path with spaces;
`~/.local/bin` missing; neither `sha256sum` nor `shasum` present (exit 1, names both);
`checksums.txt` without a line for the asset; an installed `athome` that fails to run
(replaced); a clone with no tags; a prompt answered with surrounding spaces or upper case
(`Y`, ` n `); an invalid menu answer (asked again); stdin closed mid-run; Ctrl-C at an
open prompt (exit 130).

Test list (unit unless marked):

- installer (bats, `test/bootstrap.bats`, release served from a `file://` base): good
  checksum installs and execs a stub `athome` that records its args; mismatch exits 1 with
  no binary left; missing asset runs a stub legacy script with the same args and a note; an unreachable
  `http://127.0.0.1:9` base exits 1 without the fallback; no `curl` exits 1; unsupported `uname` (faked) exits 1;
  `--install-from` installs that file with no download; `--install-from` with no path or an empty path exits 1;
  version already installed downloads nothing; no tags exits 1 naming `--install-from`;
  `ATHOME_VERSION` overrides the tag; `--help` prints usage.
- release (CI): `goreleaser check`; `goreleaser build --snapshot` lists four binaries and
  the checksum file.
- prompt package: confirm with `""`, `y`, `Y`, ` n `, EOF; choose with a first letter, a
  full word, an invalid answer then a valid one; string re-asks while empty when required.
- each prompt site: interactive with a scripted stdin gives the same recorded commands as
  the matching env value; a preset value prints no prompt; non-interactive reads no stdin.
- e2e: arch and fedora, both runners.

Out of scope for build 2: the TUI, `teardown`, `secrets`, `update`, `status`, self-update,
and moving the release host to termage.

## Build 3: the TUI (migration step 3)

Goal: an interactive `athome init` fills one form, then shows one progress view over the
same step list; `athome` with no arguments on a terminal opens a home screen.

Decisions:

- Libraries: Bubble Tea, huh, lipgloss and bubbles, latest stable releases at build time.
  TUI tests use `github.com/charmbracelet/x/exp/teatest`.
- The TUI runs when `init` would prompt today: stdin and stdout are terminals and none of
  `-y`, `--yes`, `--non-interactive` is given. `--plain` (and `ATHOME_PLAIN=1`) keeps the
  build 2 line prompts. Non-interactive runs, `--config` runs and e2e are unchanged.
- The form has the pages of "The init form": Machine (machineType `personal|work`,
  hostname), Identity (githubHandle, githubId (derived, hidden), personalName, workEmail, workName,
  sidebizEmail, sidebizName), Keys and secrets (bitwardenUrl, ssh key action, store the key
  in Bitwarden, upload keys to GitHub, restore secrets), Network (meshProvider
  `meshnet|netbird|none`, nordvpnCountry when meshnet, netbirdManagementUrl when netbird),
  Extras (includeAgents, agentsRepo, extraPackages, Slack and Matterhorn keys), Review.
- Prefill order per field: flag or `ATHOME_*` env, config file, stored `chezmoi.toml`
  `[data]`, then the step's own default (githubHandle from `gh api user`, hostname as the
  data step derives it). A field set by flag or env is shown read-only.
- Hostname: The hostname answer sets `[data].hostname` for that run only; the ssh key keeps chezmoi's hostname, and a later non-interactive run rewrites `hostname` as build 2 does.
- Answers go into `env.Cfg.Data` under the keys the steps already read
  (`sshKeyAction`, `generateSshKey`, `useBitwarden`), plus three new ones the steps honour:
  `uploadSshKeys`, `restoreSecrets` and `bwBackupSshKey` (`"true"|"false"`; the last is the
  "store the ssh key in Bitwarden" answer, honoured by ssh-bw-backup). So no step asks a question
  the form answered. The ssh key field shows `use|generate|reupload` when a key exists,
  else `generate|skip`.
- Validation on leaving a field: emails must match `^[^@\s]+@[^@\s]+\.[^@\s]+$` (empty
  allowed where the template allows empty); bitwardenUrl empty or an `https://` URL that
  answers an HTTP GET within 5 s; githubHandle must exist (`gh api users/<h>`, else the
  public API), checked within 5 s; a failed network check is an inline error the user can
  override by pressing enter again (offline machines must still be able to init).
- Review lists every answer; enter applies, `b` goes back to edit, `q` or Ctrl-C quits
  with exit 130 and writes nothing.
- The progress view lists every step with a spinner, a tick, "skipped" or a cross. `l`
  toggles the run log (what `env.Log` gets) under the list. On failure it stays on screen
  until a key is pressed, then exits 1 with the same error line the plain run prints.
- A question a step still asks (the repo symlink confirm, the gh scope refresh) and every
  `Interactive` or `NeedsSudo` command (sudo, `bw-setup`, `secrets-restore`, the gh
  device flow) runs with the TUI's terminal released and restored afterwards
  (`Program.ReleaseTerminal`/`RestoreTerminal`), so sudo asks only when a step needs it,
  as today.
- After the progress view exits, the run log is printed, then the finish summary, so follow-up lines (manual upload commands, warnings) stay visible.
- `athome` with no arguments on a terminal opens the home screen: version and commit, repo
  root, machine type, hostname and GitHub handle from `chezmoi.toml` (or "not set up"),
  then a menu: init, update, quit. Machines joins the menu in a later build. Without a
  terminal it prints usage as today.

Acceptance criteria:

1. On a terminal, `athome init` shows the form, not line prompts; with `--plain` or
   `ATHOME_PLAIN=1` it shows the build 2 prompts.
2. Filling the form and applying writes the same `chezmoi.toml` `[data]` as a
   `bootstrap.toml` with the same values run with `--config`.
3. No step asks a question the form answered (ssh key, Bitwarden, upload, restore).
4. Fields set by flag or env are read-only and their values used.
5. An invalid email blocks the page with an inline error; a failed network check shows an
   error and a second enter accepts the value.
6. Quitting at any form page exits 130 and leaves `chezmoi.toml` untouched.
7. The progress view shows each step's state and the log on `l`; a failing step exits 1
   with the step's error.
8. Interactive and sudo commands run with the terminal released.
9. `athome` on a terminal shows the home screen with the stored values; init from it runs
   the form.
10. `-y`, `--config` and e2e behave exactly as in build 2.

Edge cases:

- No `chezmoi.toml` yet: every field from defaults; home screen says "not set up".
- `gh` missing or logged out: githubHandle is empty and required.
- Unicode in names (`Jürgen Müller`, `山田`) round-trips into `chezmoi.toml`.
- A terminal narrower than 60 columns still renders (huh wraps).
- Ctrl-C during the progress view cancels the context, runs cleanup, exits 130.
- `meshProvider=none` hides both mesh fields and writes them empty.

Test list:

- unit: form model with every field filled produces the `Data` map equal to the one
  `config.Load` gives for the equivalent `bootstrap.toml` (criterion 2).
- unit: prefill precedence: env over file over stored over default; env field read-only.
- unit: email validator accepts `a@b.co`, rejects `a@b`, `a b@c.d`, accepts empty where
  optional.
- unit: network check fake failing once, second enter accepts.
- unit: steps honour `uploadSshKeys=false` and `restoreSecrets=false` without asking
  (`env.Ask` set to an asker that fails the test on any read).
- unit: quit at page 1 and at review exits 130, `chezmoi.toml` absent.
- teatest: progress view over three fake steps (done, applied, failing) shows tick,
  skipped and cross, `l` shows the log, exit code 1.
- unit: a runner wrapper calls release before and restore after an `Interactive` and a
  `NeedsSudo` command, and neither for a plain command.
- teatest: home screen with a stored `chezmoi.toml` shows handle and machine type; without
  one shows "not set up".
- unit: `--plain` and `ATHOME_PLAIN=1` select the line prompts; `-y` selects neither.
- unit: unicode name round-trip through `writeData`.
- e2e: unchanged matrix stays green.

Out of scope: the machines menu entry, `athome status`, teardown and secrets
(build 4).

## Build 4: teardown, secrets, update and status (migration step 4)

Goal: `athome teardown`, `athome secrets backup|restore`, `athome update` and `athome status`
replace the bash that did those jobs, and the bash kept for one release
(`bootstrap-legacy.sh`, the installer's fallback to it, the e2e `bash` runner) is deleted,
with the e2e round trip tearing down through `athome teardown`.

Decisions:

- **New packages:** `internal/teardown`, `internal/secrets`, `internal/selfupdate` and
  `internal/status`, each reaching external tools only through `run.Runner`. The update
  list is `steps.Update()` in `internal/steps`, built from `steps.Step` like `steps.All`,
  so a progress view can run it. No new dependencies.
- **`scripts/teardown.sh` is deleted, not wrapped.** Every machine bootstrapped since
  build 2 has `~/.local/bin/athome`, and a wrapper would have to find the binary that
  `--all` deletes. `test/teardown.bats` is ported to Go tests and deleted, the
  `mise run teardown` task runs `athome teardown`, and `scripts/lib/colors.sh` and
  `scripts/lib/pm.sh` go with their last users. `scripts/lib/ssh.sh` stays, because
  `internal/steps/github_config_test.go` reads it.
- **Teardown is a 1:1 port.** Same flags (`--state`, `--dotfiles`, `--all`, the last one
  given wins; `--execute` and its alias `--run`; `--dry-run`; `--yes` and `-y`; `-h`,
  `--help`), same tiers, same section titles, same order, same `  would: <cmd>` and
  `  + <cmd>` lines, colour only when stdout is a terminal. File removals run in process
  (`os.Remove`, `os.RemoveAll`, a missing path is not an error) and print as the `rm -f`
  or `rm -rf` line bash printed. `--execute` prints the dry-run plan, asks for the typed
  tier name (unless `--yes`), then runs the plan. Deliberate differences from bash:
  - `--all` adds a section `athome binary` before the user-data note, removing
    `~/.local/bin/athome` only (a binary elsewhere on PATH is left alone).
  - With `chezmoi` not on PATH, its two calls print `  skip: chezmoi not found` instead of
    failing, so a rerun after a half-finished `--all` completes.
  - A failing action stops the run with exit 1 naming the command (bash exited with the
    command's own status under `set -e`); `rbw stop-agent` failures are still ignored.
  - The hostname for the ssh key paths is the one init names the key from (the short hostname, the part before the first dot, which is what chezmoi's `.chezmoi.hostname` is; computed in process so a dry run runs no `chezmoi execute-template`), not the full `os.Hostname()` (bash: `uname -n`).
  - The em dashes in bash's messages become a colon or a comma. Every substring
    `test/teardown.bats` checks is kept: `pick a tier`, `unknown argument`, `DRY-RUN`,
    `would:`, `LEFT ALONE`, `.secrets`, `aborted`.
- **`athome secrets backup` and `restore` are 1:1 ports** of the two scripts: the same
  mirrored paths and exclusions, the same vault (`SECRETS_VAULT`, default
  `~/.local/share/attic`; `SECRETS_VAULT_NAME`, default `attic`), the same plaintext gate,
  commit identity (`secrets-backup <secrets@local>`) and message, and the same exit codes.
  Deliberate differences: `git-crypt` and `rbw` are found on PATH (the scripts hardcoded
  `/opt/nanobrew/prefix/bin`, which does not exist on Linux); an underivable vault owner
  exits 1 instead of cloning `/attic`; the decoded key file is removed on every path; any
  argument to `backup`, or one other than `--dry-run` to `restore`, exits 2 (the scripts
  ignored them). A failed `git commit` in `backup` exits 1 (the script carried on to the push). A failing `git add -A` in `backup` exits 1 with git's stderr (the script could print "vault already in sync" and exit 0), and so does a failing `git diff --cached --name-only -z` (the script checked nothing, then committed and pushed). A failed key read from Bitwarden or a failed `git-crypt unlock` in `restore` exits 1 before any file is copied (the script carried on and copied the still-encrypted files). A failing `git ls-files` in `restore` exits 1 with a message (the script ignored it); a
  vault with no tracked files still copies nothing and exits 0. `backup`'s default vault stays
  `~/.local/share/attic` whatever `SECRETS_VAULT_NAME` is, as in the script. `restore --dry-run` still clones or pulls and unlocks, as the script does;
  only the copy is skipped.
- **The old names stay one release as wrappers.** `home/dot_local/bin/executable_secrets-backup`
  and `executable_secrets-restore` become a shebang plus one line,
  `exec athome secrets backup "$@"` and `exec athome secrets restore "$@"`, and go in the
  next release. `executable_mise-secret-init` and `run_once_17-secrets-restore-reminder.sh`
  call and name `athome secrets backup|restore`.
- **init restores in process.** The `secrets-restore` step of `athome init` calls
  `internal/secrets` instead of running `~/.local/bin/secrets-restore`, so the
  "no script yet" state and the `secrets-restore-hint` step go. The prompt reads
  `restore your encrypted secrets now (athome secrets restore)?` and the follow-up line
  names `athome secrets restore`. Build 1 acceptance criterion 7's "no `secrets-restore`
  script" exception no longer applies.
- **What "pinned versions" means.** athome's own version is the release tag; the binary
  and the chezmoi source come from one tag, so nothing else pins it. grofe and canopy are
  pinned in one new file, `home/.chezmoidata/pins.toml`:

  ```toml
  [pins]
  grofe = "v0.3.1"
  canopy = "f445cd0f9f665d2ea3fb6e0694934ae45ef10942"
  ```

  `run_onchange_17-install-grofe.sh.tmpl` and `run_onchange_18-install-canopy.sh.tmpl`
  render `{{ .pins.grofe }}` and `{{ .pins.canopy }}` in place of their hardcoded refs, so
  bumping either is one edit and chezmoi reruns the script because its rendered text
  changed. Those two scripts are the tools' own updaters; athome never installs grofe or
  canopy itself, it only applies. `athome status` reads the pins from this file.
- **grofe installs from its release.** `run_onchange_17` keeps `ATHOME_GROFE_BIN` first,
  then downloads
  `https://github.com/termage/grofe/releases/download/<pin>/grofe-<pin>-<target>.tar.gz`
  and its `<tarball>.sha256` beside it (the pin is the tag, e.g. `v0.3.1`), targets `x86_64-unknown-linux-musl`,
  `aarch64-unknown-linux-musl`, `x86_64-apple-darwin`, `aarch64-apple-darwin`, checks the
  sha256 (`sha256sum`, else `shasum -a 256`), and installs `grofe-<pin>-<target>/grofe`.
  The cargo build goes. A download that fails prints
  `grofe: cannot download <url>, so grofe is not installed; tmux stays tmux` and exits 0, as
  an unreachable repo does today; a checksum mismatch or a missing `.sha256` file exits 1.
- **`athome update [--to <version>] [--yes|-y]`** first resolves the target, so the step
  checks only read: `git -C <repo> fetch --quiet --tags --force origin`, then `--to`, else
  the highest local `vX.Y.Z` tag. A failed fetch exits 1 before any step. It then runs
  `steps.Update()`, four steps:
  1. `self`: check: the running version, with any leading `v` stripped, equals the
     target's, or is newer and `--to` was not given. Apply: print the
     changelog, `git -C <repo> log --no-merges --format='  %s' v<current>..<target>`
     (omitted, with a note, when the current version is not a tag); on a terminal without
     `--yes`, confirm `update athome from <current> to <target>?` (default yes; no prints
     `athome update: cancelled, nothing changed` and exits 0); download
     `<base>/<target>/athome_<os>_<arch>` and `checksums.txt` with the installer's
     `ATHOME_RELEASE_BASE` and default, check the sha256, write `~/.local/bin/.athome.<pid>`
     with mode 0755 and rename it over `~/.local/bin/athome`, then exec
     `~/.local/bin/athome update --to <target>` with `--yes` passed on if given. The new
     binary's `self` check passes and the run continues there.
     Without `--to`, a target older than the running version is not installed (no
     downgrade); with `--to` it is, because a box is told its version by the laptop.
     A version that is not `X.Y.Z` (`dev`, `e2e`, a snapshot) skips `self` with
     `athome: <version> is not a release; not replacing it (pass --to to install one)`,
     unless `--to` is given.
  2. `source`: on a branch, `git -C <repo> pull --ff-only`. On a detached HEAD at a tag
     (a box), `git -C <repo> checkout --quiet --detach <target>`, so the source matches
     the binary. A detached HEAD at no tag is left as is, with a note. Check: none, it
     always runs.
  3. `apply`: `chezmoi apply`. This installs new native packages (`run_onchange_02` keyed
     on `packages.yaml`) and moves grofe and canopy when their pins changed. athome does
     not upgrade OS packages (`nb upgrade`, `pacman -Syu`, `dnf upgrade`). Check: none.
  4. `mise`: check `mise ls --missing --json` lists nothing; apply `mise install`, run in
     `$HOME`.

  It ends with the init summary line, `athome: applied N, skipped M`. Exit codes: 0 done
  or cancelled, 1 a step failed (the error names the step, as init does), 2 a usage error,
  130 Ctrl-C at the prompt. `--to` takes `vX.Y.Z` or `X.Y.Z`; anything else exits 2, and a
  well-formed tag that `origin` lacks exits 1 naming it. `sync --all` joins the end of the
  list with the remote-machines spec.
- **`athome status [--json]`** reads, never writes, and reports five components:
  - `athome`: the running version against the highest `vX.Y.Z` tag from
    `git -C <repo> ls-remote --tags --refs origin`, 10 second timeout.
  - `dotfiles`: `chezmoi status --exclude=scripts`; current when it prints nothing.
  - `mise`: `mise ls --missing --json`; current when it lists nothing.
  - `grofe`: `~/.local/lib/grofe/grofe -V` (`grofe 0.3.1`) against `pins.grofe`, a leading
    `v` ignored.
  - `canopy`: `git -C ~/.local/share/canopy rev-parse HEAD` against
    `git -C ~/.local/share/canopy rev-parse --verify <pins.canopy>^{commit}`.

  Each status is one of `current`, `not current`, `not installed` (the tool or its store is
  missing) or `unknown` (offline, no `pins.toml`, a version that is not a release). Text
  output is a header `athome <version> (<commit>), repo <repo> at <ref>`, `<ref>` being
  the branch, else the exact tag, else the short commit, then one line per component,
  `fmt.Sprintf("%-9s %-14s %s", name, status, detail)`. `--json` prints

  ```json
  {"version": "0.5.0", "commit": "abc1234", "repo": "/home/u/athome", "ref": "main",
   "current": false,
   "components": [{"name": "athome", "status": "not current",
                   "installed": "0.5.0", "wanted": "v0.5.2", "detail": ""}]}
  ```

  with the five components in the order above, and top-level `current` true only when all
  five are `current`. Exit 0 whenever the report printed, whatever it says; 1 when the
  repo is not found (the init lookup and message); 2 a usage error.
- **TUI:** build 4 imports nothing from `internal/tui`. The home menu gained `update` when
  build 3 merged after build 4; it runs the same path as `athome update`.
- **Legacy goes.** `scripts/bootstrap-legacy.sh` is deleted. In `scripts/bootstrap.sh`, a
  missing asset (404, or a missing file over `file://`) now exits 1 with
  `bootstrap: no athome release asset for <tag> at <url>; <hint>` whether the tag came
  from git or `ATHOME_VERSION`, and the `explicit` flag goes. `test/e2e/scenario.sh`
  drops `ATHOME_E2E_RUNNER` and its `bash` branch, always bootstraps through
  `scripts/bootstrap.sh --install-from "$athome_src"`, always checks the second init is a
  no-op, and tears down with `athome teardown --all --execute --yes` (the
  `~/.local/bin/athome` the bootstrap installed, which deletes itself; phase 3 reinstalls
  from `$athome_src`, `/usr/local/bin/athome` in the image). `test/e2e/run.sh` and
  `.github/workflows/e2e.yml` drop the runner, leaving one job per distro.
- **`athome` usage** lists `init`, `update`, `status`, `teardown`, `secrets`, `version`.
  `athome secrets` with no subcommand, or one other than `backup` or `restore` (`rekey`
  included), exits 2 with usage naming `backup|restore`.

Acceptance criteria:

1. `athome teardown` with no tier exits 2 printing usage and `pick a tier: --state |
   --dotfiles | --all`; an unknown argument exits 2 with
   `athome teardown: unknown argument: <arg> (try --help)`; `--help` exits 0 and names all
   three tiers.
2. Without `--execute`, teardown prints the header
   `athome teardown: tier=<tier>  mode=DRY-RUN (no changes)`, only `  would: ` action lines,
   and `Dry run only, nothing changed. Re-run with --execute to apply.`; it changes nothing
   and runs no external command except the read-only `chezmoi managed`,
   `chezmoi source-path` and `nb list`.
3. Each tier plans exactly what `scripts/teardown.sh` plans for the same home and OS, in
   the same order, plus, for `--all`, `rm -f <home>/.local/bin/athome` in the `athome binary`
   section; `--all` ends with the user-data note naming `~/.secrets` and
   `~/personal, ~/sidebiz, ~/work` under `LEFT ALONE`, and nothing under those paths is
   ever removed.
4. `--execute` prints the dry-run plan, then
   `This will PERMANENTLY remove the above for tier "<tier>".` and
   `Type <tier> to proceed (anything else aborts): `, and reads one line from stdin. The
   tier name, spaces trimmed, proceeds; anything else, or EOF, prints
   `athome teardown: aborted, nothing was changed` and exits 2 having changed nothing.
   `--yes` skips the question.
5. A completed `--execute` prints `+ <cmd>` per action and ends with
   `✓ teardown (<tier>) complete. Re-run ./scripts/bootstrap.sh for a fresh start.`, exit 0.
   A failing action exits 1 naming it, after the actions before it ran.
6. `athome secrets backup` mirrors the same files into the vault as `secrets-backup`, prints
   `✓ vault already in sync (no changes)` and exits 0 when nothing changed, exits 2 with
   `ABORT: plaintext detected in staged files` and no commit when any staged file lacks the
   git-crypt header, commits and pushes otherwise (a failed push prints the
   `committed locally ... but push failed` warning and still exits 0), and exits 1 with
   `vault not found at <vault>; run 'athome secrets restore' to clone it first` without a
   vault.
7. `athome secrets restore` clones (or pulls, ignoring a failed pull), unlocks with the key
   from `rbw get git-crypt/<name>-key` when the first tracked file is encrypted, copies
   every tracked file except `.gitattributes` to `$HOME/<path>` with mode 0600, makes
   `~/.secrets` and its directories 0700, and exits 1 if any copy failed. A locked or
   missing rbw on an encrypted vault exits 1 with
   `rbw is locked; run: rbw unlock, then re-run athome secrets restore`. `--dry-run` prints
   `  would restore  <target>` per file and writes nothing under `$HOME`.
8. `secrets-backup` and `secrets-restore` on PATH run the matching `athome secrets`
   command with their arguments and exit code.
9. `athome init`'s `secrets-restore` step restores in process; init's step list no longer
   has `secrets-restore-hint`, and its prompt and follow-up name `athome secrets restore`.
10. `athome update` with a newer release replaces `~/.local/bin/athome` only after its
    sha256 matches `checksums.txt`, prints the commit subjects between the two tags first,
    then runs `source`, `apply` and `mise` in the new binary. A checksum mismatch, a
    missing checksums line or a missing asset exits 1 with the installed binary unchanged
    and no `.athome.*` file left behind.
11. `athome update --to <version>` installs that version, older or newer; without `--to` a
    newer running version is kept. Already at the target, `self` is skipped and nothing is
    downloaded.
12. On a branch `source` runs `git pull --ff-only`; on a detached HEAD at a tag it checks
    out the target tag; at a detached untagged commit it changes nothing and prints a note.
13. `athome status` prints the header and the five rows; `--json` prints the documented
    object; both exit 0 whatever the statuses, with `unknown` for `athome` when `origin` is
    unreachable.
14. `home/.chezmoidata/pins.toml` holds the grofe and canopy pins; the two install scripts
    render them and contain no other copy of the refs; changing a pin changes the rendered
    script (so chezmoi reruns it).
15. `run_onchange_17` installs grofe from the pinned release after checking the sha256,
    exits 0 with a note when the download fails, and exits 1 on a checksum mismatch.
16. `scripts/bootstrap-legacy.sh`, `scripts/teardown.sh`, `test/teardown.bats`,
    `scripts/lib/colors.sh` and `scripts/lib/pm.sh` are gone; no file in the repo outside
    `docs/superpowers`, `docs/plans` and the Go `B:` comments names them, and nothing reads
    `ATHOME_E2E_RUNNER`.
17. The e2e jobs (arch, fedora) pass `verify-baseline.sh`, `verify.sh` and
    `verify-teardown.sh`, the last with one new check that `~/.local/bin/athome` is gone,
    with the teardown done by `athome teardown`; after phase 1, `athome status --json`
    exits 0 and reports `dotfiles` as `current`.

Edge cases: two tier flags (the last wins); `--execute` with stdin closed (aborts, exit
2); an answer of ` all ` (proceeds); `chezmoi` missing (both calls skipped with a note); a
managed path that is a directory or a dangling symlink (skipped, as `-f` does); a managed
path or `$HOME` with spaces; `~/.claude/CLAUDE.md` a symlink into a different directory, or
a real file (kept); `athome` run from `/usr/local/bin` (only `~/.local/bin/athome` removed);
a rerun of `--all --execute` after a run that failed halfway; a vault with no tracked
files (restore copies nothing, exit 0); a vault already unlocked (no `rbw` call); neither
`ATHOME_GITHUB_HANDLE`, `gh` nor `SECRETS_VAULT_REPO` (restore exits 1 naming them); a
`.env.example` in `~/.secrets` (not mirrored); `update` offline (the fetch fails, exit 1, no
step runs); a tag pushed before its release assets exist (exit 1, asset URL named);
`~/.local/bin` not writable (exit 1, binary unchanged); a branch that has diverged from
its upstream (`source` fails, exit 1); a dev build with and without `--to`; `--to v0.5`
(exit 2); `status` with no `pins.toml` (an older clone: grofe and canopy `unknown`), with
grofe or the canopy store missing (`not installed`), or with a pin the canopy store has
not fetched (`not current`, the pin named).

Test list (unit unless marked):

- teardown flags: no tier exits 2 with `pick a tier`; `--bogus` exits 2 with
  `unknown argument`; `--help` exits 0 and names `--state`, `--dotfiles`, `--all`;
  `--state --all` plans `all`; `--run` equals `--execute`; `-y` equals `--yes`.
- teardown plan, against a temp home with a fake runner, for `OS` darwin and linux (arch,
  and fedora: `dnf` found, `pacman` not): golden plan per tier, captured from
  `scripts/teardown.sh`'s dry run on the same fixture before it is deleted, with only the
  differences listed under Decisions applied; `--state` names the three `~/.local/state/athome` markers; `--all` names
  `nanobrew` on darwin and `pacman` on arch, `rm -f <home>/.local/bin/athome`, `.secrets`
  and `LEFT ALONE`; a dry run records only `chezmoi managed`, `chezmoi source-path` and
  `nb list`, and no file in the fixture changes.
- teardown fixture: `chezmoi managed` lists a regular file, a directory, a dangling
  symlink and a path with a space; only the regular file and the spaced path are planned;
  `(2 managed files)`. `~/.claude/skills` pointing into `~/.config/agents` is planned,
  `~/.claude/CLAUDE.md` pointing into `/elsewhere` and a real `~/.claude/RTK.md` are not.
- teardown execute: stdin `"\n"` exits 2 with `aborted` and no file removed; stdin
  `" dotfiles \n"` with `--dotfiles --execute` removes the planned files and keeps
  `~/.secrets/x/.env` and `~/personal/r/f`; `--yes` reads no stdin; a fake
  `chezmoi state reset` exiting 1 makes the run exit 1 naming it with nothing after it
  run; `chezmoi` absent prints `skip: chezmoi not found` twice and exits 0.
- secrets backup, against a temp vault (a real git repo whose index content is faked
  with and without the `\x00GITCRYPT` header): `~/.secrets/a/.env`, `.env.local` and
  `.env.example` mirror the first two only; the five fixed paths mirror when present;
  no change prints `vault already in sync` exit 0; a plaintext staged file exits 2 with no
  commit; a push to an unreachable remote warns and exits 0; no vault exits 1; any
  argument exits 2.
- secrets restore (fake `gh`, `rbw`, `git-crypt`): a missing vault records
  `gh repo clone <owner>/attic <vault> -- -q`; owner from `ATHOME_GITHUB_HANDLE`, else
  `gh api user -q .login`, `SECRETS_VAULT_REPO` over both; an encrypted sentinel with rbw
  locked exits 1 with the message above; unlocked, records
  `rbw get git-crypt/attic-key` and `git-crypt unlock <tmpfile>` and the tmpfile is gone
  afterwards; files land with mode 0600 and `~/.secrets/a` is 0700; `--dry-run` prints
  `would restore` lines and writes nothing under `$HOME`; `--bogus` exits 2.
- wrappers (bats, `test/secrets-wrappers.bats`): with a stub `athome` on PATH recording
  its args, `secrets-restore --dry-run` records `secrets restore --dry-run` and returns
  the stub's exit code 3.
- init: the `secrets-restore` step with rbw unlocked and no `.env` calls the in-process
  restore (recorded `gh repo clone`); `steps.All` has no `secrets-restore-hint`.
- update self (httptest release server, fake runner for git): current 0.5.0, tags
  v0.5.0 and v0.5.2 installs v0.5.2 after printing the faked `git log` subjects, then
  records the exec of `~/.local/bin/athome update --to v0.5.2`; a wrong checksum, no
  checksums line, and a 404 asset each exit 1 with the old binary byte-identical and no
  `.athome.*` left; current v0.5.2 with `--to v0.5.2` downloads nothing; current v0.5.2,
  latest v0.5.0, no `--to`, downloads nothing; `--to v0.5.0` downgrades; version `dev`
  without `--to` skips with the note; `--to v0.5` and `--to latest` exit 2; `--to v9.9.9`
  absent from the tag list exits 1 naming it; a scripted `n` at the prompt prints
  `cancelled` and exits 0; `--yes` reads no stdin.
- update steps: `source` on a branch records `git pull --ff-only`, at a detached tag
  records `checkout --quiet --detach v0.5.2`, at an untagged detached commit records no
  checkout; `apply` records `chezmoi apply`; `mise` with `mise ls --missing --json`
  printing `{}` is skipped, with a missing tool records `mise install`; a failing step
  exits 1 naming it; the last line is `athome: applied N, skipped M`.
- status (fake runner, temp home): all current gives five `current` rows and top-level
  `"current": true`; ls-remote failing gives `athome` `unknown` and exit 0; `chezmoi
  status` printing two lines gives `dotfiles` `not current`; grofe `-V` printing
  `grofe 0.3.0` against pin `v0.3.1` gives `not current`; no `~/.local/lib/grofe/grofe`
  gives `not installed`; no `pins.toml` gives grofe and canopy `unknown`; the JSON has the
  documented keys and component order; the header shows the branch, else the tag, else
  the short commit; a missing repo exits 1; `--bogus` exits 2.
- pins: `chezmoi execute-template` of both install scripts against `pins.toml` contains
  the pinned values and no other 40-hex or `v*` ref literal; changing `pins.grofe`
  changes the rendered `run_onchange_17`.
- grofe install script (bats, `test/install-grofe.bats`, release served from a `file://`
  base through a `GROFE_RELEASE_BASE` override, rendered with a fake home): a good
  tarball installs `~/.local/lib/grofe/grofe` and the `bin/tmux` link; a wrong
  `.sha256` file exits 1 with nothing installed; a missing tarball exits 0 with the
  `tmux stays tmux` note; `ATHOME_GROFE_BIN` wins over the download.
- installer (bats, `test/bootstrap.bats`): the fallback test becomes a missing asset
  exiting 1 naming the tag and URL with no legacy script run, with and without
  `ATHOME_VERSION`.
- cli: `athome` usage lists the six commands; `athome secrets`, `athome secrets rekey` and
  `athome secrets bogus` exit 2.
- e2e: arch and fedora, one job each, `athome teardown --all --execute --yes`, the
  verify scripts with the new `~/.local/bin/athome` check, and `athome status --json`
  after phase 1.

Docs (same PR): `README.md` teardown block uses `athome teardown`; `docs/commands.md`
gains `update`, `status`, `teardown` and `secrets` with every flag and exit code;
`docs/secrets.md` and `AGENTS.md` name `athome secrets backup|restore`; `docs/install.md`
drops the legacy fallback; `docs/how-it-works.md` drops `scripts/teardown.sh` from the tree,
adds `pins.toml` and how `update` orders its steps; `docs/customizing.md` drops
`colors.sh` and `pm.sh` and says a grofe or canopy bump is one edit in `pins.toml`.

Out of scope for build 4: `athome secrets rekey` and restoring the inventory (keys and
recovery spec); `sync --all` at the end of `update`, `machine` and `project` (remote
machines spec); removing
the `secrets-backup` and `secrets-restore` wrappers (the release after this one);
upgrading OS packages; release attestations; moving the release host to termage;
deleting `scripts/lib/ssh.sh`; rewording the Go comments that cite `bootstrap-legacy.sh`
line numbers (the file stays in git history).
