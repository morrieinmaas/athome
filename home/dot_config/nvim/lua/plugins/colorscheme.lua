return {
  {
    "sainnhe/everforest",
    name = "everforest",
    lazy = false,
    priority = 1000,
    config = function()
      -- everforest uses ONE colorscheme ("everforest") and switches light/dark
      -- via `vim.opt.background` (same pattern the old gruvbox setup used).
      -- These globals must be set BEFORE `:colorscheme`.
      vim.g.everforest_background = "medium" -- hard | medium | soft
      vim.g.everforest_enable_italic = 1 -- italic comments
      vim.g.everforest_better_performance = 1
      vim.g.everforest_transparent_background = 0
      vim.opt.background = "light" -- light-mode primary
      vim.cmd.colorscheme("everforest")
    end,
  },
  {
    "f-person/auto-dark-mode.nvim",
    lazy = false,
    priority = 999,
    opts = {
      update_interval = 3000,
      -- Same colorscheme both ways; everforest derives the palette from `background`.
      set_dark_mode = function()
        vim.opt.background = "dark"
        vim.cmd.colorscheme("everforest")
      end,
      set_light_mode = function()
        vim.opt.background = "light"
        vim.cmd.colorscheme("everforest")
      end,
    },
  },
}
