-- blink.cmp — Rust-fuzzy completion. Replaces the old nvim-cmp stack
-- (nvim-cmp + cmp-nvim-lsp/buffer/path + LuaSnip + cmp_luasnip = 5 plugins).
-- pack.lua pins it to the latest stable tag (version = range("*")) so the
-- prebuilt fuzzy binary is downloaded — no Rust toolchain build on a fresh box.
-- friendly-snippets (the LuaSnip snippet corpus) is added alongside in pack.lua.
return function()
  require("blink.cmp").setup({
    -- Keep the old muscle memory: Tab/S-Tab select (and jump snippets),
    -- <CR> accepts, <C-space> toggles, <C-b>/<C-f> scroll docs.
    keymap = {
      preset = "default",
      ["<Tab>"]   = { "select_next", "snippet_forward", "fallback" },
      ["<S-Tab>"] = { "select_prev", "snippet_backward", "fallback" },
      ["<CR>"]    = { "accept", "fallback" },
    },
    appearance = { nerd_font_variant = "mono" },
    sources = { default = { "lsp", "path", "snippets", "buffer" } },
    completion = {
      documentation = { auto_show = true, auto_show_delay_ms = 200 },
      menu = { border = "rounded" },
    },
    signature = { enabled = true },
    fuzzy = { implementation = "prefer_rust_with_warning" },
  })
end
