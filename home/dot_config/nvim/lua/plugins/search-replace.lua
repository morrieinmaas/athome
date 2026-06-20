-- grug-far.nvim — project-wide find & replace in a dedicated buffer: ripgrep
-- search + live preview, edit the replacement inline, apply across all matches.
-- Lives under the <leader>r "refactor / replace" group (next to LSP rename
-- <leader>rn). Plain search lives in snacks.picker (<leader>/ etc.); this is the
-- replace workflow that nvim lacked.
return {
  {
    "MagicDuck/grug-far.nvim",
    cmd = "GrugFar",
    opts = { headerMaxWidth = 80 },
    keys = {
      {
        "<leader>rr",
        function() require("grug-far").open() end,
        desc = "Replace: project-wide (grug-far)",
      },
      {
        "<leader>rw",
        function() require("grug-far").open({ prefills = { search = vim.fn.expand("<cword>") } }) end,
        desc = "Replace: word under cursor",
      },
      {
        "<leader>rf",
        function() require("grug-far").open({ prefills = { paths = vim.fn.expand("%") } }) end,
        desc = "Replace: in current file",
      },
      {
        "<leader>r",
        function() require("grug-far").with_visual_selection() end,
        mode = "x",
        desc = "Replace: visual selection",
      },
    },
  },

  -- Lightweight alternative to grug-far using the tools already loaded: a
  -- ripgrep live-grep via snacks.picker, then a quickfix-wide substitute. This
  -- is a `keys` fragment merged into the snacks.nvim spec in snacks.lua (Lazy
  -- merges same-source fragments), so it adds NO new plugin.
  --   <leader>rs → ripgrep search; inside the picker press <c-q> to send the
  --                matches you want to the quickfix list.
  --   <leader>rq → substitute across every quickfix entry (:cfdo s/…/…/g).
  {
    "folke/snacks.nvim",
    optional = true,
    keys = {
      { "<leader>rs", function() Snacks.picker.grep() end, desc = "Replace: ripgrep search (→ <c-q> to quickfix)" },
      {
        "<leader>rq",
        function()
          local body = vim.fn.input("Quickfix replace  s/{old}/{new}/g  → enter  old/new : ")
          if body == "" then return end
          -- /e: don't error in files with no match · update: write each changed buffer
          vim.cmd("silent! cfdo s/" .. body .. "/ge | update")
        end,
        desc = "Replace: across quickfix list (:cfdo)",
      },
    },
  },
}
