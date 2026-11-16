package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
)

func TestNotARepoIsExit1(t *testing.T) {
	sandbox(t)
	inNoRepo(t)
	code, _, errOut := runMain("init", "--non-interactive")
	if code != 1 {
		t.Errorf("exit = %d, want 1 (an ordinary error, not a usage error)", code)
	}
	if !strings.Contains(errOut, "chezmoiroot") && !strings.Contains(strings.ToLower(errOut), "repo") {
		t.Errorf("stderr %q should say the athome repo was not found", errOut)
	}
}

func TestRepoRootFromSubdirectory(t *testing.T) {
	sandbox(t)
	repo := inRepo(t)
	sub := filepath.Join(repo, "scripts", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	// The repo is found from a subdirectory, so config validation (exit 2) is
	// reached rather than the no-repo error (exit 1).
	code, _, _ := runMain("init", "--machine", "laptop")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

// With no .chezmoiroot above the cwd, the repo comes from the sourceDir pinned
// in chezmoi's config; the bad machine then ends the run with 2, not 1.
func TestRepoRootFromPinnedSourceDir(t *testing.T) {
	home := sandbox(t)
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "chezmoi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pin := "sourceDir = \"" + filepath.Join(repo, "home") + "\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "chezmoi.toml"), []byte(pin), 0o644); err != nil {
		t.Fatal(err)
	}
	inNoRepo(t)
	code, _, errOut := runMain("init", "--machine", "laptop")
	if code != 2 {
		t.Errorf("exit = %d stderr %q, want 2 (repo found via sourceDir, then the bad machine)", code, errOut)
	}
}

// With no cwd repo and no pin, the clone chezmoi keeps at ~/.local/share/chezmoi
// is the repo, whether it is a directory or a symlink to one.
func TestRepoRootFromChezmoiClone(t *testing.T) {
	mkRepo := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "home"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("directory", func(t *testing.T) {
		home := sandbox(t)
		mkRepo(t, filepath.Join(home, ".local", "share", "chezmoi"))
		inNoRepo(t)
		if code, _, errOut := runMain("init", "--machine", "laptop"); code != 2 {
			t.Errorf("exit = %d stderr %q, want 2 (repo found at ~/.local/share/chezmoi)", code, errOut)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		home := sandbox(t)
		target := t.TempDir()
		mkRepo(t, target)
		link := filepath.Join(home, ".local", "share", "chezmoi")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		inNoRepo(t)
		if code, _, errOut := runMain("init", "--machine", "laptop"); code != 2 {
			t.Errorf("exit = %d stderr %q, want 2 (repo found via the symlink)", code, errOut)
		}
	})
	t.Run("no .chezmoiroot there", func(t *testing.T) {
		home := sandbox(t)
		if err := os.MkdirAll(filepath.Join(home, ".local", "share", "chezmoi"), 0o755); err != nil {
			t.Fatal(err)
		}
		inNoRepo(t)
		if code, _, _ := runMain("init", "--machine", "laptop"); code != 1 {
			t.Errorf("exit = %d, want 1 (not a repo)", code)
		}
	})
}

// R5: a repo path chezmoi.toml cannot pin is rejected before any step.
func TestRepoPathMustBeUTF8(t *testing.T) {
	if err := cli.CheckRepoPath("/ok/repo"); err != nil {
		t.Errorf("a UTF-8 path was rejected: %v", err)
	}
	bad := "/tmp/repo-\xff\xfe"
	err := cli.CheckRepoPath(bad)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") || !strings.Contains(err.Error(), "repo-") {
		t.Errorf("CheckRepoPath(%q) = %v, want an error naming the path", bad, err)
	}
}

func TestNonUTF8RepoIsUsageError(t *testing.T) {
	sandbox(t)
	repo := filepath.Join(t.TempDir(), "repo-\xff")
	if err := os.MkdirAll(filepath.Join(repo, "home", ".chezmoiscripts"), 0o755); err != nil {
		t.Skipf("this filesystem rejects non-UTF-8 names: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	code, _, errOut := runMain("init")
	if code != 2 || !strings.Contains(errOut, "UTF-8") {
		t.Errorf("exit %d stderr %q; want 2 naming the UTF-8 problem", code, errOut)
	}
}
