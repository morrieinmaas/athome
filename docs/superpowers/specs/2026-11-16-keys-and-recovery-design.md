# Keys, secrets and recovery: design

**Date:** 2026-11-16
**Status:** Draft, awaiting review
**Repo:** athome
**Depends on:** `2026-11-16-athome-binary-design.md` (the commands below are its
subcommands).
**Related:** `2026-11-16-remote-machines-design.md` (inventory, boxes, scopes).

## Problem

Remote boxes add ssh keys and secrets that live on more than one machine. Two things need
a clear answer: which key reaches which machine, and how to get working again after
losing the laptop.

Today, `~/.secrets/<repo>/.env` is backed up to a private git-crypt repo, and the
git-crypt key lives in Vaultwarden, read through `rbw` (`docs/secrets.md`). Bootstrap can
already store the laptop ssh key in Bitwarden and restore it (`--import-ssh-bw`), but both
are opt-in, and nothing refreshes the stored copy when a key changes.

## Threat model

- **Lost or stolen laptop.** The disk is encrypted (FileVault or LUKS) and needs the
  login password; the device can be wiped remotely once it is online. The realistic risk
  is small. The case it doesn't cover is a laptop taken while unlocked, or with a known
  password. Rotation closes that.
- **Breached box.** Someone with root on a client box. They must get that box's key and
  that scope's secrets, and nothing else.

## Where everything lives

| What | Where | Backed up |
| --- | --- | --- |
| git-crypt key | Vaultwarden | it is the backup |
| Laptop ssh keys | laptop, plus a copy in Vaultwarden (now the default, was opt-in) | Vaultwarden, refreshed on every rotation |
| Break-glass ssh key, one per scope | Vaultwarden only, in the scope's folder, strong passphrase | Vaultwarden |
| Every `.env`, the machine inventory | git-crypt secrets repo | the repo |
| A box's own key | that box only, created there | never; revoked and recreated |
| A box's `.env` files | that box, only its scope's | the secrets repo holds the source |

A box never clones the secrets repo and never gets the git-crypt key, because that key
decrypts every context. A breached work box then leaks that scope's secrets and nothing
else.

## Box keys

Each box gets its own key, created on the box at `machine add` and never copied off.

- It works while you are detached, so agents on the box can push at 3am, which an
  agent-forwarded key cannot.
- A breach leaks that one key; you revoke it on its own.
- athome registers it with `gh` at `machine add` (as an authentication key and a signing
  key) on the scope's GitHub account, and removes it at `machine rm`. That account is the
  inventory's optional `github_account`, defaulting to the account `gh` is logged in as.
  When it is set, athome uses `gh` with that account for every key step.
- It signs commits on the box. athome adds it to the context's `allowed_signers`, so
  those commits show as verified. Personal keys never touch a work box.

Agent forwarding stays an opt-in per machine (`forward_agent = true` in the inventory),
using one ssh-agent per context selected with `IdentityAgent`, so a forwarded agent holds
only that context's key.

## The break-glass key

There is one break-glass key per scope, created at the first `machine add` for that scope.
Every box in the scope authorizes it. Its private half exists only in Vaultwarden, in the
scope's folder, with a strong passphrase, and is never used day to day. It is how you reach
your boxes when the laptop's keys are gone or revoked. One key per scope means a leaked
break-glass key opens one client's boxes, not all of them.

## Rotation

```
athome keys rotate [--laptop] [--boxes] [--all]
```

- **`--laptop`** creates new laptop keys, authorizes them on GitHub and on every box in the
  inventory, removes the old ones from both, and writes the new keys to Vaultwarden.
- **`--boxes`** recreates each box's key on the box, registers the new one with `gh`,
  updates `allowed_signers`, and removes the old one.
- **`--all`** does both. Each step is one command so the Vaultwarden copy never drifts.

Runs on a schedule (default every 90 days, as a reminder in `athome status` and the TUI home,
not an unattended job), or by hand after a loss or theft. After a theft, run it at once, so the
stolen copies stop working.

## Recovering a lost laptop

1. Install athome on the new laptop.
2. `rbw login`, which needs only the Vaultwarden master password.
3. `athome restore`: fetch the laptop ssh keys through `rbw` (what `--import-ssh-bw` does
   today), then run `athome secrets restore` (git-crypt key, secrets repo, every `.env`), which now also brings back
   the inventory.
4. You are working again; boxes are reachable with the restored keys.
5. `athome keys rotate --laptop` to cut off the old laptop's copies.

If the laptop keys were revoked before you restored them, use the break-glass key from
Vaultwarden to reach each box and run the rotation from there.

## Testing

- A rehearsal of the whole drill against the test sshd container: a fresh "laptop" user
  with nothing but athome and Vaultwarden access, then `restore`, reach the box, `rotate`,
  and confirm the old key no longer works on the box or on a GitHub test account.
- `keys rotate --boxes`: the old box key is gone from the box, from `authorized_keys`
  and from `allowed_signers`, and a commit signed with the new key verifies.
- The break-glass key logs in to a box whose laptop key has been removed.
- A box's filesystem contains no git-crypt key and no `.env` outside its scope.

## Vaultwarden layout

Folders follow the context tree, so a scope's material sits together and can be shared or
exported on its own. Bitwarden shows `/` in folder names as nesting.

| Folder | Items |
| --- | --- |
| `athome` | `git-crypt` (the secrets repo key), `ssh/<laptop-host>` (each laptop's key) |
| `athome/<context>` | context-wide tokens, for example `athome/work` |
| `athome/<context>/<scope>` | `break-glass`, plus that scope's tokens, for example `athome/work/acme` |

Items are secure notes holding the key or token in the notes field, as
`--import-ssh-bw` reads today. Keys created on a box never appear here.

## Open questions

None at this stage.
