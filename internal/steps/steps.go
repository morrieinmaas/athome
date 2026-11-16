package steps

import (
	"context"
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

func Execute(ctx context.Context, env *Env, list []Step) (Report, error) {
	panic("not implemented: T3")
}
