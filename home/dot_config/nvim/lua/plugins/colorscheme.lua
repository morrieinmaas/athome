-- Colorschemes + the dark/light + theme-picker wiring.
--
-- All six colorscheme plugins are declared lazy and loaded on demand by
-- config/theme.apply() (it reads ~/.config/themes/active). Each is configured to
-- derive its palette from vim.opt.background, so the ONE colorscheme handles both
-- light and dark and auto-dark-mode.nvim just flips background. Switch the family
-- with the shared `theme` command — running nvims pick it up via theme.watch().
return {
  { "ellisonleao/gruvbox.nvim", lazy = true },

  {
    "sainnhe/everforest",
    lazy = true,
    init = function()
      vim.g.everforest_background = "medium" -- hard | medium | soft
      vim.g.everforest_enable_italic = 1
      vim.g.everforest_better_performance = 1
    end,
  },

  {
    "catppuccin/nvim",
    name = "catppuccin",
    lazy = true,
    opts = { flavour = "auto", background = { light = "latte", dark = "mocha" } },
  },

  {
    "folke/tokyonight.nvim",
    lazy = true,
    opts = { style = "moon", light_style = "day" },
  },

  {
    "rose-pine/neovim",
    name = "rose-pine",
    lazy = true,
    opts = { dark_variant = "moon" }, -- light → dawn (derived from background)
  },

  {
    "rebelot/kanagawa.nvim",
    lazy = true,
    opts = { background = { dark = "wave", light = "lotus" } },
  },

  -- Owns the dark/light toggle. On startup AND whenever the OS appearance flips,
  -- it sets background then re-applies the active theme so the palette follows.
  {
    "f-person/auto-dark-mode.nvim",
    lazy = false,
    priority = 999,
    opts = {
      update_interval = 3000,
      set_dark_mode = function()
        vim.opt.background = "dark"
        require("config.theme").apply()
      end,
      set_light_mode = function()
        vim.opt.background = "light"
        require("config.theme").apply()
      end,
    },
  },
}
