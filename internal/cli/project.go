package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/morrieinmaas/athome/internal/machines"
)

const projectUsage = `Usage: athome project clone <repo> [--machine <name>]

  clone   clone <owner/name> (or a GitHub URL) onto a remote machine, with its
          .env files; --machine is needed when there is more than one machine
`

func runProject(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, projectUsage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help":
		fmt.Fprint(stdout, projectUsage)
		return exitOK
	case "clone":
	default:
		return badUsage(stderr, projectUsage, "athome project: unknown command %q", args[0])
	}
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		fmt.Fprint(stdout, projectUsage)
		return exitOK
	}
	fs := flag.NewFlagSet("athome project clone", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	machine := fs.String("machine", "", "")
	pos, err := parseMixed(fs, args[1:])
	if err != nil {
		return badUsage(stderr, projectUsage, "athome project clone: %v", err)
	}
	if len(pos) != 1 {
		return badUsage(stderr, projectUsage, "athome project clone: expected one repo, got %d argument(s)", len(pos))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome project: %v\n", err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := machines.Clone(ctx, machineDeps(home, stdout, stderr), pos[0], *machine); err != nil {
		return report(stderr, "athome project", err)
	}
	return exitOK
}
