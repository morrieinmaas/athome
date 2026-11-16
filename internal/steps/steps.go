package steps

import (
	"context"
	"fmt"
	"io"

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
	OS       string
	Distro   string
	Opts     Opts
	Log      io.Writer
	Warnings int
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
