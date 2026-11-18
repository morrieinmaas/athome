package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/machines"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/secrets"
	"github.com/morrieinmaas/athome/internal/steps"
)

const machineUsage = `Usage: athome machine <command>

  add <name> --scope s --target t [--user u] [--jump j] [--port p]
             [--github-account a] [--forward-agent] [--athome <tag>]
                   set up a remote dev machine (resumable: rerun the same command)
  sync [<name> | --all]
                   bring a machine's bootstrap and .env files up to date
  rm <name> [--force]
                   revoke a machine's keys and remove it
  ls               list the machines
`

var plainVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// parseMixed parses flags that may come before or after the positional words.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// machineDeps builds the machines.Deps shared by the machine and project commands.
func machineDeps(home string, stdout, stderr io.Writer) machines.Deps {
	return machines.Deps{
		Run:  newRunner(),
		Home: home,
		Host: steps.KeyHostname(),
		Out:  stdout,
		Backup: func(ctx context.Context) error {
			if code := secrets.Backup(ctx, secrets.BackupFromEnv(home, newRunner(), stdout, stderr)); code != 0 {
				return fmt.Errorf("athome secrets backup exited %d", code)
			}
			return nil
		},
	}
}

// report prints err (prefixed unless it already is) and returns the exit code:
// 2 for a usage error, 1 for anything else.
func report(stderr io.Writer, prefix string, err error) int {
	msg := err.Error()
	if !strings.HasPrefix(msg, prefix) {
		msg = prefix + ": " + msg
	}
	fmt.Fprintln(stderr, msg)
	var ue machines.UsageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	return exitError
}

func badUsage(stderr io.Writer, usage, format string, a ...any) int {
	fmt.Fprintf(stderr, format+"\n", a...)
	fmt.Fprint(stderr, usage)
	return exitUsage
}

func runMachine(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, machineUsage)
		return exitUsage
	}
	switch args[0] {
	case "-h", "--help":
		fmt.Fprint(stdout, machineUsage)
		return exitOK
	case "add", "sync", "rm", "ls":
	default:
		return badUsage(stderr, machineUsage, "athome machine: unknown command %q", args[0])
	}
	sub := args[0]
	fs := flag.NewFlagSet("athome machine "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var m machines.Machine
	var all, force bool
	switch sub {
	case "add":
		fs.StringVar(&m.Scope, "scope", "", "")
		fs.StringVar(&m.Target, "target", "", "")
		fs.StringVar(&m.User, "user", "", "")
		fs.StringVar(&m.Jump, "jump", "", "")
		fs.IntVar(&m.Port, "port", 0, "")
		fs.StringVar(&m.GitHubAccount, "github-account", "", "")
		fs.BoolVar(&m.ForwardAgent, "forward-agent", false, "")
		fs.StringVar(&m.Athome, "athome", "", "")
	case "sync":
		fs.BoolVar(&all, "all", false, "")
	case "rm":
		fs.BoolVar(&force, "force", false, "")
	}
	rest := args[1:]
	if len(rest) == 1 && (rest[0] == "-h" || rest[0] == "--help") {
		fmt.Fprint(stdout, machineUsage)
		return exitOK
	}
	pos, err := parseMixed(fs, rest)
	if err != nil {
		return badUsage(stderr, machineUsage, "athome machine %s: %v", sub, err)
	}
	wantNames := map[string]int{"add": 1, "rm": 1, "ls": 0}
	if n, ok := wantNames[sub]; ok && len(pos) != n {
		return badUsage(stderr, machineUsage, "athome machine %s: expected %d argument(s), got %d", sub, n, len(pos))
	}
	if sub == "sync" && len(pos) > 1 {
		return badUsage(stderr, machineUsage, "athome machine sync: at most one machine name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome machine: %v\n", err)
		return exitError
	}
	if sub == "ls" {
		if err := machines.List(home, stdout); err != nil {
			return report(stderr, "athome machine", err)
		}
		return exitOK
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d := machineDeps(home, stdout, stderr)
	switch sub {
	case "add":
		m.Name = pos[0]
		if code := fillAdd(&m, &d, stdout, stderr); code != exitOK {
			return code
		}
		err = machines.Add(ctx, d, m)
	case "sync":
		name := ""
		if len(pos) == 1 {
			name = pos[0]
		}
		err = machines.Sync(ctx, d, name, all)
	case "rm":
		err = machines.Remove(ctx, d, pos[0], force)
	}
	if err != nil {
		return report(stderr, "athome machine", err)
	}
	return exitOK
}

// fillAdd applies the defaults and asks for what a terminal can be asked.
func fillAdd(m *machines.Machine, d *machines.Deps, stdout, stderr io.Writer) int {
	if stdinIsTTY() {
		d.Ask = prompt.New(os.Stdin, stdout)
		if m.Scope == "" {
			m.Scope = d.Ask.String("scope (work/<name>, personal/<name> or sidebiz/<name>): ", true)
		}
		if m.Target == "" {
			m.Target = d.Ask.String("target (host, mesh name or IPv4 address): ", true)
		}
	}
	for _, f := range []struct{ name, val string }{{"scope", m.Scope}, {"target", m.Target}} {
		if f.val == "" {
			return badUsage(stderr, machineUsage, "athome machine add: --%s is required", f.name)
		}
	}
	if m.User == "" {
		m.User = os.Getenv("USER")
	}
	if m.Athome == "" {
		v := strings.TrimPrefix(Version, "v")
		if !plainVersion.MatchString(v) {
			return badUsage(stderr, machineUsage, "athome machine add: this is a development build (%s); pass --athome <tag>, for example --athome v0.5.2", Version)
		}
		m.Athome = "v" + v
	}
	return exitOK
}

// useMachinesInventory points athome keys at the machines inventory.
func useMachinesInventory() {
	keys.Inventory = func(home string) ([]keys.Box, error) {
		return machines.KeysInventory(home, newRunner(), steps.KeyHostname())
	}
}
