package steps_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

func TestSudoCache(t *testing.T) {
	t.Run("caches credentials once", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("chezmoi")
		fx.MustExec(steps.Prepare())
		if !fx.called("sudo", "-v") {
			t.Errorf("`sudo -v` not recorded:\n%s", fx.dump())
		}
	})
	t.Run("a refused sudo is a warning, not an error", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("chezmoi")
		fx.Fake.On("sudo", "-v").Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.Prepare()); err != nil {
			t.Fatalf("sudo refusal must not fail the run: %v", err)
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
