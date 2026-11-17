package steps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
)

type Step struct {
	Name  string
	Check func(context.Context, *Env) (bool, error)
	Apply func(context.Context, *Env) error
}

type Env struct {
	Cfg          config.Config
	Run          run.Runner
	Home         string
	RepoRoot     string
	Source       string // chezmoi source dir; set by the --ref step, default RepoRoot/home, see sourceDir
	OS           string
	Distro       string
	Opts         Opts
	Log          io.Writer
	Warnings     int
	KeyGenerated bool          // set by SSHKey when it generated this run's key; ssh-bw-backup runs only then, and github adds a note
	DataChanged  bool          // set by chezmoi-data when it wrote chezmoi.toml; chezmoi-apply must then run (bash always re-inits after seeding)
	depWarned    bool          // a baseline dep failure already counted as the run's one dep warning
	Cleanup      []func()      // run in reverse order when the run ends, see RunCleanup
	Ask          *prompt.Asker // nil means non-interactive: never prompt, never read stdin
	Events       func(Event)   // nil means no-op; Execute reports each step transition
}

type Opts struct {
	Ref         string
	ImportFrom  string
	ImportSSH   bool
	ImportSSHBW string
	CI          bool
}

// Event is one step transition. State is check, skip, apply, done or fail.
type Event struct {
	Name, State string
	Err         error
}

type Report struct {
	Applied []string
	Skipped []string
}

// Setup does the run-wide writes the checks depend on, once, before Execute:
// it empties the follow-up log, so notes from an earlier run do not bleed into
// this one even when the apply is skipped (B:770), puts the dirs a
// just-installed chezmoi, mise and nb land in first on PATH (B:935), and drops
// worktrees leaked by hard-killed earlier runs (B:240; a no-op otherwise).
func Setup(ctx context.Context, env *Env) error {
	if err := os.WriteFile(FollowupLogPath(), nil, 0o644); err != nil {
		return fmt.Errorf("empty the follow-up log: %w", err)
	}
	prependPath(nanobrewBinDir + string(os.PathListSeparator) + filepath.Join(env.Home, ".local", "bin"))
	env.Run.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "worktree", "prune"}})
	skPubMarkerTidy(env)
	secretsMarkerTidy(env)
	announceRef(ctx, env)
	return nil
}

// announceRef ports B:270-280: without --ref, say which ref is bootstrapped
// from and point at the latest release. Informational; it never changes what is
// built and ignores every failure.
func announceRef(ctx context.Context, env *Env) {
	if env.Opts.Ref != "" {
		return
	}
	ref, err := prepGitBranch(ctx, env)
	if err != nil {
		return
	}
	if ref == "HEAD" {
		envLogln(env, "==> bootstrapping from the checked-out commit (detached HEAD)")
	} else {
		envLogln(env, "==> bootstrapping from the cloned ref: %s", ref)
	}
	env.Run.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "fetch", "--tags", "--quiet"}})
	res, err := env.Run.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "describe", "--tags", "--abbrev=0"}})
	if err != nil || res.Code != 0 {
		return
	}
	if tag := strings.TrimSpace(res.Stdout); tag != "" && tag != ref {
		envLogln(env, "    (latest tagged release is %s: pass --ref %s to pin it)", tag, tag)
	}
}

// Execute runs list in order. A step whose Check reports done is skipped;
// the first failing Check or Apply stops the run with an error naming the step.
// There is no progress file: a rerun resumes because earlier checks now pass.
func Execute(ctx context.Context, env *Env, list []Step) (Report, error) {
	var rep Report
	for _, s := range list {
		if err := ctx.Err(); err != nil {
			return rep, fmt.Errorf("step %s: %w", s.Name, err)
		}
		env.emit(Event{Name: s.Name, State: "check"})
		done, err := s.Check(ctx, env)
		if err != nil {
			env.emit(Event{s.Name, "fail", err})
			return rep, fmt.Errorf("step %s: check: %w", s.Name, err)
		}
		if done {
			rep.Skipped = append(rep.Skipped, s.Name)
			env.emit(Event{Name: s.Name, State: "skip"})
			envLogf(env, "skip  %s\n", s.Name)
			continue
		}
		envLogf(env, "apply %s\n", s.Name)
		env.emit(Event{Name: s.Name, State: "apply"})
		if err := s.Apply(ctx, env); err != nil {
			env.emit(Event{s.Name, "fail", err})
			return rep, fmt.Errorf("step %s: %w", s.Name, err)
		}
		rep.Applied = append(rep.Applied, s.Name)
		env.emit(Event{Name: s.Name, State: "done"})
	}
	return rep, nil
}

func (e *Env) emit(ev Event) {
	if e.Events != nil {
		e.Events(ev)
	}
}

func envLogf(env *Env, format string, a ...any) {
	if env.Log != nil {
		fmt.Fprintf(env.Log, format, a...)
	}
}

// sourceDir is the chezmoi source dir: the --ref worktree's home/ when there is
// one, else the checkout's home/ (bash's SOURCE_FOR_CHEZMOI).
func sourceDir(env *Env) string {
	if env.Source != "" {
		return env.Source
	}
	return filepath.Join(env.RepoRoot, "home")
}

// RunCleanup runs the registered cleanups, last registered first.
func (e *Env) RunCleanup() {
	for i := len(e.Cleanup) - 1; i >= 0; i-- {
		e.Cleanup[i]()
	}
	e.Cleanup = nil
}

// envLogln is envLogf with a trailing newline.
func envLogln(env *Env, format string, a ...any) {
	envLogf(env, format+"\n", a...)
}
