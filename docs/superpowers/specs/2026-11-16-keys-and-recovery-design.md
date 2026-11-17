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

## Build 1: laptop keys, rotation engine, rekey and restore (before remote machines)

Goal: everything in this spec that runs on the laptop alone ships now: `athome keys rotate`
with its ordering, progress file and `--resume`; the laptop key stored in Bitwarden and
refreshed in place; the break-glass key's create and store; `athome secrets rekey`;
`athome init --restore` (and `bootstrap.sh --restore`, which passes it through); the
90-day reminder in `athome status`. The parts that need a box or the inventory are seams
the remote-machines build fills; until then the inventory is empty, so rotation touches
GitHub and Bitwarden only.

Decisions:

- **New package `internal/keys`**, reaching every tool through `run.Runner`. It never
  imports `internal/steps`; callers pass the hostname (`steps.KeyHostname()`).
- **The laptop key** is `~/.ssh/id_ed25519` when that regular file exists, else
  `~/.ssh/<host>_ed25519` (`keys.LaptopKey`). Its Bitwarden item is `ssh/` plus that file's
  name. Items keep today's shape: password `ssh-key-backup`, notes the private key, which is
  what `--import-ssh-bw` reads (`rbw get --field notes`).
- **Storing an item (`keys.Store`)** needs `rbw unlocked` to exit 0, else it fails with
  `rbw is locked; run: rbw unlock, then re-run`. It counts the lines of `rbw list` equal to
  the item name: none runs `rbw add <item>`, one runs `rbw edit <item>`, more fails with
  `Bitwarden has <n> items named <item>; delete the extras, then re-run` and writes nothing.
  The content (`<password>\n<notes>\n`) reaches rbw through `EDITOR`/`VISUAL` set to a temp
  script that copies a 0600 temp file over its argument, so no secret is on a command line;
  both temp files are removed on every path. init's `ssh-bw-backup` step uses it too, so a
  regenerated key updates its item instead of adding a second one.
- **Break-glass items** are named `athome/<scope>/break-glass` (`work/acme` gives
  `athome/work/acme/break-glass`), stored by `keys.Store` in the same shape.
  `keys.NewBreakGlass(scope)` generates the key with `ssh-keygen -t ed25519 -N ""` in a
  `os.MkdirTemp` directory, stores it, removes the directory and returns the public key;
  `keys.BreakGlassPub(scope)` reads the item back and derives the public key with
  `ssh-keygen -y` from a 0600 temp file that is then removed. A failed `rbw get` is `keys.ErrNoItem` only when `rbw list` lacks the item; a locked rbw or a failed read is an error, so a box is never reset without its break-glass key by mistake. `machine add` (remote-machines)
  composes the two.
- **The box seam.** `keys.Box` is what rotation needs from a machine: `Name()`, `Scope()`,
  `Authorize(ctx, pub)`, `Login(ctx, keyPath)` and `Reset(ctx, managed []string)` (replace
  `authorized_keys` with exactly those public keys). An error from any of them skips
  the box, listed with the error. `keys.Inventory(home)` lists the boxes; in this
  build it returns none, and the remote-machines build replaces it with the inventory reader
  and an ssh-backed `Box`. The managed set for a box is the laptop public key plus its
  scope's break-glass public key.
- **`athome keys rotate [--laptop] [--boxes] [--break-glass] [--all] [--resume]`.** At least
  one flag; none exits 2 with `pick what to rotate: --laptop | --boxes | --break-glass |
  --all | --resume`. `--resume` combined with any other flag, an unknown flag or an argument
  exits 2. `--all` is the three kinds, run laptop, boxes, break-glass. No prompt: the user
  runs it by hand. Exit codes: 0 done, 1 a failure or skipped boxes (the run lists them and
  ends with `run athome keys rotate --resume once they are reachable`), 2 usage.
