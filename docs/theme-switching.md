# Unified theme switching (`theme`)

One command recolours **ghostty + tmux + nvim** at once. The dark/light
auto-toggle stays — each theme just supplies the light *and* dark palette and the
apps follow the OS appearance as before.

```bash
theme                 # fzf picker
theme everforest      # set directly
theme --current       # print active theme
theme --list          # list available themes
```

Available: `gruvbox` (default) · `everforest` · `catppuccin` · `tokyonight` ·
`rose-pine` · `kanagawa`.

## How it works

Single source of truth: **`~/.config/themes/registry.sh`** maps each theme to a
ghostty theme pair, an nvim colorscheme, and a tmux pill palette. The active
theme name is written to **`~/.config/themes/active`**.

| App | Mechanism |
|-----|-----------|
| **ghostty** | picker rewrites the live `theme = dark:…,light:…` line (reload with ⌘⇧,) |
| **tmux** | pills read `@theme_*` user options; `apply-tmux.sh` sets them live, `tmux.conf` restores the pick on a fresh server |
| **nvim** | 6 colorscheme plugins, all deriving light/dark from `vim.opt.background`; `config/theme.lua` reads `active` and fs-watches it, so open nvims switch instantly. `auto-dark-mode.nvim` still owns the OS toggle |
| **bat / delta / zed** | stay **gruvbox** — the only family all three bundle (bat ships no everforest/catppuccin/etc.) |

## Runtime-only

`theme` edits your live `~/.config` for instant effect — it never touches the
athome repo. The committed **default is gruvbox-light** (ghostty line, tmux
`@theme_*` defaults, nvim fallback). A fresh `chezmoi apply` / sync always lands
on gruvbox-light; the picker is a per-machine runtime layer on top.

## Adding a theme

1. Add a case to each function in `~/.config/themes/registry.sh`
   (`theme_ghostty`, `theme_nvim`, `theme_tmux`) and append the name to
   `THEME_LIST`.
2. Add its colorscheme plugin to `nvim/lua/plugins/colorscheme.lua` (lazy, set up
   to honour `vim.opt.background`) and map it in `nvim/lua/config/theme.lua`.
3. Confirm the ghostty theme name exists: `ghostty +list-themes | grep -i <name>`.
