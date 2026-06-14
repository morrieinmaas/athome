# Spatial model — tmux + nvim coming from VSCode

This is the doc you read when you're new to a terminal-first workflow
and you keep mentally translating "open in new tab" / "split editor".

## 0. The two-layer mental model

```text
tmux session       ≈ VSCode "workspace" / project root
  └─ tmux window   ≈ VSCode "tab group" or a separate context inside the project
      └─ tmux pane ≈ VSCode "split terminal"

inside one tmux pane running nvim:

nvim buffer        ≈ VSCode "open file" (in memory; not necessarily visible)
nvim window        ≈ VSCode "editor split"  (a viewport onto a buffer)
nvim tab           ≈ VSCode "editor group" (collection of windows; rarely used)
```

The thing that confuses VSCoders most: **VSCode's "tab strip at top" maps
to nvim BUFFERS, not nvim TABS**. We use `bufferline.nvim` to show the
buffer list at the top of nvim, so it feels like VSCode's tab strip.

## 1. The seven things you do every day in VSCode

Memorize these seven and you have 90% parity.

### 1.1 Open a file

VSCode: `Cmd-P` → type name.
nvim:   `<leader>ff` → snacks picker (files) → type name → `<CR>`.

The file opens in the current window as a new **buffer**. It also
appears in the bufferline at top. You haven't lost the file that was
already there — it's still a buffer too.

### 1.2 Switch to a recently-opened file

VSCode: click a tab.
nvim: `<S-l>` (next buffer) or `<S-h>` (prev buffer). Or
`<leader>fb` to fuzzy-pick from all open buffers.

For files you visit constantly, **pin them with harpoon**:

- `<leader>a` to pin the current file (slot 1, 2, 3, or 4 — auto-assigned)
- `<leader>1` / `<leader>2` / `<leader>3` / `<leader>4` to jump
- `<leader>hm` to open the harpoon menu and reorder

### 1.3 Open file in split view (side by side)

VSCode: drag the tab, or `Cmd-\`.
nvim — leader keys (they show up in the `<leader>s` which-key group, so
you don't have to remember them):

```text
<leader>sv    vertical split │  (current buffer beside itself = VSCode "Split Editor")
<leader>sh    horizontal split ─ (above/below)
<leader>sn    new empty buffer in a vertical split
<leader>sc    close this split          <leader>so   close the OTHER splits
<leader>s=    equalize the split sizes
<leader>sz    zoom this split fullscreen (toggle — the tmux `prefix z` for nvim)
```

Or the raw vim commands when you want to open *another* file in the split:

```vim
:vsp path/to/file.py    " vertical split, open another file
:sp  path/to/file.py    " horizontal split, open another file
```

Move between the splits with **`<C-h>` `<C-j>` `<C-k>` `<C-l>`** — and
this also crosses into tmux panes via vim-tmux-navigator.

Close the current split: `<C-w>c`. Close all OTHER splits (zen mode):
`<C-w>o`. Make current split full-screen: `<C-w>_` (max height) or
`<C-w>|` (max width); equalize with `<C-w>=`.

### 1.4 Find in files (project-wide grep)

VSCode: `Cmd-Shift-F`.
nvim:  `<leader>fg` → snacks picker (grep) → type → arrow → `<CR>`.

### 1.5 Toggle a sidebar file tree

VSCode: `Cmd-B` shows/hides the explorer.
nvim:  `<leader>e` (snacks.explorer).

Inside the tree: `a` add file, `d` delete, `r` rename, `<CR>` open.
Open a file straight into a split from the tree: **`<C-v>`** (vertical),
**`<C-s>`** (horizontal), or `<C-t>` (new tab). Press `?` for the full
key list. Quit with `q`.

**Move focus between the tree and your code** with **`<C-l>`** (right,
into the editor) and **`<C-h>`** (left, back to the tree) — the same
window-nav keys that cross tmux panes. Inside the tree `<C-j>`/`<C-k>`
move the selection, so use `<C-l>`/`<C-h>` to leave it.

### 1.6 Toggle integrated terminal

VSCode: `` Ctrl-` `` opens a panel at the bottom.
nvim:  `<leader>tt` opens a floating snacks terminal. Press it again
to hide.

For something more persistent, open a new tmux pane (`prefix |` or
`prefix -`) and run there. The pane stays even after detach.

### 1.7 Command palette

