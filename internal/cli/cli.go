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
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
	"golang.org/x/term"
)

var Version = "dev"

// newRunner builds the Runner every step reaches external tools through. Tests
// replace it, so no test can start a real process.
var newRunner = run.Exec

var Commit = "none"

// isTerminal reports whether f is a terminal.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// stdinIsTTY reports whether stdin is a terminal. Tests replace it.
var stdinIsTTY = func() bool { return isTerminal(os.Stdin) }

// isInteractive decides whether init may prompt.
func isInteractive(nonInter, tty bool) bool { return tty && !nonInter }

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
	fs.BoolVar(&o.Yes, "y", false, "same as --non-interactive")
	fs.BoolVar(&o.Yes, "yes", false, "same as --non-interactive")
	fs.Usage = func() {}
	return fs
}

// bash's exact flag set (B:115-130). Go's flag package also takes -ref, --y and
// --name=value, which bash rejects.
var (
	valueFlags = map[string]bool{"--ref": true, "--machine": true, "--config": true, "--import-from": true, "--import-ssh-bw": true}
	boolFlags  = map[string]bool{"--no-agents": true, "--import-ssh": true, "--non-interactive": true, "-y": true, "--yes": true}
)

// checkInitArgs rejects every argument form bash's case statement does not know.
// It stops at -h/--help, which bash answers before reading anything after it.
func checkInitArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help":
			return nil
		case valueFlags[a]:
			i++ // the value is taken as is, even when it looks like a flag
		case boolFlags[a]:
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			return fmt.Errorf("unexpected argument %q", a)
		}
	}
	return nil
}

// parseInit parses the init flags. flag.ErrHelp means -h was given.
func parseInit(args []string) (initOpts, error) {
	var o initOpts
	if err := checkInitArgs(args); err != nil {
		return o, err
	}
	fs := initFlags(&o, io.Discard)
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	fs.Visit(func(f *flag.Flag) { o.MachineSet = o.MachineSet || f.Name == "machine" })
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
	env, code := newInitEnv(o, stdout, stderr)
	if env == nil {
		return code
	}
	return runInitEnv(env, stdout, stderr)
}

// newInitEnv resolves the home, the repo and the answers and builds the Env. On
// failure it prints the error and returns a nil Env with the exit code.
func newInitEnv(o initOpts, stdout, stderr io.Writer) (*steps.Env, int) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return nil, exitError
	}
	repo, err := findRepoRoot(home)
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return nil, exitError
	}
	if err := checkRepoPath(repo); err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return nil, exitUsage
	}
	cfgOpts, stepOpts := buildOptions(o, repo, home, environMap(), os.Getenv("ATHOME_CI") != "")
	cfg, err := config.Load(cfgOpts)
	if cfg.Source != "" { // B:188, before the answers are validated
		fmt.Fprintf(stdout, "==> non-interactive config: %s\n", cfg.Source)
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		if errors.Is(err, config.ErrUsage) {
			return nil, exitUsage
		}
		return nil, exitError
	}
	r := newRunner()
	var ask *prompt.Asker
	if isInteractive(o.NonInter || o.Yes, stdinIsTTY()) {
		ask = prompt.New(os.Stdin, stderr)
	}
	return &steps.Env{
		Ask:      ask,
		Cfg:      cfg,
		Run:      r,
		Home:     home,
		RepoRoot: repo,
		OS:       runtime.GOOS,
		Distro:   distro(r),
		Opts:     stepOpts,
		Log:      stdout,
	}, exitOK
}

