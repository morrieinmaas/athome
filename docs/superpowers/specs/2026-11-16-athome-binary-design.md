# The athome binary: design

**Date:** 2026-11-16
**Status:** Draft, awaiting review
**Repo:** athome
**Build order:** first. `2026-11-16-remote-machines-design.md` and
`2026-11-16-keys-and-recovery-design.md` add subcommands to this binary.

## Problem

athome's logic lives in bash: `scripts/bootstrap.sh` (733 lines), `scripts/lib/*.sh`,
`scripts/teardown.sh`, and helper scripts in `home/dot_local/bin` (`secrets-backup`,
`secrets-restore`, `cockpit`). First run is about 20 plain-text prompts in a row from
`home/.chezmoi.toml.tmpl`. Updating means running chezmoi, mise and friends by hand.

The remote-machines and keys work adds an inventory with validation, bootstrap over ssh,
key rotation across GitHub and every box, and parallel sync. That is more than bash should
carry, and the first-run experience is the first thing a new termage user sees.

## Decision

One Go binary, `athome`, built with Bubble Tea, huh and lipgloss, released with
GoReleaser as checksummed macOS and Linux binaries.

- **Go over Rust:** the TUI is mostly forms and progress views, which huh and Bubble Tea
  cover directly; ratatui would need hand-built form widgets. The CLI is orchestration
  (ssh, `gh`, `rbw`, `chezmoi`, `git`, `mise`), which Go's standard library handles well.
  chezmoi, gh and the Charm tools are Go too. athome shares no code with grofe, so a shared
  Rust toolchain would only save a CI pipeline.
- **One binary, not two:** one install path, one version pin, one checksum.
- **External tools stay external.** athome runs `chezmoi`, `mise`, `gh`, `rbw` and `git` as
  programs and never imports chezmoi as a library, so their versions stay pinned
  separately.

## Three ways to run it

1. **TUI**, when stdout is a terminal and no subcommand is given, or on `athome init` and
   `athome update` run interactively.
2. **Config file**, for scripts and boxes: `athome init --config bootstrap.toml`. Same
   discovery order and precedence as today's `bootstrap.sh --config` (flag, then
   `./bootstrap.local.toml`, then `~/.config/athome/bootstrap.toml`; `ATHOME_*` env vars
   and flags win over the file). `test/e2e/bootstrap.toml` keeps working unchanged.
3. **Plain subcommands** for everything else, scriptable and with `--json` where output is
   data.

Nothing requires the TUI. With no terminal, `init` without `--config` fails with a message
naming the missing answers instead of hanging on a prompt.

## Commands

```
athome                       # TUI home: status, then init / update / machines
athome init [--config f]     # first run: answers, ssh keys, chezmoi init --apply, baseline
athome update                # self, chezmoi, mise, packages, grofe, canopy, then sync --all
athome status                # what athome manages on this machine and whether it is current
athome teardown [--execute]  # today's teardown.sh, dry run by default
athome secrets backup|restore
athome machine ...           # remote-machines spec
athome keys ...              # keys-and-recovery spec
athome project clone ...     # remote-machines spec
athome restore               # keys-and-recovery spec
athome version
```

## The init form

The form replaces the prompts in `home/.chezmoi.toml.tmpl` and bootstrap's own prompts,
grouped into pages:

1. **Machine:** machine type (`personal`, `work`, `remote`), hostname.
2. **Identity:** GitHub handle (prefilled from `gh`), names and emails per context.
3. **Keys and secrets:** Bitwarden server (US, EU or a Vaultwarden URL), ssh key
   (keep, generate, restore from Bitwarden), store the key in Bitwarden.
4. **Network:** mesh provider (meshnet, netbird, none) and its settings.
5. **Extras:** agents repo, extra packages, Slack and Matterhorn settings.
6. **Review:** every answer on one page, editable, then apply.

Each field validates as you type (email shape, a reachable Bitwarden URL, a known GitHub
handle). Answers are written to `~/.config/chezmoi/chezmoi.toml` `[data]`, as
`bootstrap.sh` does today, so chezmoi's `promptStringOnce` finds them and never prompts. The
template's prompts stay as a fallback for anyone running plain `chezmoi init`.

The apply step shows one progress view: each step with a spinner, a tick or a failure, and
the failing step's log on demand. The same view serves `update`.

## What happens to the bash

- **`scripts/bootstrap.sh` shrinks to an installer.** It detects OS and architecture,
  downloads the matching `athome` release, checks it against the release's checksum file,
  installs it to `~/.local/bin`, and runs `athome init` with its arguments passed through.
  The README's install steps (clone, then `./scripts/bootstrap.sh`) stay the same.
- **Everything else in `bootstrap.sh` moves to Go:** config loading, sudo caching,
  installing chezmoi, ssh key import, generation and the Bitwarden copy, GitHub key and
  signing-key registration, the github.com ssh config seed, `chezmoi init --apply`, the
  baseline (package manager, gh, rbw, gitleaks, project dirs, mise), pinning chezmoi's
  source directory, and the global git hook.
- **`teardown.sh`** becomes `athome teardown`.
- **`secrets-backup` and `secrets-restore`** become `athome secrets backup|restore`; the old
  names stay as one-line wrappers for a release, then go.
- **chezmoi run scripts stay bash.** They are chezmoi's mechanism and render from templates;
  moving them gains nothing.
- **`cockpit`** stays as it is.

## Versioning and self-update

- The binary carries its version; `athome version` prints it with the commit.
- `athome update` checks for a newer release, shows the changelog, and replaces itself after
  verifying the checksum. On a box it updates only to the version pinned in the inventory.
- The binary and the chezmoi source are released together from one tag, so a binary always
  matches the dotfiles it applies.

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
apply. The TUI and `--config` runs execute the same list, so they cannot drift.

## Testing

- The e2e containers (`test/e2e/Dockerfile`, `Dockerfile.fedora`) run
  `scripts/bootstrap.sh --config test/e2e/bootstrap.toml` as today, which now installs a
  locally built binary. `verify.sh`, `verify-baseline.sh` and `verify-teardown.sh` pass
  unchanged: that is the migration's acceptance test.
- Go unit tests per step: check and apply against a temp home, external tools faked.
- Config precedence tests: flag over env over file over default.
- TUI tests with Bubble Tea's test helpers: the form fills, validates and produces the
  same `[data]` as the equivalent `bootstrap.toml`.
- Running `athome init` twice changes nothing the second time.

## Migration

1. Build the binary with `init --config` and the e2e suite green, while `bootstrap.sh`
   still holds the full logic. Run both in CI.
2. Switch `bootstrap.sh` to the installer. Keep the old script as `bootstrap-legacy.sh` for
   one release.
3. Add the TUI on top of the same steps.
4. Move `teardown` and `secrets`, then delete the legacy script.

## Updating the rest of termage

`athome update` also updates grofe and canopy, through their own updaters (grofe's
release install with checksum, canopy's pull), at the versions athome pins. One command
updates the whole termage set. Each tool keeps its own updater, so it still works without
athome.

## Release hosting

GitHub releases on the athome repo for now. The installer reads one base URL, so moving to
termage later changes one value.

## Open questions

None at this stage.
