# Reproducible nvim plugin installs (lazy-lock.json)

`lazy.nvim` writes `~/.config/nvim/lazy-lock.json` whenever you run
`:Lazy update` or `:Lazy install`. The file pins every plugin to a
specific commit. Committing it to chezmoi means **every machine reads
the same pins** and installs identical versions.

## First time on a fresh machine

```bash
# Already done by chezmoi apply: nvim config deployed, no lock file yet.
nvim                              # lazy.nvim auto-installs latest of each plugin
:Lazy update                      # confirm
:qa
```

Now `~/.config/nvim/lazy-lock.json` exists. Capture it:

```bash
chezmoi add ~/.config/nvim/lazy-lock.json
chezmoi cd
git add . && git commit -m "chore(nvim): pin lazy-lock.json after first install" && git push
exit
```

## Subsequent machines

After cloning this repo into chezmoi source dir, the lockfile gets
deployed to `~/.config/nvim/lazy-lock.json` BEFORE first nvim launch.
When nvim starts, lazy.nvim sees the lockfile and installs the *exact*
commits — no version drift, no "works on my mac".

## Updating plugins (any machine)

```bash
nvim
:Lazy update                      # bumps lock file
:qa

chezmoi re-add ~/.config/nvim/lazy-lock.json
chezmoi cd && git add . && git commit -m "chore(nvim): bump plugin pins" && git push
exit
```

Other machines pull, `chezmoi apply`, re-launch nvim → lazy syncs to
the new commits.

## What about treesitter parsers?

`nvim-treesitter` parsers are downloaded + compiled at install time;
they're not in `lazy-lock.json`. To pin those across machines, you'd
need `nvim-treesitter`'s `lock` feature or commit `~/.local/share/nvim/lazy/nvim-treesitter/parser/`
manifests — usually overkill. Latest parsers on each machine is fine.

## What about Mason packages (LSP servers)?

Mason installs binaries (lua-language-server, ruff, etc.) per-machine.
They auto-install via `ensure_installed` in `lsp.lua` — versions float
to latest. Pin manually only if you hit a bad release.
