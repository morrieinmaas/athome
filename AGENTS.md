# AGENTS.md — operational runbook for future Claude / LLM agents working in this repo

This file captures the *non-obvious* operational knowledge an agent needs to safely modify the `morrieinmaas/athome` chezmoi setup without breaking the user's running machines. It complements `README.md` (which is for humans setting things up the first time).

**Read this BEFORE running `chezmoi apply --force`, before touching anything under `home/dot_config/niri/`, and before bumping a tag.**

---

## The most important rule: capture user state BEFORE apply --force

`chezmoi apply --force` overwrites destination files from the source tree. It does NOT uninstall packages. It does NOT touch files chezmoi has never managed.

Before running `apply --force` on a user's machine, ALWAYS:

```bash
# 1. PREVIEW. Shows every diff between source and destination. No writes.
chezmoi diff

# 2. For any file with user-tuned local state you want to preserve, capture FIRST:
chezmoi add <path>
# e.g. chezmoi add ~/.config/noctalia/      # Noctalia settings the user tuned via UI
#      chezmoi add ~/.config/some-tool/      # any other tool the user has configured

# 3. Verify the capture landed in chezmoi source:
chezmoi cd && git status && git diff HEAD home/dot_config/<thing>/ | head

# 4. Commit captured state so future installs reproduce it:
git add . && git commit -m "feat(<scope>): capture <user-tuned config>"
git push

# 5. NOW apply is safe — destination matches source for captured paths.
chezmoi apply --force
```

**Why this matters**: the user spends real time configuring tools via their UIs (Noctalia's wallpaper picker, theme tuning, etc.). Those tools write to `~/.config/<tool>/`. Until you `chezmoi add` those paths, chezmoi doesn't know they exist and `apply --force` won't clobber them — but it also won't re-create them on a fresh install. The capture step is what makes a setup truly reproducible.

## Files chezmoi DOES manage vs LEAVES ALONE

`chezmoi apply` only touches paths that exist in the source tree under `home/` (per `.chezmoiroot`). Anything else is invisible to chezmoi.

| Path on disk | Managed by chezmoi? |
| --- | --- |
| `~/.zshrc`, `~/.gitconfig`, `~/.ssh/config`, `~/.config/ghostty/config`, `~/.config/niri/config.kdl`, `~/.config/tmux/tmux.conf`, etc. | Yes — sources are under `home/dot_*` |
| `~/Pictures/Wallpapers/` | Yes — ships a plain black default (`black.png`); the user drops their own images in and picks via Noctalia's UI |
| `~/.config/noctalia/` | **No** — Noctalia writes this on first launch; user tunes via UI; we only capture it when the user runs `chezmoi add ~/.config/noctalia/` |
| `~/.local/state/*`, `~/.cache/*` | No — runtime state, never tracked |
| Secrets | **Two models** (see `docs/secrets.md`). **Default/preferred = B**: mise + plaintext `~/.secrets/<repo>/.env`, Bitwarden as backup (works offline, no unlock to use; via `secrets-backup`/`secrets-restore`). **Exception = A**: direnv `use_rbw` — pulled from Bitwarden at runtime, nothing on disk (high-sensitivity/shared machines). Neither path is chezmoi-managed — `~/.secrets/<repo>/.env` is restored by `secrets-restore`, never `chezmoi apply`. The dedicated `~/.secrets/` tree keeps secrets out of `~/.config` so the dotfiles repo can manage `~/.config/*` cleanly. |

Use `.chezmoiignore` to gate OS-specific paths. The existing block at `home/.chezmoiignore` already filters Linux-only Wayland-shell paths off macOS. **Add to that block** when shipping new Linux-only config; don't ignore via per-file conditionals.

## Tag discipline

`scripts/bootstrap.sh` defaults `--ref` to the latest git tag (via `git describe --tags --abbrev=0`). **Stale tags = old behavior on every new install.**

Whenever you commit a fix that should be the default for new installs, **push a tag**:

```bash
git tag -a v<X.Y.Z> -m "<one-line summary of what's new since prior tag>"
git push origin v<X.Y.Z>
```

Semver discipline (loose):

- **patch** (`vX.Y.z`): bugfix, no behavior change for happy-path users (e.g., missing package added; race fixed).
- **minor** (`vX.y.0`): user-visible feature shift (e.g., a shell-stack swap; layout reorg).
- **major** (`vX.0.0`): would require a manual migration step from a user on a prior tag.

> History note: the public repo starts at `v0.1.0` — the git history was squashed
> to a single root commit to drop accumulated PII before open-sourcing. There are
> no older tags or commit SHAs; don't reference them.

## Drift detection workflow (the loop you'll run a lot)

```bash
# On any user machine after time has passed:
cd ~/.local/share/chezmoi
git fetch origin
git log HEAD..origin/main --oneline      # what's new upstream
chezmoi diff                              # what would change locally

# Triage the diff:
#   - upstream changes to files I don't care about → just apply
#   - upstream changes to files where I have local edits → either:
#       (a) capture local first: chezmoi add <path>; merge upstream in
#       (b) discard local: chezmoi apply --force <path>
#       (c) merge interactively: chezmoi merge <path>

# Apply once you've decided:
git pull
chezmoi apply --force        # if you've already captured anything precious
# OR
chezmoi apply --interactive  # prompts per file
```

## Known pitfalls

Failure modes we hit and fixed — kept as operational knowledge (commit SHAs are
gone after the history squash; the fixes live in the current tree).

| Symptom | Root cause | Fix (current behavior) |
| --- | --- | --- |
| `git pull` in `~/.local/share/chezmoi` fails with "Could not resolve hostname github-personal" | gitconfig's `url.insteadOf` rewrote to an SSH alias that wasn't in `~/.ssh/config` yet | bootstrap.sh AND chezmoi `run_before_00` seed the SSH host aliases first |
| niri config silently doesn't apply (no Mod keys, no rounded corners, the shell reports "failed to parse config file") | niri config has a parse error — cascade affects every customization at once | keep `home/dot_config/niri/config.kdl` valid against the current niri schema |
| `chezmoi apply` hangs on file-modified prompt with broken TTY input in tmux | Default `chezmoi apply` prompts when destination diverges from source; the prompt input is flaky in tmux | bootstrap passes `--force` to `chezmoi init --apply` |
| Bootstrap uploads a work SSH key to GitHub on a personal machine | SSH keygen + upload looped over all three identities regardless of `--machine` | gate by the `IDENTITIES` array |
| First boot lands in TTY not a graphical login | archinstall's Niri profile pins lightdm, our setup expects greetd | `run_once_11` handles any display-manager and only enables greetd if NO DM is configured (respects archinstall's pick) |
| zsh's Ctrl+A prints literal `^A` | `$EDITOR=nvim` triggers zsh's vi-mode auto-detect (substring match on "vi") | force `bindkey -e` |
| Git push / ssh auth | GPG/gpg-agent retired (Phase 5). SSH keys are passphrase-less (`ssh-keygen -N ""`) and pinned per-host via `IdentityFile` + `IdentitiesOnly`, so ssh + SSH commit signing read the key files directly — **no agent needed**. Don't re-introduce an `SSH_AUTH_SOCK` override pointing at a gpg/ssh agent. | Phase 5 design |
| Noctalia bar says "wifi disabled" but `nmcli` works | Noctalia shells out to `nmcli` (not D-Bus); needs to be RESTARTED after switching backend so `nmcli -t monitor` re-attaches | `networkmanager` is pinned; recover with `pkill -f "qs.*noctalia-shell" && qs -c noctalia-shell &` |
| Noctalia never spawns on niri start | `noctalia-shell` is a Quickshell config NAME, not a binary — must launch via `qs -c noctalia-shell` | spawn-at-startup uses `qs -c noctalia-shell` |
| Bootstrap installs an outdated shell stack on a fresh box | bootstrap defaulted `--ref` to a stale tag | tag after every shell-stack change |

