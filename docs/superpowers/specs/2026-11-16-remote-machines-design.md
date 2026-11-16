# Remote dev machines: design

**Date:** 2026-11-16
**Status:** Draft, awaiting review
**Repo:** athome
**Build order:** 3 of 4. Needs `2026-11-16-athome-binary-design.md` (the commands are its
subcommands) and `2026-11-16-keys-and-recovery-design.md` (box keys, break-glass keys).
Also needs grofe's ssh-remote work (`docs/specs/2026-11-15-ssh-remote.md` in grofe) to have
landed, because athome configures `grofe -r` and `grofe machine`. canopy's
`2026-11-16-remote-awareness-design.md` builds on this one.

## Problem

athome sets up one local machine with `~/work`, `~/personal` and `~/sidebiz` trees. Some
work, and more of it over time, happens on ssh dev machines that belong to one client.
There is no inventory of those machines, no ssh config for them, and no way to give a box
the same shell, tools and agents as the laptop.

The goal is that working on a box feels like working locally, stays safe, and needs nothing
beyond the termage repos and a browser.

## The model: a box holds one scope

A box is not a second laptop. It is the home of one slice of a context tree.

- A **scope** is a context plus a path under it, for example `work/acme`. It maps to
  `~/work/acme`.
- Each machine in the inventory is bound to exactly one scope.
- A client may have one or more boxes. A box never holds two scopes.
- A scope is either fully local or fully remote: never some repos on the laptop and some on
  a box.

On the box, only that scope exists, at the same relative path (`~/work/acme/<repo>`). The
context's git identity, mise configs and the worktree layout apply there as on the laptop.
Nothing else from `~/work`, `~/personal` or `~/sidebiz` is on it.

On the laptop, a remote scope has no directory at all, so a `personal` laptop with no
`~/work` can still have `work/acme` boxes. Routing to the box comes from the inventory (see
"Projects").

## Goals

- One declarative inventory of dev machines, synced through the git-crypt vault.
- Generated ssh `Host` blocks and grofe machine entries from that inventory.
- A box set up by the same `athome init`, chezmoi and mise as the laptop, headless and
  scoped.
- One command to resync a box after a dotfile change on the laptop.
- Projects on a box reachable from the laptop's project picker like local ones.

## Non-goals

- **No new distro support.** Boxes run what athome already supports (Arch, Fedora, macOS).
  A new distro gets added only when a real box needs it, as its own change.
- **No separate provisioning layer.** The box runs the same `athome init`. No Nix or NixOS.
  `infra/` (pyinfra, with its own `inventory.py`) stays as it is, for machines that need
  provisioning as root beyond what athome does. The two inventories are unrelated; a
  machine can be in both.
- **No GUI editing.** Editing happens on the box in the terminal (nvim inside grofe). No
  VS Code Remote and no file sync.
- **No transport work.** Connecting, reconnects, forwarding and latency are grofe's
  ssh-remote spec.
- **No mixed scopes** (see the model above).

## Inventory

