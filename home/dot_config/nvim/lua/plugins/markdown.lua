-- Markdown preview, three complementary ways (all under the <leader>m group):
--   1. render-markdown.nvim — renders pretty INSIDE the buffer (headings, code
--      blocks, tables, bullets, checkboxes). Works in the terminal + tmux, no
--      browser. This is the "live preview while editing" experience. <leader>mr
--   2. glow — a full render in a terminal SPLIT (no browser, stay in nvim).
--      <leader>mg
--   3. markdown-preview.nvim — opens a live GitHub-styled preview in the
--      BROWSER (the VSCode "Open Preview" equivalent), scroll-synced. <leader>mp
return {
  {
    "MeanderingProgrammer/render-markdown.nvim",
    ft = { "markdown", "markdown.mdx", "codecompanion" },
    dependencies = {
      "nvim-treesitter/nvim-treesitter", -- needs markdown + markdown_inline parsers (already in treesitter.lua)
      "nvim-tree/nvim-web-devicons",
    },
    opts = {
      completions = { lsp = { enabled = true } },
      -- on by default in markdown buffers; toggle with the keymap below
    },
    keys = {
      { "<leader>mr", "<cmd>RenderMarkdown toggle<cr>", desc = "Markdown: toggle in-buffer render", ft = "markdown" },
      -- glow render in a vertical split (file captured before the split so `%`
      -- still points at the markdown buffer). glow comes from mise.
      {
        "<leader>mg",
        function()
          local file = vim.fn.shellescape(vim.fn.expand("%:p"))
          vim.cmd("vsplit")
          -- Tell glow the split's actual width so it wraps instead of
          -- overflowing the pane (it doesn't reliably detect the pty width).
          local w = math.max(40, vim.api.nvim_win_get_width(0) - 2)
          vim.cmd("terminal glow -s auto -w " .. w .. " -p " .. file)
          vim.cmd("startinsert")
        end,
        desc = "Markdown: glow render (vertical split)",
        ft = "markdown",
      },
    },
  },
  {
    "iamcco/markdown-preview.nvim",
    cmd = { "MarkdownPreview", "MarkdownPreviewStop", "MarkdownPreviewToggle" },
    ft = { "markdown" },
    -- Downloads the prebuilt preview server (no global npm install needed).
    build = function() vim.fn["mkdp#util#install"]() end,
    keys = {
      { "<leader>mp", "<cmd>MarkdownPreviewToggle<cr>", desc = "Markdown: browser preview", ft = "markdown" },
    },
    init = function()
      vim.g.mkdp_theme = "light" -- match the everforest-light setup
    end,
  },
}
