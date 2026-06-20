-- nvim-treesitter `main` branch. The classic `master` branch is FROZEN
-- (2026-03) and breaks on neovim 0.12: its query_predicates assume one node per
-- capture, but 0.12 passes a LIST per capture, so `get_node_text` hits
-- `node:range()` on a table → "attempt to call method 'range' (a nil value)" on
-- every markdown / injected buffer. `main` is the supported rewrite: install()
-- for parsers, vim.treesitter.start() per filetype for highlight, indentexpr for
-- indent. incremental_selection was removed → reimplemented below.
return {
  {
    "nvim-treesitter/nvim-treesitter",
    branch = "main",
    lazy = false, -- main does NOT support lazy-loading
    build = ":TSUpdate",
    config = function()
      local parsers = {
        "lua", "vim", "vimdoc", "bash", "fish",
        "python", "go", "rust", "typescript", "javascript", "tsx",
        "json", "yaml", "toml", "kdl", "markdown", "markdown_inline",
        "dockerfile", "gitignore", "gitcommit",
        "html", "css", "sql", "make", "just",
      }
      require("nvim-treesitter").install(parsers)

      -- Highlight + treesitter indent for any buffer that has a parser.
      -- vim.treesitter.start() derives the language from the filetype and errors
      -- when no parser is installed, so guard it with pcall.
      vim.api.nvim_create_autocmd("FileType", {
        group = vim.api.nvim_create_augroup("ts_enable", { clear = true }),
        callback = function(ev)
          if pcall(vim.treesitter.start, ev.buf) then
            vim.bo[ev.buf].indentexpr = "v:lua.require'nvim-treesitter'.indentexpr()"
          end
        end,
      })

      -- ── incremental selection (dropped from main) — minimal reimpl ──
      -- <leader>v grows the visual selection to the enclosing node; <leader>V
      -- shrinks back one step. A per-call stack of nodes tracks the history.
      local stack = {}
      local function select_node(node)
        local sr, sc, er, ec = node:range()
        if ec == 0 then -- range ends at column 0 of er → really end of prev line
          er = er - 1
          ec = #vim.fn.getline(er + 1)
        end
        vim.fn.setpos("'<", { 0, sr + 1, sc + 1, 0 })
        vim.fn.setpos("'>", { 0, er + 1, ec, 0 }) -- ec (0-idx exclusive) == inclusive 1-idx col
        vim.cmd("normal! gv")
      end
      local function grow()
        local node = stack[#stack]
        if not node then -- first press: node under cursor
          node = vim.treesitter.get_node()
        else
          node = node:parent()
        end
        if not node then return end
        stack[#stack + 1] = node
        select_node(node)
      end
      local function shrink()
        if #stack > 1 then
          stack[#stack] = nil
          select_node(stack[#stack])
        end
      end
      vim.keymap.set("n", "<leader>v", function()
        stack = {}
        grow()
      end, { desc = "Treesitter: select node" })
      vim.keymap.set("x", "<leader>v", grow, { desc = "Treesitter: grow selection" })
      vim.keymap.set("x", "<leader>V", shrink, { desc = "Treesitter: shrink selection" })
    end,
  },
}
