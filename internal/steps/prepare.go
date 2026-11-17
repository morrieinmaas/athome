package steps

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

// Prepare returns the steps that ready the checkout and the tools:
// repo check (B:155-167), chezmoi source link, --ref worktree (B:267-324) and
// chezmoi install (B:340-347). The sudo cache (B:326-338) is WarmSudo, not a step.
func Prepare() []Step {
	return []Step{
		{Name: "repo-check", Check: prepRepoCheck, Apply: prepNoApply},
		{Name: "chezmoi-link", Check: prepLinkCheck, Apply: prepLinkApply},
		{Name: "ref-worktree", Check: prepWorktreeCheck, Apply: prepWorktreeApply},
		{Name: "chezmoi-install", Check: prepChezmoiCheck, Apply: prepInstallChezmoi},
	}
}

func prepNoApply(context.Context, *Env) error { return nil }

// prepRepoCheck ports B:155-167. It validates and either skips (true) or fails.
func prepRepoCheck(_ context.Context, env *Env) (bool, error) {
	root := env.RepoRoot
	if _, err := os.Stat(filepath.Join(root, ".chezmoiroot")); err != nil {
		return false, prepRepoErr(root)
	}
	if fi, err := os.Stat(filepath.Join(root, "home", ".chezmoiscripts")); err != nil || !fi.IsDir() {
		return false, prepRepoErr(root)
	}
	return true, nil
}

func prepRepoErr(root string) error {
	return fmt.Errorf("%s doesn't look like the athome repo (expected .chezmoiroot at the root and home/.chezmoiscripts/); clone first: gh repo clone morrieinmaas/athome ~/.local/share/chezmoi", root)
}

func prepLinkPath(env *Env) string {
	return filepath.Join(env.Home, ".local", "share", "chezmoi")
}

func prepSamePath(a, b string) bool {
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// prepLinkCheck: done when the chezmoi source path already is, or points at, the
// repo. Anything else is an error; a real directory is never replaced.
func prepLinkCheck(_ context.Context, env *Env) (bool, error) {
	link := prepLinkPath(env)
	fi, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(link)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(target) { // a relative target is relative to the link's directory
			target = filepath.Join(filepath.Dir(link), target)
		}
		if prepSamePath(target, env.RepoRoot) {
			return true, nil
		}
		return false, fmt.Errorf("%s points to %s, not the repo %s; remove it or run from that checkout", link, target, env.RepoRoot)
	}
	if prepSamePath(link, env.RepoRoot) {
		return true, nil
	}
	return false, fmt.Errorf("%s is a real directory that is not this repo (%s); move it aside first", link, env.RepoRoot)
}

func prepLinkApply(_ context.Context, env *Env) error {
	link := prepLinkPath(env)
	if env.Ask != nil && !env.Ask.Confirm(fmt.Sprintf("==> repo at %s, chezmoi expects %s. symlink it?", env.RepoRoot, link), true) {
		envLogln(env, "continuing with source=%s", env.RepoRoot)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	return os.Symlink(env.RepoRoot, link)
}

func prepGitBranch(ctx context.Context, env *Env) (string, error) {
	res, err := env.Run.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "rev-parse", "--abbrev-ref", "HEAD"}})
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("git rev-parse --abbrev-ref HEAD in %s failed: %s", env.RepoRoot, strings.TrimSpace(res.Stderr))
	}
	return strings.TrimSpace(res.Stdout), nil
}

// prepWorktreeCheck: no worktree is needed without --ref or when it equals HEAD.
func prepWorktreeCheck(ctx context.Context, env *Env) (bool, error) {
	if env.Opts.Ref == "" {
		return true, nil
	}
	branch, err := prepGitBranch(ctx, env)
	if err != nil {
		return false, err
	}
	return env.Opts.Ref == branch, nil
}