VSCode: `Cmd-Shift-P`.
nvim:  `;` opens the noice cmdline popup (we swapped `;`/`:` so command
mode needs no Shift; looks like VSCode's command
palette). Or `<leader>fc` to fuzzy-search commands (snacks picker). Type `:Lazy`,
`:Mason`, `:LspInfo`, etc.

## 2. "I want to do exactly what I do in VSCode"

| VSCode keystroke | Result | nvim equivalent |
| --- | --- | --- |
| `Cmd-P` | Quick file open | `<leader>ff` |
| `Cmd-Shift-P` | Command palette | `;` (swapped with `:`; or `<leader>fc`) |
| `Cmd-Shift-F` | Find in files | `<leader>fg` |
| `Cmd-B` | Toggle file tree | `<leader>e` |
| `Ctrl-`` ` | Toggle terminal | `<leader>tt` |
| `Cmd-S` | Save | `<leader>w` (or `WW`, or `:w`). Format-on-save runs automatically. |
| `Cmd-W` | Close tab | `<leader>bd` (snacks bufdelete; window stays) |
| `Cmd-\` | Split editor right | `<leader>sv` (or `:vsp`) |
| `Cmd-K Cmd-\` | Split editor down | `<leader>sh` (or `:sp`) |
| Drag tab to split | move buffer to new split | `<leader>sv` then `<C-w>x` to swap |
| `Cmd-Click` | Goto definition | `gd` |
| `F12` | Goto definition | `gd` |
| `Cmd-.` | Quick fix / code action | `<leader>ca` |
| `F2` | Rename symbol | `<leader>rn` |
| `Cmd-/` | Toggle comment | `gcc` (line) or `gc{motion}` |
| `Cmd-D` | Add cursor at next match | not configured (ask if you want `vim-visual-multi`) |
| `Alt-Up/Down` | Move line up/down | `J`/`K` in visual mode |
| `Alt-Shift-F` | Format document | `<leader>cf` |
| `Cmd-Shift-O` | Goto symbol in file | `<leader>fs` (or `:Trouble symbols`) |
| `Cmd-T` | Goto symbol in workspace | `:Trouble lsp_document_symbols` |
| Inline hover | Hover docs | `K` (in normal mode over a symbol) |
| Bottom problems pane | Diagnostics | `<leader>xx` (trouble.nvim) |
| Source control panel | Git | `<leader>gg` (snacks lazygit) |
| Right-click → Open file in browser | GitHub link | `<leader>gb` (snacks gitbrowse) |
| Markdown preview | Render `.md` | `<leader>mr` in-buffer · `<leader>mg` glow split · `<leader>mp` browser |

## 3. Where tmux comes in

tmux gives you **persistence** and **multiple concurrent terminals** —
things VSCode handles via reopening workspaces and tabbed terminals.

### Use tmux when you want to

| Scenario | tmux feature |
| --- | --- |
| Open a project so you can `cd` back later, after reboot | Session via `ts myproject` |
| Have code + service logs + git all in view | Multiple panes in one window |
| Have several "modes" within one project (code, db, debug) | Multiple windows (`prefix c`) |
| Detach and reattach (e.g. SSH from cafe) | `prefix d` to detach, `ta` to reattach |
| Switch between two projects fast | `prefix C-j` (fzf session popup) |

### Don't use tmux for

- A single quick command (`<leader>tt` in nvim is faster)
- A throwaway shell (just open a new ghostty window)

## 4. The three-pane VSCode-like layout

Open a project workspace exactly like VSCode shows: file tree on the
left, code in the middle, terminal at the bottom.

```bash
# from anywhere
ts myproject                    # creates / attaches tmux session "myproject"
cd ~/personal/myproject
nvim .                          # nvim starts; oil opens cwd as a buffer
# In nvim:
<leader>e                       # toggle snacks.explorer sidebar (left)
<leader>tt                      # toggle snacks floating terminal (bottom-ish)
```

For a more persistent terminal, do it tmux-side instead of nvim-side:

- `prefix -` opens a horizontal pane below in the same window
- Run your dev server / logs there
- `<C-k>` to move focus back into nvim above (vim-tmux-navigator crosses
  the pane boundary)

## 5. Working with multiple projects simultaneously

VSCode's solution is "multiple Code windows" or "workspace folders".
tmux's is sessions:

```bash
# In project A, working...
prefix d                        # detach (session stays alive)

