package steps

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/run"
)

type Step struct {
	Name  string
	Check func(context.Context, *Env) (bool, error)
	Apply func(context.Context, *Env) error
}

type Env struct {
	Cfg      config.Config
	Run      run.Runner
	Home     string
	RepoRoot string
	Source   string // chezmoi source dir; set by the --ref step, default RepoRoot/home, see sourceDir
	OS       string
	Distro   string
	Opts     Opts
	Log      io.Writer
	Warnings int
	FreshKey bool     // set by SSHKey on generate or sshKeyAction=reupload; forces the GitHub upload
	Cleanup  []func() // run in reverse order when the run ends, see RunCleanup
}

type Opts struct {
	Ref         string
	ImportFrom  string
	ImportSSH   bool
	ImportSSHBW string
	CI          bool
}

type Report struct {
	Applied []string
	Skipped []string
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
		done, err := s.Check(ctx, env)
		if err != nil {
			return rep, fmt.Errorf("step %s: check: %w", s.Name, err)
		}
		if done {
			rep.Skipped = append(rep.Skipped, s.Name)
			envLogf(env, "skip  %s\n", s.Name)
			continue
		}
		envLogf(env, "apply %s\n", s.Name)
		if err := s.Apply(ctx, env); err != nil {
			return rep, fmt.Errorf("step %s: %w", s.Name, err)
		}
		rep.Applied = append(rep.Applied, s.Name)
	}
	return rep, nil
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
