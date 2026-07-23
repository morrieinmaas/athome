-- grug-far.nvim — project-wide find & replace in a dedicated buffer: ripgrep
-- search + live preview, edit the replacement inline, apply across all matches.
-- Lives under the <leader>r "refactor / replace" group (next to LSP rename
-- <leader>rn). Plain search lives in snacks.picker (<leader>/ etc.); this is the
-- replace workflow that nvim lacked.
--
-- The <leader>rs / <leader>rq keymaps reuse the already-loaded snacks.picker +
-- quickfix — no extra plugin (they were a merged snacks fragment under lazy).
return function()
  require("grug-far").setup({ headerMaxWidth = 80 })

  vim.keymap.set("n", "<leader>rr", function() require("grug-far").open() end, { desc = "Replace: project-wide (grug-far)" })
  vim.keymap.set("n", "<leader>rw", function()
    require("grug-far").open({ prefills = { search = vim.fn.expand("<cword>") } })
  end, { desc = "Replace: word under cursor" })
  vim.keymap.set("n", "<leader>rf", function()
    require("grug-far").open({ prefills = { paths = vim.fn.expand("%") } })
  end, { desc = "Replace: in current file" })
  vim.keymap.set("x", "<leader>r", function() require("grug-far").with_visual_selection() end, { desc = "Replace: visual selection" })

  -- Lightweight alternative using tools already loaded: a ripgrep live-grep via
  -- snacks.picker, then a quickfix-wide substitute.
  --   <leader>rs → ripgrep search; inside the picker press <c-q> to send matches
  --                to the quickfix list.
  --   <leader>rq → substitute across every quickfix entry (:cfdo s/…/…/g).
  vim.keymap.set("n", "<leader>rs", function() Snacks.picker.grep() end, { desc = "Replace: ripgrep search (→ <c-q> to quickfix)" })
  vim.keymap.set("n", "<leader>rq", function()
    local body = vim.fn.input("Quickfix replace  s/{old}/{new}/g  → enter  old/new : ")
    if body == "" then return end
    -- /e: don't error in files with no match · update: write each changed buffer
    vim.cmd("silent! cfdo s/" .. body .. "/ge | update")
  end, { desc = "Replace: across quickfix list (:cfdo)" })
end
