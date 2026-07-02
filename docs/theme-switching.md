# Unified theme switching (`theme`)

One command recolours **ghostty + tmux + nvim** at once. The dark/light
auto-toggle stays — each theme just supplies the light *and* dark palette and the
apps follow the OS appearance as before.

```bash
theme                 # fzf picker (with a live colour-swatch preview)
theme everforest      # set directly
theme --current       # print active theme
theme --list          # list available themes
```

Or from tmux: **`prefix T`** opens the picker in a `display-popup` (same style as
the `prefix P` pet-icon picker).

Available: `gruvbox` (default) · `everforest` · `catppuccin` · `tokyonight` ·
`rose-pine` · `kanagawa` · `flexoki` · `melange` · `zenbones` · `selenized`.

## How it works

Single source of truth: **`~/.config/themes/registry.sh`** maps each theme to a
ghostty theme pair, an nvim colorscheme, and a tmux pill palette. The active
theme name is written to **`~/.config/themes/active`**.

| App | Mechanism |
|-----|-----------|
| **ghostty** | picker rewrites the live `theme = dark:…,light:…` line and hot-reloads it with `SIGUSR2` — no restart, no ⌘⇧, |
| **tmux** | pills read `@theme_*` user options; `apply-tmux.sh` sets them live, `tmux.conf` restores the pick on a fresh server |
| **nvim** | 10 colorscheme plugins; most derive light/dark from `vim.opt.background`, but flexoki + selenized ship separate names so `config/theme.lua` maps them to `{ light, dark }` and picks per background. `config/theme.lua` reads `active` and fs-watches it, so open nvims switch instantly. `auto-dark-mode.nvim` still owns the OS toggle |
| **btm** (bottom) | **not switched** — its config styles everything with ANSI `Reset` + named accent colours, so it follows the terminal's active palette automatically (any family, light or dark). See `dot_config/bottom/bottom.toml` |
| **bat / delta / zed** | stay **gruvbox** — the only family all three bundle (bat ships no everforest/catppuccin/etc.) |
| **slk** (Slack TUI) | follows for the six original families; flexoki/melange/zenbones/selenized have no slk built-in, so they fall back to **gruvbox** (readable). tmux/superfile/gh-dash still track them via the hex palette |

btm (bottom) replaced btop. Rather than per-theme `.theme` files it colours every
element with ANSI `Reset` (the terminal's own foreground) + named accents, so it
tracks whatever ghostty palette is live across all ten families with no rewrite
and no OS-appearance branch. (btop washed out on the light themes because, with
`theme_background = false`, it painted its own light foreground on the transparent
terminal background — `Reset` can't.)

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
