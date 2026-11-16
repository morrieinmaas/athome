package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

var Version = "dev"

var Commit = "none"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// initOpts is everything `athome init` takes from its flags.
type initOpts struct {
	Ref         string
	Machine     string
	Config      string
	NoAgents    bool
	ImportFrom  string
	ImportSSH   bool
	ImportSSHBW string
	NonInter    bool
	Yes         bool
}

func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stdout)
		return exitOK
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return exitOK
	case "version":
		fmt.Fprintf(stdout, "athome %s (%s)\n", Version, Commit)
		return exitOK
	case "init":
		return runInit(args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "athome: unknown command %q\n", args[0])
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprint(w, "Usage: athome <command>\n\nCommands:\n  init      set up this machine (what scripts/bootstrap.sh does)\n  version   print the version\n\n")
	fmt.Fprint(w, "Run `athome init --help` for the init flags.\n")
}

func initFlags(o *initOpts, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("athome init", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.Ref, "ref", "", "git ref of the athome repo to apply")
	fs.StringVar(&o.Machine, "machine", "", "machine type: personal or work")
	fs.StringVar(&o.Config, "config", "", "answers file (bootstrap.toml)")
	fs.BoolVar(&o.NoAgents, "no-agents", false, "do not set up agent tooling")
	fs.StringVar(&o.ImportFrom, "import-from", "", "directory to import files from")
	fs.BoolVar(&o.ImportSSH, "import-ssh", false, "import the ssh key from --import-from")
	fs.StringVar(&o.ImportSSHBW, "import-ssh-bw", "", "Bitwarden item holding the ssh key")
	fs.BoolVar(&o.NonInter, "non-interactive", false, "never prompt")
	fs.BoolVar(&o.Yes, "y", false, "assume yes")
	fs.BoolVar(&o.Yes, "yes", false, "assume yes")
	fs.Usage = func() {}
	return fs
}

// parseInit parses the init flags. flag.ErrHelp means -h was given.
func parseInit(args []string) (initOpts, error) {
	var o initOpts
	fs := initFlags(&o, io.Discard)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.ImportSSH && o.ImportFrom == "" {
		return o, errors.New("--import-ssh needs --import-from <dir>")
	}
	return o, nil
}

func runInit(args []string, stdout, stderr io.Writer) int {
	o, err := parseInit(args)
	if errors.Is(err, flag.ErrHelp) {
		var tmp initOpts
		fmt.Fprint(stdout, "Usage: athome init [flags]\n\nFlags:\n")
		initFlags(&tmp, io.Discard).VisitAll(func(f *flag.Flag) {
			dash := "--"
			if len(f.Name) == 1 {
				dash = "-"
			}
			fmt.Fprintf(stdout, "  %s%s\t%s\n", dash, f.Name, f.Usage)
		})
		return exitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitUsage
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	repo, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	cfg, err := config.Load(config.Options{
		ConfigPath: o.Config,
		RepoRoot:   repo,
		Home:       home,
		Env:        environMap(),
		Machine:    o.Machine,
		NoAgents:   o.NoAgents,
	})
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		if errors.Is(err, config.ErrUsage) {
			return exitUsage
		}
		return exitError
	}

	r := run.Exec()
	env := &steps.Env{
		Cfg:      cfg,
		Run:      r,
		Home:     home,
		RepoRoot: repo,
		OS:       runtime.GOOS,
		Distro:   distro(r),
		Opts: steps.Opts{
			Ref:         o.Ref,
			ImportFrom:  o.ImportFrom,
			ImportSSH:   o.ImportSSH,
			ImportSSHBW: o.ImportSSHBW,
			CI:          os.Getenv("ATHOME_CI") != "",
		},
		Log: stdout,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	rep, err := steps.Execute(ctx, env, steps.All())
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "athome init: %d applied, %d skipped, %d warnings\n", len(rep.Applied), len(rep.Skipped), env.Warnings)
	return exitOK
}

// findRepoRoot is the nearest directory at or above the cwd with a .chezmoiroot.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".chezmoiroot")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("athome repo not found: no .chezmoiroot at or above the current directory")
		}
		dir = parent
	}
}

func environMap() map[string]string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// distro mirrors scripts/lib/pm.sh: dnf without pacman is fedora, else arch.
func distro(r run.Runner) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	_, dnf := r.LookPath("dnf")
	_, pacman := r.LookPath("pacman")
	if dnf == nil && pacman != nil {
		return "fedora"
	}
	return "arch"
}
