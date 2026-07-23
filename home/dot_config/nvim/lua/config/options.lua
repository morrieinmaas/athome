local opt = vim.opt

vim.g.mapleader      = " "
vim.g.maplocalleader = ","

-- Disable builtin runtime plugins we don't use (was lazy.nvim's
-- performance.rtp.disabled_plugins). Must be set before they source — options
-- is the first require in init.lua, and runtime plugins load after init. netrw
-- is replaced by snacks.explorer; matchit/matchparen by treesitter. Saves ~20ms.
for _, p in ipairs({
  "gzip", "matchit", "matchparen", "netrwPlugin", "tarPlugin",
  "tohtml", "tutor", "zipPlugin",
}) do
  vim.g["loaded_" .. p] = 1
end
vim.g.loaded_netrw = 1 -- netrwPlugin checks both loaded_netrw and loaded_netrwPlugin

opt.number         = true
opt.relativenumber = true
opt.signcolumn     = "yes"
opt.cursorline     = true
opt.scrolloff      = 8
opt.sidescrolloff  = 8

opt.expandtab   = true
opt.shiftwidth  = 2
opt.tabstop     = 2
opt.softtabstop = 2
opt.smartindent = true

opt.ignorecase = true
opt.smartcase  = true
opt.hlsearch   = false
opt.incsearch  = true

opt.splitright = true
opt.splitbelow = true

opt.undofile   = true
opt.swapfile   = false
opt.backup     = false

opt.termguicolors = true
opt.background    = "light"

opt.clipboard = "unnamedplus"
opt.mouse     = "a"
opt.updatetime = 250
opt.timeoutlen = 300

opt.completeopt = { "menu", "menuone", "noselect" }
opt.wildmode    = { "longest", "list", "full" }
