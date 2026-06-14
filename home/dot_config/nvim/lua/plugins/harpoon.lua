return {
  {
    "ThePrimeagen/harpoon",
    branch = "harpoon2",
    dependencies = { "nvim-lua/plenary.nvim" },
    config = function()
      local harpoon = require("harpoon")
      harpoon:setup()

      -- NOTE: harpoon's docs use <C-e> for the menu, but that's nvim's built-in
      -- "scroll down one line". Menu lives at <leader>hm — keeps all harpoon
      -- keys under <leader>h* and frees <leader>m for the markdown group.
      vim.keymap.set("n", "<leader>a",  function() harpoon:list():add()        end, { desc = "Harpoon: add" })
      vim.keymap.set("n", "<leader>hm", function() harpoon.ui:toggle_quick_menu(harpoon:list()) end, { desc = "Harpoon: menu" })
      vim.keymap.set("n", "<leader>1",  function() harpoon:list():select(1)   end, { desc = "Harpoon: 1" })
      vim.keymap.set("n", "<leader>2",  function() harpoon:list():select(2)   end, { desc = "Harpoon: 2" })
      vim.keymap.set("n", "<leader>3",  function() harpoon:list():select(3)   end, { desc = "Harpoon: 3" })
      vim.keymap.set("n", "<leader>4",  function() harpoon:list():select(4)   end, { desc = "Harpoon: 4" })
      vim.keymap.set("n", "<leader>hp", function() harpoon:list():prev()      end, { desc = "Harpoon: prev" })
      vim.keymap.set("n", "<leader>hn", function() harpoon:list():next()      end, { desc = "Harpoon: next" })
    end,
  },
}