# Start work on project B
ts project-b
cd ~/personal/project-b
nvim .

# Switch back to A
prefix C-j                       # fzf popup of all sessions, pick A
# (Or: prefix t to jump to last session)
```

Continuum auto-saves every 5 min and on reboot resurrects everything —
all your windows, panes, scroll history, the nvim file you had open.
Sessions are durable in a way VSCode workspaces aren't.

### Closing nvim and coming back to the same buffers

Two ways, depending on what you want:

- **Leave nvim running (zero effort).** Don't `:q` — `prefix d` to detach
  the tmux session. nvim keeps running with every buffer/split open;
  reattach (or let continuum restore after a reboot) and it's exactly as
  you left it. This is the tmux-native answer.
- **Actually quit nvim, restore later.** `persistence.nvim` auto-saves a
  session per directory on exit **and auto-restores it**: just `:qa`, then
  later run `nvim` (no file args) in that same dir and all your
  buffers/windows/folds come straight back — no keypress. (Manual controls:
  `<leader>Sr` restore this dir, `<leader>Sl` last session, `<leader>Sd` don't
  save this run; the start dashboard also has `s → Restore Session`.)

## 6. Cheat strip (you'll memorize these in a week)

```text
Tmux  -----------------------------------------------------------
  prefix C-j     fzf session switch
  prefix K       fzf workspace roots → new session
  prefix s       sessionx (richer session picker w/ previews)
  prefix t       jump to last session
  prefix c       new window (cwd)
  prefix |       vertical pane split (cwd)
  prefix -       horizontal pane split (cwd)
  prefix d       detach (session keeps running)
  prefix r       reload tmux.conf
  prefix [       enter copy mode (vi keys; y to copy)
  prefix 1..9    jump to window number

Cross-boundary (works in BOTH tmux + nvim):
  C-h C-j C-k C-l    move pane left/down/up/right

Nvim  -----------------------------------------------------------
  <leader>w / WW         save
  <leader>ff             find file
  <leader>fg             grep
  <leader>fb             pick buffer
  <leader>fr             recent files
  <leader>fc             commands
  <leader>e              file tree sidebar (snacks.explorer)
  <leader>tt             floating terminal
  <leader>gg             lazygit
  <leader>a              harpoon pin
  <leader>hm             harpoon menu
  <leader>1..4           harpoon jump
  <S-h> / <S-l>          prev / next buffer (capital H / L — NOT leader)
  <leader>bj             jump to a buffer by letter (BufferLinePick)
  <leader>bn             new buffer
  <leader>bd             close buffer (keeps split)
  <leader>cf             format
  <leader>ca             code action
  <leader>rn             rename symbol
  <leader>xx             diagnostics panel (trouble)
  gd                     goto definition
  gr                     goto references
  K                      hover docs
  gcc / gc{motion}       toggle comment
  <leader>sv / sh        split vertical / horizontal (which-key group)
  <leader>sz             zoom split fullscreen (toggle — like tmux prefix z)
  :vsp / :sp             split + open another file (right / down)
  <C-w>c                 close split
  <C-w>o                 close OTHER splits (zen current)
  <C-w>=                 equalize splits
  <leader>mr/mg/mp       markdown: in-buffer / glow split / browser
  jj                     escape from insert
  ;                      cmdline (swapped with : — no Shift; : repeats f/F/t/T)
  <C-h/j/k/l>            move focus between splits AND the file tree
  <leader>Sr / Sl        restore session: this dir / last (persistence.nvim)

Daily startup -------------------------------------------------------
  ghostty            (opens to zsh)
  ts myproject       (new or attach tmux session)
  cd ~/personal/...  (your project)
  nvim .             (start editing)
  <leader>e          (sidebar on)
```

## 7. What this setup IS NOT

- **Not a 1:1 VSCode clone.** Some VSCode behaviors are impossible or
  awkward (drag-and-drop tab reordering, GUI settings panel). You get
  faster + scriptable + persistent in exchange.
- **Not Lazyvim / NvChad / kickstart.** This config is hand-written
  and deliberately minimal. You can read every plugin file in a coffee.
- **Not multi-cursor heaven.** If `Cmd-D` is non-negotiable, ask and
  I'll add `vim-visual-multi`.
- **Not tab-heaven.** nvim's `:tabnew` exists but you'll almost never
  use it — buffers + bufferline + harpoon cover all the cases.
