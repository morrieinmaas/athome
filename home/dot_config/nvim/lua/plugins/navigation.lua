-- Cross-pane navigation: same C-h/j/k/l moves between tmux panes AND nvim splits.
-- Pair with `christoomey/vim-tmux-navigator` on the tmux side (already in
-- ~/.config/tmux/tmux.conf as `set -g @plugin 'christoomey/vim-tmux-navigator'`).
return {
  {
    "christoomey/vim-tmux-navigator",
    lazy = false,
    cmd = {
      "TmuxNavigateLeft", "TmuxNavigateDown",
      "TmuxNavigateUp",   "TmuxNavigateRight", "TmuxNavigatePrevious",
    },
    keys = {
      { "<C-h>", "<cmd>TmuxNavigateLeft<cr>",  desc = "← pane (tmux/nvim)" },
      { "<C-j>", "<cmd>TmuxNavigateDown<cr>",  desc = "↓ pane (tmux/nvim)" },
      { "<C-k>", "<cmd>TmuxNavigateUp<cr>",    desc = "↑ pane (tmux/nvim)" },
      { "<C-l>", "<cmd>TmuxNavigateRight<cr>", desc = "→ pane (tmux/nvim)" },
    },
  },

  -- VSCode-like buffer/tab strip at the top
  {
    "akinsho/bufferline.nvim",
    version = "*",
    event   = "VeryLazy",
    dependencies = { "nvim-tree/nvim-web-devicons" },
    keys = {
      -- <S-h>/<S-l>/<leader>bn now live in config/keymaps.lua (native, always
      -- loaded). Only the bufferline-specific actions stay here.
      { "<leader>bj", "<cmd>BufferLinePick<cr>",       desc = "Buffer: jump (pick by letter)" },
      { "<leader>bp", "<cmd>BufferLineTogglePin<cr>",  desc = "Buffer: pin" },
      { "<leader>bP", "<cmd>BufferLineGroupClose ungrouped<cr>", desc = "Buffer: close unpinned" },
      { "<leader>bo", "<cmd>BufferLineCloseOthers<cr>", desc = "Buffer: close others" },
      { "<leader>br", "<cmd>BufferLineCloseRight<cr>",  desc = "Buffer: close right" },
      { "<leader>bl", "<cmd>BufferLineCloseLeft<cr>",   desc = "Buffer: close left" },
    },
    opts = {
      options = {
        mode = "buffers",          -- show buffers (VSCode-tab feel)
        diagnostics = "nvim_lsp",
        always_show_bufferline = true,
        offsets = {
          { filetype = "snacks_picker_list", text = "Explorer", text_align = "center" },
          { filetype = "trouble",  text = "Problems", text_align = "center" },
        },
        show_buffer_close_icons = true,
        show_close_icon         = false,
        -- bufferline only does angled tabs via its 4 NAMED styles (slant /
        -- slope / padded_*), all parallelograms — a custom {l,r} table is read
        -- as {focused,unfocused} and falls back to a straight bar (ui.lua
        -- get_separator + is_slant). A symmetric trapezoid isn't possible.
        separator_style         = "slant",
      },
    },
  },
}
