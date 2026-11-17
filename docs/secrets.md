# Secrets — Bitwarden via rbw

Secrets live in **Bitwarden**, accessed through **[`rbw`](https://github.com/doy/rbw)**
(an agent-based terminal client). This replaced `pass` + GPG + gpg-agent + browserpass + age.

**Model B (mise + `~/.config`) is the default and preferred model** — it works **offline**, needs
no `rbw unlock` to *use* a repo, and is fast. Reach for **Model A (direnv injection)** only as the
exception, for high-sensitivity secrets that must never touch disk (or on shared/ephemeral machines).

| | **B — mise + `~/.secrets`** *(default ✅)* | **A — direnv injection** *(exception)* |
| --- | --- | --- |
| At rest | plaintext `~/.secrets/<repo>/.env` (chmod 600) | **nothing on disk** — `rbw get` at `cd` |
| Loader | committed `mise.toml` `_.file` | `.envrc` → `use rbw VAR entry` |
| Source of truth | the `~/.secrets` file; **Bitwarden = backup** | Bitwarden (live) |
| Works offline / no unlock to use? | **yes** | no — needs rbw unlocked every session |
| Use for | everyday dev — fast, simple, offline | high-sensitivity / shared / ephemeral machines |

The rbw setup below applies to **both**. **Model B** (the mise pattern + `athome secrets backup`/`restore`)
is the final section and the one to use by default. **Model A** is "Daily use" + direnv, for the exceptions.

> **Why Bitwarden/rbw over pass:** cross-device incl. mobile "just works" (native
> apps + browser extensions + web vault), while `rbw` keeps the terminal-native
> workflow (`rbw get` in `.envrc`/scripts) that `pass` users want. The FOSS escape
> hatch is self-hosting **Vaultwarden** — same `rbw` client, your own server.

---

## First-time setup on a machine

The quickest path is the **`bw-setup`** helper (deployed to `~/.local/bin/bw-setup`,
on PATH). It sets the rbw config, then logs in — registering the device first on
official cloud, or a plain login against Vaultwarden/EU (`ATHOME_BW_BASE_URL`):

```bash
bw-setup        # bootstrap.sh also runs this; safe to re-run anytime
```

A plain `chezmoi apply` on a machine where rbw isn't set up prints a one-time
nudge to run it (never a prompt). To do the steps by hand instead:

### Official Bitwarden cloud (the default)

Bitwarden's cloud has bot-detection that **blocks plain-password logins from
third-party CLIs**, so a new device must register once with a **personal API
key** — having an account with contents doesn't waive this; `register`
authenticates the *device*, not the account.

```bash
# 1. Grab a personal API key: web vault → Settings → Security → Keys → "View API Key"
#    (you'll get a client_id and client_secret)
rbw config set email you@example.com
rbw register        # paste client_id + client_secret, then your master password
rbw unlock          # master password → agent caches it (lock_timeout)
rbw get <name>      # fetch. also: rbw list / rbw search / rbw code (TOTP)
```

After registration it's **master-password-only** on that device.

> The official `bw` CLI does NOT help here: it's a separate client with separate
> auth/state (a `bw login` doesn't carry over to `rbw`), and it hits the same
> bot-detection (needs `--apikey` or a captcha token too). Just use `rbw register`.

### EU region or self-hosted Vaultwarden

Point rbw at the right server before logging in. A **Vaultwarden** server skips
the API-key register dance — plain `rbw login` works.

```bash
export ATHOME_BW_BASE_URL=https://api.bitwarden.eu/        # EU cloud
# or:  export ATHOME_BW_BASE_URL=https://vault.example.com  # your Vaultwarden
# bootstrap reads ATHOME_BW_BASE_URL; by hand:
rbw config set base_url "$ATHOME_BW_BASE_URL"
rbw config set email you@example.com
rbw login           # Vaultwarden: no register needed
rbw unlock
```

---

## Config (rbw owns `~/.config/rbw/config.json`)

Set via `rbw config set <key> <value>` — **not** chezmoi-managed (a `chezmoi
apply --force` would clobber rbw's own device state). Defaults bootstrap sets:

| Key | Value | Why |
| --- | --- | --- |
| `pinentry` | `pinentry-curses` (Linux) / `pinentry-mac` (macOS) | terminal-native prompt; works in the TTY bootstrap and any terminal, no display/polkit dependency |
| `lock_timeout` | `28800` (8h) | "type it once per workday" — matches the old gpg-agent cache |
| `base_url` | unset (= Bitwarden US cloud) | set for EU region or Vaultwarden |
| `email` | your Bitwarden email | non-secret; lets `rbw login`/`register` skip the prompt |

Useful: `rbw config show`, `rbw sync`, `rbw lock`, `rbw stop-agent`.

---

## Daily use

```bash
rbw unlock                                   # once per session (agent caches it)
rbw get personal/github-token                # fetch a secret
echo "$(rbw get -f username work/db)"        # a specific field (uri/username/password/notes/custom)
```

Per-project secrets flow through `direnv` — see the
[per-project secrets section](how-it-works.md#per-project-secrets).
The helpers (`use ctx`, `use_rbw`, `use_aws`) live in
[`home/dot_config/direnv/direnvrc`](../home/dot_config/direnv/direnvrc).

## Bootstrap env knobs

| Env var | Effect |
| --- | --- |
| `ATHOME_BW_EMAIL` | pre-fill the Bitwarden email (skips the prompt) |
| `ATHOME_BW_BASE_URL` | EU cloud or Vaultwarden server URL (empty = US cloud) |

---

## Model B — mise + `~/.secrets/<repo>/.env` (default for dev repos)

Everyday project env vars live as a **plaintext file outside the repo**, loaded by **mise**;
Bitwarden is the **backup**, not the live source. Faster than injection, works offline, no
`rbw unlock` needed to *use* a repo.

### The pattern
```
~/.secrets/<repo>/.env         # real secrets, chmod 600, outside the repo & outside ~/.config
<repo>/mise.toml  (committed):
  [env]
  _.file = "~/.secrets/<repo>/.env"   # base .env ONLY — never .env.production
```
mise injects the vars into the shell at `cd`; `just` / docker / `npm` / subshells **inherit** them.

- **No symlinks.** If a tool reads the `.env` *file* directly (ignoring `process.env`), fix *that
  tool* — compose `${VAR}` interpolation, or `just` `set dotenv-load` / env passthrough — never a symlink.
- **Why outside `~/.config`?** Secrets live in a dedicated `~/.secrets/` tree so the dotfiles repo can
  manage `~/.config/*` with zero risk of ever sweeping up an `.env`. (chezmoi is allowlist-based and
  never would anyway — this makes the separation structural and self-documenting.)
- The committed `mise.toml` references `~/.secrets/<repo>/.env` — a **personal** path, not a generic
  XDG one. In **shared repos**, keep that `_.file` edit local (don't push it) or override the path in a
  gitignored `mise.local.toml`, so teammates aren't pinned to your layout. mise silently skips a
  missing `_.file` (one `mise trust` prompt on first `cd`).
- Personal values (`AWS_PROFILE`, `PORT_OFFSET`) → gitignored `mise.local.toml`, not `mise.toml`.
- mise's dotenv parser is **strict** — bare lines (no `KEY=`), unquoted values with spaces, and
  `<placeholder>` values make it error and inject nothing. Clean the `.env` first.

### Backup = an encrypted git vault (git-crypt); Bitwarden holds only the key
`athome secrets backup` mirrors `~/.secrets/*/.env*`, `~/.aws/*`, scw/stripe, and MCP configs into a
**private GitHub repo** (working tree `~/.local/share/attic`), encrypts every file with
**git-crypt** on commit, and pushes. **GitHub only ever sees ciphertext.** The git-crypt key
lives as a *single* Bitwarden item (`git-crypt/attic-key`). Result: **versioned** (every change
is a commit + timestamp), **encrypted**, **off-site** — and Bitwarden shrinks to one secret.

### Commands
```bash
mise-secret-init [repo-dir] [name]     # onboard a repo: move .env → ~/.secrets, write mise.toml, …, back up
athome secrets backup                  # mirror secrets → git-crypt vault → commit (encrypts) → push
athome secrets restore [--dry-run]     # clone vault → unlock with the BW key → copy decrypted files into place
```
`secrets-backup` and `secrets-restore` stay on PATH for one release as one-line wrappers that
`exec athome secrets backup|restore "$@"`; use the `athome` form. Both read `SECRETS_VAULT`
(default `~/.local/share/attic`) and `SECRETS_VAULT_NAME` (default `attic`); `restore` also
reads `SECRETS_VAULT_REPO` or `ATHOME_GITHUB_HANDLE` (else the logged-in `gh` user) to find the
vault repo. `git-crypt`, `rbw` and `gh` are found on PATH. Exit codes: `backup` 1 without a vault,
2 when a staged file is plaintext (nothing is committed); `restore` 1 when rbw is locked on an
encrypted vault, the key cannot be read or `git-crypt unlock` fails (nothing is copied; the old script
carried on and copied still-encrypted files), or any file failed to copy. Any other argument exits 2.
`athome init` restores in process (it offers `athome secrets restore` when rbw is unlocked and no
`~/.secrets/*/.env` exists yet).
- git-crypt is **deterministic** → unchanged files produce no diff, so `athome secrets backup` is a clean
  **no-op when nothing changed** (safe to run on a timer/watcher).
- A **safety gate** aborts the commit/push if any staged file isn't encrypted (no accidental plaintext leak).
- **Re-key** with `athome secrets rekey` (git-crypt has no in-place key rotation, so it makes a new repo with a new key; see below).

### Lost-laptop recovery
Short version: `./scripts/bootstrap.sh --restore` on the new laptop, then
`athome keys rotate --laptop`. The full drill is under
[Recovering a lost laptop](#recovering-a-lost-laptop).

> Deep-dive + the backup/restore mechanism rationale also lives in the agent skill
> `~/.config/agents/skills/mise-secrets/` (symlinked into `~/.claude/skills/` for Claude Code).

## Keys, rotation and recovery

### Where everything lives

| What | Where |
| --- | --- |
| git-crypt key | Bitwarden, `git-crypt/attic-key` |
| Laptop ssh key | the laptop (`~/.ssh/id_ed25519` if present, else `~/.ssh/<host>_ed25519`), plus Bitwarden `ssh/<that file name>`, refreshed in place on every rotation |
| Break-glass key, one per scope | Bitwarden only, `athome/<context>/<scope>/break-glass` |
| `.env` files | the git-crypt vault (`~/.local/share/attic`) |

Bitwarden items are login items: the password is `ssh-key-backup` (or the base64 git-crypt
key), the notes hold the private key. athome edits an existing item with `rbw edit` and
never adds a second item with the same name, because two items with one name make
`rbw get` fail. If you already have duplicates, athome stops and asks you to delete the
extras.

Break-glass keys are for remote boxes, which arrive with `athome machine`. Until then the
inventory is empty and rotation reaches GitHub and Bitwarden only.

### Rotation

```bash
athome keys rotate --laptop       # this laptop's key
athome keys rotate --break-glass  # each scope's break-glass key
athome keys rotate --all          # laptop, box keys and break-glass keys
athome keys rotate --resume       # finish a run that skipped boxes or stopped midway
```

Every rotation runs in the same order, so a failure never locks you out: generate the new
key, store it in Bitwarden, add it to GitHub (authentication and signing) and to each box,
check a login with only the new key, and only then remove the old key everywhere. A box's
`authorized_keys` is reset to the managed set (laptop key and the scope's break-glass key),
which also drops any key someone else added. An unreachable box is skipped and listed; it
keeps the old key, and so does the laptop (as `<key>.old`) until `--resume` reaches it.
Progress is in `~/.local/state/athome/rotate.json`.
A machine that shares the key (restored with `--import-ssh-bw`) loses GitHub access when the
key is rotated; restore the new key there with `--import-ssh-bw` again.
The new key waits in Bitwarden as `<item>.rotate` until it works everywhere it must (GitHub
for the laptop key, every box in the scope for a break-glass key), so the item under the real
name holds a key that logs in wherever the rotation has not finished yet. A box already reset
during an unfinished break-glass rotation accepts only the new key, which is in the `.rotate`
item until the rotation is done. A locked rbw stops the run before anything changes.

`gh` needs the key scopes: `gh auth refresh -h github.com -s
admin:public_key,admin:ssh_signing_key`. `athome status` reminds you once the key is more
than 90 days old. Rotation is never an unattended job; run it by hand, and always after a
loss or theft.

### Recovering a lost laptop

On the replacement laptop:

1. Clone athome and run `./scripts/bootstrap.sh --restore`. That runs `athome init --restore`,
   which does steps 2 to 4 before any key is generated.
2. Bitwarden login (`bw-setup`: `rbw register` then `rbw login` on Bitwarden's cloud,
   `rbw login` on Vaultwarden), then `gh auth login`, needed to clone the vault.
3. The laptop key is restored from `ssh/<old-host>_ed25519`. With several such items it
   asks which one; pass `--import-ssh-bw <item>` to choose up front.
4. `athome secrets restore` brings back the vault and every `.env`. The rest of `init` runs
   as usual.
5. `athome keys rotate --laptop` cuts off the old laptop's copy of the key. The new key is stored as
   `ssh/<new-host>_ed25519`; then run `rbw remove ssh/<old-host>_ed25519`, so the revoked key is
   not offered at the next `--restore`.

**If the laptop was taken unlocked,** whoever had it could reach everything rbw could while
it was unlocked. Also:

6. Run `athome keys rotate --all` instead of step 5, so box and break-glass keys rotate
   too and every box's `authorized_keys` is reset.
7. Revoke the old laptop's and every box's `gh` token at GitHub (Settings, Applications,
   Authorized OAuth apps), log each box in again, and rotate the tokens in the affected
   `.env` files at their providers.
8. Re-key the vault with `athome secrets rekey` (below), then delete the old repo.

If the laptop key was revoked before you restored it, reach each box with its scope's
break-glass key (`rbw get --field notes athome/<context>/<scope>/break-glass`), or from
`athome/<context>/<scope>/break-glass.rotate` when that item exists (a break-glass rotation was
unfinished), and run the
rotation from there.

### Re-keying the vault

```bash
athome secrets rekey [--yes]
```

git-crypt cannot change its key in place, so `rekey` builds a new repo: it copies every
tracked file of the unlocked vault into a fresh git-crypt repo with a new key, renames the
old GitHub repo to `<name>-old-<YYYYMMDD>`, creates a new private `<owner>/<name>` and pushes,
stores the new key as `git-crypt/<name>-key` and keeps the old one as
`git-crypt/<name>-old-<YYYYMMDD>-key`. The names stay the same, so `restore` keeps working. It
needs the vault unlocked, clean and up to date, and rbw unlocked. Once you have checked the
new vault, delete the old repo and key yourself; athome prints the commands
(`gh repo delete ...`, `rbw remove ...`) and never runs them.

### Accepted risk: one GitHub account

A key registered on your GitHub account reaches every repo that account can push to. A
breached box key can therefore push to more than its own client's repos. This is accepted
to keep plain `gh` and git over ssh; revoking the key ends it, and a per-client GitHub
account narrows it where a client requires one.

### The live drill (manual, before a release)

The unit tests cover rotation, rekey and restore with fake `gh` and `rbw`. Before a release, run it once for real
against a GitHub test account and a Vaultwarden instance, never your own accounts:

1. On a scratch machine or VM logged in to the test account, run `./scripts/bootstrap.sh`
   and let it store the key in Bitwarden.
2. On a second scratch machine, run `./scripts/bootstrap.sh --restore`; check that
   `ssh -T git@github.com` authenticates and `~/.secrets` is restored.
3. Run `athome keys rotate --laptop`; check that the test account lists only the new key
   and that the first machine's key no longer authenticates.
4. Run `athome keys rotate --laptop` again and check Vaultwarden still has one
   `ssh/<host>_ed25519` item.
5. Run `athome secrets rekey --yes`, then `athome secrets restore` on the first machine
   after removing its vault checkout.
