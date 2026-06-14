# tmux cockpit — design

**Date:** 2026-06-13
**Status:** Approved, pre-implementation
**Repo:** athome (`~/personal/athome`)

## Problem

After migrating repos into the `~/work | ~/personal | ~/sidebiz` context trees, there's
no fast, reproducible way to spin up a standard tmux workspace. continuum restores the
*last* layout and `prefix K` creates ad-hoc sessions, but neither gives a deterministic
"here is my standard cockpit" — useful on a fresh machine and as an on-demand reset.

## Goals

- Bootstrap **4 sessions** — `work`, `personal`, `sidebiz`, `misc` (scratchpad) — from one
  source of truth, usable **both** on demand (keybind) and offered at genuine cold start.
- Every window (bootstrap *and* interactive `prefix c`) uses one reusable **pane layout**:
  left full-height pane ┃ right column split top/bottom (3 panes), i.e. tmux `main-vertical`.
- Coexist with continuum: never fight a restored layout; bootstrap is idempotent.

## Non-goals (explicitly out of scope)

- No folder-derived windows (one window per repo/entity). Rejected: window explosion.
- No named/custom layout templates beyond `main-vertical`. One layout now; structure so
  more can be added later.
- No replacement of continuum or `prefix K` for daily lazy flow.

## Tool decision

**smug** (single Go binary, YAML). It applies the pane layout natively via `layout: main-vertical`
(verified against its README). It is one-shot (exits after building the session) and has **no**
named-template reuse — both fine here: `main-vertical` is the shared reference, and the
interactive-window layout is a tmux binding regardless of tool. smug/tmuxp's window-tree
enumeration is unused (4 sessions × 1 window), but smug remains the cleanest declarative
bootstrap. Bespoke-script alternative rejected (no dependency, but no upside at this scope).

## Components

### 1. smug configs — `home/dot_config/smug/{work,personal,sidebiz,misc}.yaml`
Each: one session rooted at its tree, one `main` window, `main-vertical`, 3 empty-shell panes.
```yaml
session: work
root: ~/work
windows:
  - name: main
    layout: main-vertical
    panes: ["", "", ""]
```
Roots: `work`→`~/work`, `personal`→`~/personal`, `sidebiz`→`~/sidebiz`, `misc`→`~`.

### 2. `prefix c` rebinding — `home/dot_config/tmux/tmux.conf`
```tmux
bind c new-window -c "#{pane_current_path}" \; split-window -h -c "#{pane_current_path}" \; \
       split-window -v -c "#{pane_current_path}" \; select-layout main-vertical \; select-pane -t 1
bind C new-window -c "#{pane_current_path}"    # escape hatch: plain single-pane window
```
Replaces the existing simple `bind c`. Left (main) pane focused. Width = tmux default
(`main-pane-width`, tunable later).

### 3. Bootstrap script — `home/dot_local/bin/executable_cockpit`
Idempotent. For each of the 4: `tmux has-session -t <name>` → skip if present, else build it.
**Must build DETACHED** — `smug start <name>` attaches and would block on the 2nd session.
Use smug's detach flag if it exists (verify: `smug start <name> --detach`/`-d`); otherwise the
script falls back to tmux-native creation of that session + applying the layout (smug stays the
config source for the interactive case). Safe to re-run; won't clobber continuum-restored sessions.

### 4. `prefix B` binding — `home/dot_config/tmux/tmux.conf`
Runs `cockpit` (build any missing sessions) from inside tmux, no detach.

### 5. Cold-start offer — `home/dot_zsh/functions.zsh`
On interactive shell start, **only when** `$TMUX` is empty AND `tmux ls` shows zero sessions:
prompt `bootstrap cockpit? [y/N]` (default N). Fires only on a fresh machine; once continuum
has a save, sessions exist → stays quiet.

### 6. Install — `home/.chezmoidata/packages.yaml`
Add `smug`. Live: `nb install smug`.

## Defaults chosen
`misc` rooted at `~` · panes are empty shells (no auto-run) · `prefix C` = plain window ·
single layout `main-vertical`.

## Verification
- `cockpit` from a clean server → 4 sessions, each `main` window with the 3-pane main-vertical layout.
- Re-run `cockpit` → no duplicates, existing sessions untouched.
- `prefix c` in any session → new window with the layout; `prefix C` → plain window.
- Cold start (kill server, no continuum save) → prompt appears; with sessions present → no prompt.
- Identity unaffected (sessions are just cwd; git identity still per-tree).

## Files touched
- new: `home/dot_config/smug/{work,personal,sidebiz,misc}.yaml`
- new: `home/dot_local/bin/executable_cockpit`
- edit: `home/dot_config/tmux/tmux.conf` (`bind c`, `bind C`, `bind B`)
- edit: `home/dot_zsh/functions.zsh` (cold-start offer)
- edit: `home/.chezmoidata/packages.yaml` (smug)
- live mirror (no chezmoi apply): `~/.config/smug/*`, `~/.local/bin/cockpit`, `~/.config/tmux/tmux.conf`, smug install