- **Laptop rotation, in the spec's order.**
  1. Generate `<key>.rotate` (and `.rotate.pub`) in `~/.ssh` with
     `ssh-keygen -t ed25519 -N "" -C <comment> -f <key>.rotate`, the comment kept from the old
     public key (else `athome@<host>`); a leftover
     `.rotate` from a crashed run is replaced.
  2. `keys.Store(ssh/<keyfile>.rotate, ...)`: a staging item, so the item under the real
     name keeps the key that still works until GitHub accepts the new one.
  3. `gh ssh-key add <key>.rotate.pub --title <host>` and
     `gh ssh-key add <key>.rotate.pub --title <host>-sign --type signing`, then
     `Authorize` on each box.
  4. A GitHub login with only the new key:
     `ssh -F /dev/null -i <key>.rotate -o IdentitiesOnly=yes -o BatchMode=yes -o
     ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -T git@github.com` passes when its
     output contains `successfully authenticated`; then `Login` on each box.
  5. Only then: the key files move into place (`<key>` and `<key>.pub` to `<key>.old` and
     `<key>.old.pub`, `<key>.rotate*` to `<key>*`), the staging item is copied to `ssh/<keyfile>`
     and removed (`rbw remove`; a failed removal is a note), the old key is removed from GitHub (each
     id of `gh api --paginate user/keys` and `gh api --paginate user/ssh_signing_keys` whose key matches the old
     blob, deleted with `gh api -X DELETE user/keys/<id>` or `user/ssh_signing_keys/<id>`),
     each box that passed step 4 is `Reset` to the managed set, and with no box left,
     `<key>.old*` are deleted. When `chezmoi` is on PATH,
     `chezmoi apply <home>/.config/git/allowed_signers` re-renders the signers file from the
     new public key; a failure there is a note.

  A failure in steps 1 to 4 on GitHub or Bitwarden exits 1 with the old key still the
  laptop key and still on GitHub. The progress file says how far it got, so `--resume`
  continues and never generates a second key. A crash after the file swap but before it was
  recorded, even halfway through it, is recognised on resume (`ssh-keygen -y` of `<key>` gives the
  new key) and the swap is finished. Finished jobs are dropped even when a later job fails, and
  `ssh/<keyfile>` is written from the installed key file, so a crash after the staging item
  was removed never blocks a retry. Every run checks `rbw unlocked` before any job is saved
  and exits 1 if it is locked; an exclusive `flock` on
  `rotate.lock` beside the progress file stops two rotations running at once.
- **Progress file** `~/.local/state/athome/rotate.json`, written (0600, via a temp file and
  rename) after every step: `{"jobs": [...], "last_rotated": "<RFC 3339>"}`. A job is
  `{"kind": "laptop"|"break-glass", "scope": "", "item": "", "key": "", "old_pub": "",
  "new_pub": "", "stored": bool, "installed": bool, "promoted": bool, "targets": {"github": "<phase>",
  "box:<name>": "<phase>"}}`, phases `added`, `checked`, `removed`, `skipped`. A job is done
  when every target is `removed`; done jobs are dropped, and `last_rotated` is set when a
  laptop job finishes (it dates the laptop key for the reminder). While a break-glass job is
  pending, a laptop reset of a box that already has the new break-glass key keeps that key. Likewise a
  break-glass reset keeps both laptop keys on a box where the laptop job has not finished. A
  box where the new break-glass key is only `added` or `checked` keeps the old one beside it.
  `Box` implementations connect with `<key>.old` while it exists. A new
  rotation while a job is pending exits 1 with
  `a rotation is unfinished (<kind>); run athome keys rotate --resume`. `--resume` with no
  pending job prints `nothing to resume` and exits 0; otherwise it runs each pending job from
  its recorded phases, retrying skipped boxes (a box still unreachable stays skipped). A box that has left the
  inventory is dropped from the job with a note.
- **`--boxes`** with an empty inventory prints `no machines in the inventory: no box keys to
  rotate`. A box's own key is created on the box, so its rotation is the remote-machines
  build's; with a non-empty inventory this build exits 1 saying so.
