package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/steps"
)

const keysUsage = `Usage: athome keys rotate [--laptop] [--boxes] [--break-glass] [--all] [--resume]

  --laptop       this laptop's ssh key: Bitwarden, GitHub, every box
  --boxes        each box's own key
  --break-glass  each scope's break-glass key
  --all          all three
  --resume       finish a rotation that skipped boxes or stopped midway
`

const pickKind = "pick what to rotate: --laptop | --boxes | --break-glass | --all | --resume\n"

func runKeys(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprint(stdout, keysUsage)
		return exitOK
	}
	if len(args) == 0 || args[0] != "rotate" {
		fmt.Fprint(stderr, keysUsage)
		return exitUsage
	}
	var k keys.Kinds
	resume := false
	for _, a := range args[1:] {
		switch a {
		case "-h", "--help":
			fmt.Fprint(stdout, keysUsage)
			return exitOK
		case "--laptop":
			k.Laptop = true
		case "--boxes":
			k.Boxes = true
		case "--break-glass":
			k.BreakGlass = true
		case "--all":
			k = keys.Kinds{Laptop: true, Boxes: true, BreakGlass: true}
		case "--resume":
			resume = true
		default:
			fmt.Fprintf(stderr, "athome keys rotate: unknown argument: %s\n", a)
			fmt.Fprint(stderr, keysUsage)
			return exitUsage
		}
	}
	none := k == keys.Kinds{}
	if none && !resume {
		fmt.Fprint(stderr, pickKind)
		return exitUsage
	}
	if resume && !none {
		fmt.Fprint(stderr, "athome keys rotate: --resume takes no other flag\n")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome keys: %v\n", err)
		return exitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o := keys.Options{Home: home, Host: steps.KeyHostname(), Run: newRunner(), Out: stdout, Now: time.Now}
	if resume {
		err = keys.Resume(ctx, o)
	} else {
		err = keys.Rotate(ctx, o, k)
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome keys rotate: %v\n", err)
		return exitError
	}
	return exitOK
}
