package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func chezmoiLink(fx *fixture) string { return fx.Path(".local", "share", "chezmoi") }

func TestRepoCheck(t *testing.T) {
	t.Run("missing .chezmoiroot", func(t *testing.T) {
		fx := newFixture(t)
		bare := t.TempDir()
		fx.Env.RepoRoot = bare
		_, err := fx.Exec(steps.Prepare())
		if err == nil {
			t.Fatal("a directory without .chezmoiroot must fail")
		}
		if !strings.Contains(err.Error(), "repo-check") {
			t.Errorf("error %q does not name the step repo-check", err)
		}
		if !strings.Contains(err.Error(), bare) && !strings.Contains(err.Error(), "athome repo") {
			t.Errorf("error %q says nothing useful about the repo", err)
		}
		if exists(chezmoiLink(fx)) {
			t.Errorf("nothing may be changed after the repo check failed")
		}
	})
	t.Run("missing home/.chezmoiscripts", func(t *testing.T) {
		fx := newFixture(t)
		if err := os.RemoveAll(filepath.Join(fx.Repo, "home", ".chezmoiscripts")); err != nil {
			t.Fatal(err)
		}
		if _, err := fx.Exec(steps.Prepare()); err == nil {
			t.Fatal("a repo without home/.chezmoiscripts must fail")
		}
	})
	t.Run("valid repo passes", func(t *testing.T) {
		fx := newFixture(t)
		if _, err := fx.Exec(steps.Prepare()); err != nil {
			t.Fatalf("valid repo: %v\n%s", err, fx.Log)
		}
	})
}

func TestRepoCheckIsFirst(t *testing.T) {
	list := steps.Prepare()
	if len(list) == 0 || list[0].Name != "repo-check" {
		t.Fatalf("first Prepare step must be repo-check, got %v", names(list))
	}
}

func names(list []steps.Step) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

func TestSymlinkCreated(t *testing.T) {
	fx := newFixture(t)
	fx.MustExec(steps.Prepare())
	got, err := os.Readlink(chezmoiLink(fx))
	if err != nil {
		t.Fatalf("~/.local/share/chezmoi is not a symlink: %v", err)
	}
	if got != fx.Repo {
		t.Errorf("link points to %q, want %q", got, fx.Repo)
	}
}

func TestSymlinkExisting(t *testing.T) {
	fx := newFixture(t)
	mustMkdir(t, filepath.Dir(chezmoiLink(fx)))
	if err := os.Symlink(fx.Repo, chezmoiLink(fx)); err != nil {
		t.Fatal(err)
	}
	fx.MustExec(steps.Prepare())
	if got, _ := os.Readlink(chezmoiLink(fx)); got != fx.Repo {
		t.Errorf("link = %q, want it untouched at %q", got, fx.Repo)
	}
}

func TestSymlinkRepoAlreadyAtExpectedPath(t *testing.T) {
	fx := newFixture(t)
	// Put the checkout where chezmoi expects it: no link is needed.
	expected := chezmoiLink(fx)
	mustMkdir(t, filepath.Dir(expected))
	if err := os.Rename(fx.Repo, expected); err != nil {
		t.Fatal(err)
	}
	fx.Env.RepoRoot = expected
	fx.Fake.On("git", "-C", expected, "rev-parse", "--abbrev-ref", "HEAD").Return(run.Result{Stdout: "main\n"})
	fx.MustExec(steps.Prepare())
	if fi, err := os.Lstat(expected); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the checkout must stay a real directory: %v %v", fi, err)
	}
}

func TestSymlinkDifferentLinkFails(t *testing.T) {
	fx := newFixture(t)
	other := t.TempDir()
	mustMkdir(t, filepath.Dir(chezmoiLink(fx)))
	if err := os.Symlink(other, chezmoiLink(fx)); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.Exec(steps.Prepare()); err == nil {
		t.Fatal("a link to a different directory must fail, not be replaced")
	}
	if got, _ := os.Readlink(chezmoiLink(fx)); got != other {
		t.Errorf("link now points to %q, must stay %q", got, other)
	}
}

