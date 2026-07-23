-- Plugin management via Neovim's builtin vim.pack (0.12+). Replaces lazy.nvim.
--
-- Plugins install into stdpath('data')/site/pack/core/opt and are pinned by
-- nvim-pack-lock.json (tracked in athome — the cross-machine sync mechanism
-- that replaces lazy-lock.json; do NOT hand-edit it). On a fresh machine the
-- very first vim.pack call installs every locked plugin at its locked revision.
--
-- Startup strategy (mirrors the fredrikaverpil "lazy.nvim → vim.pack" writeup):
-- vim.pack has no per-plugin lazy-loading, so anything add()ed during init has
-- its plugin/ files sourced up front. We therefore add()+set up ONLY what the
-- first frame needs eagerly (colorschemes, treesitter, snacks, lsp, gitsigns,
-- persistence); everything interactive/UI is add()ed AND set up on VimEnter, so
-- its cost lands after the first paint. A `User VeryLazy` event is emitted after
-- that so autocmds written against lazy's old pseudo-event still fire.

local gh = function(repo) return "https://github.com/" .. repo end
local range = vim.version.range
local ADD = { confirm = false } -- fresh-machine install without a blocking prompt

-- ── Build hooks (replace lazy's `build =`). Registered BEFORE add() so they
--    fire on first install and on update. ──
vim.api.nvim_create_autocmd("PackChanged", {
  callback = function(ev)
    local name, kind = ev.data.spec.name, ev.data.kind
    if kind ~= "install" and kind ~= "update" then return end
    if name == "nvim-treesitter" then
      -- `main` branch: refresh parsers after the plugin's own code changes
      -- (treesitter.lua also install()s the parser set on config).
      vim.schedule(function() pcall(vim.cmd, "TSUpdate") end)
    elseif name == "markdown-preview.nvim" then
      -- fetch the prebuilt preview server (was build = mkdp#util#install)
      pcall(vim.fn["mkdp#util#install"])
    end
  end,
})

-- Each plugins/* module returns a setup function.
local function setup(mod, phase)
  local ok, err = pcall(function() require("plugins." .. mod)() end)
  if not ok then
    vim.notify("plugin setup failed (" .. phase .. "): " .. mod .. "\n" .. err, vim.log.levels.ERROR)
  end
end

-- ── EAGER: added + set up during init (needed for the first frame). ──
-- Bare URL → default branch. `version` pins a branch/tag/commit or semver range
-- (range("*") = latest stable tag; replaces lazy's version="*").
vim.pack.add({
  -- colorschemes — usable via :colorscheme from packpath; theme.lua configures
  -- and applies the active one.
  gh("ellisonleao/gruvbox.nvim"),
  gh("sainnhe/everforest"),           -- also provides lualine's "everforest" theme
  { src = gh("catppuccin/nvim"), name = "catppuccin" },
  gh("folke/tokyonight.nvim"),
  { src = gh("rose-pine/neovim"), name = "rose-pine" },
  gh("rebelot/kanagawa.nvim"),
  gh("kepano/flexoki-neovim"),
  gh("savq/melange-nvim"),
  gh("zenbones-theme/zenbones.nvim"),
  gh("RRethy/base16-nvim"),
  gh("f-person/auto-dark-mode.nvim"),
  -- first-frame plugins
  gh("nvim-tree/nvim-web-devicons"),  -- snacks/dashboard icons (+ bufferline/render-markdown later)
  { src = gh("nvim-treesitter/nvim-treesitter"), version = "main" },
  gh("folke/snacks.nvim"),
  gh("folke/persistence.nvim"),
  gh("neovim/nvim-lspconfig"),
  -- blink is set up on VimEnter, but its MODULE is needed eagerly by lsp.lua
  -- (blink.get_lsp_capabilities) so its LSP capabilities reach servers that
  -- attach to the first buffer.
  { src = gh("saghen/blink.cmp"), version = range("*") }, -- stable tag → prebuilt fuzzy binary
  gh("rafamadriz/friendly-snippets"),
}, ADD)

for _, m in ipairs({ "colorscheme", "treesitter", "snacks", "persistence", "lsp" }) do
  setup(m, "eager")
end

-- ── LATER: added AND set up on VimEnter (interactive / UI). ──
vim.api.nvim_create_autocmd("VimEnter", {
  once = true,
  callback = function()
    vim.schedule(function()
      vim.pack.add({
        gh("stevearc/conform.nvim"),
        gh("lewis6991/gitsigns.nvim"),
        { src = gh("ThePrimeagen/harpoon"), version = "harpoon2" },
        gh("nvim-lua/plenary.nvim"), -- harpoon / todo-comments dep
        gh("christoomey/vim-tmux-navigator"),
        { src = gh("akinsho/bufferline.nvim"), version = range("*") },
        gh("nvim-lualine/lualine.nvim"),
        gh("folke/which-key.nvim"),
        gh("folke/noice.nvim"),
        gh("MunifTanjim/nui.nvim"), -- noice dep
        gh("sindrets/diffview.nvim"),
        { src = gh("kylechui/nvim-surround"), version = range("*") },
        gh("NMAC427/guess-indent.nvim"),
        gh("windwp/nvim-autopairs"),
        gh("norcalli/nvim-colorizer.lua"),
        gh("folke/todo-comments.nvim"),
        gh("folke/trouble.nvim"),
        gh("MagicDuck/grug-far.nvim"),
        gh("MeanderingProgrammer/render-markdown.nvim"),
        gh("iamcco/markdown-preview.nvim"),
      }, ADD)

      for _, m in ipairs({
        "completion", "format", "git", "harpoon", "navigation", "ui",
        "noice", "quality", "search-replace", "markdown",
      }) do
        setup(m, "later")
      end

      -- Fire VeryLazy for autocmds written against lazy's pseudo-event.
      pcall(vim.api.nvim_exec_autocmds, "User", { pattern = "VeryLazy", modeline = false })
    end)
  end,
})

-- ── Apply the active colorscheme now (theme reads ~/.config/themes/active and
--    watches it for the picker switching live). ──
local theme = require("config.theme")
theme.apply()
theme.watch()