- **`--break-glass`** runs per scope found in the inventory: a new key stored as the staging item
  `athome/<scope>/break-glass.rotate`, then each box in the scope is authorized, logged into
  with the new key (read back from the staging item into a 0600 temp file, removed after) and
  reset to the managed set. Only once every box in the scope is reset is the staging item
  copied over `athome/<scope>/break-glass` and removed, so a skipped box stays reachable
  with the key under the real name. With an empty inventory it
  prints `no scopes in the inventory: no break-glass keys to rotate`.
- **`athome secrets rekey [--yes]`** keeps the vault's names stable: the old GitHub repo is
  renamed and a fresh one takes its name, so `restore` and `SECRETS_VAULT_NAME` keep working.
  Preconditions, each exit 1 with nothing changed: the vault exists, `git-crypt` and `rbw`
  are on PATH and rbw is unlocked, the vault is unlocked (its first tracked file other than
  `.gitattributes` lacks the git-crypt header), `git status --porcelain` is empty, and no
  same-day clash: Bitwarden has no item `git-crypt/<name>-old-<YYYYMMDD>-key` (exact line of
  `rbw list`) and `gh repo view <owner>/<name>-old-<YYYYMMDD>` does not exit 0; a clash names
  itself and says to delete it or retry tomorrow. The owner comes as `restore` finds it, except
  that a set `SECRETS_VAULT_REPO` must be exactly `<owner>/<repo>` (a URL or extra slashes exit 1)
  and its `<repo>` must equal `SECRETS_VAULT_NAME`, which names the Bitwarden item; otherwise
  exit 1 before anything changes. The slug, the rename and the messages then use that owner and
  repo. On a
  terminal without `--yes` it asks `re-key <owner>/<name>? The old repo becomes
  <owner>/<name>-old-<YYYYMMDD>` (default no; no exits 0 with `athome secrets rekey:
  cancelled, nothing changed`); with no terminal, `--yes` is required (exit 2). Only after
  that confirmation does `git pull --ff-only` run (failure: exit 1, nothing else changed), so
  a `n` answer leaves the vault untouched. Then:
  1. `<vault>.rekey` (removed first if left over) gets `git init -q -b main`,
     `git-crypt init`, the old vault's `.gitattributes` and every tracked file, `git add -A`,
     the same plaintext gate as `backup` (exit 2, nothing pushed), and a commit as
     `secrets-backup <secrets@local>` with message `rekey: new git-crypt key`.
  2. `git-crypt export-key ~/.local/state/athome/rekey-<YYYYMMDD>.key` (0600).
  3. The old key's base64 (`rbw get git-crypt/<name>-key`) is stored as
     `git-crypt/<name>-old-<YYYYMMDD>-key`, so the old repo stays readable until deleted.
  4. `gh repo rename <name>-old-<YYYYMMDD> --repo <owner>/<name> --yes`.
  5. `gh repo create <owner>/<name> --private --source <vault>.rekey --remote origin --push`.
  6. The new key's base64 is stored as `git-crypt/<name>-key` (`rbw edit`, in place); the
     exported key file is then removed.
  7. `<vault>` is renamed to `<vault>.old-<YYYYMMDD>` (a numeric suffix is added if that
     exists), `<vault>.rekey` is renamed to `<vault>`, then the `.old-<YYYYMMDD>` directory is
     removed, unless `git status --porcelain --ignored` shows files git ignores there: then it
     is kept and named, so nothing outside the vault is deleted.

  It prints `✓ vault re-keyed` and the two commands to run once you have checked the new
  vault: `gh repo delete <owner>/<name>-old-<YYYYMMDD>` and
  `rbw remove git-crypt/<name>-old-<YYYYMMDD>-key`. athome never deletes either. A failure
  exits 1 and names the undo for where it happened. After step 2 it names the exported key's
  path. If step 4 fails it also says `rbw remove git-crypt/<name>-old-<YYYYMMDD>-key`, since
  step 3's item is written and would block a same-day retry. If step 5 fails it first says to delete `<owner>/<name>` with
  `gh repo delete <owner>/<name>` if it was created (a failed push can create it), then names
  `gh repo rename <name> --repo <owner>/<name>-old-<YYYYMMDD> --yes`. If step 6 fails the new
  repo is live with the new key at the key-file path: it says to retry with
  `rbw edit git-crypt/<name>-key` and that file's base64 (never printed), and that the old
  key is preserved as `git-crypt/<name>-old-<YYYYMMDD>-key`; no rename undo. A step 7 failure
  reports where each checkout now is and names no GitHub undo, since GitHub and Bitwarden
  are already correct.