func TestSymlinkRealDirFails(t *testing.T) {
	fx := newFixture(t)
	dir := chezmoiLink(fx)
	writeFile(t, filepath.Join(dir, "precious.txt"), "keep me", 0o644)
	if _, err := fx.Exec(steps.Prepare()); err == nil {
		t.Fatal("a real directory at ~/.local/share/chezmoi must fail")
	}
	if got := readFile(t, filepath.Join(dir, "precious.txt")); got != "keep me" {
		t.Errorf("the real directory was modified: %q", got)
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		t.Errorf("the real directory was deleted or replaced: %v", err)
	}
}

func TestRefWorktree(t *testing.T) {
	t.Run("ref different from HEAD adds a detached worktree", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Opts.Ref = "v1.2.3"
		// The Fake does not create the worktree, so a later "home/ missing at
		// ref" guard may fail the run. Only the recorded command matters here.
		fx.Exec(steps.Prepare())
		calls := fx.callsTo("git", "worktree", "add", "--detach")
		if len(calls) == 0 {
			t.Fatalf("no `git worktree add --detach` recorded:\n%s", fx.dump())
		}
		if last := calls[0].Args[len(calls[0].Args)-1]; last != "v1.2.3" {
			t.Errorf("worktree add ends with %q, want the ref v1.2.3", last)
		}
		if !containsSeq(calls[0].Args, "-C", fx.Repo) && calls[0].Dir != fx.Repo {
			t.Errorf("worktree add must run against the repo %q: %+v", fx.Repo, calls[0])
		}
	})
	t.Run("ref equal to the current branch adds none", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Opts.Ref = "main"
		fx.MustExec(steps.Prepare())
		if fx.called("git", "worktree", "add") {
			t.Errorf("no worktree wanted when ref == HEAD branch:\n%s", fx.dump())
		}
	})
	t.Run("no ref adds none", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.Prepare())
		if fx.called("git", "worktree", "add") {
			t.Errorf("no worktree wanted without --ref:\n%s", fx.dump())
		}
	})
}

// B:240: leaked worktrees of killed runs are pruned first, ref or not.
func TestWorktreePrunedFirst(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.Ref = "v1.2.3"
	fx.Exec(steps.Prepare())
	var prune, add = -1, -1
	for i, c := range fx.Fake.Calls() {
		if c.Name == "git" && containsSeq(c.Args, "worktree", "prune") && containsSeq(c.Args, "-C", fx.Repo) && prune < 0 {
			prune = i
		}
		if c.Name == "git" && containsSeq(c.Args, "worktree", "add") && add < 0 {
			add = i
		}
	}
	if prune < 0 || prune > add {
		t.Errorf("want `git -C repo worktree prune` before worktree add (prune=%d add=%d):\n%s", prune, add, fx.dump())
	}
}

// F1: the ref step publishes the worktree's home/ as the chezmoi source.
func TestRefWorktreeSetsSource(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.Ref = "v1.2.3"
	fx.Exec(steps.Prepare()) // the Fake creates no home/, so this errors after the add
	adds := fx.callsTo("git", "worktree", "add", "--detach")
	if len(adds) == 0 {
		t.Fatalf("no worktree add recorded:\n%s", fx.dump())
	}
	wt := adds[0].Args[len(adds[0].Args)-2]
	if want := filepath.Join(wt, "home"); fx.Env.Source != want {
		t.Errorf("Env.Source = %q, want %q", fx.Env.Source, want)
	}
}

func TestChezmoiInstall(t *testing.T) {
	t.Run("skipped when chezmoi is on PATH", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("chezmoi")
		fx.MustExec(steps.Prepare())
		if fx.mentions("get.chezmoi.io") {
			t.Errorf("chezmoi found, installer must not run:\n%s", fx.dump())
		}
	})
	t.Run("installed when missing", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.Prepare())
		if !fx.mentions("get.chezmoi.io") {
			t.Errorf("chezmoi missing, installer must be fetched from get.chezmoi.io:\n%s", fx.dump())
		}
	})
}

