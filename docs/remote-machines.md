# Remote dev machines

Some work happens on ssh dev machines that belong to one client. athome keeps an inventory
of them, writes their ssh config, sets each one up with the same shell, tools and agents as
the laptop, and makes their projects reachable from the laptop. The design is in
[the spec](superpowers/specs/2026-11-16-remote-machines-design.md).

## A box holds one scope

A **scope** is a context plus a path under it, for example `work/acme`, which is
`~/work/acme`. Every machine in the inventory holds exactly one scope; one client may have
several boxes for the same scope.

- On the box, only that scope exists, at the same relative path (`~/work/acme/<repo>`).
  The context's git identity, mise config and worktree layout apply there as on the laptop.
- On the laptop, a remote scope has no directory. `machine add` refuses a scope whose
  `~/<scope>` holds a git repository, and `athome status` warns if one appears later.
- A scope is either local or remote, never split between the two.

## What a box needs before `machine add`

- Arch, Fedora or macOS, reachable over ssh as a user with sudo (a password prompt is fine).
  Not root: the bootstrap turns off root login, so `machine add` refuses `--user root`.
- Your laptop key in that user's `~/.ssh/authorized_keys` (the cloud console, cloud-init or
  `ssh-copy-id` put it there).
- `git` and `curl`.
- On the laptop: rbw unlocked (the scope's break-glass key lives in Bitwarden), `gh` logged
  in with the `admin:public_key` and `admin:ssh_signing_key` scopes, and a `~/.ssh/config`
  that includes `athome/config` (any `chezmoi apply` after this release adds the line).

## Adding a box

```
athome machine add acme-dev --scope work/acme --target dev.acme.example --user dev
```

Every inventory field has a flag: `--jump`, `--port`, `--github-account`,
`--forward-agent` and `--athome <tag>` (default: the laptop's own version). On a terminal a
missing `--scope` or `--target` is asked for; the home screen's `machines` view has the same
form.

`machine add` runs eight steps, each checking before it acts, so rerunning the same command
after a failure resumes where it stopped:

1. Validate the entry and the scope.
2. Pin the box's host key in `~/.ssh/athome/known_hosts` (trust on first use; the
   fingerprint is printed) and read the box's home and hostname.
3. Create the box's own key on the box and register it on GitHub as an authentication and a
   signing key, titled `athome-<name>` and `athome-<name>-sign`.
4. Authorize the scope's break-glass key on the box, creating it first if the scope has none.
5. Bootstrap the box: athome is cloned at the pinned tag to `~/.local/share/athome` and runs
   `athome init` headless with a `bootstrap.toml` generated on the laptop.
6. `gh auth login` on the box; its device code opens in your browser.
7. `grofe machine add <name> <name>`, so `M-m` in grofe lists the box.
8. Write the inventory entry and run `athome secrets backup`.

The inventory is written last. If a step fails, the error lists what earlier steps left
behind, and `athome machine rm <name> --force` removes it.

`--forward-agent` forwards your laptop key while you are attached, so root on the box can
use it; `machine add` warns about that.

## What the bootstrap sets up

The box runs `athome init` with `machineType = "remote"` and the scope:

- only the scope's directory, with its `use ctx` envrc and git identity;
- the shell, prompt, nvim, mise and every portable tool; no GUI packages and no desktop;
  no commercial VPN either (NordVPN, ProtonVPN): those packages carry the same `gui` flag,
  and the box always uses NetBird for its mesh, whatever the laptop uses;
- grofe, canopy's config and the coding agents with their hooks;
- sshd hardening (keys only, no root login), user lingering, the laptop's timezone and
  locale;
- `ATHOME_MACHINE`, `ATHOME_SCOPE` and `ATHOME_CONTEXT_COLOR` in `~/.zshenv` and
  `~/.config/environment.d/athome.conf`, and a prompt that reads `acme-dev · work/acme` in
  the context's colour;
- a systemd user unit, `grofe-restore.service`, that starts `grofe start-server --restore`
  at boot;
- `canopy remote set --machine <name> --scope <scope> --color <colour>`, run on every
  bootstrap and `machine sync`, so grofe's and tmux's status line and the terminal title
  say `acme-dev · work/acme` in the context's colour; a running grofe server is reloaded
  when that changed it (see [Your theme on the box](#your-theme-on-the-box)).

A box never gets Bitwarden: init never runs `bw-setup`, `rbw register` or `rbw login` there,
because a logged-in rbw would reach the whole vault. It never gets the git-crypt key or the
vault either. Its repo secrets are copied from the laptop, for its own repos only.

Mesh VPN enrolment is not automatic: when the target is a mesh name, run `sudo netbird up`
(or your provider's login) on the box once.

## Day to day

```
athome machine ls
athome machine sync acme-dev     # rerun the bootstrap after a dotfile change
athome machine sync --all        # every box in parallel, without a terminal
athome project clone acme/api --machine acme-dev
athome machine rm acme-dev
```

- `machine sync` regenerates `bootstrap.toml` from the current answers, checks out the
  pinned tag on the box and reruns the bootstrap; it is idempotent. `--all` runs without a
  terminal, so sudo on the boxes has to work without a password for the steps that need it;
  each box's output is in `~/.local/state/athome/sync-<name>.log`.
- A box stays on the tag the laptop pinned: `athome update` on a box applies and installs tools
  at that tag, never fetches a newer one and refuses a different `--to`. Change the pin in
  `machines.toml` and run `machine sync`. git to GitHub on a box (and `project clone`) reads
  only GitHub's published host keys in `~/.ssh/athome_github_known_hosts`.
- `project clone` clones into the scope's directory on the box, creates the `worktrees`
  directory beside it, runs `mise install`, copies `~/.secrets/<repo>/.env*` from the laptop
  and records the repo in the inventory. `--machine` can be left out when there is one
  machine.
- `machine rm` first revokes the box's GitHub keys (so an unreachable box never stops that),
  then logs `gh` out on the box, removes the
  break-glass authorization, the grofe entry, the pinned host key, the ssh block and the
  inventory entry. It never wipes the box.

## Reaching a project on a box

`repo <name>` finds local projects first, then remote ones from
`~/.local/state/athome/remote-projects`, and for a remote one runs

```
grofe -r acme-dev new-session -A -s api -c /home/dev/work/acme/api
```

which attaches to that repo's session on the box, creating it if needed.

Inside grofe, a plain `ssh acme-dev` prints a one-line hint to use `grofe -r acme-dev`
instead (reconnects, forwarding, no nested multiplexer), then runs ssh as asked.

## Your theme on the box

Every zsh prompt, on the laptop and on a box, exports `CANOPY_THEME` as the name in
`~/.config/themes/active` (what `theme` last set). An attach to a box carries it there, and
canopy runs the box's own `theme <name>` when the box knows that theme and is not already
on it. So a box follows the laptop's theme on each attach, not live while you stay attached.

- `grofe -r` sends the client's environment itself; nothing else is needed.
- Real tmux over plain ssh needs ssh to carry the variable. A box shell that arrived with it keeps it rather than exporting the box's own theme, so `tmux attach` from there carries the laptop's. The generated
  `~/.ssh/athome/config` has `SendEnv CANOPY_THEME` in each box's block, and on a Linux box
  the sshd drop-in `/etc/ssh/sshd_config.d/10-athome.conf` has `AcceptEnv CANOPY_THEME`. athome
  does not manage sshd on a macOS box: add `AcceptEnv CANOPY_THEME` to its sshd config by
  hand if you attach there with real tmux.

This needs the pinned canopy (`pins.canopy`) to have `canopy remote`; with an older one the
bootstrap says so and goes on. The box keeps the `theme` command and `~/.config/themes`.

## Moving a local scope to a box

athome never deletes local code, so this is by hand:

1. Push every repo under `~/<scope>`.
2. Move `~/<scope>` aside (`machine add` refuses a scope that still holds repositories).
3. `athome machine add <name> --scope <scope> ...`.
4. `athome project clone <repo> --machine <name>` for each repo.
5. Delete the local copies once the box has them.

## Reboots

grofe restores saved sessions when its server starts, and on a box the
`grofe-restore.service` user unit, with lingering on, starts it at boot. Agents resume before
anyone attaches; the first `grofe -r` after a reboot finds everything back.

## Keys and the accepted risk

Each box has its own key, created on the box and never copied off, registered on your GitHub
account (or the machine's `github_account`). A GitHub ssh key reaches every repo that account
can push to, so a breached box can push to more than its client's repos. That risk is
accepted to keep plain `gh` and git over ssh; revoking the box key ends it. See
[where everything lives](secrets.md#where-everything-lives) and
[rotation](secrets.md#rotation): `athome keys rotate --boxes` rotates every box's key, and
every rotation resets each box's `authorized_keys` to the managed set (laptop key and the
scope's break-glass key). After `--boxes`, the box re-renders its `allowed_signers` from the new
key on the next `athome machine sync`; until then `git log --show-signature` on the box shows
its new commits as unverified (GitHub verifies them at once).

## A second laptop

`athome secrets restore` brings back the inventory, but not the pinned host keys, and the
boxes only accept the laptop key that added them. To work on a box from a second laptop,
authorize that laptop's key on the box from the first one (or restore the same laptop key
with `--import-ssh-bw`), then run any `athome machine` command that reaches a box (`sync`,
`rm`, `project clone`, `keys rotate --boxes`; each rewrites `~/.ssh/athome/config` first, so
a box is never reached outside its pinned host-key block). Pinning host keys on a second laptop is not automated yet.

## The manual box drill

CI checks the ssh-backed key handling against a real sshd container. Before a release, run
the rest once against a throwaway VM:

1. `athome machine add` against a fresh Arch or Fedora VM with a sudo user.
2. On the box: only `~/<scope>` exists; `rbw` has no configuration; `~/.ssh` holds only the
   `<hostname>_ed25519` key; `sshd -T` shows `passwordauthentication no`;
   `loginctl show-user $USER -p Linger` is `yes`.
3. `athome machine sync <name>` twice: the second run changes nothing.
4. `athome project clone <repo> --machine <name>`: repo, `worktrees`, `mise install` and the
   `.env` are there.
5. Reboot the VM: `grofe -r <name> ls` lists the saved sessions without an attach first.
6. `athome machine rm <name>`: the GitHub keys and the grofe entry are gone.