- **`athome init --restore`** runs a different list, `steps.RestoreAll()`: `Prepare`,
  `Data`, `Baseline` (which installs `rbw`, `gh` and `bw-setup`), then four restore steps,
  then `SSHKey`, `GitHub` and `Finish` as usual:
  1. `restore-bitwarden`: done when `rbw unlocked` exits 0. Apply runs `bw-setup` on a
     terminal; without one, or if rbw is still locked after it, it fails with
     `rbw is not unlocked: run bw-setup, then re-run athome init --restore`.
  2. `restore-gh`: done when `gh auth status` exits 0. Apply runs `gh auth login --git-protocol https --skip-ssh-key`
     interactively on a terminal (so gh never makes a key before the old one is restored); without one it fails with
     `gh is not logged in: run gh auth login, then re-run athome init --restore`.
  3. `restore-ssh-key`: done when the laptop key exists (kept, never overwritten). A
     present key also sets `sshKeyAction = "use"` (and `generateSshKey = "false"` when it is
     `id_ed25519`), so the later `ssh-keygen` step never makes another. Apply
     picks the item: `--import-ssh-bw` when given, else the one `rbw list` name matching
     `ssh/<name>_ed25519`; several are offered as a choice on a terminal and fail without
     one, listing them and naming `--import-ssh-bw`; none fails naming
     `ssh/<old-host>_ed25519`. It restores through the existing `--import-ssh-bw` code to
     `~/.ssh/<host>_ed25519`, fails if no key file results, sets `sshKeyAction = "use"` for
     the rest of the run (so `ssh-keygen` does not ask), and runs `chezmoi apply` so the
     signing config and `allowed_signers` pick up the restored key.
  4. `restore-secrets`: `athome secrets restore` in process; done when a
     `~/.secrets/*/.env` exists; a non-zero exit fails the run.

  Any failure stops the run before `ssh-keygen`, so no key is generated. The flag is taken by
  a small wrapper in `internal/cli/restore.go` that strips `--restore` and selects the list
  through the existing `stepList` seam, so the TUI and plain runs both use it;
  `athome init --help` lists it. `scripts/bootstrap.sh` already passes unknown flags to
  `athome init`, so `bootstrap.sh --restore` needs no change.
- **The 90-day reminder.** `athome status` adds a `keys` object to `--json`:
  `{"last_rotated": "<RFC 3339>"|"", "due": bool}`. The date is `last_rotated` from
  `rotate.json`, else the laptop key file's mtime, and only when a laptop key exists; `due` is true when it is more than 90
  days old. Text output adds one line after the rows only when due:
  `keys: last rotated <n> days ago; run athome keys rotate --laptop`. It never changes the
  top-level `current` or the exit code. With no laptop key, `keys` is
  `{"last_rotated": "", "due": false}` and no line is printed.
- **TUI.** Build 3 landed first, so the home screen shows the same reminder line
  (`tui.HomeInfo.KeysReminder`, filled from `status.Keys`). `--restore` adds no form
  question: its one question (which of several `ssh/*_ed25519` items) depends on Bitwarden
  at run time, so it is asked through `env.Ask`, which the progress view suspends around,
  as it does for `bw-setup` and `gh auth login`.
