# Remote dev machines: design

**Date:** 2026-11-16
**Status:** Approved 2026-11-16, pre-implementation
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
configured. `use ctx` (direnv's `use_ctx`) reads only rbw, so on a box it loads nothing;
repo secrets load from `~/.secrets/<repo>/.env` through each repo's mise `_.file`, as on the
laptop.

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
                  [--github-account a] [--forward-agent] [--athome <tag>]
athome machine sync [<name> | --all]
athome machine rm <name> [--force]
athome machine ls
athome project clone <repo> [--machine <name>]
```

Every inventory field has a `machine add` flag, so the steps below have the values before
the entry is written. `--athome` defaults to the version of the laptop's own `athome`, and
`--github-account` to the account `gh` is logged in as.

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

When canopy's themes (M5) add their starship overlay, canopy's merged config takes
athome's rendered `~/.config/starship.toml` as its base and changes only colours, so the
remote segment survives. canopy's remote-awareness spec states the same rule.

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

## Build

Goal: the whole laptop side of this spec and the box side that athome's own templates can
express ship in one build: the inventory and its validation, the generated ssh config and
pinned host keys, `athome machine add|sync|rm|ls` and `athome project clone`, the
`machineType = "remote"` answer through config, init and the chezmoi templates (scope
directory, headless packages, no Bitwarden, no init-generated key, `ATHOME_*` variables,
the remote prompt, sshd hardening, lingering, the restore unit), the ssh nudge, remote
projects in `repo`, and the box-side seams the keys build left: the inventory reader, an
ssh-backed `keys.Box`, box-key rotation and real `authorized_keys` edits, with a CI drill
against a real sshd container. grofe is reached only through its CLI as merged in
termage/grofe#52: `grofe machine add <name> <ssh-target>`, `grofe machine ls` (columns
name, target, session, transport, enabled; no header), `grofe machine rm <name>`, and
`grofe -r <host> [command...]`.

Decisions:

- **New package `internal/machines`**, reaching every tool through `run.Runner`. It may
  import `internal/keys`, `internal/secrets` and `internal/steps`; `internal/keys` never
  imports it. The cli sets `keys.Inventory = machines.KeysInventory` at start-up.
- **Inventory** `~/.config/athome/machines.toml`, the spec's table plus two fields athome
  fills in itself at `machine add`: `home` (the box's `$HOME`, read over ssh) and
  `hostname` (the box's short hostname, which names its key `~/.ssh/<hostname>_ed25519`).
  Written 0600 through a temp file and rename, its directory 0700. A missing file is an
  empty inventory. Every command that reads it validates every entry and exits 1 naming
  the machine and field on the first error:
  - name: `^[a-z0-9][a-z0-9._-]{0,62}$`, and not `local` (grofe's own name);
  - scope: `work/`, `personal/` or `sidebiz/`, then one or more path segments of
    `[A-Za-z0-9._-]`, none `.` or `..`;
  - target: `^[A-Za-z0-9][A-Za-z0-9.-]*$` (a host or mesh name, an IPv4 address);
  - user: `^[a-z_][a-z0-9_.-]*$`, required; jump: empty or `^[A-Za-z0-9@._:-]+$`;
    port: 1 to 65535, default 22;
  - athome: `^v[0-9]+\.[0-9]+\.[0-9]+$`; github_account: empty or `^[A-Za-z0-9-]+$`;
  - repos: each `^[A-Za-z0-9._-]+$`, not `.` or `..`, no duplicates;
  - home: empty or an absolute path of `[A-Za-z0-9._/-]`; hostname: empty or
    `^[A-Za-z0-9-]+$`.

  These sets are what keeps a value from reaching an ssh config line or a remote shell
  as anything but itself; every remote command still single-quotes its arguments. A
  machine cannot hold two scopes because an entry has one `scope` key; TOML rejects a
  duplicated key. Two machines may share a scope.
- **Local scope check.** `machine add` refuses a scope whose `~/<scope>` holds a `.git`
  at any depth up to 4 (`~/work/acme/r/.git` counts, the walk does not descend into a
  `.git`): `~/<scope> holds git repositories (<first path>); a scope is local or remote,
  never both`. `athome status` runs the same check per inventory scope and adds a
  `machines` object to `--json`, `{"count": n, "warnings": ["..."]}`, and one text line per
  warning after the rows; it never changes `current` or the exit code.
- **Generated ssh config.** athome writes `~/.ssh/athome/config` (0600, directory 0700)
  from the inventory plus any pending add, after every change; `private_config.tmpl` gains
  `Include athome/config` beside `Include config.local`. This replaces "rendered by
  chezmoi": `machine add` must reach the box through its alias before the inventory entry
  exists (it is written last), so the file cannot wait for an apply. One block per machine,
  in name order:

  ```
  Host acme-dev
      HostName dev.acme.example
      User dev
      Port 22
      ProxyJump j            # only when jump is set
      IdentityFile ~/.ssh/<laptop key file>
      IdentitiesOnly yes
      ForwardAgent no        # yes when forward_agent
      HostKeyAlias acme-dev
      UserKnownHostsFile ~/.ssh/athome/known_hosts
      StrictHostKeyChecking yes
      ControlMaster auto
      ControlPath ~/.ssh/athome/cm-%C
      ControlPersist 10m
  ```

  The laptop key file is `keys.LaptopKey`'s base name. `HostKeyAlias` keys the pinned host
  key by machine name, so `machine rm` can remove it with `ssh-keygen -R <name>`. Every
  athome command that connects first checks that `~/.ssh/config` contains the line
  `Include athome/config` and otherwise exits 1 with `~/.ssh/config does not include
  athome/config; run chezmoi apply ~/.ssh/config`.
- **ssh from athome.** A non-interactive call is
  `ssh -o BatchMode=yes <name> sh -c '<script>' athome '<arg>'...` (arguments
  single-quoted; data goes on stdin, never in arguments); an interactive one is
  `ssh -t <name> ...` with `Interactive`. A login check (`Box.Login`) uses a temp config
  written from the same block without `IdentityFile`, `ControlMaster` and `ControlPath`
  (`ControlPath none`), and with a jump as `ProxyCommand ssh -W [%h]:%p <jump>`, run as
  `ssh -F <tmp> -i <key> -o IdentitiesOnly=yes -o BatchMode=yes <name> true`, so neither
  the laptop key nor a shared master connection can make it pass.
- **Pending adds** live in `~/.local/state/athome/machines/<name>.toml` (the entry with
  everything known so far, 0600) from step 2 on, so a rerun resumes with the same values
  and `machine rm --force` knows the target. A rerun with different flags for a pending
  name exits 1 with `machine add <name> is pending with other values; finish it with the
  same flags or run athome machine rm <name> --force`. The pending file is removed when the
  inventory entry is written.
- **`athome machine add <name> --scope s --target t [--user u] [--jump j] [--port p]
  [--github-account a] [--forward-agent] [--athome <tag>]`.** `--user` defaults to the
  laptop's `$USER`; `--athome` to `v<running version>`, and a running version that is not
  `X.Y.Z` exits 2 asking for `--athome`; `--github-account` to empty (the account `gh` is
  logged in as). On a terminal a missing `--scope` or `--target` is asked as a line prompt
  (and as the TUI form, below); without one it exits 2 naming the flag. A name already in
  the inventory exits 1 (`machine <name> already exists`). `--forward-agent` prints
  `warning: root on <name> can use your laptop key while you are attached`. The steps run
  through `steps.Execute`, each a `steps.Step` whose check runs before its apply, in this
  order (the box key moves before the bootstrap, because the bootstrap's chezmoi apply
  renders the signing config and `allowed_signers` from whichever key file exists then,
  and the box's first push needs the key registered):
  1. `machine-validate`: the entry, the local scope check, the `Include` line, rbw
     unlocked (`keys.ErrLocked`'s message otherwise, since step 4 needs the break-glass
     key). Check: none.
  2. `machine-host-key`: done when `ssh-keygen -F <name> -f ~/.ssh/athome/known_hosts`
     finds it and the pending entry has `home` and `hostname`. Apply writes the pending
     entry and the ssh config, then runs
     `ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes <name> sh -c 'printf "%s\n" "$HOME"; uname -n'`,
     pins whatever key the box offers (trust on first use, the fingerprint printed with
     `ssh-keygen -l -F <name> -f <known_hosts>`), and stores `home` and the part of
     `uname -n` before the first dot as `hostname`.
  3. `machine-box-key`: done when the box has `~/.ssh/<hostname>_ed25519.pub` and its
     blob is on GitHub as an authentication and a signing key (`gh api --paginate
     user/keys` and `user/ssh_signing_keys`). Apply creates the key on the box when absent
     (`ssh-keygen -q -t ed25519 -N '' -C athome@<name> -f ~/.ssh/<hostname>_ed25519`),
     reads its public key, adds the missing ones with
     `gh ssh-key add <tmp.pub> --title athome-<name>` and
     `--title athome-<name>-sign --type signing`, then checks from the box with
     `ssh -i ~/.ssh/<hostname>_ed25519 -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new -T git@github.com`
     (passes when the output contains `successfully authenticated`; this also pins
     github.com on the box). With `github_account` set, every `gh` call gets
     `GH_TOKEN` from `gh auth token --hostname github.com --user <account>`.
  4. `machine-break-glass`: done when the scope's break-glass public key is in the box's
     `~/.ssh/authorized_keys`. Apply reads it with `keys.BreakGlassPub`, creates it with
     `keys.NewBreakGlass` on `keys.ErrNoItem`, and authorizes it (`Box.Authorize`).
  5. `machine-bootstrap`: done when the box's `~/.local/bin/athome version` reports the
     pinned version and `~/.config/athome/bootstrap.toml` equals the one generated now.
     Apply sends `bootstrap.toml` on stdin to `~/.config/athome/bootstrap.toml` (0600),
     clones `https://github.com/morrieinmaas/athome` at the tag to
     `~/.local/share/athome` (or fetches the tag and checks it out detached), then runs
     `ssh -t <name> sh -c 'cd ~/.local/share/athome && ATHOME_VERSION=<tag> ./scripts/bootstrap.sh --config ~/.config/athome/bootstrap.toml'`
     interactively, so sudo can prompt once.
  6. `machine-gh-login`: done when `gh auth status` exits 0 on the box. Apply runs
     `ssh -t <name> gh auth login --hostname github.com --git-protocol ssh --skip-ssh-key --web`.
  7. `machine-grofe`: done when the first column of `grofe machine ls` has the name. Apply
     runs `grofe machine add <name> <name>`. grofe is `grofe` on PATH, else
     `~/.local/lib/grofe/grofe`; with neither, the step prints
     `grofe not installed: run grofe machine add <name> <name> once it is` and passes.
  8. `machine-inventory`: writes the entry, removes the pending file, rewrites the ssh
     config and the remote projects file, then runs `athome secrets backup` in process; a
     failed backup is a warning naming `athome secrets backup`. Check: none.

  A failed step exits 1 with `athome machine add: <step>: <error>`, then lists what
  exists so far (`left behind: pinned host key, GitHub keys athome-<name>[-sign],
  break-glass authorization`, from the steps that passed) and
  `rerun the same command to resume, or remove it with athome machine rm <name> --force`.
- **`bootstrap.toml` for a box** is built from the laptop's chezmoi data: `machine =
  "remote"`, `scope`, `machineName`, `githubHandle`, `githubId`, `personalName`, the
  scope's context identity (`workEmail` and `workName`, or `sidebizEmail` and
  `sidebizName`; nothing more for `personal`), `includeAgents`, `agentsRepo`,
  `meshProvider`, `netbirdManagementUrl`, `generateSshKey = "false"`,
  `useBitwarden = "false"`, `timezone` (the laptop's `/etc/localtime` target after
  `zoneinfo/`) and `locale` (`$LANG`). Keys with empty values are left out.
- **`machineType = "remote"`** is a third answer. `config.Load` accepts `remote` and reads
  `scope`, `machineName`, `timezone` and `locale` (env `ATHOME_SCOPE`, `ATHOME_MACHINE_NAME`,
  `ATHOME_TIMEZONE`, `ATHOME_LOCALE`); `remote` without a valid scope or machine name exits
  2. Only on a remote machine does the data step write those four keys into chezmoi's
  `[data]`, so laptops see no change. On remote, the steps force what the box must never
  do whatever the answers say: `bitwarden` is done without running `bw-setup`, the
  `secrets-restore` step is done without asking, `generateSshKey` is `false` (no
  `ssh-keygen` and no GitHub upload by init), and the baseline's project-dir check expects
  `~/<scope>/.envrc`. The init form's machine type choice stays `personal | work`: remote
  is only ever answered by a `bootstrap.toml` from the laptop.
- **Templates.** `.chezmoi.toml.tmpl` carries `scope`, `machineName`, `timezone` and
  `locale` from existing data (empty when absent, never prompted). `run_03` seeds only
  `~/<scope>` with `use ctx <context>` on remote. New `.chezmoidata/contextColors.toml`
  holds the spec's table. `dot_config/starship.toml` becomes `starship.toml.tmpl`; on
  remote the `$hostname` module in `format` is replaced by
  `[<machineName> · <scope>](bold <colour>)`, and for `personal` and `work` it renders
  byte for byte as today. `dot_zshenv` becomes `dot_zshenv.tmpl` and on remote exports
  `ATHOME_MACHINE`, `ATHOME_SCOPE` and `ATHOME_CONTEXT_COLOR`; new
  `dot_config/environment.d/athome.conf.tmpl` holds the same three as `KEY=value` and is
  empty elsewhere (chezmoi then creates no file). `packages.yaml` entries that are GUI apps
  or desktop stack become `{name: x, gui: true}` maps and `run_onchange_02` skips them and
  all `darwin.brew_casks` on remote; plain string entries are unchanged. The desktop-only
  scripts (`run_once_09`, `run_once_11`, `run_once_15`, `run_once_after_20`,
  `run_once_after_23`, `run_onchange_after_24`, `run_onchange_after_25`) render empty on
  remote. New `run_onchange_after_30-remote-box.sh.tmpl` (remote Linux only, skipped under
  `ATHOME_CI`) writes `/etc/ssh/sshd_config.d/10-athome.conf` (`PasswordAuthentication
  no`, `KbdInteractiveAuthentication no`, `PermitRootLogin no`), checks `sshd -t` before
  reloading `sshd`, runs `loginctl enable-linger $USER`, sets the timezone and locale when
  given, and enables new `dot_config/systemd/user/grofe-restore.service.tmpl`
  (`Type=forking`, `ExecStart=%h/.local/lib/grofe/grofe start-server --restore`,
  `WantedBy=default.target`; empty on non-remote).
- **`athome machine sync [<name> | --all]`** reruns step 5 for each named machine
  (bootstrap.toml regenerated from the current answers, the pinned tag checked out) and
  then copies `.env` files for the machine's `repos` (below). One name runs interactively
  with `ssh -t`. `--all` runs every machine in parallel without a terminal (`ssh -T`,
  bootstrap with `--non-interactive`), each box's output to
  `~/.local/state/athome/sync-<name>.log`, then one line per box, `✓ <name>` or
  `✗ <name>: <error> (log: <path>)`; an unreachable box is one of those lines. Exit 0 when
  all passed, 1 otherwise. No argument, or both a name and `--all`, exits 2.
- **`.env` copies** (clone and sync) take `~/.secrets/<repo>/.env*` except `.env.example`
  and send each on stdin to
  `sh -c 'umask 077; mkdir -p "$HOME/.secrets/$1" && cat > "$HOME/.secrets/$1/$2"'`.
  Only repos in the machine's `repos` list are copied.
- **`athome machine rm <name> [--force]`**: on the box, `gh auth logout --hostname
  github.com` and removal of the scope's break-glass key from `authorized_keys`; on GitHub,
  every authentication and signing key titled `athome-<name>` or `athome-<name>-sign`
  (`gh api -X DELETE user/keys/<id>`, `user/ssh_signing_keys/<id>`); then
  `grofe machine rm <name>` when listed, `ssh-keygen -R <name> -f ~/.ssh/athome/known_hosts`,
  the inventory entry and pending file, the rewritten ssh config and remote projects file,
  and `athome secrets backup` when the inventory changed. Without `--force` an unknown name
  exits 1 and any failing step stops the run with exit 1 before the inventory changes.
  With `--force` it works from the inventory entry, else the pending file, else the name
  alone, and each failing step is a note; the exit is 0.
- **`athome machine ls`** prints `fmt.Sprintf("%-16s %-20s %-28s %s", name, scope,
  user@target[:port], <n> repos)` per machine in name order, or `no machines` and exit 0.
- **`athome project clone <repo> [--machine <name>]`.** `<repo>` is `owner/name` (cloned
  as `git@github.com:owner/name.git`) or a `git@github.com:owner/name.git` or
  `https://github.com/owner/name(.git)` URL; anything else exits 2. Without `--machine`
  the inventory must hold exactly one machine; otherwise exit 2 naming them. On the box:
  `mkdir -p <home>/<scope>/worktrees`, `git clone <url> <home>/<scope>/<name>` unless
  `<name>/.git` exists, `mise trust` and `mise install` (`~/.local/bin/mise`) in it when it
  has a `mise.toml`, `.mise.toml` or `.tool-versions`; then the `.env` copies, the repo
  added to `repos`, the inventory saved and backed up.
- **Remote projects.** Every inventory write also writes
  `~/.local/state/athome/remote-projects` (0600), one tab-separated line per repo:
  `<repo>  <machine>  <scope>  <home>/<scope>/<repo>`. `machines.ProjectCommand` gives
  `grofe -r <machine> new-session -A -s <repo> -c <home>/<scope>/<repo>`. The `repo`
  zsh function falls back to that file when no local project matches and runs the same
  grofe command.
- **The ssh nudge** is `home/dot_zsh/ssh-nudge.zsh`, sourced by `dot_zshrc.tmpl`: an
  `ssh` function that, when `$GROFE` is set, the arguments hold no `-T` and exactly one
  non-option word (options that take a value skip it), and that word, or its `ssh -G`
  hostname, matches the name, target or target's `ssh -G` hostname of a row of
  `grofe machine ls`, prints `grofe: for reconnects and forwarding, use grofe -r <machine>`
  to stderr. It then runs `command ssh "$@"` in every case.
- **Boxes for `athome keys`.** `machines.KeysInventory(home)` returns one ssh `Box` per
  inventory machine. `Authorize(pub)` appends the key when its blob is absent; `Reset(keys)`
  writes exactly those keys through `authorized_keys.athome` and a rename; both send keys
  on stdin, check each is one `ssh-ed25519 <base64>[ comment]` line, and keep the file
  0600 in a 0700 `~/.ssh`. `Login(key)` is the login check above. While `<laptop key>.old`
  exists every other call adds `-i <key>.old`. The box also implements `keys.KeyBox`
  for box-key rotation: `NewKey` (create or reuse `~/.ssh/<hostname>_ed25519.rotate` on the
  box, return its public key), `CheckGitHub(rotating bool)` (the github.com check above
  with the `.rotate` key or the current one), `PromoteKey` (move `.rotate` over the key on
  the box; a no-op when `.rotate` is gone and the key's public key is the new one) and
  `CurrentPub`.
- **`athome keys rotate --boxes`** with machines adds one job per box, kind `box`, `item`
  the machine name: new key on the box (`NewKey`), both `gh ssh-key add` calls titled
  `athome-<name>` and `athome-<name>-sign` (target `github` `added`), `CheckGitHub(true)`
  (`checked`), `PromoteKey`, then the old blob's GitHub keys deleted as for the laptop key
  (`removed`, `promoted`). Any failure on the box skips that box's job with the error;
  `--resume` continues it from its phase. A box that left the inventory is dropped with a
  note. With an empty inventory the existing note stays.
- **Secrets backup** mirrors `.config/athome/machines.toml`; restore brings it back like
  every other mirrored file.
- **TUI.** The home menu gains `machines` (between `update` and `quit`). It opens a list of
  the inventory (`machine ls` lines) with `add`, `sync`, `sync all`, `remove` and `back`.
  `add` is a form (name, scope, target, user, port, jump, GitHub account, forward agent),
  validated with the inventory's rules, and runs `machine add` with those values in the
  progress view; `remove` asks `remove <name>? This revokes its GitHub keys` (default no).
- **`athome` usage** gains `machine   manage remote dev machines (add|sync|rm|ls)` and
  `project   clone a project onto a remote machine (clone)`.

Acceptance criteria:

1. `machines.Load` rejects, with the machine and field named: a scope `home/acme`, a scope
   `work`, a scope `work/../x`, a target with a space, a user `-oProxyCommand=x`, a port
   70000, a repo `../x`; it accepts the spec's example; two machines sharing a scope load.
2. `machine add` of a scope whose `~/<scope>` holds `r/.git` exits 1 with `holds git
   repositories` and runs no command; `athome status --json` with such a scope has
   `machines.warnings` naming it and the exit is 0.
3. The generated `~/.ssh/athome/config` for the example, with and without a jump and with
   `forward_agent`, equals the block above; `ssh -G -F <home>/.ssh/config acme-dev`
   (bats, no network) reports the hostname, user, port, `identityfile`, `hostkeyalias` and
   `userknownhostsfile`.
4. `machine add` against a fake runner runs its eight steps in order and records, in this
   order: the host key `ssh -o StrictHostKeyChecking=accept-new`, the box `ssh-keygen`, both
   `gh ssh-key add` calls, the box github.com check, the break-glass `authorized_keys`
   append, the `bootstrap.toml` upload, the interactive bootstrap, the interactive
   `gh auth login`, `grofe machine add acme-dev acme-dev`, and finally `machines.toml` holds
   the entry with `home` and `hostname`, the pending file is gone and the backup ran.
5. `machine add` failing at each step in turn exits 1 naming the step, writes no inventory
   entry, lists what was left behind; rerunning with the same flags skips every step that
   passed (no second `ssh-keygen`, no second `gh ssh-key add`) and ends in the same state
   as criterion 4.
6. The generated `bootstrap.toml` for a `work/acme` machine has `machine = "remote"`,
   `scope`, `machineName`, the work identity, `generateSshKey = "false"` and
   `useBitwarden = "false"`, and no `sidebizEmail`.
7. `athome init --config` with `machine = "remote"` and a scope runs no `bw-setup`,
   `rbw register`, `rbw login` or `ssh-keygen -t`, writes `scope` and `machineName` to
   chezmoi's data, and its project-dir check expects `~/<scope>/.envrc`; `remote` without
   a scope exits 2. For `personal` and `work` the data step writes the same keys as before.
8. `chezmoi execute-template` of `starship.toml.tmpl` with `machineType = "remote"`,
   `machineName = "acme-dev"`, `scope = "work/acme"` contains
   `[acme-dev · work/acme](bold yellow)` and no `$hostname` in `format`; for `personal` and
   `work` it equals the old `starship.toml`. `run_03` on remote seeds `~/work/acme/.envrc`
   with `use ctx work` and nothing else. `dot_zshenv.tmpl` on remote exports the three
   variables, elsewhere renders as the old `dot_zshenv`. `run_onchange_02` on remote has no
   `gui: true` entry and no cask; elsewhere it installs every name it did before.
9. `machine sync acme-dev` reruns the upload and bootstrap and copies `.env` files only for
   repos in `repos`; `sync --all` with one reachable and one failing box prints `✓` and `✗`
   lines, writes both logs and exits 1; `sync` with no argument exits 2.
10. `machine rm acme-dev` records the box `gh auth logout`, the break-glass removal, the
    GitHub deletes of exactly the ids titled `athome-acme-dev` and `athome-acme-dev-sign`,
    `grofe machine rm acme-dev`, `ssh-keygen -R acme-dev`, and leaves no entry, no block in
    the ssh config and no pending file. After a partial add, `rm --force` removes the same
    things from the pending file and exits 0 even when the box is unreachable; plain `rm` of
    an unknown name exits 1.
11. `project clone acme/api` records the clone into `<home>/work/acme/api`, the worktrees
    dir, `mise install` when the fake box reports a `mise.toml`, one upload per `.env*` file
    except `.env.example`, and `repos = ["api"]`; the remote projects file has
    `api  acme-dev  work/acme  /home/dev/work/acme/api`; `ProjectCommand` is
    `grofe -r acme-dev new-session -A -s api -c /home/dev/work/acme/api`, and no path in it
    starts with the laptop's home.
12. The ssh-backed `Box` sends `Authorize` and `Reset` keys on stdin only, refuses a key
    with a newline or an option prefix, and `Login` uses `-F <tmp>` without the laptop key
    and with `ControlPath none`. Against a real sshd container (CI job `sshd`): Authorize
    lets a new key in, Reset leaves exactly the managed keys (a hand-added one is refused),
    and after `athome keys rotate --laptop` with fake `gh` and `rbw` the old laptop key no
    longer logs in while the new one and the break-glass key do.
13. `athome keys rotate --boxes` with two fake `KeyBox`es, one failing `CheckGitHub`,
    registers and promotes the good box's new key, deletes only its old blob's ids, skips
    the other with its old key still current and exits 1 with the `--resume` hint;
    `--resume` with it passing finishes it with one `NewKey` call per box over both runs.
14. The ssh nudge (bats, stub `ssh` and `grofe`): `GROFE=1 ssh acme-dev` prints the hint
    and runs ssh; without `GROFE`, with `-T`, with a remote command, and for a host not in
    `grofe machine ls` it prints nothing; every case runs ssh with the same arguments.
15. `athome secrets backup` mirrors `~/.config/athome/machines.toml` when present.
16. The TUI home lists `machines`; the machines view lists the inventory; the add form
    rejects a bad scope with the inventory's message; `remove` asks before running.
17. `athome` usage lists `machine` and `project`; `athome machine bogus` and
    `athome project` exit 2.

Edge cases: an inventory file that does not parse (exit 1 naming the file); a missing
`~/.ssh/config` include line; `--athome` with a dev build; a rerun of `add` with other
flags for a pending name; a box whose `uname -n` has dots; the break-glass item missing
(created) or rbw locked (step 1 fails, nothing done); `gh ssh-key add` failing with the
scope hint as for the laptop key; a box already holding the key file (reused, not
regenerated); grofe absent; `grofe machine add` reporting the name exists (the check
already passed, so it is not run); `sync --all` with an empty inventory (`no machines`,
exit 0); `project clone` of a repo already on the box (no second clone, the `.env` copies
and the record still happen); a repo name already in `repos` (not duplicated); `rm` of the
last machine in a scope (the break-glass item stays in Bitwarden); `rm --force` with no
pending file and no entry (removes by name only: GitHub titles, grofe, known_hosts);
`~/.ssh/athome` missing (created 0700).

Test list (unit unless marked):

- machines inventory: criterion 1, round trip keeping order and the cached fields, the
  file 0600 and its directory 0700, a missing file is empty, a corrupt one names the file.
- machines local scope: `~/work/acme/r/.git` refused, `~/work/acme/r` without `.git`
  accepted, no `~/work/acme` accepted, a `.git` at depth 5 ignored.
- machines ssh config: golden blocks (criterion 3), name order, pending entries included,
  the include-line check message.
- machines add (stateful fake box runner): criterion 4 ordered calls; criterion 5 for each
  of steps 2 to 8; the pending-with-other-values refusal; `--forward-agent` warning; the
  github_account `GH_TOKEN`; grofe absent passes with the note.
- machines bootstrap.toml: criterion 6 for `work/acme`, `sidebiz/x` and `personal/y`.
- machines sync: criterion 9; one box interactive with `ssh -t`.
- machines rm: criterion 10, including the GitHub listing with a third unrelated key that
  is never deleted.
- machines clone: criterion 11; owner/name, ssh and https forms; a bad form exits 2;
  `--machine` omitted with two machines exits 2.
- machines box: criterion 12's argument and stdin checks against the fake runner; the
  `.old` key used while it exists.
- sshd drill (integration, `ATHOME_SSHD_ADDR` set by the CI job, skipped otherwise):
  criterion 12's container part.
- keys box rotation: criterion 13.
- config: `remote` accepted with a scope, refused without; `ATHOME_SCOPE` over the file.
- steps remote: criterion 7 against the fake runner.
- templates (bats, `test/templates.bats`, chezmoi installed in the bats job): criterion 8.
- ssh config (bats, `test/ssh-config.bats`): criterion 3's `ssh -G`.
- ssh nudge (bats, `test/ssh-nudge.bats`): criterion 14.
- secrets backup: criterion 15.
- status: criterion 2's warning.
- tui: criterion 16.
- cli: criterion 17; `machine add` without `--scope` and no terminal exits 2.

Out of scope for this build: the full box drill in a systemd container (bootstrap over ssh,
lingering, the reboot restore) stays a manual check before a release, listed in
`docs/remote-machines.md`, because a headless bootstrap of a whole distro inside CI's
sshd container doubles the e2e job; the sshd job covers keys and `authorized_keys`.
Mesh enrolment on the box (`netbird up` needs the user's SSO; the remote doc names it as a
manual step). canopy's `remote set` and `CANOPY_THEME` (canopy's spec and M5). Listing
remote projects in tmux-sessionx (canopy's remote-awareness spec); this build covers the
`repo` function. `sync --all` at the end of `athome update`. The lingering check in grofe's
`reboot-check` (upstream in grofe). Bumping the grofe pin to the release with
`machine` and `-r` (done once grofe releases).

Docs (same PR): new `docs/remote-machines.md` (the scope model, prerequisites: a box with
your laptop key authorized, git, curl and sudo; `machine add|sync|rm|ls`, `project clone`,
moving a scope to a box, reboots, the manual box drill, the accepted GitHub risk linking
`docs/secrets.md`); `docs/spatial-model.md` (scopes, remote scopes have no local
directory); `docs/how-it-works.md` (`machineType = "remote"`, headless packages, the
generated ssh config); `docs/customizing.md` (`contextColors`, the remote prompt, `gui:
true`); `docs/commands.md` (`machine`, `project`, `keys rotate --boxes`); `docs/secrets.md`
(box keys and `--boxes` now real); `README.md` (one line linking the new page).
