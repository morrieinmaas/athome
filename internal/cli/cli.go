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
	"syscall"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
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
	MachineSet  bool // --machine was given, even as ""
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
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	fs.Visit(func(f *flag.Flag) { o.MachineSet = o.MachineSet || f.Name == "machine" })
	if o.ImportSSH && o.ImportFrom == "" {
		return o, errors.New("--import-ssh needs --import-from <dir>")
	}
	if o.ImportFrom != "" {
		if fi, err := os.Stat(o.ImportFrom); err != nil || !fi.IsDir() {
			return o, fmt.Errorf("--import-from: directory not found: %s", o.ImportFrom)
		}
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
	repo, err := findRepoRoot(home)
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	if err := checkRepoPath(repo); err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitUsage
	}
	cfgOpts, stepOpts := buildOptions(o, repo, home, environMap(), os.Getenv("ATHOME_CI") != "")
	cfg, err := config.Load(cfgOpts)
	if cfg.Source != "" { // B:188, before the answers are validated
		fmt.Fprintf(stdout, "==> non-interactive config: %s\n", cfg.Source)
	}
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
		Opts:     stepOpts,
		Log:      stdout,
	}
	defer env.RunCleanup()
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	rep, err := execute(ctx, env, steps.All())
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	printFinish(stdout, rep, env.Warnings, steps.FollowupLogPath(home))
	return exitOK
}

// buildOptions turns the parsed flags into the config and step options.
func buildOptions(o initOpts, repo, home string, environ map[string]string, ci bool) (config.Options, steps.Opts) {
	return config.Options{
		ConfigPath: o.Config,
		RepoRoot:   repo,
		Home:       home,
		Env:        environ,
		Machine:    o.Machine,
		MachineSet: o.MachineSet,
		NoAgents:   o.NoAgents,
	}, steps.Opts{
		Ref:         o.Ref,
		ImportFrom:  o.ImportFrom,
		ImportSSH:   o.ImportSSH,
		ImportSSHBW: o.ImportSSHBW,
		CI:          ci,
	}
}

// checkRepoPath rejects a path chezmoi.toml cannot pin, before any step runs.
func checkRepoPath(repo string) error {
	if !utf8.ValidString(repo) {
		return fmt.Errorf("the repo path %q is not valid UTF-8, so chezmoi.toml cannot pin its source; move the repo to a UTF-8 path", repo)
	}
	return nil
}

// execute starts the sudo keep-alive (not a step), then runs the steps.
func execute(ctx context.Context, env *steps.Env, list []steps.Step) (steps.Report, error) {
	defer steps.StartSudo(ctx, env)()
	return steps.Execute(ctx, env, list)
}

// stopSignals cancel the run; the deferred cleanup (the --ref worktree) still runs.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// printFinish ports B:1051-1068: the warnings count and the follow-up log, then
// the summary line, which stays last.
func printFinish(w io.Writer, rep steps.Report, warnings int, followupLog string) {
	if warnings > 0 {
		fmt.Fprintf(w, "athome: finished with %d warning(s), not fully ready; see the notes above\n", warnings)
	}
	if b, err := os.ReadFile(followupLog); err == nil && len(b) > 0 {
		fmt.Fprintf(w, "\nFollow-up actions you (or the system) need to do:\n%s", b)
		if b[len(b)-1] != '\n' {
			fmt.Fprintln(w)
		}
	}
	printSummary(w, rep)
}

// printSummary is the one final line of a successful run.
func printSummary(w io.Writer, rep steps.Report) {
	fmt.Fprintf(w, "athome: applied %d, skipped %d\n", len(rep.Applied), len(rep.Skipped))
}

// findRepoRoot is the nearest directory at or above the cwd with a .chezmoiroot.
// findRepoRoot looks for .chezmoiroot at or above the working directory, then
// falls back to the parent of the sourceDir pinned in chezmoi's config.
func findRepoRoot(home string) (string, error) {
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
			break
		}
		dir = parent
	}
	if root := pinnedRepoRoot(home); root != "" {
		return root, nil
	}
	return "", errors.New("athome repo not found: no .chezmoiroot at or above the current directory, and no sourceDir pinned in chezmoi's config")
}

func pinnedRepoRoot(home string) string {
	var c struct {
		SourceDir string `toml:"sourceDir"`
	}
	if _, err := toml.DecodeFile(filepath.Join(home, ".config", "chezmoi", "chezmoi.toml"), &c); err != nil || c.SourceDir == "" {
		return ""
	}
	src := c.SourceDir
	if rest, ok := strings.CutPrefix(src, "~/"); ok {
		src = filepath.Join(home, rest)
	}
	root := filepath.Dir(filepath.Clean(src))
	if _, err := os.Stat(filepath.Join(root, ".chezmoiroot")); err != nil {
		return ""
	}
	return root
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
