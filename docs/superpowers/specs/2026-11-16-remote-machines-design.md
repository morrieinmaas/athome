# Remote dev machines: design

**Date:** 2026-11-16
**Status:** Draft, awaiting review
**Repo:** athome
**Depends on:** grofe ssh-remote (`docs/specs/2026-11-15-ssh-remote.md` in grofe). Build after
that PR lands: athome generates config for `grofe -r` and `grofe machine`.
**Depends on:** `2026-11-16-athome-binary-design.md` (the commands below are its
subcommands).
**Related:** `2026-11-16-keys-and-recovery-design.md` (keys, secrets, recovery), canopy
`2026-11-16-remote-awareness-design.md`.

## Problem

athome sets up one local machine with `~/work`, `~/personal` and `~/sidebiz` trees. Some
work, and more of it over time, happens on ssh dev machines that belong to one client.
There is no inventory of those machines, no ssh config for them, and no way to give a
box the same shell, tools and agents as the laptop. Working there today feels like a
foreign machine.

The goal is that working on a box feels like working locally, stays safe, and needs
nothing beyond the termage repos and a browser.

## The model: a box holds one scope

A box is not a second laptop. It is the home of one slice of a context tree.

- A **scope** is a context plus a path under it, for example `work/acme`. It maps to
  `~/work/acme` on every machine.
- Each machine in the inventory is bound to exactly one scope.
- A client may have one or more boxes. A box never holds two scopes, so a breached box
  leaks one client only.
- A scope is either fully local or fully remote. Never some repos on the laptop and some
  on a box: that would make the project picker and the `.env` copy ambiguous.

On the box, only that scope exists, at the same relative path (`~/work/acme/<repo>`). The
context's git identity, mise configs and the worktree layout apply there as they do on
the laptop. Nothing else from `~/work`, `~/personal` or `~/sidebiz` is on it.

On the laptop, `~/work/acme` holds no code. It holds a marker saying the scope lives on
`acme-dev`, so the project picker can route to it (see "Projects").

## Goals

- One declarative inventory of dev machines, synced through the secrets repo.
- Generated ssh `Host` blocks and `grofe machine` entries from that inventory.
- A box bootstrapped with the same `athome init` and chezmoi as the laptop, headless and
  scoped.
- One command to resync a box after a dotfile change on the laptop.
- Projects on a box reachable from the laptop's project picker like local ones.

## Non-goals

- **No new distro support.** Boxes run what athome already supports. A new distro gets
  added only when a real box needs it, as its own change.
- **No separate provisioning layer.** The box runs the same `athome init`. No pyinfra step; `infra/` stays as it is.
  No Nix or NixOS.
- **No GUI editing.** Editing happens on the box in the terminal (nvim inside grofe). No
  VS Code Remote and no file sync.
- **No transport work.** Connecting, reconnects, forwarding and latency are grofe's
  ssh-remote spec.
- **No mixed scopes** (see the model above).

## Inventory

One TOML file in the git-crypt secrets repo, so every machine with the git-crypt key sees
it and hostnames never reach the public athome repo:

```toml
# ~/.secrets/athome/machines.toml
[machines.acme-dev]
scope   = "work/acme"        # context/path; context is work | personal | sidebiz
target  = "dev.acme.example" # host or mesh name
user    = "mo"
jump    = ""                 # optional ProxyJump
port    = 22
athome  = "v1.4.0"           # pinned athome version for this box
forward_agent = false        # opt-in, see the keys spec
github_account = ""          # optional; empty = the account `gh` is logged in as
```

The context is the first segment of `scope`. Validation, on every athome command that
reads the file:

- `scope` starts with `work/`, `personal/` or `sidebiz/` and has a non-empty path.
- No two machines share a scope.
- No scope is also present locally with code in it (the "fully remote" rule).

## Generated config

From the inventory, chezmoi renders:

1. **ssh `Host` blocks** into an include file next to `private_config.tmpl`, one per
   machine: `HostName`, `User`, `Port`, `ProxyJump`, `ControlMaster auto`,
   `ControlPersist 10m`, the context's identity file, and `ForwardAgent` per the
   inventory. `UserKnownHostsFile` points at a managed `known_hosts` with host keys pinned
   at `machine add`.
2. **grofe machine entries**, so `M-m` in grofe lists every box. athome runs
   `grofe machine add <name> <name>` (the target is the generated ssh `Host` alias) and
   `grofe machine rm`, and never writes grofe's `machines.toml` itself. The CLI is the
   contract, so a change to grofe's file format cannot break athome.
3. **Scope markers** on the laptop: `~/work/acme/.athome-remote` naming the machine.

## Bootstrapping a box

`athome machine add` copies the `athome` binary to the box over ssh (or has the box
download the pinned release) and runs `athome init --config` there with answers generated
from the inventory. It sets one new machine type and one new data key:

