-- Cross-pane navigation: same C-h/j/k/l moves between tmux panes AND nvim splits.
-- Pair with `christoomey/vim-tmux-navigator` on the tmux side (already in
-- ~/.config/tmux/tmux.conf as `set -g @plugin 'christoomey/vim-tmux-navigator'`).
-- Plus the VSCode-like buffer/tab strip (bufferline).
return function()
  -- vim-tmux-navigator ships the :TmuxNavigate* commands; just bind them.
  vim.keymap.set("n", "<C-h>", "<cmd>TmuxNavigateLeft<cr>",  { desc = "← pane (tmux/nvim)" })
  vim.keymap.set("n", "<C-j>", "<cmd>TmuxNavigateDown<cr>",  { desc = "↓ pane (tmux/nvim)" })
  vim.keymap.set("n", "<C-k>", "<cmd>TmuxNavigateUp<cr>",    { desc = "↑ pane (tmux/nvim)" })
  vim.keymap.set("n", "<C-l>", "<cmd>TmuxNavigateRight<cr>", { desc = "→ pane (tmux/nvim)" })

  require("bufferline").setup({
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
      -- bufferline only does angled tabs via its 4 NAMED styles (slant / slope /
      -- padded_*), all parallelograms — a custom {l,r} table is read as
      -- {focused,unfocused} and falls back to a straight bar. A symmetric
      -- trapezoid isn't possible.
      separator_style         = "slant",
    },
  })

  -- <S-h>/<S-l>/<leader>bn live in config/keymaps.lua (native, always loaded).
  -- Only the bufferline-specific actions stay here.
  vim.keymap.set("n", "<leader>bj", "<cmd>BufferLinePick<cr>",      { desc = "Buffer: jump (pick by letter)" })
  vim.keymap.set("n", "<leader>bp", "<cmd>BufferLineTogglePin<cr>", { desc = "Buffer: pin" })
  vim.keymap.set("n", "<leader>bP", "<cmd>BufferLineGroupClose ungrouped<cr>", { desc = "Buffer: close unpinned" })
  vim.keymap.set("n", "<leader>bo", "<cmd>BufferLineCloseOthers<cr>", { desc = "Buffer: close others" })
  vim.keymap.set("n", "<leader>br", "<cmd>BufferLineCloseRight<cr>",  { desc = "Buffer: close right" })
  vim.keymap.set("n", "<leader>bl", "<cmd>BufferLineCloseLeft<cr>",   { desc = "Buffer: close left" })
end
