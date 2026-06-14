-- noice.nvim — the centered command-line popup (the `command_palette` preset)
-- instead of stock nvim's bottom `:` line, plus the completion menu. This is the
-- original spec MINUS the one fragile bit: the `lsp.override` block monkey-patched
-- core LSP util fns (convert_input_to_markdown_lines / stylize_markdown) and broke
-- on Neovim updates — the reason noice was dropped in d23f6fe — so it's now empty.
-- LSP hover keeps its native rounded border (lsp.lua); noice still owns the
-- cmdline + signature popup. Notifications still go through snacks.notifier.
return {
  {
    "folke/noice.nvim",
    event        = "VeryLazy",
    dependencies = { "MunifTanjim/nui.nvim" },
    opts = {
      lsp = {
        override  = {},                  -- WAS the fragile monkey-patch — keep empty
        signature = { enabled = true },  -- we don't ship cmp-nvim-lsp-signature-help
      },
      presets = {
        bottom_search         = true,   -- :/ stays at the bottom (not floating)
        command_palette       = true,   -- cmdline popup + completion menu grouped
        long_message_to_split = true,
        inc_rename            = false,
        lsp_doc_border        = false,
      },
      -- Center the cmdline popup vertically (command_palette puts it near the
      -- top by default). The completion menu sits just below it.
      views = {
        cmdline_popup = {
          position = { row = "45%", col = "50%" },
          size     = { min_width = 60, width = "auto", height = "auto" },
        },
        popupmenu = {
          relative = "editor",
          position = { row = "52%", col = "50%" },
          size     = { width = 60, height = 10 },
          border   = { style = "rounded", padding = { 0, 1 } },
        },
      },
      routes = {
        { filter = { event = "msg_show", kind = "",             find = "written" }, opts = { skip = true } },
        { filter = { event = "msg_show", kind = "search_count" },                   opts = { skip = true } },
        -- Routine search misses are not worth a big red error popup — send
        -- "E486: Pattern not found" and search-wrap notices to the small mini
        -- view in the corner instead.
        { filter = { event = "msg_show", find = "E486" },        view = "mini" },
        { filter = { event = "msg_show", find = "search hit" },  view = "mini" },
      },
    },
    keys = {
      { "<leader>nm", "<cmd>NoiceDismiss<cr>", desc = "Noice: dismiss messages" },
      { "<leader>nH", "<cmd>NoiceHistory<cr>", desc = "Noice: history"          },
    },
  },
}
