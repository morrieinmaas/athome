package steps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/selfupdate"
)

// ErrUpdateCancelled is returned by the self step when the user declines; the
// command treats it as a clean exit.
var ErrUpdateCancelled = errors.New("update cancelled")

// UpdateOpts is what athome update resolved before any step runs.
type UpdateOpts struct {
	Version string // the running version
	Target  string // vX.Y.Z to move to
	ToGiven bool   // --to was passed
	Yes     bool   // --yes: no confirmation
	Base    string // release base URL, default selfupdate.DefaultBase
	// Exec replaces the process with the new binary; nil means syscall.Exec.
	Exec func(path string, argv []string) error
}

type updateKey struct{}

// WithUpdate carries the update options to the steps of Update.
func WithUpdate(ctx context.Context, o UpdateOpts) context.Context {
	return context.WithValue(ctx, updateKey{}, o)
}

func updateOpts(ctx context.Context) UpdateOpts {
	o, _ := ctx.Value(updateKey{}).(UpdateOpts)
	return o
}

func Update() []Step {
	return []Step{
		{Name: "self", Check: selfCheck, Apply: selfApply},
		{Name: "source", Check: never, Apply: sourceApply},
		{Name: "apply", Check: never, Apply: func(ctx context.Context, env *Env) error {
			return updateRun(ctx, env, run.Cmd{Name: "chezmoi", Args: []string{"apply"}, Interactive: true})
		}},
		{Name: "mise", Check: miseCheck, Apply: func(ctx context.Context, env *Env) error {
			return updateRun(ctx, env, run.Cmd{Name: "mise", Args: []string{"install"}, Dir: env.Home, Interactive: true})
		}},
	}
}

func never(context.Context, *Env) (bool, error) { return false, nil }

func updateRun(ctx context.Context, env *Env, c run.Cmd) error {
	res, err := env.Run.Run(ctx, c)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("%s %s exited %d: %s", c.Name, strings.Join(c.Args, " "), res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func selfCheck(ctx context.Context, env *Env) (bool, error) {
	o := updateOpts(ctx)
	if _, ok := selfupdate.Parse(o.Version); !ok {
		if o.ToGiven {
			return false, nil
		}
		envLogln(env, "athome: %s is not a release; not replacing it (pass --to to install one)", o.Version)
		return true, nil
	}
	c := selfupdate.Compare(o.Version, o.Target)
	return c == 0 || (c > 0 && !o.ToGiven), nil
}

func selfApply(ctx context.Context, env *Env) error {
	o := updateOpts(ctx)
	if cur, ok := selfupdate.Normalize(o.Version); ok {
		changelog(ctx, env, cur, o.Target)
	} else {
		envLogln(env, "  (%s is not a release, so there is no changelog)", o.Version)
	}
	if env.Ask != nil && !o.Yes {
		if !env.Ask.Confirm(fmt.Sprintf("update athome from %s to %s?", o.Version, o.Target), true) {
			envLogln(env, "athome update: cancelled, nothing changed")
			return ErrUpdateCancelled
		}
	}
	base := o.Base
	if base == "" {
		base = selfupdate.DefaultBase
	}
	dest := filepath.Join(env.Home, ".local", "bin", "athome")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	if err := selfupdate.Install(ctx, base, o.Target, runtime.GOOS, runtime.GOARCH, dest); err != nil {
		return err
	}
	argv := []string{dest, "update", "--to", o.Target}
	if o.Yes {
		argv = append(argv, "--yes")
	}
	exec := o.Exec
	if exec == nil {
		exec = execBinary
	}
	return exec(dest, argv)
}

func changelog(ctx context.Context, env *Env, cur, target string) {
	git := func(args ...string) (run.Result, error) {
		return env.Run.Run(ctx, run.Cmd{Name: "git", Args: append([]string{"-C", env.RepoRoot}, args...)})
	}
	res, err := git("rev-parse", "--verify", "--quiet", "refs/tags/"+cur)
	if err != nil || res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		envLogln(env, "  (no changelog: %s is not a tag in this clone)", cur)
		return
	}
	res, err = git("log", "--no-merges", "--format=  %s", cur+".."+target)
	if err != nil || res.Code != 0 {
		return
	}
	envLogln(env, "athome %s -> %s:", cur, target)
	if out := strings.TrimRight(res.Stdout, "\n"); out != "" {
		envLogln(env, "%s", out)
	}
}

func sourceApply(ctx context.Context, env *Env) error {
	o := updateOpts(ctx)
	git := func(args ...string) (run.Result, error) {
		return env.Run.Run(ctx, run.Cmd{Name: "git", Args: append([]string{"-C", env.RepoRoot}, args...)})
	}
	res, err := git("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return err
	}
	if res.Code == 0 && strings.TrimSpace(res.Stdout) != "" {
		return gitOK(git("pull", "--ff-only"))
	}
	res, err = git("describe", "--tags", "--exact-match", "HEAD")
	if err != nil {
		return err
	}
	if res.Code == 0 && strings.TrimSpace(res.Stdout) != "" {
		return gitOK(git("checkout", "--quiet", "--detach", o.Target))
	}
	envLogln(env, "athome: %s is at a detached commit with no tag; leaving the source as it is", env.RepoRoot)
	return nil
}

func gitOK(res run.Result, err error) error {
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("git exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}

func miseCheck(ctx context.Context, env *Env) (bool, error) {
	res, err := env.Run.Run(ctx, run.Cmd{Name: "mise", Args: []string{"ls", "--missing", "--json"}, Dir: env.Home})
	if err != nil {
		return false, err
	}
	if res.Code != 0 {
		return false, fmt.Errorf("mise ls --missing --json exited %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return true, nil
	}
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return false, fmt.Errorf("mise ls --missing --json: %w", err)
	}
	switch m := v.(type) {
	case map[string]any:
		return len(m) == 0, nil
	case []any:
		return len(m) == 0, nil
	}
	return false, nil
}