One TOML file at `~/.config/athome/machines.toml`, added to the mirror list of `athome secrets backup` (today's `secrets-backup`)
so it lives in the vault and reaches every laptop with the git-crypt key. Hostnames never
reach the public athome repo.

```toml
[machines.acme-dev]
scope          = "work/acme"        # context/path; context is work | personal | sidebiz
target         = "dev.acme.example" # host or mesh name
user           = "dev"
jump           = ""                 # optional ProxyJump
port           = 22
athome         = "v1.4.0"           # pinned athome version for this box
forward_agent  = false              # opt-in, see the keys spec
github_account = ""                 # optional; empty = the account `gh` is logged in as
repos          = []                 # repos on this box, maintained by `project clone`
```

Validation, on every athome command that reads the file:

- `scope` starts with `work/`, `personal/` or `sidebiz/` and has a non-empty path.
- Two machines may share a scope (one client, several boxes); a machine never has two.
- A remote scope has no git repositories on the laptop: `~/<scope>` must not exist, or must
  hold no directory containing `.git`. `machine add` refuses otherwise, and `athome status`
  warns if one appears later, for example after a hand-run `git clone`.

**Moving a local scope to a box** is manual and documented: push every repo, run
`machine add`, `project clone` each repo on the box, then delete the local copies. athome
does not delete local code.

## Generated config

From the inventory:

1. **ssh `Host` blocks**, rendered by chezmoi into an include file next to
   `private_config.tmpl`, one per machine: `HostName`, `User`, `Port`, `ProxyJump`,
   `ControlMaster auto`, `ControlPersist 10m`, `IdentityFile` set to the laptop's key, and
   `ForwardAgent` per the inventory. `UserKnownHostsFile` points at a managed `known_hosts`
   with host keys pinned at `machine add`.
2. **grofe machine entries**, so `M-m` in grofe lists every box. athome runs
   `grofe machine add <name> <name>` (the target is the generated ssh alias) and
   `grofe machine rm`, and never writes grofe's `machines.toml` itself. The CLI is the
   contract, so a change to grofe's file format cannot break athome.

## Bootstrapping a box

The box has no vault and no inventory, so the laptop drives everything over ssh and passes
what the box needs on each run.

1. The laptop runs `ssh -t <alias>`, so sudo can prompt once; bootstrap caches it for the
   run, as today. Passwordless sudo also works.
2. On the box, athome is cloned at the inventory's pinned tag (the repo is public; https,
   no credentials) to `~/.local/share/athome`, and `scripts/bootstrap.sh` runs from that
   clone, so the chezmoi source is there as on the laptop.
3. The laptop passes a generated `bootstrap.toml` (machine type, scope, identity, the
   version) through ssh, and bootstrap runs `athome init --config` with it. That file turns
   off ssh key generation and the GitHub upload (`ATHOME_GENERATE_SSH_KEY=false`), because
   `machine add` creates the box key itself in step 4.

**No Bitwarden on a box.** `init` with `machineType = "remote"` never runs `bw-setup`,
`rbw register` or `rbw login`, even though `ssh -t` gives it a terminal. A logged-in rbw on
a box would reach the whole vault: the git-crypt key, every break-glass key and every
context's items. rbw may be installed (it is in `packages.yaml` `common`) but is never
configured. `use ctx` envrcs on a box read the scope's `.env` files only, never rbw.

It sets one new machine type and one new data key:

- **`machineType = "remote"`.** Today `machineType` decides which context trees
  `run_03-setup-project-dirs.sh` creates (`personal`: `~/personal` and `~/sidebiz`; `work`:
  `~/work`), and `cockpit` follows whichever directories exist. `remote` is one more
  answer: only the scope's directory. A remote machine is also headless.
- **`scope = "work/acme"`:** the directory `run_03` creates, and the context whose git
  identity file, `use ctx` envrc and mise config are rendered.

**Headless packages.** Portable tools come from mise as on every machine, so most of a box's
tooling is unchanged. In `packages.yaml`, the `darwin` casks and the Linux desktop stack are
skipped on a remote machine, and every GUI entry left in a shared list (`common`, `linux`,
`fedora`) gets a `gui: true` tag that remote machines skip. Tagging those entries is part of
this work.

What the box gets:

- shell, prompt, nvim, mise and every portable tool from mise;
- grofe, and canopy's generated config, so keys and plugins match the laptop (the theme
  comes from the laptop on attach: canopy's spec);
- the coding agents with their grofe hooks installed;
- sshd hardening (keys only, no root login) and user lingering, using the sudo from step 1;
- timezone and locale copied from the laptop;
- mesh VPN enrolment when the inventory target is a mesh name;
- `ATHOME_MACHINE`, `ATHOME_SCOPE` and `ATHOME_CONTEXT_COLOR`, written by chezmoi to
  `~/.zshenv` (read by every zsh, including the non-login shell that starts
  `grofe proxy`; `home/dot_zshenv` becomes `dot_zshenv.tmpl` for this) and to
  `~/.config/environment.d/athome.conf` (read by systemd user units). They are never added
  to grofe's `update-environment`, so the laptop's environment cannot override them;