- `machineType = "remote"`. Today `machineType` decides which context trees a machine gets
  (`personal`: `~/personal` and `~/sidebiz`; `work`: `~/work`), in
  `run_03-setup-project-dirs.sh` and `cockpit`. `remote` is one more answer: only the
  scope's directory. A remote machine is also headless: no GUI apps, casks, fonts, desktop
  stack, Ghostty or GNOME extras. In `packages.yaml` that is a filter on the existing lists,
  not a second package list. A headless local machine (a home server) gets its own setting
  the day one exists.
- `scope = "work/acme"`: the directory `run_03` creates, and the context whose git identity
  file, `use ctx` envrc and mise config are rendered.

What the box gets, all through the existing chezmoi and mise paths:

- shell, prompt, nvim, mise and the portable CLI tools;
- grofe and canopy's generated config, so keys, theme and plugins match the laptop;
- the coding agents with their grofe hooks installed;
- sshd hardening (keys only, no root login), user lingering so services outlive logout,
  and timezone and locale copied from the laptop. These need sudo, which every box has.
- mesh VPN enrolment when the inventory target is a mesh name.
- `ATHOME_MACHINE`, `ATHOME_SCOPE` and `ATHOME_CONTEXT_COLOR` in the login environment,
  which canopy reads for its status line and window title.

## Where you are: prompt and context colours

Each context has one colour, chosen from the terminal's 16 ANSI colours and kept in
athome's chezmoi data:

```toml
[data.contextColors]
    work     = "yellow"
    personal = "green"
    sidebiz  = "magenta"
```

ANSI names follow whatever theme is active, because themes set the terminal palette, so no
theme needs to define context colours. The same names are used by the prompt and by
canopy's status line, so the two always match.

The prompt is starship, from `home/dot_config/starship.toml`, which becomes a template.
On a remote machine its host segment is replaced, at render time, by fixed text such as
`acme-dev · work/acme` in the scope's context colour. Nothing runs per prompt. On the
laptop the template renders as today.

Each box runs its own Claude and Codex logins. The OAuth URL opens in the laptop's browser
through grofe; no credentials are copied.

## Commands

These are subcommands of the `athome` binary (`2026-11-16-athome-binary-design.md`), and the
TUI's machines view offers the same actions.

```
athome machine add <name> --scope work/acme --target <host> [--user u] [--jump j]
athome machine sync [<name> | --all]
athome machine rm <name>
athome machine ls
athome project clone <repo> [--machine <name>]
```

- **`machine add`** writes the inventory entry, pins the host key, runs `athome init`
  on the box with `machineType = "remote"` and the scope, creates the box's key (keys spec), and
  commits the inventory to the secrets repo.
- **`machine sync`** runs `athome update` on the box at its pinned athome
  version. It is idempotent: a dotfile change on the laptop reaches every box with
  `sync --all`.
- **`machine rm`** revokes the box's key (keys spec), removes the inventory entry, the
  generated blocks and the scope marker. It does not wipe the box.
- **`project clone`** clones the repo into the scope's directory on the box, creates the
  worktree layout, runs `mise install`, and copies that repo's `.env` from the secrets
  repo. `--machine` is optional when the repo's scope already names a machine.

## Projects

The laptop's project picker (sessionx, zoxide) lists remote projects next to local ones.
For each scope marker, athome keeps a cached project list from the box, refreshed on
`machine sync` and `project clone`. Picking a remote project opens a grofe session on the
box in that repo (`grofe -r <machine>` with the repo as the working directory). You never
need to remember which client lives where.

## What makes it feel local

Covered by this spec:

- same relative paths, git identity, mise and worktree layout inside the scope;
- same shell, nvim, grofe keys and canopy config;
- projects ready after one `project clone`;
- remote projects in the local picker.

Covered by grofe's ssh-remote spec: reconnects, sleep, clipboard, notifications, URLs and
OAuth logins opening in the local browser, file paste, forwarded ports, agent states in one
list.

Covered by canopy's remote-awareness spec: the machine and context indicator, theme
following, the nested-grofe nudge, and session restore across a box reboot.

## Testing

- A throwaway sshd container on a supported distro as the box, with a sudo user.
- `machine add` against it: inventory written, host key pinned, bootstrap completes
  headless, only the scope's directory and git identity exist.
- `machine sync` twice: the second run changes nothing.
- `project clone`: repo, worktree layout, `mise install` and `.env` all present.
- Inventory validation: duplicate scope, bad context, and a scope with local code are each
  rejected with a clear message.
- `machine rm`: generated blocks, marker and inventory entry gone.
- bats tests for the generated ssh config, run without a network.
- `chezmoi execute-template` on `starship.toml`: with `machineType = "remote"` the host
  segment shows machine and scope in the context colour; for `personal` and `work` the
  output is unchanged from today.

## Open questions

None. grofe's ssh-remote spec already defines `grofe machine add/ls/rm`; check the
flags against the merged PR before building.