-- Small quality-of-life plugins bundled in one file (each is small enough).
return function()
  -- Surround motions: `ys` add, `cs` change, `ds` delete
  require("nvim-surround").setup({})

  -- Auto-detect indent settings per file (modern vim-sleuth replacement)
  require("guess-indent").setup({})

  -- Auto-close brackets / quotes
  require("nvim-autopairs").setup({ check_ts = true })

  -- Render #rgb / #rrggbb / rgb(...) inline
  require("colorizer").setup()

  -- TODO / FIXME / HACK highlighting + :TodoTrouble (uses the trouble panel
  -- below; no telescope dependency). plenary is its dep, added in pack.lua.
  require("todo-comments").setup({})
  vim.keymap.set("n", "<leader>xt", "<cmd>TodoTrouble<cr>", { desc = "Todo (Trouble)" })

  -- Diagnostics / quickfix / LSP refs in a clean panel
  require("trouble").setup({})
  vim.keymap.set("n", "<leader>xx", "<cmd>Trouble diagnostics toggle<cr>",              { desc = "Diagnostics" })
  vim.keymap.set("n", "<leader>xX", "<cmd>Trouble diagnostics toggle filter.buf=0<cr>", { desc = "Buffer diagnostics" })
  vim.keymap.set("n", "<leader>xs", "<cmd>Trouble symbols toggle focus=false<cr>",      { desc = "Symbols" })
  vim.keymap.set("n", "<leader>xl", "<cmd>Trouble lsp toggle focus=false win.position=right<cr>", { desc = "LSP refs/defs" })
  vim.keymap.set("n", "<leader>xq", "<cmd>Trouble qflist toggle<cr>",                   { desc = "Quickfix" })
end