// runInitEnv runs every step against env, then prints the finish report.
func runInitEnv(env *steps.Env, stdout, stderr io.Writer) int {
	defer env.RunCleanup()
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
	defer stop()
	if err := steps.Setup(ctx, env); err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	rep, err := execute(ctx, env, steps.All())
	if err != nil {
		fmt.Fprintf(stderr, "athome init: %v\n", err)
		return exitError
	}
	printFinish(stdout, rep, env.Warnings, steps.FollowupLogPath())
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

// execute runs the steps with a runner that asks for sudo only when a step
// first runs a sudo command (not a step itself).
func execute(ctx context.Context, env *steps.Env, list []steps.Step) (steps.Report, error) {
	inner := env.Run
	var stop func()
	env.Run, stop = steps.WarmSudo(inner, env.Log)
	defer func() {
		stop()
		env.Run = inner
	}()
	return steps.Execute(ctx, env, list)
}

// stopSignals cancel the run; the deferred cleanup (the --ref worktree) still runs.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// printFinish ports B:1051-1068: the warnings count and the follow-up log, then
// the summary line, which stays last.
func printFinish(w io.Writer, rep steps.Report, warnings int, followupLog string) {
	if warnings > 0 {
		fmt.Fprintf(w, "athome: finished with %d warning(s), not fully ready; see the notes above\n", warnings)
	} else {
		fmt.Fprint(w, "\nbaseline ready: chezmoi, mise, ssh, gh, rbw, config files, project dirs.\n\nNext: install everything else\n  mise run apply      # full package set + desktop/shell scripts (re-runnable daily)\n")
	}
	if b, err := os.ReadFile(followupLog); err == nil && len(b) > 0 {
		fmt.Fprintf(w, "\nFollow-up actions you (or the system) need to do:\n%s", b)
		if b[len(b)-1] != '\n' {
			fmt.Fprintln(w)
		}
	}
	fmt.Fprint(w, finishFooter)
	printSummary(w, rep)
}

// finishFooter is the heredoc at the end of bash (B:1071-1105).
const finishFooter = `
Day-to-day (mise is the front door, run from the repo):

  mise run apply               # render source -> $HOME (the de facto next step; idempotent)
  mise run update              # pull latest, then apply
  mise run diff                # preview pending changes
  mise run status              # show what has drifted

Shell aliases (anywhere):

  czu                          # chezmoi update -v (pull + apply)
  czd                          # chezmoi diff
  cze ~/.zshrc                 # edit a tracked file (auto-applies)
  czdoc                        # chezmoi doctor

Next manual steps:

  1. Secrets (Bitwarden): handled above if you said yes. To (re)run it anytime:
       bw-setup          # registers a new device (API key) if needed, then unlocks
     See docs/secrets.md for the API-key / EU-region / Vaultwarden details.

  2. (Optional) Reuse this SSH key on another box. It's stored in Bitwarden as a
     secure note (ssh/<hostname>_ed25519); restore it elsewhere with:
       athome init --import-ssh-bw ssh/<hostname>_ed25519

  3. VPNs (when ready):
       sudo netbird up                                   # mesh (NetBird Cloud SSO)
       nordvpn login --username "..." --password "..."   # Linux; macOS uses the GUI

  4. After ` + "`mise run apply`" + ` (it installs zsh + makes it your login shell),
     log out and back in so new terminals pick it up (the running desktop
     session keeps the old $SHELL). For THIS terminal right now:
       exec zsh
`

// printSummary is the one final line of a successful run.
func printSummary(w io.Writer, rep steps.Report) {
	fmt.Fprintf(w, "athome: applied %d, skipped %d\n", len(rep.Applied), len(rep.Skipped))
}

// findRepoRoot looks for .chezmoiroot at or above the working directory, then
// falls back to the parent of the sourceDir pinned in chezmoi's config, then to
// chezmoi's own clone at ~/.local/share/chezmoi.
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
	if root := chezmoiCloneRoot(home); root != "" {
		return root, nil
	}
	return "", errors.New("athome repo not found: no .chezmoiroot at or above the current directory, no sourceDir pinned in chezmoi's config, and none in ~/.local/share/chezmoi")
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

// chezmoiCloneRoot returns ~/.local/share/chezmoi, with a symlink resolved,
// when it holds .chezmoiroot.
func chezmoiCloneRoot(home string) string {
	root, err := filepath.EvalSymlinks(filepath.Join(home, ".local", "share", "chezmoi"))
	if err != nil {
		return ""
	}
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