- canopy's remote indicator: after canopy is installed, `init` runs
  `canopy remote set --machine <name> --scope <scope> --color <colour>` with the values
  passed as flags (canopy's spec). It does not rely on the `ATHOME_*` variables, which the
  chezmoi run that installs canopy cannot see yet;
- a systemd user unit that runs `grofe start-server --restore` at boot (see "Reboots").

Each box runs its own Claude, Codex and `gh` logins. The OAuth URL or device code opens in
the laptop's browser through grofe; no credentials are copied. The box's `gh` token is
needed for `gh` itself (pull requests, issues); git over ssh uses the box key. It reaches
everything your GitHub account does, so the keys spec lists it under "Breached box".

On the laptop, athome's zsh config exports `CANOPY_THEME` as the active theme's name (the
basename of `$CANOPY_STATE/current/theme`'s target, not a path), so canopy can carry it to a
box on attach. That export is added when canopy's themes (M5) land.

## Commands

These are subcommands of the `athome` binary, and the TUI's machines view offers the same
actions.

```
athome machine add <name> --scope work/acme --target <host> [--user u] [--jump j] [--port p]
athome machine sync [<name> | --all]
athome machine rm <name> [--force]
athome machine ls
athome project clone <repo> [--machine <name>]
```

**`machine add`** runs these steps in order. Each step checks before it acts, so rerunning
`machine add` after a failure resumes at the failed step:

1. Validate the entry (scope rules above).
2. Pin the box's host key in the managed `known_hosts`.
3. Bootstrap the box (above).
4. Create the box's key on the box, register it on GitHub from the laptop (keys spec).
5. Create the scope's break-glass key if it has none, and authorize it on the box.
6. `gh auth login` on the box, its device code opened in the laptop's browser.
7. `grofe machine add`.
8. Write the inventory entry and run `athome secrets backup`.

The inventory is written last, so a failed `machine add` leaves no entry. Whatever earlier
steps created (a pinned host key, a GitHub key) is listed in the error, and
`machine rm <name> --force` removes it without needing an entry.

- **`machine sync`** runs the bootstrap again with the current answers and pinned version.
  It is idempotent: a dotfile change on the laptop reaches every box with `sync --all`.
  Boxes are synced in parallel; an unreachable one is reported and skipped.
- **`machine rm`** revokes the box's key on GitHub, removes the break-glass authorization,
  the grofe entry, the generated ssh block and the inventory entry. It does not wipe the box.
- **`project clone`** clones the repo into the scope's directory on the box, creates the
  worktree layout, runs `mise install`, copies `~/.secrets/<repo>/.env*` from the laptop to
  the same path on the box over ssh, and records the repo in the inventory under the
  machine (`repos = [...]`). `--machine` can be left out when the scope has one machine.
  `machine sync` copies `.env` files only for repos in that list, which is what keeps a box
  to its own scope's secrets. The vault keys `.env` files by repo name alone, so two repos
  with the same name in different contexts share one entry; that limit exists today and is
  not changed here.

## Projects

The laptop's project picker (sessionx, zoxide) lists remote projects next to local ones.
The list comes from the inventory's `repos` per machine. athome also caches the box's
`$HOME`, read over ssh at `machine add`, so it can build the box's absolute path. Picking a
remote project runs

```
grofe -r <name> new-session -A -s <repo> -c <box-home>/<scope>/<repo>
```

with `<box-home>` filled in from the cache, so the laptop's shell never expands a `~` into
the laptop's own home.

which attaches to that repo's session on the box, creating it if needed. You never need to
remember which client lives where.

## Where you are: prompt and context colours

Each context has one colour, from the terminal's 16 ANSI colours, kept in athome's chezmoi
data:

```toml
[data.contextColors]
    work     = "yellow"
    personal = "green"
    sidebiz  = "magenta"
```

ANSI colours come from the terminal's palette, which is the laptop's terminal even when you
are on a box, so they follow your terminal theme without any canopy theme defining them.
The same names are used by the prompt and by canopy's status line, so the two always match.

The prompt is starship, from `home/dot_config/starship.toml`, which becomes a template. On a
remote machine its host segment is replaced, at render time, by fixed text such as
`acme-dev · work/acme` in the scope's context colour. Nothing runs per prompt. On the laptop
the template renders as today.

## The ssh nudge

Running plain `ssh <machine>` from a grofe pane gives a remote shell with no reconnects or
forwarding, and starting grofe there nests two multiplexers. athome's zsh config adds an
`ssh` function that prints one line per run, suggesting `grofe -r <machine>`, and then runs
ssh as asked. It never blocks. It prints only when all of these hold:

- `$GROFE` is set (grofe sets it in every pane; it is set under the `tmux` name too);
- the command is `ssh <name>` with no remote command and no `-T`;
- `<name>`, resolved with `ssh -G`, matches a machine in `grofe machine ls`.

## Reboots

On a box, grofe already restores saved sessions when its server starts, so the first
`grofe -r` after a reboot gets everything back. The systemd user unit, with lingering,
starts `grofe start-server --restore` at boot, so agents resume before anyone attaches.
grofe's `reboot-check` already looks for a unit running `start-server --restore`; athome
adds a lingering check to it upstream in grofe.

## What makes it feel local

- Same relative paths, git identity, mise and worktree layout inside the scope.
- Same shell, nvim, grofe keys and canopy config, and your theme on attach.
- Projects ready after one `project clone`, and remote projects in the local picker.
- The prompt and status line say where you are, in the context's colour.
- From grofe's ssh-remote spec: reconnects, sleep, clipboard, notifications, URLs and OAuth
  logins opening in the local browser, file paste, forwarded ports, agent states in one
  list.

## Testing

- A throwaway sshd container on a supported distro as the box, with a sudo user and
  systemd as PID 1, so lingering and user units work.
- `machine add` against it: host key pinned, bootstrap completes headless, only the scope's
  directory and git identity exist, the box key is registered with a fake `gh`, the
  inventory is written last.
- `machine add` stopped after each step, then rerun: it resumes and ends in the same state.
- `machine sync` twice: the second run changes nothing.
- `project clone`: repo, worktree layout, `mise install` and `.env` all present.
- Inventory validation: a bad context, two scopes on one machine, and a remote scope with a
  local repo are each rejected with a clear message.
- `machine rm` and `machine rm --force` after a partial add: nothing is left behind.
- The ssh nudge: prints for a listed machine inside grofe; silent outside grofe, with a
  remote command, with `-T`, and for an unlisted host.
- Reboot: restart the container; the unit has started grofe and restored the sessions before
  any attach.
- `chezmoi execute-template` on `starship.toml`: with `machineType = "remote"` the host
  segment shows machine and scope in the context colour; for `personal` and `work` the
  output is unchanged from today.
- The box after `machine add`: rbw has no configuration and no login, and no ssh key was
  generated by `init` itself (only the step 4 key exists).
- `project clone` then `machine sync`: the box holds `.env` files only for repos in its
  `repos` list.
- The project picker's command uses the box's home path, never the laptop's.
- bats tests for the generated ssh config, run without a network.

## Docs

- A new `docs/remote-machines.md`: the scope model, `machine add/sync/rm/ls`,
  `project clone`, moving a scope to a box, reboots, and the accepted GitHub risk (linking
  the keys spec's section in `docs/secrets.md`).
- `docs/spatial-model.md`: scopes, and remote scopes having no local directory.
- `docs/how-it-works.md`: `machineType = "remote"` and headless packages.
- `docs/customizing.md`: `contextColors` and the remote prompt.
- `README.md`: one line on remote dev machines, linking the new page.

## Open questions

None. grofe's ssh-remote spec already defines `grofe -r <host> [command...]` and
`grofe machine add/ls/rm`; check the flags against the merged PR before building.
