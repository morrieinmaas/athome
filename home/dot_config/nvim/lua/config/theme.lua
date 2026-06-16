-- Multi-theme support, driven by the shared `theme` picker.
--
-- The active theme NAME lives in ~/.config/themes/active (written by the `theme`
-- command). We map it to a colorscheme and apply it, letting the colorscheme
-- derive light vs dark from vim.opt.background — which auto-dark-mode.nvim keeps
-- in sync with the OS. A libuv fs-watch on the themes dir re-applies the moment
-- the picker rewrites the file, so already-open nvims switch live too.

local M = {}

local active_file = vim.fn.expand("~/.config/themes/active")

-- theme name -> { plugin = lazy spec name (for on-demand load), scheme = :colorscheme arg }
M.themes = {
  gruvbox       = { plugin = "gruvbox.nvim",    scheme = "gruvbox" },
  everforest    = { plugin = "everforest",      scheme = "everforest" },
  catppuccin    = { plugin = "catppuccin",      scheme = "catppuccin" },
  tokyonight    = { plugin = "tokyonight.nvim", scheme = "tokyonight" },
  ["rose-pine"] = { plugin = "rose-pine",       scheme = "rose-pine" },
  kanagawa      = { plugin = "kanagawa.nvim",   scheme = "kanagawa" },
}

function M.read()
  local f = io.open(active_file, "r")
  if not f then return "gruvbox" end
  local name = (f:read("*l") or ""):gsub("%s+", "")
  f:close()
  return M.themes[name] and name or "gruvbox"
end

function M.apply()
  local t = M.themes[M.read()]
  -- ensure the colorscheme's plugin is loaded (they're lazy), then apply
  pcall(function() require("lazy").load({ plugins = { t.plugin } }) end)
  pcall(vim.cmd.colorscheme, t.scheme)
end

function M.watch()
  local uv = vim.uv or vim.loop
  local fse = uv.new_fs_event()
  if not fse then return end
  local d = vim.fn.fnamemodify(active_file, ":h")
  vim.fn.mkdir(d, "p")
  fse:start(d, {}, vim.schedule_wrap(function()
    M.apply()
  end))
end

return M