// S8: a chezmoi that an earlier run left in ~/.local/bin is installed.
func TestChezmoiInstalledInLocalBin(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	fx := newFixture(t)
	bin := fx.Path(".local", "bin")
	writeFile(t, filepath.Join(bin, "chezmoi"), "#!/bin/sh\n", 0o755)
	fx.MustExec(steps.Prepare())
	if fx.mentions("get.chezmoi.io") {
		t.Errorf("~/.local/bin/chezmoi exists, installer must not run:\n%s", fx.dump())
	}
	if got := os.Getenv("PATH"); !strings.HasPrefix(got, bin+":") {
		t.Errorf("PATH = %q, want ~/.local/bin first", got)
	}
}

// The sudo keep-alive is not a step (B:326-338): the runner wrapper starts it
// when a step first runs a sudo command.
func TestSudoIsNotAStep(t *testing.T) {
	for _, s := range steps.All() {
		if strings.Contains(s.Name, "sudo") {
			t.Errorf("step %q: the sudo keep-alive is not a step", s.Name)
		}
	}
}

func sudoVCount(f *run.Fake) int {
	n := 0
	for _, c := range f.Calls() {
		if c.Name == "sudo" && len(c.Args) == 1 && c.Args[0] == "-v" {
			n++
		}
	}
	return n
}

func TestWarmSudo(t *testing.T) {
	ctx := context.Background()
	t.Run("no sudo command, no sudo -v", func(t *testing.T) {
		fx := newFixture(t)
		r, stop := steps.WarmSudo(fx.Fake, fx.Log)
		r.Run(ctx, run.Cmd{Name: "git", Args: []string{"status"}})
		r.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"-n", "true"}})
		stop()
		if n := sudoVCount(fx.Fake); n != 0 {
			t.Errorf("sudo -v ran %d times with no sudo step:\n%s", n, fx.dump())
		}
	})
	t.Run("two sudo commands: one sudo -v before the first, then kept warm until stopped", func(t *testing.T) {
		fx := newFixture(t)
		t.Cleanup(steps.SetSudoInterval(5 * time.Millisecond))
		r, stop := steps.WarmSudo(fx.Fake, fx.Log)
		r.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"pacman", "-S", "x"}})
		r.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"systemctl", "enable", "y"}})
		calls := fx.Fake.Calls()
		if sudoVCount(fx.Fake) != 1 || calls[0].Name != "sudo" || calls[0].Args[0] != "-v" || !calls[0].Interactive {
			t.Fatalf("want exactly one interactive `sudo -v` before the first sudo command:\n%s", fx.dump())
		}
		deadline := time.Now().Add(2 * time.Second)
		for len(fx.callsTo("sudo", "-n", "true")) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		stop()
		n := len(fx.callsTo("sudo", "-n", "true"))
		if n == 0 {
			t.Fatalf("the keep-warm loop never ran:\n%s", fx.dump())
		}
		time.Sleep(30 * time.Millisecond)
		if after := len(fx.callsTo("sudo", "-n", "true")); after != n {
			t.Errorf("keep-warm kept running after stop: %d -> %d calls", n, after)
		}
	})
	t.Run("a refused sudo is a note, the command still runs, no keep-warm", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.On("sudo", "-v").Return(run.Result{Code: 1})
		r, stop := steps.WarmSudo(fx.Fake, fx.Log)
		r.Run(ctx, run.Cmd{Name: "sudo", Args: []string{"true"}})
		stop()
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, a refused sudo -v is only a note (B:337)", fx.Env.Warnings)
		}
		if !strings.Contains(fx.Log.String(), "sudo refused") {
			t.Errorf("want a note in the log, got %q", fx.Log)
		}
		if !fx.called("sudo", "true") || fx.called("sudo", "-n", "true") {
			t.Errorf("the command must run, without keep-warm:\n%s", fx.dump())
		}
	})
}

