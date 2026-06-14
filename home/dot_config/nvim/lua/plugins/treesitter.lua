return {
  {
    "nvim-treesitter/nvim-treesitter",
    branch = "master",  -- pin the classic API (require("nvim-treesitter.configs").setup); `main` removed it
    build = ":TSUpdate",
    event = { "BufReadPost", "BufNewFile" },
    opts = {
      ensure_installed = {
        "lua", "vim", "vimdoc", "bash", "fish",
        "python", "go", "rust", "typescript", "javascript", "tsx",
        "json", "yaml", "toml", "kdl", "markdown", "markdown_inline",
        "dockerfile", "gitignore", "gitcommit",
        "html", "css", "sql", "make", "just",
      },
      highlight  = { enable = true },
      indent     = { enable = true },
      incremental_selection = {
        enable = true,
        keymaps = {
          init_selection    = "<leader>v",   -- was <C-space>; freed for cmp (insert-mode complete)
          node_incremental  = "<leader>v",
          scope_incremental = false,
          node_decremental  = "<leader>V",
        },
      },
    },
    config = function(_, opts)
      require("nvim-treesitter.configs").setup(opts)
    end,
  },
}
