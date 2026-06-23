-- Requires Neovim ≥ 0.11 (for vim.lsp.config / vim.lsp.enable).
-- Language servers are installed by mise (see home/dot_config/mise/config.toml),
-- not Mason — mise is the single cross-platform installer for all portable
-- tooling on this setup, so the servers are already on PATH when nvim runs.
-- nvim-lspconfig stays purely as the config library: it ships the lsp/<name>.lua
-- definitions (cmd/filetypes/root_markers) that vim.lsp.enable() reads.
return {
  {
    "neovim/nvim-lspconfig",
    event        = { "BufReadPre", "BufNewFile" },
    config = function()
      -- ── Defaults applied to every server ────────────────────────────────
      -- Completion capabilities come from blink.cmp (replaces cmp-nvim-lsp).
      local capabilities = vim.lsp.protocol.make_client_capabilities()
      local ok, blink = pcall(require, "blink.cmp")
      if ok then capabilities = blink.get_lsp_capabilities(capabilities) end

      vim.lsp.config("*", { capabilities = capabilities })

      -- ── Enable servers (replaces mason-lspconfig auto-enable) ───────────
      -- Map lspconfig config name -> the binary mise puts on PATH. Enable a
      -- server only if its binary is actually present, so a box that hasn't run
      -- `mise install` yet just skips it silently instead of erroring.
      local servers = {
        lua_ls   = "lua-language-server",
        ty       = "ty",   -- Astral ty: Python type checker (replaces pyright)
        ts_ls    = "typescript-language-server",
        bashls   = "bash-language-server",
        jsonls   = "vscode-json-language-server",
        yamlls   = "yaml-language-server",
        marksman = "marksman",
        ruff     = "ruff",   -- mise: `ruff server`
        taplo    = "taplo",  -- mise: `taplo lsp stdio`
        -- gopls / rust_analyzer: uncomment the matching mise entries first.
      }
      for name, bin in pairs(servers) do
        if vim.fn.executable(bin) == 1 then vim.lsp.enable(name) end
      end

      -- ── Per-server overrides ────────────────────────────────────────────
      vim.lsp.config("lua_ls", {
        settings = {
          Lua = {
            workspace   = { checkThirdParty = false },
            telemetry   = { enable = false },
            diagnostics = { globals = { "vim" } },
          },
        },
      })

      -- ── Keymaps on LspAttach (modern idiom; replaces on_attach) ─────────
      vim.api.nvim_create_autocmd("LspAttach", {
        callback = function(args)
          local bufnr = args.buf
          local map = function(mode, lhs, rhs, desc)
            vim.keymap.set(mode, lhs, rhs, { buffer = bufnr, desc = desc })
          end
          map("n", "gd",         vim.lsp.buf.definition,                              "Goto def")
          map("n", "gD",         vim.lsp.buf.declaration,                             "Goto decl")
          map("n", "gr",         vim.lsp.buf.references,                              "References")
          map("n", "gi",         vim.lsp.buf.implementation,                          "Implementation")
          map("n", "K",          vim.lsp.buf.hover,                                   "Hover")
          map("n", "<leader>rn", vim.lsp.buf.rename,                                  "Rename")
          map("n", "<leader>ca", vim.lsp.buf.code_action,                             "Code action")
          map("n", "[d",         vim.diagnostic.goto_prev,                            "Prev diagnostic")
          map("n", "]d",         vim.diagnostic.goto_next,                            "Next diagnostic")
          -- <leader>cf is owned by conform.nvim (see plugins/format.lua)
        end,
      })

      -- ── Diagnostic display ──────────────────────────────────────────────
      vim.diagnostic.config({
        virtual_text     = { spacing = 4, prefix = "●" },
        severity_sort    = true,
        update_in_insert = false,
        float            = { border = "rounded" },
      })
    end,
  },
}
