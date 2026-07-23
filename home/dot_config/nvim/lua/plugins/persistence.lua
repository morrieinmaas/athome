-- Session persistence — quit nvim and come back to the same buffers/windows,
-- scoped to the working directory (a tmux-session feel, but for nvim).
-- Auto-saves on exit AND auto-restores when you open nvim with no file args in
-- a directory that has a saved session — so `:qa` then `nvim` later just brings
-- everything back, no keypress needed.
--
-- Set up EAGERLY (config/pack.lua) so the VimEnter auto-restore autocmd below is
-- registered before VimEnter fires.
--
-- Tip: with tmux you can also `prefix d` to DETACH and leave nvim running
-- untouched — continuum even restores it after a reboot. Use this when you
-- actually close nvim itself.
return function()
  require("persistence").setup({})

  -- Auto-restore on a no-args launch (with file args or piped stdin: skip, so
  -- `nvim file.txt` / `nvim .` behave normally). Snacks dashboard only shows
  -- when there's no session to restore.
  vim.api.nvim_create_autocmd("VimEnter", {
    nested = true,
    callback = function()
      if vim.fn.argc() ~= 0 then return end
      if vim.g.persistence_restored then return end
      vim.g.persistence_restored = true
      require("persistence").load()
    end,
  })

  vim.keymap.set("n", "<leader>Sr", function() require("persistence").load() end, { desc = "Session: restore (this dir)" })
  vim.keymap.set("n", "<leader>Sl", function() require("persistence").load({ last = true }) end, { desc = "Session: restore last used" })
  vim.keymap.set("n", "<leader>Sd", function() require("persistence").stop() end, { desc = "Session: stop saving (this run)" })
end
