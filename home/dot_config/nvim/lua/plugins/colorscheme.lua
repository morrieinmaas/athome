-- auto-dark-mode.nvim owns the dark/light toggle. On startup AND whenever the OS
-- appearance flips, it sets vim.opt.background then re-applies the active theme
-- (config/theme.lua) so the palette follows.
--
-- The colorscheme PLUGINS themselves are added by config/pack.lua and each is
-- configured per-theme in config/theme.lua (setup()/vim.g run once, on apply).
-- Nothing here loads a colorscheme — :colorscheme finds them all on packpath.
return function()
  require("auto-dark-mode").setup({
    update_interval = 3000,
    set_dark_mode = function()
      vim.opt.background = "dark"
      require("config.theme").apply()
    end,
    set_light_mode = function()
      vim.opt.background = "light"
      require("config.theme").apply()
    end,
  })
end