// prepWorktreeApply ports B:302-324. The worktree is removed by env.Cleanup,
// like bash's EXIT trap.
func prepWorktreeApply(ctx context.Context, env *Env) error {
	wt, err := os.MkdirTemp("", "athome-bootstrap-")
	if err != nil {
		return err
	}
	if err := os.Remove(wt); err != nil { // git worktree add needs the dir absent
		return err
	}
	envLogln(env, "==> materializing %s in worktree: %s", env.Opts.Ref, wt)
	res, err := env.Run.Run(ctx, run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "worktree", "add", "--detach", wt, env.Opts.Ref}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("git worktree add %s: %s", env.Opts.Ref, strings.TrimSpace(res.Stderr))
	}
	env.Cleanup = append(env.Cleanup, func() {
		env.Run.Run(context.Background(), run.Cmd{Name: "git", Args: []string{"-C", env.RepoRoot, "worktree", "remove", "--force", wt}})
		os.RemoveAll(wt)
	})
	env.Source = filepath.Join(wt, "home")
	if fi, err := os.Stat(filepath.Join(wt, "home")); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s doesn't exist at ref %s: this ref has no chezmoi sources under home/", filepath.Join(wt, "home"), env.Opts.Ref)
	}
	return nil
}

var sudoInterval = 60 * time.Second

// sudoRunner asks for sudo credentials only when a step first runs a sudo
// command (B:326-338 asks up front), then keeps them warm until stop is called.
type sudoRunner struct {
	run.Runner
	log    io.Writer
	once   sync.Once
	cancel context.CancelFunc
	done   chan struct{}
}

// WarmSudo wraps r: the first `sudo ...` command (other than `sudo -n ...`)
// runs one interactive `sudo -v` and then starts a `sudo -n true` loop every
// sudoInterval. A refused `sudo -v` is a note, not a warning, and the command
// then runs as it would. stop ends the loop; it is safe when sudo never ran.
func WarmSudo(r run.Runner, log io.Writer) (run.Runner, func()) {
	s := &sudoRunner{Runner: r, log: log}
	return s, func() {
		s.once.Do(func() {}) // nothing may start after stop
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
	}
}

func (s *sudoRunner) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.NeedsSudo || (c.Name == "sudo" && (len(c.Args) == 0 || c.Args[0] != "-n")) {
		s.once.Do(func() { s.warm(ctx) })
	}
	return s.Runner.Run(ctx, c)
}

func (s *sudoRunner) warm(ctx context.Context) {
	if s.log != nil {
		fmt.Fprintln(s.log, "==> caching sudo credentials (one prompt, kept warm for the rest of the run)")
	}
	res, err := s.Runner.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"-v"}, Interactive: true})
	if err != nil || res.Code != 0 {
		if s.log != nil {
			fmt.Fprintln(s.log, "sudo refused: package install and systemd-enable steps may prompt repeatedly")
		}
		return
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(sudoInterval):
				s.Runner.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"-n", "true"}})
			}
		}
	}()
}

func prepChezmoiCheck(_ context.Context, env *Env) (bool, error) {
	// A chezmoi a previous run put in ~/.local/bin counts; Setup put it on PATH.
	if fileExists(filepath.Join(env.Home, ".local", "bin", "chezmoi")) {
		return true, nil
	}
	_, err := env.Run.LookPath("chezmoi")
	return err == nil, nil
}

// prepInstallChezmoi ports B:341-346.
func prepInstallChezmoi(ctx context.Context, env *Env) error {
	bin := filepath.Join(env.Home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	envLogln(env, "==> installing chezmoi")
	res, err := env.Run.Run(ctx, run.Cmd{Name: "sh", Args: []string{"-c", `curl -fsLS https://get.chezmoi.io | sh -s -- -b "$1"`, "sh", bin}})
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("chezmoi installer failed: %s", strings.TrimSpace(res.Stderr))
	}
	prependPath(bin)
	// `curl | sh` exits with sh's status, so a failed download looks like success.
	if !fileExists(filepath.Join(bin, "chezmoi")) {
		if _, err := env.Run.LookPath("chezmoi"); err != nil {
			return fmt.Errorf("chezmoi installer finished but chezmoi is not in %s; check the network and re-run", bin)
		}
	}
	return nil
}

func prependPath(dir string) {
	path := os.Getenv("PATH")
	if path == dir || strings.HasPrefix(path, dir+string(os.PathListSeparator)) {
		return
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+path)
}