- **`athome` usage** gains `keys      rotate ssh keys (--laptop, --break-glass, --all,
  --resume)`; `athome secrets` usage names `backup|restore|rekey`.

Acceptance criteria:

1. `athome keys rotate --laptop` against a temp home with fake `gh` and `rbw` runs, in
   order: key generated, item stored, both `gh ssh-key add` calls, the GitHub login check
   with only the new key, the file swap, both old-key deletes; afterwards `<key>` holds the
   new key, no `<key>.old*` or `<key>.rotate*` remains, `rotate.json` has no jobs and a
   `last_rotated`, and the exit is 0.
2. If the GitHub login check fails, the run exits 1, no `gh api -X DELETE` was run, `<key>`
   is byte-identical to before, and `rotate.json` holds the job with `github` at `added`.
   `--resume` with the check now passing completes the job without generating a second key
   (one `ssh-keygen -t` call over both runs).
3. If `gh ssh-key add` fails, the run exits 1 naming `gh auth refresh -h github.com -s
   admin:public_key,admin:ssh_signing_key`, with nothing deleted on GitHub.
4. With boxes in the inventory (fake `Box`), an unreachable box is skipped and listed, the
   run exits 1 with the `--resume` hint, the reachable box is reset to exactly the managed
   set (a hand-added key is gone), the old key is never removed from a box before its
   `Login` passed, and `<key>.old` is kept; `--resume` with the box reachable authorizes,
   checks and resets it, deletes `<key>.old*` and clears the job.
5. Rotating twice leaves exactly one Bitwarden item per name: the first run adds, the
   second edits.
6. `keys.Store` with two existing items of one name fails with the `delete the extras`
   message and runs neither `rbw add` nor `rbw edit`; with rbw locked it fails with
   `rbw is locked`. No secret appears in any recorded command's args, and no temp file is
   left.
7. `athome keys rotate` with no flag exits 2 with `pick what to rotate`; `--resume
   --laptop` exits 2; a second `--laptop` while a job is pending exits 1 with
   `a rotation is unfinished`; `--resume` with nothing pending prints `nothing to resume`,
   exit 0.
8. With an empty inventory, `--boxes` and `--break-glass` print their `no machines` /
   `no scopes` notes and exit 0; `--all` rotates the laptop key and prints both notes.
9. `keys.NewBreakGlass("work/acme")` stores `athome/work/acme/break-glass` whose notes are
   a private key matching the returned public key, and leaves no temp directory;
   `keys.BreakGlassPub` returns the same public key.
10. `athome secrets rekey --yes` against a temp vault (real git, fake `git-crypt`, `gh`,
    `rbw`) runs the seven steps in order, stores the old key as
    `git-crypt/attic-old-<date>-key` and the new as `git-crypt/attic-key`, leaves the new
    checkout at the vault path with every tracked file, and removes the exported key. Each
    precondition failing exits 1 with no `gh` or `rbw add|edit` call; a failed
    `gh repo create` exits 1 naming the `gh repo rename` undo; no terminal and no `--yes`
    exits 2; `n` at the prompt exits 0 with nothing changed.
11. `athome init --restore` runs `restore-bitwarden`, `restore-gh`, `restore-ssh-key` and
    `restore-secrets` after the baseline and before `ssh-keygen`; with the key restored,
    `ssh-keygen` is skipped. With rbw locked and no terminal it exits 1 with the
    `rbw is not unlocked` message, and no `ssh-keygen -t` call is recorded. Two matching
    items and no terminal exit 1 listing both. `athome init --help` lists `--restore`.
12. `athome status --json` has the `keys` object; a laptop key older than 90 days with no
    `rotate.json` gives `due: true` and the text line; a `last_rotated` of yesterday gives
    `due: false` and no line; top-level `current` is unchanged by it.
13. init's `ssh-bw-backup` stores through `keys.Store`: with the item present it records
    `rbw edit`, not `rbw add`.

