-- lazygit itself comes via snacks.nvim (`<leader>gg`). This file is for
-- in-buffer git ergonomics: signs and diffview.
return function()
  require("gitsigns").setup({
    signs = {
      add          = { text = "▎" },
      change       = { text = "▎" },
      delete       = { text = "" },
      topdelete    = { text = "" },
      changedelete = { text = "▎" },
      untracked    = { text = "▎" },
    },
    current_line_blame = false, -- toggle via <leader>ghB when wanted
    on_attach = function(bufnr)
      local gs = require("gitsigns")
      local map = function(mode, lhs, rhs, desc)
        vim.keymap.set(mode, lhs, rhs, { buffer = bufnr, desc = desc })
      end
      map("n", "]h", function() gs.nav_hunk("next") end, "Git: next hunk")
      map("n", "[h", function() gs.nav_hunk("prev") end, "Git: prev hunk")
      map("n", "<leader>ghs", gs.stage_hunk,   "Git: stage hunk")
      map("n", "<leader>ghr", gs.reset_hunk,   "Git: reset hunk")
      map("n", "<leader>ghp", gs.preview_hunk, "Git: preview hunk")
      map("n", "<leader>ghb", function() gs.blame_line({ full = true }) end, "Git: blame line")
      map("n", "<leader>ghd", gs.diffthis, "Git: diff this")
      map("n", "<leader>ghB", gs.toggle_current_line_blame, "Git: toggle line blame")
    end,
  })

  require("diffview").setup({
    enhanced_diff_hl = true,
    view = { merge_tool = { layout = "diff3_mixed" } },
  })
  vim.keymap.set("n", "<leader>gd", "<cmd>DiffviewOpen<cr>",          { desc = "Diffview: open" })
  vim.keymap.set("n", "<leader>gh", "<cmd>DiffviewFileHistory %<cr>", { desc = "Diffview: file history" })
  vim.keymap.set("n", "<leader>gH", "<cmd>DiffviewFileHistory<cr>",   { desc = "Diffview: branch history" })
end
