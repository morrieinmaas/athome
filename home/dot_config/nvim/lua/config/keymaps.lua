local map = vim.keymap.set

-- save / quit
map("n", "<leader>w", "<cmd>write<cr>",  { desc = "Save" })
map("n", "<leader>q", "<cmd>quit<cr>",   { desc = "Quit" })

-- Splits / windows (code-editor style). Surfaced in the <leader> which-key
-- popup under "+split / window" (group label in plugins/ui.lua). `<leader>sv`
-- is the VSCode "Split Editor" equivalent — the current buffer in a vertical
-- split. (Move between splits with <C-h/j/k/l> — vim-tmux-navigator.)
map("n", "<leader>sv", "<C-w>v",        { desc = "Split: vertical │ (current buffer)" })
map("n", "<leader>sh", "<C-w>s",        { desc = "Split: horizontal ─ (current buffer)" })
map("n", "<leader>sn", "<cmd>vnew<cr>", { desc = "Split: new empty buffer │" })
map("n", "<leader>sc", "<C-w>c",        { desc = "Split: close this one" })
map("n", "<leader>so", "<C-w>o",        { desc = "Split: only (close the others)" })
map("n", "<leader>s=", "<C-w>=",        { desc = "Split: equalize sizes" })
-- Zoom a split to fullscreen, like tmux `prefix z`. Opens the current window in
-- its own tab (the other splits vanish but the layout is kept intact) and
-- toggles back. bufferline (buffers mode) is unaffected by the extra tab.
map("n", "<leader>sz", function()
  if vim.t.zoomed then
    vim.cmd("tabclose")
  else
    vim.cmd("tab split")
    vim.t.zoomed = true
  end
end, { desc = "Split: zoom fullscreen (toggle, like tmux prefix z)" })

-- NOTE: window/pane navigation (<C-h/j/k/l>) is owned by vim-tmux-navigator
-- (plugins/navigation.lua) and seamlessly crosses tmux↔nvim panes.
-- NOTE: buffer delete (<leader>bd) is owned by snacks.bufdelete (plugins/snacks.lua).

-- Buffer navigation — defined here (native :bnext/:bprevious) rather than as
-- bufferline lazy-keys, so they're ALWAYS active from startup with no
-- plugin-load dependency. bufferline just renders the tab strip; its highlight
-- follows the current buffer. Need 2+ buffers open to see them move.
map("n", "<S-l>",      "<cmd>bnext<cr>",     { desc = "Buffer: next" })
map("n", "<S-h>",      "<cmd>bprevious<cr>", { desc = "Buffer: prev" })
map("n", "<leader>bn", "<cmd>enew<cr>",      { desc = "Buffer: new"  })

-- keep cursor centered
map("n", "n", "nzzzv")
map("n", "N", "Nzzzv")
map("n", "<C-d>", "<C-d>zz")
map("n", "<C-u>", "<C-u>zz")

-- better indent
map("v", "<", "<gv")
map("v", ">", ">gv")

-- move selection
map("v", "J", ":m '>+1<CR>gv=gv")
map("v", "K", ":m '<-2<CR>gv=gv")

-- terminal
map("t", "<Esc><Esc>", "<C-\\><C-n>")

-- muscle memory
map("i", "jj", "<Esc>", { desc = "jj → Esc (no hand off home row)" })
-- Swap ; and : so command mode starts with ; (no Shift). : now does the old ;
-- (repeat last f/F/t/T find). Applies in normal + visual.
map({ "n", "x" }, ";", ":", { desc = "Cmdline (was :)" })
map({ "n", "x" }, ":", ";", { desc = "Repeat f/F/t/T find (was ;)" })
map("n", "WW", "<cmd>w<cr>",   { desc = "Save"          })
map("n", "QQ", "<cmd>q<cr>",   { desc = "Quit"          })
map("n", "WQ", "<cmd>wq<cr>",  { desc = "Save and quit" })
map("n", "WS", "<cmd>w | source %<cr>", { desc = "Save & source current file" })
