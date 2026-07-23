-- conform.nvim — format on save, falling back to LSP if no formatter
-- is configured for the filetype. Owns the <leader>cf binding (same key as the
-- old lsp.lua one, for muscle memory) and <leader>uf to toggle format-on-save.
return function()
  require("conform").setup({
    formatters_by_ft = {
      lua             = { "stylua" },
      python          = { "ruff_format", "ruff_organize_imports" },
      sh              = { "shfmt" },
      bash            = { "shfmt" },
      zsh             = { "shfmt" },
      go              = { "goimports", "gofmt" },
      rust            = { "rustfmt", lsp_format = "fallback" },
      javascript      = { "prettierd" },
      javascriptreact = { "prettierd" },
      typescript      = { "prettierd" },
      typescriptreact = { "prettierd" },
      json            = { "prettierd" },
      jsonc           = { "prettierd" },
      yaml            = { "prettierd" },
      markdown        = { "prettierd" },
      css             = { "prettierd" },
      scss            = { "prettierd" },
      html            = { "prettierd" },
      toml            = { "taplo" },
      ["_"]           = { "trim_whitespace", "trim_newlines" },
    },
    default_format_opts = { lsp_format = "fallback" },
    format_on_save = function(bufnr)
      if vim.g.disable_autoformat or vim.b[bufnr].disable_autoformat then
        return
      end
      return { timeout_ms = 500, lsp_format = "fallback" }
    end,
    formatters = {
      shfmt = {
        prepend_args = { "-i", "2", "-ci", "-bn" }, -- match our pre-commit config
      },
    },
  })

  vim.keymap.set(
    { "n", "v" },
    "<leader>cf",
    function() require("conform").format({ async = true, lsp_format = "fallback" }) end,
    { desc = "Format buffer / selection" }
  )
  vim.keymap.set("n", "<leader>uf", function()
    if vim.b.disable_autoformat or vim.g.disable_autoformat then
      vim.b.disable_autoformat = false
      vim.g.disable_autoformat = false
      vim.notify("format-on-save: ON", vim.log.levels.INFO)
    else
      vim.b.disable_autoformat = true
      vim.notify("format-on-save: OFF (buffer)", vim.log.levels.WARN)
    end
  end, { desc = "Toggle format-on-save" })

  -- Warn on VeryLazy if any configured formatter isn't on PATH. packages.yaml /
  -- mise install them all; this catches an incomplete `chezmoi apply` / mise
  -- install. (VeryLazy is emitted by config/pack.lua after startup.)
  vim.api.nvim_create_autocmd("User", {
    pattern  = "VeryLazy",
    once     = true,
    callback = function()
      local check = { "stylua", "shfmt", "prettierd", "ruff", "rustfmt", "gofmt", "goimports", "taplo" }
      local missing = {}
      for _, fmt in ipairs(check) do
        if vim.fn.executable(fmt) == 0 then table.insert(missing, fmt) end
      end
      if #missing > 0 then
        vim.schedule(function()
          vim.notify(
            "conform: missing formatters on PATH: " .. table.concat(missing, ", ")
              .. "\nRun `chezmoi apply` / `mise install` or inspect :ConformInfo",
            vim.log.levels.WARN,
            { title = "conform.nvim" }
          )
        end)
      end
    end,
  })
end
