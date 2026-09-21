-- Multi-theme support, driven by the shared `theme` picker.
--
-- The active theme NAME lives in ~/.config/themes/active (written by the `theme`
-- command). We map it to a colorscheme and apply it, letting the colorscheme
-- derive light vs dark from vim.opt.background — which auto-dark-mode.nvim keeps
-- in sync with the OS. A libuv fs-watch on the themes dir re-applies the moment
-- the picker rewrites the file, so already-open nvims switch live too.
--
-- Under vim.pack every colorscheme plugin is already on packpath, so :colorscheme
-- finds it with no on-demand load (unlike the old lazy.load dance). We only run
-- the ACTIVE theme's config (setup()/vim.g) once, right before applying it.

local M = {}

local active_file = vim.fn.expand("~/.config/themes/active")

-- theme name -> { scheme = :colorscheme arg, config? = fn run once before apply }
-- scheme is EITHER a string (the plugin derives light/dark from vim.opt.background)
-- OR a { light = "…", dark = "…" } table for plugins that ship separate schemes
-- (flexoki, selenized) — apply() picks the right name for the current background.
-- config sets whatever must exist before :colorscheme (lua setup() or vim.g flags).
M.themes = {
  gruvbox = {
    scheme = "gruvbox",
    config = function() require("gruvbox").setup({}) end,
  },
  everforest = {
    scheme = "everforest",
    config = function()
      vim.g.everforest_background = "medium" -- hard | medium | soft
      vim.g.everforest_enable_italic = 1
      vim.g.everforest_better_performance = 1
    end,
  },
  catppuccin = {
    scheme = "catppuccin",
    config = function()
      require("catppuccin").setup({ flavour = "auto", background = { light = "latte", dark = "mocha" } })
    end,
  },
  tokyonight = {
    scheme = "tokyonight",
    config = function() require("tokyonight").setup({ style = "moon", light_style = "day" }) end,
  },
  ["rose-pine"] = {
    scheme = "rose-pine",
    config = function() require("rose-pine").setup({ dark_variant = "moon" }) end, -- light → dawn
  },
  kanagawa = {
    scheme = "kanagawa",
    config = function() require("kanagawa").setup({ background = { dark = "wave", light = "lotus" } }) end,
  },
  -- ── Warm-paper + solarized family ────────────────────────────────────────────
  -- melange + zenbones follow vim.opt.background (one :colorscheme name for both);
  -- flexoki + selenized have no background-following scheme, so they map to
  -- explicit light/dark names. zenbones runs in compat mode (no lush.nvim);
  -- base16-nvim supplies selenized (base16-selenized-*).
  melange = { scheme = "melange" },
  zenbones = {
    scheme = "zenbones",
    config = function() vim.g.zenbones_compat = 1 end, -- render without lush.nvim
  },
  flexoki = { scheme = { light = "flexoki-light", dark = "flexoki-dark" } },
  selenized = { scheme = { light = "base16-selenized-light", dark = "base16-selenized-dark" } },
  -- tuxedo Dawn/Dusk: our own colors/dawn.lua (base16-nvim), follows background
  dawn = { scheme = "dawn" },
}

function M.read()
  local f = io.open(active_file, "r")
  if not f then return "gruvbox" end
  local name = (f:read("*l") or ""):gsub("%s+", "")
  f:close()
  return M.themes[name] and name or "gruvbox"
end

local configured = {}

function M.apply()
  local name = M.read()
  local t = M.themes[name]
  if t.config and not configured[name] then
    pcall(t.config)
    configured[name] = true
  end
  local scheme = t.scheme
  if type(scheme) == "table" then -- plugin has no background-following name
    scheme = (vim.opt.background:get() == "light") and scheme.light or scheme.dark
  end
  pcall(vim.cmd.colorscheme, scheme)
end

function M.watch()
  local uv = vim.uv or vim.loop
  local fse = uv.new_fs_event()
  if not fse then return end
  local d = vim.fn.fnamemodify(active_file, ":h")
  vim.fn.mkdir(d, "p")
  fse:start(d, {}, vim.schedule_wrap(function()
    -- re-require so themes added after this nvim started resolve (else → gruvbox)
    package.loaded["config.theme"] = nil
    require("config.theme").apply()
  end))
end

return M
