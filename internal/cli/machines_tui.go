package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/morrieinmaas/athome/internal/machines"
	"github.com/morrieinmaas/athome/internal/tui"
)

// Seams for tests: the view and the machine operations (which reach over ssh).
var (
	runMachinesView = tui.RunMachines
	machinesAdd     = machines.Add
	machinesSync    = machines.Sync
	machinesRemove  = machines.Remove
)

// runMachinesScreen shows the machines view and runs the chosen action plainly,
// so ssh, gh login and the notes use the terminal once the view has exited.
// back returns to the home screen.
func runMachinesScreen(stdout, stderr io.Writer) (code int, back bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome machines: %v\n", err)
		return exitError, false
	}
	var listing bytes.Buffer
	if err := machines.List(home, &listing); err != nil {
		return report(stderr, "athome machines", err), false
	}
	info := tui.MachinesInfo{Listing: listing.String(), User: os.Getenv("USER")}
	if ms, err := machines.Load(home); err == nil {
		for _, m := range ms {
			info.Names = append(info.Names, m.Name)
		}
	}
	if v := strings.TrimPrefix(Version, "v"); plainVersion.MatchString(v) {
		info.Tag = "v" + v
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := runMachinesView(ctx, os.Stdin, os.Stdout, info)
	if err != nil {
		fmt.Fprintf(stderr, "athome machines: %v\n", err)
		return exitError, false
	}
	d := machineDeps(home, stdout, stderr)
	switch res.Action {
	case "add":
		err = machinesAdd(ctx, d, res.Machine)
	case "sync":
		err = machinesSync(ctx, d, res.Name, false)
	case "sync all":
		err = machinesSync(ctx, d, "", true)
	case "remove":
		err = machinesRemove(ctx, d, res.Name, false)
	default:
		return exitOK, true
	}
	if err != nil {
		return report(stderr, "athome machine", err), false
	}
	return exitOK, false
}
