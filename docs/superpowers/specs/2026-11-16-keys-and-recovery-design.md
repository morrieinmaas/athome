# Keys, secrets and recovery: design

**Date:** 2026-11-16
**Status:** Approved 2026-11-16, pre-implementation
**Repo:** athome
**Build order:** 2 of 4. Needs `2026-11-16-athome-binary-design.md` (these are its
subcommands). `2026-11-16-remote-machines-design.md` needs this spec, because
`machine add` creates the box key and authorizes the break-glass key.

## Problem

Remote boxes add ssh keys and secrets that live on more than one machine. Two things need a
clear answer: which key reaches which machine, and how to get working again after losing
the laptop.

What exists today:

- `~/.secrets/<repo>/.env*` and a few other files are mirrored by `secrets-backup` into a
  git-crypt vault repo, checked out at `~/.local/share/attic`.
- The git-crypt key is in Bitwarden as `git-crypt/attic-key`, read by `secrets-restore`.
- Each machine has one passphrase-less ssh key, registered on GitHub for authentication
  and signing. It is `~/.ssh/id_ed25519` when that file exists, else
  `~/.ssh/<host>_ed25519`: signing, `allowed_signers` and the github.com `IdentityFile` all
  prefer `id_ed25519`. This spec calls whichever one is in use "the laptop key". Bootstrap can store it in Bitwarden as
  `ssh/<host>_ed25519` and restore it with `--import-ssh-bw`; both are opt-in, and nothing
  refreshes the stored copy when the key changes. These are login items, because rbw
  cannot create secure notes.
- rbw stays unlocked for 8 hours (`lock_timeout` in `bw-setup`).

## Threat model

- **Lost or stolen laptop, locked.** The disk is encrypted and needs the login password; the
  device can be wiped remotely once online. Realistic risk: small.
- **Laptop taken unlocked, or with a known password.** Everything the laptop can reach is
  exposed: its ssh key, and while rbw is unlocked, the git-crypt key, every break-glass key,
  every `.env` value and the `gh` token. The drill below covers this case.
- **Breached box.** Someone with root on a client box gets that box's key, its `gh` token
  and that scope's `.env` files. The `gh` token reaches everything your GitHub account
  does; revoke it at GitHub's authorized OAuth apps, and `machine rm` runs
  `gh auth logout` on the box.

**Accepted risk:** a box key is registered on your main GitHub account, and a GitHub ssh key
reaches every repo that account can push to. A breached box can therefore push to more than
its own client's repos. This is accepted to keep plain `gh` and git over ssh. Revoking the
box key ends it. A per-client GitHub account (`github_account` in the inventory) narrows it
where a client requires one.

## Where everything lives

| What | Where |
| --- | --- |
| git-crypt key | Bitwarden, `git-crypt/attic-key` (unchanged) |
| Laptop key | the laptop, plus Bitwarden `ssh/<host>_ed25519` (whichever file is in use); storing it becomes the default, refreshed on every rotation |
| Break-glass key, one per scope | Bitwarden only, `athome/<context>/<scope>/break-glass` |
| `.env` files and the machine inventory | the git-crypt vault (`~/.local/share/attic`) |
| A box's own key | that box only, created there, never copied off |
| A box's `.env` files | that box, only its scope's; the vault holds the source |

Existing Bitwarden items keep their names. Only new items use the folder layout
`athome/<context>/<scope>/`, and they are login items like the existing ones.

A box never clones the vault and never gets the git-crypt key, because that key decrypts
every context. A breached box leaks its own scope's `.env` files and nothing else.

## Box keys

Each box gets one key, `~/.ssh/<hostname>_ed25519`, created on the box at `machine add` and
never copied off. That name matters: the generated ssh config sets `IdentitiesOnly yes` and
offers github.com only `~/.ssh/id_ed25519` and `~/.ssh/<hostname>_ed25519`, and signing uses
the same files.

- It works while you are detached, so agents on the box can push at 3am.
- A breach leaks that one key, and you revoke it on its own.
- athome registers it with `gh` as an authentication key and a signing key on the scope's
  `github_account` (default: the account `gh` is logged in as), and removes it at
  `machine rm`. Registering the signing key is what makes commits show as verified.
- On the box, the existing `dot_config/git/allowed_signers.tmpl` already picks up that
  file, so `git log --show-signature` verifies locally with no template change.

Agent forwarding is an opt-in per machine (`forward_agent = true`). It forwards the laptop's
one key, so `machine add` warns that root on the box can use that key while you are
attached.

