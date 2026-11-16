package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
)

// Prepare returns the steps that ready the checkout and the tools:
// repo check (B:155-167), chezmoi source link, --ref worktree (B:267-324),
// sudo cache (B:326-338) and chezmoi install (B:340-347).
func Prepare() []Step {
	return []Step{
		{Name: "repo-check", Check: prepRepoCheck, Apply: prepNoApply},
		{Name: "chezmoi-link", Check: prepLinkCheck, Apply: prepLinkApply},
		{Name: "ref-worktree", Check: prepWorktreeCheck, Apply: prepWorktreeApply},
		{Name: "sudo-cache", Check: prepNeverDone, Apply: prepSudoApply},
		{Name: "chezmoi-install", Check: prepChezmoiCheck, Apply: prepInstallChezmoi},
	}
}

func prepNeverDone(context.Context, *Env) (bool, error) { return false, nil }

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
	if fi, err := os.Stat(filepath.Join(wt, "home")); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s doesn't exist at ref %s: this ref has no chezmoi sources under home/", filepath.Join(wt, "home"), env.Opts.Ref)
	}
	return nil
}

// prepSudoApply ports B:332-338: one prompt, then a keep-warm loop until ctx ends.
func prepSudoApply(ctx context.Context, env *Env) error {
	res, err := env.Run.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"-v"}})
	if err != nil || res.Code != 0 {
		envLogln(env, "sudo refused: package install and systemd-enable steps may prompt repeatedly")
		env.Warnings++
		return nil
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(60 * time.Second):
				env.Run.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"-n", "true"}})
			}
		}
	}()
	return nil
}

func prepChezmoiCheck(_ context.Context, env *Env) (bool, error) {
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
	os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return nil
}
