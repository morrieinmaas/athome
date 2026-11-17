package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/selfupdate"
	"github.com/morrieinmaas/athome/internal/steps"
)

// updateExec replaces the process with the freshly installed binary; tests swap it.
var updateExec = func(path string, argv []string) error { return syscall.Exec(path, argv, os.Environ()) }

type updateOpts struct {
	To  string
	Yes bool
}

func updateFlags(o *updateOpts, out io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("athome update", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.To, "to", "", "release to install, vX.Y.Z or X.Y.Z (default: the highest tag)")
	fs.BoolVar(&o.Yes, "y", false, "same as --yes")
	fs.BoolVar(&o.Yes, "yes", false, "do not ask before replacing the binary")
	fs.Usage = func() {}
	return fs
}

func runUpdate(args []string, stdout, stderr io.Writer) int {
	var o updateOpts
	fs := updateFlags(&o, io.Discard)
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		var tmp updateOpts
		fmt.Fprint(stdout, "Usage: athome update [--to <version>] [--yes]\n\nFlags:\n")
		updateFlags(&tmp, io.Discard).VisitAll(func(f *flag.Flag) {
			dash := "--"
			if len(f.Name) == 1 {
				dash = "-"
			}
			fmt.Fprintf(stdout, "  %s%s\t%s\n", dash, f.Name, f.Usage)
		})
		return exitOK
	}
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	toGiven := false
	fs.Visit(func(f *flag.Flag) { toGiven = toGiven || f.Name == "to" })
	target := ""
	if err == nil && toGiven {
		var ok bool
		if target, ok = selfupdate.Normalize(o.To); !ok {
			err = fmt.Errorf("--to %q is not a version (want vX.Y.Z or X.Y.Z)", o.To)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome update: %v\n", err)
		return exitUsage
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "athome update: %v\n", err)
		return exitError
	}
	repo, err := findRepoRoot(home)
	if err != nil {
		fmt.Fprintf(stderr, "athome update: %v\n", err)
		return exitError
	}
	r := newRunner()
	ctx, stop := notifyContext(context.Background(), stopSignals...)
	defer stop()

	target, err = resolveUpdateTarget(ctx, r, repo, target)
	if err != nil {
		fmt.Fprintf(stderr, "athome update: %v\n", err)
		return exitError
	}

	env := &steps.Env{Run: r, Home: home, RepoRoot: repo, OS: runtime.GOOS, Log: stdout}
	if isInteractive(o.Yes, stdinIsTTY()) {
		env.Ask = prompt.New(os.Stdin, stderr)
	}
	defer env.RunCleanup()
	bindCancel(ctx, env, stderr)
	base := os.Getenv("ATHOME_RELEASE_BASE")
	if base == "" {
		base = selfupdate.DefaultBase
	}
	ctx = steps.WithUpdate(ctx, steps.UpdateOpts{
		Version: Version, Target: target, ToGiven: toGiven, Yes: o.Yes, Base: base, Exec: updateExec,
	})
	rep, err := steps.Execute(ctx, env, steps.Update())
	if errors.Is(err, steps.ErrUpdateCancelled) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "athome update: %v\n", err)
		return exitError
	}
	printSummary(stdout, rep)
	return exitOK
}

// resolveUpdateTarget fetches the tags and returns the release to move to: want
// when given (it must exist on origin), else the highest vX.Y.Z tag.
func resolveUpdateTarget(ctx context.Context, r run.Runner, repo, want string) (string, error) {
	git := func(args ...string) (run.Result, error) {
		return r.Run(ctx, run.Cmd{Name: "git", Args: append([]string{"-C", repo}, args...)})
	}
	res, err := git("fetch", "--quiet", "--tags", "--force", "origin")
	if err != nil {
		return "", fmt.Errorf("git fetch: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git fetch origin failed (offline?): %s", strings.TrimSpace(res.Stderr))
	}
	res, err = git("tag", "--list")
	if err != nil {
		return "", fmt.Errorf("git tag: %w", err)
	}
	tags := strings.Fields(res.Stdout)
	if want == "" {
		if want = selfupdate.Highest(tags); want == "" {
			return "", errors.New("no release tag (vX.Y.Z) found in the repo")
		}
		return want, nil
	}
	// --to is checked against what origin lists, not against local tags a past fetch left behind.
	res, err = git("ls-remote", "--tags", "--refs", "origin")
	if err != nil {
		return "", fmt.Errorf("git ls-remote: %w", err)
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git ls-remote origin failed (offline?): %s", strings.TrimSpace(res.Stderr))
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if _, ref, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok && ref == "refs/tags/"+want {
			return want, nil
		}
	}
	return "", fmt.Errorf("origin has no tag %s", want)
}
