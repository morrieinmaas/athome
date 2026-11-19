package steps

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/selfupdate"
	"github.com/morrieinmaas/athome/internal/status"
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

var errNoUpdateOpts = errors.New("update options not set")

func updateOpts(ctx context.Context) (UpdateOpts, error) {
	o, ok := ctx.Value(updateKey{}).(UpdateOpts)
	if !ok {
		return o, errNoUpdateOpts
	}
	return o, nil
}

func Update() []Step {
	return []Step{
		{Name: "self", Check: selfCheck, Apply: selfApply},
		{Name: "source", Check: never, Apply: sourceApply},
		{Name: "config", Check: configCurrent, Apply: configApply},
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
	o, err := updateOpts(ctx)
	if err != nil {
		return false, err
	}
	reexec := os.Getenv(updateReexecEnv) == "1"
	if _, ok := selfupdate.Parse(o.Version); !ok {
		if reexec {
			return false, fmt.Errorf("the installed athome reports %s, not %s; not retrying", o.Version, o.Target)
		}
		if o.ToGiven {
			return false, nil
		}
		envLogln(env, "athome: %s is not a release; not replacing it (pass --to to install one)", o.Version)
		return true, nil
	}
	c := selfupdate.Compare(o.Version, o.Target)
	if reexec && c != 0 {
		return false, fmt.Errorf("the installed athome reports %s, not %s; not retrying", o.Version, o.Target)
	}
	return c == 0 || (c > 0 && !o.ToGiven), nil
}

func selfApply(ctx context.Context, env *Env) error {
	o, err := updateOpts(ctx)
	if err != nil {
		return err
	}
	// Under the progress view the confirm question releases the terminal, so the
	// changelog is also held for it with Say; the run log keeps it for the report.
	say := func(format string, a ...any) {
		envLogln(env, format, a...)
		if env.Ask != nil && env.Ask.Around != nil && !o.Yes {
			env.Ask.Say(format+"\n", a...)
		}
	}
	if cur, ok := selfupdate.Normalize(o.Version); ok {
		changelog(ctx, env, cur, o.Target, say)
	} else {
		say("  (%s is not a release, so there is no changelog)", o.Version)
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
	if env.Ask != nil && env.Ask.Around != nil { // the new binary must not inherit the progress view's terminal state
		// ponytail: on the progress path a successful exec replaces the process before the buffered
		// run log is printed (the new binary starts its own view); upgrade path: flush env.Log to
		// stdout inside Around before exec.
		var err error
		env.Ask.Around(func() { err = exec(dest, argv) })
		return err
	}
	return exec(dest, argv)
}

func changelog(ctx context.Context, env *Env, cur, target string, say func(string, ...any)) {
	git := func(args ...string) (run.Result, error) {
		return env.Run.Run(ctx, run.Cmd{Name: "git", Args: append([]string{"-C", env.RepoRoot}, args...)})
	}
	res, err := git("rev-parse", "--verify", "--quiet", "refs/tags/"+cur)
	if err != nil || res.Code != 0 || strings.TrimSpace(res.Stdout) == "" {
		say("  (no changelog: %s is not a tag in this clone)", cur)
		return
	}
	res, err = git("log", "--no-merges", "--format=  %s", cur+".."+target)
	if err != nil || res.Code != 0 {
		return
	}
	say("athome %s -> %s:", cur, target)
	if out := strings.TrimRight(res.Stdout, "\n"); out != "" {
		say("%s", out)
	}
}

func sourceApply(ctx context.Context, env *Env) error {
	o, err := updateOpts(ctx)
	if err != nil {
		return err
	}
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
	n, err := status.MissingCount(res.Stdout)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// configSource is the source chezmoi apply reads: the sourceDir pinned in
// chezmoi.toml (a leading ~/ under the home, as chezmoi reads it), else the
// repo's home.
func configSource(env *Env) string {
	var doc struct {
		SourceDir string `toml:"sourceDir"`
	}
	if _, err := toml.DecodeFile(chezmoiConfigPath(env), &doc); err != nil || doc.SourceDir == "" {
		return sourceDir(env)
	}
	if rest, ok := strings.CutPrefix(doc.SourceDir, "~/"); ok {
		return filepath.Join(env.Home, rest)
	}
	return doc.SourceDir
}

// configCurrent: chezmoi.toml was generated from the source's current config
// template, by the template hash chezmoi records in its state. Without a
// template there is nothing to regenerate; an unknown hash regenerates.
func configCurrent(ctx context.Context, env *Env) (bool, error) {
	b, err := os.ReadFile(filepath.Join(configSource(env), ".chezmoi.toml.tmpl"))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	res, err := env.Run.Run(ctx, run.Cmd{Name: "chezmoi", Args: []string{"state", "get", "--bucket=configState", "--key=configState"}})
	if err != nil {
		return false, err
	}
	var st struct {
		Hash string `json:"configTemplateContentsSHA256"`
	}
	if res.Code != 0 || json.Unmarshal([]byte(res.Stdout), &st) != nil {
		return false, nil
	}
	return st.Hash == fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

// configApply regenerates chezmoi.toml from the changed template, so new
// [data] keys are written and chezmoi stops asking for chezmoi init. It never
// prompts: the template's prompt*Once calls reuse every stored answer, and a
// key the template adds gets its default. The template renders no sourceDir,
// so the line athome pinned is put back.
func configApply(ctx context.Context, env *Env) error {
	path := chezmoiConfigPath(env)
	src := configSource(env)
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := updateRun(ctx, env, run.Cmd{Name: "chezmoi", Args: []string{"init", "--no-tty", "--promptDefaults", "--source=" + src}}); err != nil {
		return err
	}
	pin := sourceDirLine(old)
	if pin == "" {
		return nil
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if sourceDirLine(cur) != "" {
		return nil
	}
	return writeConfig(path, append([]byte(pin+"\n"), cur...))
}

// sourceDirLine is the config's sourceDir line, "" when it has none.
func sourceDirLine(config []byte) string {
	for _, line := range strings.Split(string(config), "\n") {
		if strings.HasPrefix(line, "sourceDir") {
			return line
		}
	}
	return ""
}

// writeConfig replaces chezmoi.toml with b, mode 0600, through a rename.
func writeConfig(path string, b []byte) error {
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
