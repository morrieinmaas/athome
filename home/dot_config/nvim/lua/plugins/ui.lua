-- Statusline (lualine) + the which-key popup.
return function()
  -- lualine's "everforest" theme name resolves because everforest is on packpath
  -- (added in pack.lua) by the time this runs on VimEnter — no explicit dep needed.
  require("lualine").setup({
    options = {
      theme                = "everforest",
      section_separators   = { left = "", right = "" },
      component_separators = { left = "│", right = "│" },
      globalstatus         = true,
    },
  })

  -- which-key v3 auto-discovers any keymap created with `desc = "..."`, so the
  -- popup populates without registration boilerplate. Group labels below just
  -- rename the prefixes; <leader>? mirrors tmux's prefix-Space (buffer-scoped).
  require("which-key").setup({
    preset = "modern",
    delay  = 250,
    icons  = { breadcrumb = "»", separator = "➜", group = "+" },
    spec = {
      { "<leader>f",  group = "find" },
      { "<leader>g",  group = "git" },
      { "<leader>gh", group = "git hunks" },
      { "<leader>h",  group = "harpoon" },
      { "<leader>x",  group = "trouble / diagnostics" },
      { "<leader>b",  group = "buffer" },
      { "<leader>u",  group = "toggle (ui)" },
      { "<leader>n",  group = "notify" },
      { "<leader>c",  group = "code" },
      { "<leader>m",  group = "markdown" },
      { "<leader>r",  group = "refactor / replace" },
      { "<leader>s",  group = "split / window" },
      { "<leader>S",  group = "session" },
      { "<leader>t",  group = "terminal" },
    },
  })
  vim.keymap.set("n", "<leader>?", function() require("which-key").show({ global = false }) end, { desc = "Buffer keymaps (which-key)" })
end
