-- Small quality-of-life plugins bundled in one file (each is small enough).
return {
  -- Surround motions: `ys` add, `cs` change, `ds` delete
  {
    "kylechui/nvim-surround",
    version = "*",
    event   = "VeryLazy",
    opts    = {},
  },

  -- Auto-detect indent settings per file (modern vim-sleuth replacement)
  {
    "NMAC427/guess-indent.nvim",
    event = { "BufReadPre", "BufNewFile" },
    opts  = {},
  },

  -- Auto-close brackets / quotes
  {
    "windwp/nvim-autopairs",
    event = "InsertEnter",
    opts  = { check_ts = true },
  },

  -- Render #rgb / #rrggbb / rgb(...) inline
  {
    "norcalli/nvim-colorizer.lua",
    event  = "VeryLazy",
    config = function() require("colorizer").setup() end,
  },

  -- TODO / FIXME / HACK highlighting + :TodoTrouble (uses the trouble panel
  -- below; no telescope dependency).
  {
    "folke/todo-comments.nvim",
    event        = "VeryLazy",
    dependencies = { "nvim-lua/plenary.nvim" },
    keys = {
      { "<leader>xt", "<cmd>TodoTrouble<cr>", desc = "Todo (Trouble)" },
    },
    opts = {},
  },

  -- Diagnostics / quickfix / LSP refs in a clean panel
  {
    "folke/trouble.nvim",
    cmd  = "Trouble",
    keys = {
      { "<leader>xx", "<cmd>Trouble diagnostics toggle<cr>",                       desc = "Diagnostics" },
      { "<leader>xX", "<cmd>Trouble diagnostics toggle filter.buf=0<cr>",          desc = "Buffer diagnostics" },
      { "<leader>xs", "<cmd>Trouble symbols toggle focus=false<cr>",               desc = "Symbols" },
      { "<leader>xl", "<cmd>Trouble lsp toggle focus=false win.position=right<cr>",desc = "LSP refs/defs" },
      { "<leader>xq", "<cmd>Trouble qflist toggle<cr>",                            desc = "Quickfix" },
    },
    opts = {},
  },
}