## The break-glass key

One per scope, created at the first `machine add` for that scope and authorized on every box
in it.

- Generated in a temporary directory on the laptop, with no passphrase, stored in Bitwarden
  at `athome/<context>/<scope>/break-glass`, then the temporary copy is deleted. Bitwarden's
  encryption is its protection, so recovery needs only the master password.
- Never used day to day. It is how you reach a scope's boxes when the laptop's key is gone
  or revoked.
- One key per scope: a leaked break-glass key opens one client's boxes.
- Rotated with `athome keys rotate --break-glass`.

## Rotation

```
athome keys rotate [--laptop] [--boxes] [--break-glass] [--all] [--resume]
```

Every rotation runs in the same order, so a failure never leaves you locked out:

1. Generate the new key.
2. Store it in Bitwarden (laptop and break-glass keys). An existing item is updated in place
   with `rbw edit`, never added again with `rbw add`: two items with one name make
   `rbw get` fail.
3. Add it everywhere: GitHub, then each box's `authorized_keys`.
4. Check a login with the new key, on GitHub and on each box.
5. Only then remove the old key everywhere. On each box, `authorized_keys` is reset to the
   managed set (laptop key, scope break-glass key), which also drops any key someone else
   added.

An unreachable box is skipped and listed. Progress is kept in
`~/.local/state/athome/rotate.json`, so `--resume` finishes the skipped boxes later. The old
key is removed from a box only after that box passed step 4.

- **`--laptop`:** the laptop key.
- **`--boxes`:** each box's key, created on the box; GitHub registration updated.
- **`--break-glass`:** each scope's break-glass key.
- **`--all`:** all three.

`athome status` and the TUI home remind you every 90 days. It is never an unattended job.
Run it by hand after a loss or theft.

## Recovering a lost laptop

1. Clone athome on the new laptop and run `./scripts/bootstrap.sh --restore`. The installer
   installs the binary and runs `athome init --restore`, which does steps 2 to 5 before any
   key is generated.
2. Bitwarden login: `rbw register` then `rbw login` on Bitwarden's cloud, `rbw login` on
   Vaultwarden.
3. `gh auth login`, needed to clone the vault.
4. Restore the laptop ssh key from `ssh/<old-host>_ed25519` (today's `--import-ssh-bw`).
5. `athome secrets restore`: git-crypt key, vault, every `.env` and the inventory.
6. The rest of `init` runs as usual. You are working again.
7. `athome keys rotate --laptop` to cut off the old laptop's copy.

**If the laptop was taken unlocked,** also:

8. `athome keys rotate --all` instead of step 7. Whoever had the laptop could ssh to every
   box, copy its key or add their own, so box keys and break-glass keys rotate too, and every
   box's `authorized_keys` is reset to the managed set.
9. Revoke the laptop's and every box's `gh` token on GitHub, log each box in again, and
   rotate the tokens in the affected `.env` files at their providers.
10. Re-key the vault. git-crypt cannot change its key in place (`docs/secrets.md`), so
    `athome secrets rekey` makes a new vault repo with a new key, stores the key in
    Bitwarden, and you delete the old repo.

If the laptop key was revoked before you restored it, reach each box with its scope's
break-glass key and run the rotation from there.

## Testing

- CI runs the whole drill offline: `gh` and `rbw` are fakes that record calls, and the box is
  the test sshd container. A fresh laptop user restores, reaches the box, rotates, and the
  old key no longer logs in to the box.
- Rotation failure midway: stop the run after step 3 and after step 4 for one box; the old
  key still logs in where it was not yet removed, and `--resume` completes.
- An unreachable box is skipped, listed, and finished by `--resume`.
- The break-glass key logs in to a box whose laptop key has been removed.
- A key added by hand to a box's `authorized_keys` is gone after any rotation.
- Rotating twice leaves exactly one Bitwarden item per key name.
- A box's filesystem holds no git-crypt key, no vault checkout and no `.env` outside its
  scope.
- The live drill against a real GitHub test account and Vaultwarden is manual, run before a
  release, with its steps in `docs/secrets.md`.

## Docs

- `docs/secrets.md`: where everything lives, rotation, the recovery drill for both cases,
  `athome secrets rekey`, and the accepted GitHub risk.
- `docs/install.md`: `bootstrap.sh --restore` for a replacement laptop.
- `README.md`: one line pointing to the recovery drill.

## Open questions

None at this stage.
