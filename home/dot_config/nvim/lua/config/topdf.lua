-- :ToPdf — render the current markdown buffer to a timestamped PDF beside it.
--
-- Deliberately NOT in plugins/markdown.lua. Everything in there is set up on
-- VimEnter through vim.pack, so it depends on the plugin phase having run;
-- :ToPdf depends on nothing but ~/.local/bin/md2pdf, and a feature with no
-- plugin dependency should not inherit a plugin's failure modes. Required
-- eagerly from init.lua, so it exists from the first frame in every buffer.
--
-- The look comes from ~/.config/md2pdf/github.typ (pandoc + typst). See
-- `md2pdf --help`.
local M = {}

local function to_pdf(opts)
  local buf = vim.api.nvim_get_current_buf()
  local file = vim.api.nvim_buf_get_name(buf)

  if file == "" then
    return vim.notify("ToPdf: this buffer has no file on disk", vim.log.levels.ERROR)
  end
  if vim.bo[buf].filetype ~= "markdown" then
    return vim.notify("ToPdf: not a markdown buffer (filetype is '" .. vim.bo[buf].filetype .. "')",
      vim.log.levels.ERROR)
  end
  if vim.fn.executable("md2pdf") == 0 then
    return vim.notify("ToPdf: md2pdf not on PATH. It ships with athome in ~/.local/bin.",
      vim.log.levels.ERROR)
  end

  -- Write first. md2pdf reads from disk, so an unsaved buffer would quietly
  -- export the previous version, which is the kind of mistake you only notice
  -- once the PDF is already sent to someone.
  if vim.bo[buf].modified then
    vim.cmd("write")
  end

  local cmd = { "md2pdf", file }
  if opts and opts.bang then
    table.insert(cmd, "--open")
  end

  vim.notify("ToPdf: rendering…", vim.log.levels.INFO)
  vim.system(cmd, { text = true }, function(res)
    vim.schedule(function()
      if res.code == 0 then
        vim.notify("ToPdf → " .. vim.trim(res.stdout or ""), vim.log.levels.INFO)
      else
        local msg = vim.trim((res.stderr or "") .. (res.stdout or ""))
        vim.notify("ToPdf failed: " .. (msg ~= "" and msg or "exit " .. res.code),
          vim.log.levels.ERROR)
      end
    end)
  end)
end

function M.setup()
  vim.api.nvim_create_user_command("ToPdf", to_pdf, {
    bang = true,
    desc = "Markdown → timestamped PDF beside the file (:ToPdf! also opens it)",
  })

  vim.api.nvim_create_autocmd("FileType", {
    pattern = "markdown",
    group = vim.api.nvim_create_augroup("athome_topdf", { clear = true }),
    callback = function(ev)
      vim.keymap.set("n", "<leader>mP", "<cmd>ToPdf<cr>",
        { buffer = ev.buf, desc = "Markdown: export to PDF" })
    end,
  })
end

return M
