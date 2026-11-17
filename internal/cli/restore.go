package cli

import (
	"fmt"
	"io"

	"github.com/morrieinmaas/athome/internal/steps"
)

const restoreFlagHelp = "restore Bitwarden, gh, the ssh key and the vault before any key is made (new laptop)"

// runInitCmd is `athome init`: it takes --restore, which selects the restore
// step list, and hands every other argument to runInit.
func runInitCmd(args []string, stdout, stderr io.Writer) int {
	rest, restore, help := stripRestore(args)
	if restore {
		old := stepList
		stepList = steps.RestoreAll
		defer func() { stepList = old }()
	}
	code := runInit(rest, stdout, stderr)
	if help && code == exitOK {
		fmt.Fprintf(stdout, "  --restore\t%s\n", restoreFlagHelp)
	}
	return code
}

// stripRestore removes --restore from args. A value flag takes the next
// argument as is, so in `--ref --restore` that is the ref. Like checkInitArgs
// it stops at -h/--help.
func stripRestore(args []string) (rest []string, restore, help bool) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help":
			return append(rest, args[i:]...), restore, true
		case valueFlags[a]:
			rest = append(rest, a)
			if i+1 < len(args) {
				i++
				rest = append(rest, args[i])
			}
		case a == "--restore":
			restore = true
		default:
			rest = append(rest, a)
		}
	}
	return rest, restore, false
}