## Things Noctalia replaces — don't double-spawn

Noctalia (Quickshell-based) provides these natively. Do NOT add them to `packages.yaml` or `spawn-at-startup`:

- launcher (was fuzzel) — use `qs -c noctalia-shell ipc call launcher toggle`
- notifications (was mako) — built-in; `notifications toggleHistory` etc.
- lockscreen (was swaylock) — `lockScreen lock`
- idle handler (was swayidle) — `idleInhibitor toggle`
- clipboard history (was cliphist) — `launcher clipboard`
- polkit agent (was polkit-gnome) — Noctalia ships its own
- wallpaper daemon (was swww as primary) — `wallpaper toggle/random/set/get/refresh`
- media keys → wpctl; brightness keys → brightnessctl — both via `volume increase` / `brightness increase` IPC

**Noctalia does NOT ship a greeter.** It has `Modules/LockScreen/` but no `Modules/Greeter/`. The login screen at boot is `ly` / `greetd-tuigreet` / `lightdm` — Noctalia takes over after login.

Authoritative IPC verb list: [`Services/Control/IPCService.qml`](https://github.com/noctalia-dev/noctalia-shell/blob/main/Services/Control/IPCService.qml). On-machine discovery: `qs -c noctalia-shell ipc show`.

## Packaging discipline

**Two installers, one dividing line.** Portable & user-space → **mise**; GUI / OS-integration / library / privileged → the **native PM** (`nb`/brew on macOS, `paru` on Arch).

- **mise** (`home/dot_config/mise/config.toml` `[tools]`) owns ALL portable tooling on both OSes: language runtimes + the modern-CLI set. Bare names resolve via mise's registry (aqua/ubi backend) — `cargo:` prefix only for tools not in the registry (procs, tealdeer, just-lsp). This is what killed the old per-OS name-translation lists. mise itself is curl-bootstrapped (`mise.run`), NOT a brew/pacman package — don't re-add it to `packages.yaml`.
- **`packages.yaml`** is native-only now: casks, the niri/Noctalia desktop stack, fonts, libraries, podman/syncthing, `uv` (Python), and a few OS-integrated CLIs kept native on purpose (ICMP tools needing caps: `bandwhich`/`gping`/`trippy`; npm daemon `prettierd`). **Adding a portable CLI? It goes in mise, not here.**
- **The native-exception rule:** a tool that's invoked OUTSIDE an interactive shell — global git hooks, systemd services, the bootstrap — must be native, NOT mise. mise is only activated in interactive shells (+ shims-on-PATH via `path.zsh`, which those contexts don't source), so a mise shim wouldn't be found and the tool would silently no-op. This is why **`gitleaks`** (global pre-commit hook) and **`rbw`** (used by direnv, but load-bearing for secrets) are in `packages.yaml`, not `[tools]`. Same reasoning applies to anything a hook/service ever shells out to.

