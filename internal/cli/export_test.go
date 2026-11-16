package cli

import (
	"context"

	"github.com/morrieinmaas/athome/internal/config"
	"github.com/morrieinmaas/athome/internal/steps"
)

// OptionsFromArgs parses init flags and builds the options runInit would use.
func OptionsFromArgs(args []string, repo, home string, environ map[string]string, ci bool) (config.Options, steps.Opts, error) {
	o, err := parseInit(args)
	if err != nil {
		return config.Options{}, steps.Opts{}, err
	}
	c, s := buildOptions(o, repo, home, environ, ci)
	return c, s, nil
}

func CheckRepoPath(repo string) error { return checkRepoPath(repo) }

func Execute(ctx context.Context, env *steps.Env, list []steps.Step) (steps.Report, error) {
	return execute(ctx, env, list)
}
