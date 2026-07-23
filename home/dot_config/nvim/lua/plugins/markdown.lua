-- Markdown preview, three complementary ways (all under the <leader>m group):
--   1. render-markdown.nvim — renders pretty INSIDE the buffer (headings, code
--      blocks, tables, bullets, checkboxes). Works in the terminal + tmux, no
--      browser. This is the "live preview while editing" experience. <leader>mr
--   2. glow — a full render in a terminal SPLIT (no browser, stay in nvim). <leader>mg
--   3. markdown-preview.nvim — opens a live GitHub-styled preview in the BROWSER
--      (the VSCode "Open Preview" equivalent), scroll-synced. <leader>mp
--
-- render-markdown needs the markdown + markdown_inline treesitter parsers (already
-- in treesitter.lua) and nvim-web-devicons (added in pack.lua). The three <leader>m
-- keymaps are markdown-buffer-local (a FileType autocmd), matching the old ft= keys.
-- markdown-preview's prebuilt server is fetched by pack.lua's PackChanged hook.
return function()
  require("render-markdown").setup({
    completions = { lsp = { enabled = true } },
    -- on by default in markdown buffers; toggle with <leader>mr below
  })

  vim.g.mkdp_theme = "light" -- match the everforest-light setup

  vim.api.nvim_create_autocmd("FileType", {
    pattern = "markdown",
    callback = function(ev)
      local map = function(lhs, rhs, desc)
        vim.keymap.set("n", lhs, rhs, { buffer = ev.buf, desc = desc })
      end
      map("<leader>mr", "<cmd>RenderMarkdown toggle<cr>", "Markdown: toggle in-buffer render")
      -- glow render in a vertical split (file captured before the split so `%`
      -- still points at the markdown buffer). glow comes from mise.
      map("<leader>mg", function()
        local file = vim.fn.shellescape(vim.fn.expand("%:p"))
        vim.cmd("vsplit")
        -- Tell glow the split's actual width so it wraps instead of overflowing
        -- the pane (it doesn't reliably detect the pty width).
        local w = math.max(40, vim.api.nvim_win_get_width(0) - 2)
        vim.cmd("terminal glow -s auto -w " .. w .. " -p " .. file)
        vim.cmd("startinsert")
      end, "Markdown: glow render (vertical split)")
      map("<leader>mp", "<cmd>MarkdownPreviewToggle<cr>", "Markdown: browser preview")
    end,
  })
end
