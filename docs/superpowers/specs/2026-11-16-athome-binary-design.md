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
  then a menu: init, quit. Update and machines join the menu in later builds. Without a
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

Out of scope: update and machines menu entries, `athome status`, teardown and secrets
(build 4).
