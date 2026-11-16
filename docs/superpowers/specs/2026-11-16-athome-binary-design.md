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
7. A second `athome init` with the same inputs applies no step.
8. With `ATHOME_CI` set no GitHub key upload is attempted.
9. The e2e containers (arch, fedora) pass `verify-baseline.sh`, `verify.sh` and
   `verify-teardown.sh` unchanged with `ATHOME_E2E_RUNNER=binary`; CI runs both runners.
10. `athome version` prints the version and commit set at build time.

Edge cases: an empty config file; a config value with `=` or `#` inside quotes; an
existing ssh key with no `.pub`; `~/.local/share/chezmoi` already a different symlink or a
real directory; a detached HEAD; `gh` missing or not authenticated; a non-UTF-8 or
space-containing `$HOME` path; `--import-ssh` without `--import-from`.

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
