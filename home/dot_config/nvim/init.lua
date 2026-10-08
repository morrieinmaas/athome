-- ~/.config/nvim/init.lua
-- This config uses Neovim 0.11+ APIs (vim.lsp.config, vim.lsp.enable).
-- mise / Homebrew / Arch all ship that or newer; warn if you ever drop below.
if vim.fn.has("nvim-0.11") == 0 then
  vim.schedule(function()
    vim.notify(
      "athome nvim config wants Neovim >= 0.11 (LSP API). You're on "
        .. tostring(vim.version()),
      vim.log.levels.WARN
    )
  end)
end

require("config.options")
require("config.keymaps")
require("config.topdf").setup()
require("config.pack")