Edge cases: `~/.ssh/id_ed25519` present beside `<host>_ed25519` (it is the one rotated, item
`ssh/id_ed25519`); a leftover `<key>.rotate` with no job (replaced); `rotate.json` corrupt
(exit 1 naming the file, nothing done); the old key already gone from GitHub on resume (no
delete, not an error); `gh api` listing failing at removal (exit 1, the job stays at
`checked`, resume retries); `chezmoi` absent after the swap (note only); `rbw list`
failing (Store fails, nothing written); a vault with spaces in its path; `<vault>.rekey`
left over (removed and rebuilt); the rekey date colliding with an existing
`<name>-old-<date>` repo (`gh repo rename` fails, exit 1 before anything else changed on
GitHub); `--restore` with the key already present (kept, step skipped); `--restore`
combined with `--import-ssh-bw <item>` (that item is used).

Test list (unit unless marked):

- keys store: a stateful fake `rbw` (list, add, edit, get) behind a `runtest.Fake`: empty
  vault records `rbw add ssh/h_ed25519` and the item's content is
  `ssh-key-backup\n<pem>\n`; a second store records `rbw edit`; two items of one name fail
  with `delete the extras` and record no add or edit; `rbw unlocked` exit 1 fails with
  `rbw is locked`; no recorded arg contains the PEM; the temp dir holds no `athome-*`
  file afterwards.
- keys laptop key: `LaptopKey` prefers `id_ed25519`, else `<host>_ed25519`.
- keys rotate laptop (real `ssh-keygen` in a temp home, fake `gh`, `ssh`, `rbw`, `chezmoi`):
  the ordered call list in criterion 1; login check failing (criterion 2) then `--resume`;
  `gh ssh-key add` exit 1 (criterion 3); `gh api user/keys` returning
  `7 <old blob>` and `8 <other blob>` deletes only `user/keys/7`; resume with the old blob
  absent deletes nothing and finishes; corrupt `rotate.json` exits 1.
- keys rotate boxes (fake `Box` recording calls, one reachable, one failing every
  call): criterion 4, including a pre-existing hand-added key absent after
  `Reset`, `Reset` never called on a box before its `Login`, and `rotate.json` showing
  `box:b2` `skipped`; `--resume` finishes it.
- keys rotate twice: one item per name (criterion 5).
- keys break-glass: criterion 9; `--break-glass` with fake boxes in two scopes creates two
  items and resets each box to laptop pub plus its own scope's new pub.
- cli keys: criterion 7 and 8 through `cli.Main` with `newRunner` and `keys.Inventory`
  faked.
- secrets rekey: criterion 10, the ordered call list, the plaintext gate exiting 2 with no
  `gh` call, and each precondition.
- steps restore: criterion 11 against the fake runner; `RestoreAll` order; the restore
  step with `--import-ssh-bw` set uses that item without `rbw list`.
- cli restore: `--restore` is stripped and `stepList` is `RestoreAll` for the run, restored
  afterwards; `--help` names `--restore`; `--ref x --restore` keeps `--ref x`.
- status keys: criterion 12.
- steps ssh-bw-backup: criterion 13.
- bats (`test/bootstrap.bats`, existing): unknown flags pass through to `athome init`, which
  covers `--restore`.

Out of scope (the remote-machines build): the inventory reader and the ssh-backed `Box`;
creating, registering and rotating box keys (`--boxes` with machines);
`authorized_keys` on real boxes; `gh auth logout` at `machine rm`; the CI drill against the
test sshd container (fresh laptop restores, reaches the box, rotates, old key refused) and
the box filesystem checks. The live drill against a real GitHub test account and
Vaultwarden stays manual, with its steps in `docs/secrets.md`.

Docs (same PR): `docs/secrets.md` gains where everything lives, rotation, the recovery
drill for both cases, `athome secrets rekey`, the accepted GitHub risk and the manual live
drill; `docs/commands.md` gains `athome keys`, `athome secrets rekey` and
`init --restore`; `docs/install.md` gains `bootstrap.sh --restore`; `README.md` gets one
line pointing to the drill.
