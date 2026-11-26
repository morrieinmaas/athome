package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/machines"
	"github.com/morrieinmaas/athome/internal/status"
	"github.com/morrieinmaas/athome/internal/steps"
)

func runStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("athome status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	quiet := fs.Bool("quiet", false, "print only what is not current, one name per line; exit 3 if any")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, "Usage: athome status [--json | --quiet]\n")
			return exitOK
		}
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "athome status: unexpected argument %q\n", fs.Arg(0))
		return exitUsage
	}
	if *asJSON && *quiet {
		fmt.Fprint(stderr, "athome status: --json and --quiet do not go together\n")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitError
	}
	repo, err := findRepoRoot(home)
	if err != nil {
		fmt.Fprintf(stderr, "athome status: %v\n", err)
		return exitError
	}
	rep := status.Collect(context.Background(), newRunner(), Version, Commit, home, repo)
	if *quiet {
		drift := rep.Drift()
		for _, name := range drift {
			fmt.Fprintln(stdout, name)
		}
		if len(drift) > 0 {
			return exitNotCurrent
		}
		return exitOK
	}
	rep.Keys = status.Keys(home, steps.KeyHostname(), time.Now())
	ms, merr := machines.Load(home)
	rep.Machines = status.MachinesFrom(len(ms), machines.ScopeWarnings(home, ms), merr)
	if *asJSON {
		b, _ := json.Marshal(rep)
		fmt.Fprintf(stdout, "%s\n", b)
		return exitOK
	}
	fmt.Fprintf(stdout, "athome %s (%s), repo %s at %s\n", rep.Version, rep.Commit, rep.Repo, rep.Ref)
	for _, c := range rep.Components {
		fmt.Fprintf(stdout, "%-9s %-14s %s\n", c.Name, c.Status, detailText(c))
	}
	for _, w := range rep.Machines.Warnings {
		fmt.Fprintf(stdout, "machines: %s\n", w)
	}
	if r := reminderText(rep.Keys, time.Now()); r != "" {
		fmt.Fprintln(stdout, r)
	}
	return exitOK
}

func detailText(c status.Component) string {
	switch {
	case c.Detail != "":
		return c.Detail
	case c.Status != "current" && c.Installed != "" && c.Wanted != "" &&
		strings.TrimPrefix(c.Installed, "v") != strings.TrimPrefix(c.Wanted, "v"):
		return c.Installed + " (wanted " + c.Wanted + ")"
	}
	return c.Installed
}

// reminderText is the rotation reminder line, "" when none is due.
func reminderText(k status.KeysStatus, now time.Time) string {
	if !k.Due {
		return ""
	}
	return fmt.Sprintf("keys: last rotated %d days ago; run athome keys rotate --laptop", k.DaysSince(now))
}

// keysReminder is reminderText for this laptop, for the home screen.
func keysReminder(home string, now time.Time) string {
	return reminderText(status.Keys(home, steps.KeyHostname(), now), now)
}
