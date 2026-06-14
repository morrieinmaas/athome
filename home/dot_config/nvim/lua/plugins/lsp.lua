-- Requires Neovim ≥ 0.11 (for vim.lsp.config / vim.lsp.enable).
-- Brew/mise both install latest stable; chezmoi-managed package list
-- pulls neovim from common, which is ≥0.11 on current Homebrew + Arch.
return {
  -- Mason owns binary downloads of language servers + linters + formatters.
  {
    "mason-org/mason.nvim",        -- moved from williamboman/* to mason-org/* in 2025
    build = ":MasonUpdate",
    opts  = {},
  },

  -- Bridges Mason package names <-> lspconfig server names; auto-enables
  -- installed servers via vim.lsp.enable().
  {
    "mason-org/mason-lspconfig.nvim",
    dependencies = {
      "mason-org/mason.nvim",
      "neovim/nvim-lspconfig",
    },
    opts = {
      -- Lean core that installs cleanly without a per-language toolchain on a
      -- fresh box. Heavier / toolchain-tied servers (gopls needs Go, plus
      -- rust_analyzer, clangd, etc.) are installed on demand: `:MasonInstall
      -- gopls rust_analyzer` when you actually open that project.
      -- NOTE: ruff + taplo are intentionally NOT here — they're CLI tools owned
      -- by mise (ruff ships `ruff server`, taplo `taplo lsp stdio`), enabled
      -- straight from PATH in the nvim-lspconfig config below. Letting mason
      -- also install them duplicated the binary and failed on `ruff`.
      ensure_installed = {
        "lua_ls", "pyright", "ts_ls",
        "bashls", "jsonls", "yamlls", "marksman",
      },
      automatic_enable = true,    -- v2 default; explicit for clarity
    },
  },

  -- Per-server config + LspAttach keymaps. Pure setup, no per-server
  -- lspconfig.setup() loop — mason-lspconfig auto-enables via the new API.
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

      -- ruff + taplo come from mise (PATH), not mason. mason-lspconfig only
      -- auto-enables mason-installed servers, so enable these two ourselves —
      -- but only if the binary is actually on PATH (so a box that hasn't run
      -- `mise install` yet just skips them silently instead of erroring).
      for _, server in ipairs({ "ruff", "taplo" }) do
        if vim.fn.executable(server) == 1 then vim.lsp.enable(server) end
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
