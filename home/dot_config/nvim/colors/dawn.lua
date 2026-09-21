-- Tuxedo "Dawn" (github.com/webstonehq/tuxedo) + its dark twin "Dusk", rendered
-- through base16-nvim. Follows vim.opt.background like melange/zenbones.
-- Slots follow how tuxedo spends colour: mostly plain fg text, terracotta + teal
-- as the main accents, amber/green/red sparingly, no blue or purple.
vim.cmd("hi clear")
vim.g.colors_name = "dawn"

local palettes = {
  light = { -- Dawn: tuxedo's own values
    bg = "#faf6f0", panel = "#f3ede2", sel = "#ede0c8", cursor = "#e8dec8", border = "#e0d6c4",
    fg = "#3d3528", subtle = "#5a4f3d", dim = "#8a7e6a",
    accent = "#a35d3a", teal = "#3a7a6a", amber = "#a3722a", green = "#5a7a3a", red = "#b8483a",
  },
  dark = { -- Dusk: same hues lifted for a warm brown background
    bg = "#1f1b16", panel = "#28231c", sel = "#3d3528", cursor = "#2e2820", border = "#3a3328",
    fg = "#e6dccb", subtle = "#c4b8a3", dim = "#8a7e6a",
    accent = "#d38c69", teal = "#72b5a4", amber = "#d6a95c", green = "#9ab573", red = "#da796c",
  },
}
local c = palettes[vim.o.background] or palettes.light

require("base16-colorscheme").setup({
  base00 = c.bg, base01 = c.panel, base02 = c.sel, base03 = c.dim,
  base04 = c.subtle, base05 = c.fg, base06 = c.fg, base07 = c.fg,
  base08 = c.red,    -- errors, diff removed, tags (variables reset to fg below)
  base09 = c.red,    -- constants, numbers, booleans    (tuxedo: priority A)
  base0A = c.amber,  -- types, search                   (tuxedo: due dates)
  base0B = c.green,  -- strings                         (tuxedo: priority C)
  base0C = c.teal,   -- escapes, specials
  base0D = c.teal,   -- functions, titles, directories  (tuxedo: +project)
  base0E = c.accent, -- keywords, operators             (tuxedo: @context, mode pill)
  base0F = c.subtle, -- punctuation
})

-- base16 paints every variable/identifier red (base08); tuxedo's body text is plain fg.
local function set(group, spec) vim.api.nvim_set_hl(0, group, spec) end
for _, g in ipairs({ "TSVariable", "Identifier", "TSNamespace" }) do set(g, { fg = c.fg }) end
set("TSVariableBuiltin", { fg = c.accent, italic = true })
set("Statement", { fg = c.accent })
-- UI chrome in tuxedo's terms: dim line numbers, cursor row, faint borders.
set("LineNr", { fg = c.dim })
set("CursorLine", { bg = c.cursor })
set("CursorLineNr", { fg = c.accent, bg = c.cursor })
set("WinSeparator", { fg = c.border })
set("VertSplit", { fg = c.border })