// The --ref worktree is removed when the run ends (bash's EXIT trap, B:290-300),
// also when a later check failed.
func TestRefWorktreeCleanup(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.Ref = "v1.2.3"
	fx.Exec(steps.Prepare()) // the Fake creates no home/, so this errors after the add
	adds := fx.callsTo("git", "worktree", "add", "--detach")
	if len(adds) == 0 {
		t.Fatalf("no worktree add recorded:\n%s", fx.dump())
	}
	wt := adds[0].Args[len(adds[0].Args)-2]
	if len(fx.Env.Cleanup) != 1 {
		t.Fatalf("want one cleanup registered, got %d", len(fx.Env.Cleanup))
	}
	if fx.called("git", "worktree", "remove") {
		t.Fatalf("worktree removed before cleanup ran:\n%s", fx.dump())
	}
	fx.Env.RunCleanup()
	rm := fx.callsTo("git", "worktree", "remove", "--force")
	if len(rm) != 1 || rm[0].Args[len(rm[0].Args)-1] != wt || !containsSeq(rm[0].Args, "-C", fx.Repo) {
		t.Errorf("want `git -C %s worktree remove --force %s`, got %+v", fx.Repo, wt, rm)
	}
}

func TestRunCleanupReverseOrder(t *testing.T) {
	var got []int
	env := &steps.Env{}
	for i := 1; i <= 3; i++ {
		env.Cleanup = append(env.Cleanup, func() { got = append(got, i) })
	}
	env.RunCleanup()
	if !reflect.DeepEqual(got, []int{3, 2, 1}) {
		t.Errorf("cleanup order = %v, want [3 2 1]", got)
	}
}

// V1: the warm-up also fires for commands that run sudo internally (the
// package-manager script, yay, nb), once, before the first of them.
func TestWarmSudoForNeedsSudoCommands(t *testing.T) {
	cases := []struct{ name, os, distro, install string }{
		{"arch", "linux", "arch", "yay"},
		{"fedora", "linux", "fedora", "sudo"},
		{"darwin", "darwin", "", "nb"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := newFixture(t)
			t.Cleanup(steps.SetNanobrewBin(filepath.Join(t.TempDir(), "nb")))
			fx.Env.OS, fx.Env.Distro = c.os, c.distro
			fx.Fake.On("rpm", "-q", "dnf-plugins-core").Return(run.Result{Code: 1})
			fx.Fake.On("chezmoi", "execute-template").Return(run.Result{Stdout: "echo pm\n"})
			r, stop := steps.WarmSudo(fx.Fake, fx.Log)
			defer stop()
			fx.Env.Run = r
			fx.MustExec(steps.Baseline())
			if n := sudoVCount(fx.Fake); n != 1 {
				t.Fatalf("want exactly one sudo -v, got %d:\n%s", n, fx.dump())
			}
			calls := fx.Fake.Calls()
			vAt, bashAt, installAt := -1, -1, -1
			for i, cmd := range calls {
				switch {
				case cmd.Name == "sudo" && len(cmd.Args) == 1 && cmd.Args[0] == "-v":
					vAt = i
				case cmd.Name == "bash" && bashAt < 0:
					bashAt = i
				case cmd.Name == c.install && cmd.NeedsSudo || cmd.Name == "sudo" && len(cmd.Args) > 0 && cmd.Args[0] == "dnf":
					if installAt < 0 {
						installAt = i
					}
				}
			}
			if bashAt < 0 || installAt < 0 || vAt < 0 || vAt > bashAt || vAt > installAt {
				t.Errorf("sudo -v (%d) must precede the pm script (%d) and the install (%d):\n%s", vAt, bashAt, installAt, fx.dump())
			}
		})
	}
}

// V2: `curl | sh` hides a failed download, so the install is only trusted when
// the binary exists afterwards (bash stops there, B:347).
func TestChezmoiInstallMustLeaveABinary(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Run = fx.Fake // an installer that exits 0 without installing anything
	_, err := fx.Exec(steps.Prepare())
	if err == nil {
		t.Fatalf("a chezmoi install that left no binary must stop the run:\n%s", fx.Log)
	}
	if !strings.Contains(err.Error(), "chezmoi-install") {
		t.Errorf("error %q does not name the step chezmoi-install", err)
	}
}
