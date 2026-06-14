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

The rbw setup below applies to **both**. **Model B** (the mise pattern + `secrets-backup`/`secrets-restore`)
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
[per-project secrets section in the README](../README.md#per-project-secrets-via-direnv--rbw-bitwarden).
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
`secrets-backup` mirrors `~/.secrets/*/.env*`, `~/.aws/*`, scw/stripe, and MCP configs into a
**private GitHub repo** (working tree `~/.local/share/attic`), encrypts every file with
**git-crypt** on commit, and pushes. **GitHub only ever sees ciphertext.** The git-crypt key
lives as a *single* Bitwarden item (`git-crypt/attic-key`). Result: **versioned** (every change
is a commit + timestamp), **encrypted**, **off-site** — and Bitwarden shrinks to one secret.

### Scripts (deployed to `~/.local/bin`)
```bash
mise-secret-init [repo-dir] [name]   # onboard a repo: move .env → ~/.secrets, write mise.toml, …, back up
secrets-backup                       # mirror secrets → git-crypt vault → commit (encrypts) → push
secrets-restore [--dry-run]          # clone vault → unlock with the BW key → copy decrypted files into place
```
- git-crypt is **deterministic** → unchanged files produce no diff, so `secrets-backup` is a clean
  **no-op when nothing changed** (safe to run on a timer/watcher).
- A **safety gate** aborts the commit/push if any staged file isn't encrypted (no accidental plaintext leak).
- **Rotate** by deleting/recreating the repo (git-crypt has no in-place symmetric-key rotation).

### Lost-laptop recovery (the whole point)
1. install athome (gets `git-crypt`, mise, rbw via nanobrew) — bootstrap **step 7.6** offers `secrets-restore`
2. `gh auth login` (clone access) → `bw-setup` / `rbw unlock` — the *only* secret you carry is your master password
3. `secrets-restore` → clones the vault, decrypts with the BW key, repopulates `~/.secrets/*/.env`, `~/.aws/*`
4. clone dev repos → committed `mise.toml` + restored `~/.secrets/<name>/.env` → `mise trust` → working

> Deep-dive + the backup/restore mechanism rationale also lives in the agent skill
> `~/.config/agents/skills/mise-secrets/` (symlinked into `~/.claude/skills/` for Claude Code).
