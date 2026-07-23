# Reproducible nvim plugin installs (nvim-pack-lock.json)

The nvim config uses Neovim's built-in **`vim.pack`** (0.12+) as its plugin
manager — no lazy.nvim. `vim.pack` writes `~/.config/nvim/nvim-pack-lock.json`,
which pins every plugin to a specific git revision. Committing it to chezmoi
means **every machine reads the same pins** and installs identical versions.

The plugin set lives in `home/dot_config/nvim/lua/config/pack.lua`
(`vim.pack.add{}` specs); the lockfile is generated, not hand-edited.

## First time on a fresh machine

`chezmoi apply` deploys the nvim config **and** the committed lockfile before
the first launch. On first `nvim`, the very first `vim.pack` call reconciles the
lockfile against disk and installs every plugin at its **locked revision** — no
version drift, no "works on my mac". Installs run without a prompt
(`vim.pack.add(..., { confirm = false })`).

```bash
nvim        # first launch installs all plugins at the locked revisions, then loads
:qa
```

If there is no lockfile yet (very first machine), the first launch installs the
newest revision matching each spec's `version`, then writes the lockfile —
capture it:

```bash
chezmoi add ~/.config/nvim/nvim-pack-lock.json
chezmoi cd && git add . && git commit -m "chore(nvim): pin nvim-pack-lock.json" && git push && exit
```

## Updating plugins (any machine)

```text
nvim
:lua vim.pack.update()   # or the dashboard `u` key ("Update plugins")
```

`vim.pack.update()` opens a **confirmation buffer** in a new tab showing every
pending change (`>` applied, `<` reverted). Review it, then:

- `:write` to confirm the updates (bumps the lockfile), or
- `:quit` to discard.

`:restart` to start using the updated code. Then capture the bumped lockfile:

```bash
chezmoi re-add ~/.config/nvim/nvim-pack-lock.json
chezmoi cd && git add . && git commit -m "chore(nvim): bump plugin pins" && git push && exit
```

Other machines pull, `chezmoi apply`, relaunch nvim → `vim.pack` installs the new
locked revisions on the next `vim.pack.update(nil, { target = "lockfile" })` (or
they float to the lockfile pins on first reconcile).

## Build steps (treesitter, markdown-preview, blink)

`pack.lua` registers a `PackChanged` autocmd (the `build =` replacement) that
runs on install/update:

- **nvim-treesitter** (`main` branch) → `:TSUpdate` recompiles parsers.
- **markdown-preview.nvim** → fetches its prebuilt browser-preview server.
- **blink.cmp** is pinned to the latest stable tag (`version = range("*")`) so it
  downloads a prebuilt Rust fuzzy binary — no toolchain build needed.

Treesitter parsers are compiled per-machine and are **not** in the lockfile
(latest parsers on each machine is fine).

## LSP servers

Language servers are **not** installed by nvim — they come from **mise**
(`home/dot_config/mise/config.toml`), so they're already on `PATH` when nvim
runs. `lsp.lua` enables each server only if its binary is present. No Mason.