Adding a tool, decide: is it a portable single-binary CLI? → mise `[tools]`. Is it a GUI app, font, library, system service, or something needing root/caps? → `packages.yaml`.

All Linux `packages.yaml` entries go through `paru`. The split between `linux.pacman` and `linux.aur` is an organizational hint (which repo it lives in upstream), not a behavioral switch.

`networkmanager` is **required** by Noctalia's NetworkService (shells out to `nmcli`) — pinned in `linux.pacman`. Don't drop it.

mise-managed tools are on PATH via `~/.local/share/mise/shims` (added in `path.zsh.tmpl`) so they resolve in non-interactive shells and in `completions.zsh` BEFORE `mise activate` fires (e.g. `zoxide init`). After changing `[tools]`, a `mise install && mise reshim` (run_once_06 does this) is what makes the new binary appear.

## Authoritative sources for tools we ship

When you need ground truth (rather than my paraphrase), go here:

| Tool | Authoritative source |
| --- | --- |
| niri config grammar | [niri-wm/niri](https://github.com/niri-wm/niri) — `niri-config/src/lib.rs` for the actual struct definitions |
| niri compositor settings for Noctalia | [docs.noctalia.dev/v4/getting-started/compositor-settings/niri](https://docs.noctalia.dev/v4/getting-started/compositor-settings/niri/) |
| Noctalia IPC verbs | [`noctalia-dev/noctalia-shell` — `Services/Control/IPCService.qml`](https://github.com/noctalia-dev/noctalia-shell/blob/main/Services/Control/IPCService.qml) |
| opensessions sidebar (tmux) | [`Ataraxy-Labs/opensessions` — `docs/reference/`](https://github.com/Ataraxy-Labs/opensessions/tree/main/docs/reference) — `~/.config/opensessions/config.json` fields, built-in theme names, programmatic API |
| chezmoi `.chezmoiroot`, `chezmoi add --autotemplate`, `chezmoi state` | [chezmoi.io](https://www.chezmoi.io/) — `Reference / Special files and directories` |
| mise `[tools]` backends, registry short-names, `cargo:`/`ubi:`/`aqua:` syntax | [mise.jdx.dev](https://mise.jdx.dev/) — `Dev Tools / Registry` and `Backends`; the registry source is the per-tool `registry/*.toml` files in [jdx/mise](https://github.com/jdx/mise/tree/main/registry) |

## When in doubt: ask for diagnostics before pushing more code

After a sequence of "this should work" → "no it doesn't" cycles, **stop pushing code and ask the user for a diagnostic dump** instead. The pattern that's worked best in this repo:

```bash
{ echo "=== display ==="; ls -la /etc/systemd/system/display-manager.service 2>&1; systemctl is-enabled greetd lightdm ly 2>&1
  echo; echo "=== LUKS ==="; lsblk -f | head -20
  echo; echo "=== noctalia ==="; pacman -Q noctalia-shell 2>&1; pgrep -la qs 2>&1
  echo; echo "=== failing services ==="; systemctl --user --failed --no-pager 2>&1; systemctl --failed --no-pager 2>&1
  echo; echo "=== shell errors ==="; journalctl --user -b 0 2>&1 | grep -iE 'fail|error|missing|refused' | sort -u | head -25
} | gh gist create --filename diag.txt --secret --desc "thinkpad triage"
```

The gist URL is paste-able into a chat for a remote agent to read with `gh gist view <url>`.

---

*Last updated: 2026-06-05. Reflects the tooling & secrets migration: mise-primary
toolchain, nanobrew on macOS, NetBird mesh, Bitwarden+rbw (GPG/pass/age retired),
Zed by default. `Ideas.md` retired into `README.md`. Tag after applying on a machine.*
