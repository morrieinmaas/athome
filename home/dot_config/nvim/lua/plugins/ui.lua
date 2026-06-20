return {
  {
    "nvim-lualine/lualine.nvim",
    event        = "VeryLazy",
    -- Depend on everforest so it loads + registers the "everforest" lualine
    -- theme name BEFORE lualine reads its opts. Without this, lualine
    -- silently falls back to its default theme because the theme string
    -- doesn't resolve at lualine.setup() time (see plugins/colorscheme.lua).
    dependencies = { "sainnhe/everforest" },
    opts = {
      options = {
        theme                = "everforest",
        section_separators   = { left = "", right = "" },
        component_separators = { left = "│", right = "│" },
        globalstatus         = true,
      },
    },
  },
  {
    "folke/which-key.nvim",
    event = "VeryLazy",
    -- v3 auto-discovers any keymap created with `desc = "..."`, so the
    -- popup populates without any registration boilerplate. Mirror tmux's
    -- prefix-Space popup with <leader>? for buffer-scoped keys.
    opts = {
      preset = "modern",
      delay  = 250,
      icons  = { breadcrumb = "»", separator = "➜", group = "+" },
      -- Group labels so the popup reads "+find / +git / +harpoon" instead of
      -- raw keys. which-key v3 merges these with auto-discovered desc mappings.
      spec = {
        { "<leader>f", group = "find" },
        { "<leader>g", group = "git" },
        { "<leader>gh", group = "git hunks" },
        { "<leader>h", group = "harpoon" },
        { "<leader>x", group = "trouble / diagnostics" },
        { "<leader>b", group = "buffer" },
        { "<leader>u", group = "toggle (ui)" },
        { "<leader>n", group = "notify" },
        { "<leader>c", group = "code" },
        { "<leader>m", group = "markdown" },
        { "<leader>r", group = "refactor / replace" },
        { "<leader>s", group = "split / window" },
        { "<leader>S", group = "session" },
        { "<leader>t", group = "terminal" },
      },
    },
    keys = {
      {
        "<leader>?",
        function() require("which-key").show({ global = false }) end,
        desc = "Buffer keymaps (which-key)",
      },
    },
  },
}
