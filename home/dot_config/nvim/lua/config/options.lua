local opt = vim.opt

vim.g.mapleader      = " "
vim.g.maplocalleader = ","

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
opt.background    = "dark"

opt.clipboard = "unnamedplus"
opt.mouse     = "a"
opt.updatetime = 250
opt.timeoutlen = 300

opt.completeopt = { "menu", "menuone", "noselect" }
opt.wildmode    = { "longest", "list", "full" }
