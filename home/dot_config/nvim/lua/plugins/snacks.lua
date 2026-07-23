-- folke/snacks.nvim — collection of small QoL modules. Replaces
-- nvim-notify (via snacks.notifier) and lazygit.nvim (via snacks.lazygit).
-- Other modules opt-in below. Set up EAGERLY (dashboard on no-arg launch,
-- quickfile/statuscolumn want to be early).
return function()
  require("snacks").setup({
    bigfile   = { enabled = true },                       -- skip heavy features on >1.5MB files
    bufdelete = { enabled = true },                       -- :Bdelete that preserves windows
    explorer  = { enabled = true, replace_netrw = true }, -- file tree sidebar
    picker    = { enabled = true },                       -- backs explorer + dashboard pickers
    dashboard = {
      enabled = true,
      preset = {
        keys = {
          { icon = " ", key = "f", desc = "Find file",       action = ":lua Snacks.dashboard.pick('files')" },
          { icon = " ", key = "n", desc = "New file",        action = ":ene | startinsert" },
          { icon = " ", key = "g", desc = "Find text",       action = ":lua Snacks.dashboard.pick('live_grep')" },
          { icon = " ", key = "r", desc = "Recent",          action = ":lua Snacks.dashboard.pick('oldfiles')" },
          { icon = " ", key = "s", desc = "Restore Session", action = function() require("persistence").load() end },
          { icon = " ", key = "c", desc = "Config",          action = ":e ~/.config/nvim/init.lua" },
          { icon = "󰒲 ", key = "u", desc = "Update plugins",   action = ":lua vim.pack.update()" }, -- was :Lazy
          { icon = "󰒲 ", key = "L", desc = "lazygit",         action = ":lua Snacks.lazygit()" },
          { icon = " ", key = "q", desc = "Quit",            action = ":qa" },
        },
      },
      -- snacks' default sections include { section = "startup" }, which hard-
      -- requires `lazy.stats` — gone under vim.pack, so the dashboard errors on
      -- open (e.g. `nvim .` / no-arg launch). Replace that footer with a
      -- vim.pack plugin count; keep the default header + keys sections.
      sections = {
        { section = "header" },
        { section = "keys", gap = 1, padding = 1 },
        function()
          return {
            align = "center",
            padding = 1,
            text = {
              { "⚡ ", hl = "footer" },
              { tostring(#vim.pack.get()), hl = "special" },
              { " plugins · vim.pack", hl = "footer" },
            },
          }
        end,
      },
    },
    gitbrowse = { enabled = true },            -- :lua Snacks.gitbrowse() opens current line in GH
    indent    = { enabled = true, animate = { enabled = false } },
    input     = { enabled = true },            -- pretty vim.ui.input
    lazygit   = { enabled = true, theme = { activeBorderColor = { fg = "MatchParen", bold = true } } },
    notifier  = {                              -- drop-in for nvim-notify
      enabled  = true,
      timeout  = 4000,
      top_down = false,
      style    = "compact",
    },
    quickfile    = { enabled = true },         -- render file before plugins load (~30ms)
    scope        = { enabled = true },         -- detect scope under cursor (used by ai_indent etc.)
    scroll       = { enabled = true, animate = { duration = { step = 15 } } },
    statuscolumn = { enabled = true },         -- richer gutter (line nrs + signs + folds)
    terminal     = { enabled = true },         -- floating term via Snacks.terminal.toggle()
    words        = { enabled = true },         -- jump between LSP doc highlights with ]] / [[
  })

  local map = vim.keymap.set
  map("n", "<leader>e",  function() Snacks.explorer() end,           { desc = "Explorer (file tree)" })
  -- Pickers (replaces telescope; snacks.picker is enabled above)
  map("n", "<leader>ff", function() Snacks.picker.files() end,       { desc = "Find files" })
  map("n", "<leader>fg", function() Snacks.picker.grep() end,        { desc = "Live grep" })
  map("n", "<leader>fb", function() Snacks.picker.buffers() end,     { desc = "Buffers" })
  map("n", "<leader>fh", function() Snacks.picker.help() end,        { desc = "Help tags" })
  map("n", "<leader>fr", function() Snacks.picker.recent() end,      { desc = "Recent files" })
  map("n", "<leader>fc", function() Snacks.picker.commands() end,    { desc = "Commands" })
  map("n", "<leader>fs", function() Snacks.picker.lsp_symbols() end, { desc = "Document symbols" })
  map("n", "<leader>/",  function() Snacks.picker.lines() end,       { desc = "Search in buffer" })
  map("n", "<leader>gg", function() Snacks.lazygit() end,            { desc = "Lazygit (snacks)" })
  map("n", "<leader>gb", function() Snacks.gitbrowse() end,          { desc = "Open in browser" })
  map("n", "<leader>gl", function() Snacks.lazygit.log() end,        { desc = "Lazygit: log" })
  map("n", "<leader>tt", function() Snacks.terminal.toggle() end,    { desc = "Terminal (toggle)" })
  map("n", "<leader>bd", function() Snacks.bufdelete() end,          { desc = "Buffer: delete" })
  map("n", "<leader>nd", function() Snacks.notifier.hide() end,      { desc = "Notifications: dismiss" })
  map("n", "<leader>nh", function() Snacks.notifier.show_history() end, { desc = "Notifications: history" })
  map("n", "]]",         function() Snacks.words.jump(vim.v.count1) end,  { desc = "Next reference" })
  map("n", "[[",         function() Snacks.words.jump(-vim.v.count1) end, { desc = "Prev reference" })

  -- Toggle helpers under <leader>u, wired on VeryLazy (emitted by pack.lua).
  vim.api.nvim_create_autocmd("User", {
    pattern  = "VeryLazy",
    callback = function()
      Snacks.toggle.option("spell",          { name = "Spelling" }):map("<leader>us")
      Snacks.toggle.option("wrap",           { name = "Wrap" }):map("<leader>uw")
      Snacks.toggle.option("relativenumber", { name = "Relative #" }):map("<leader>ul")
      Snacks.toggle.diagnostics():map("<leader>ud")
      Snacks.toggle.line_number():map("<leader>uN")
      Snacks.toggle.treesitter():map("<leader>uT")
      Snacks.toggle.inlay_hints():map("<leader>uh")
      Snacks.toggle.indent():map("<leader>ug")
    end,
  })
end
