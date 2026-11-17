package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/tui"
)

func TestHomeRouting(t *testing.T) {
	for _, c := range []struct {
		choice   string
		wantCode int
		wantForm bool
	}{{"quit", 0, false}, {"init", 130, true}} {
		t.Run(c.choice, func(t *testing.T) {
			home, shown := tuiSandbox(t)
			chez := filepath.Join(home, ".config", "chezmoi")
			os.MkdirAll(chez, 0o755)
			os.WriteFile(filepath.Join(chez, "chezmoi.toml"), []byte("[data]\n  machineType = \"work\"\n  hostname = \"hb\"\n  githubHandle = \"octo\"\n"), 0o644)
			var info tui.HomeInfo
			runHome = func(_ io.Reader, _ io.Writer, i tui.HomeInfo) (string, error) { info = i; return c.choice, nil }
			code, out, _ := run2()
			if code != c.wantCode || (len(*shown) > 0) != c.wantForm {
				t.Errorf("exit %d screens %v", code, *shown)
			}
			if out != "" && strings.Contains(out, "Usage") {
				t.Error("a terminal must get the home screen, not usage")
			}
			if !info.SetUp || info.MachineType != "work" || info.Hostname != "hb" || info.Handle != "octo" || info.Version != "dev" || info.Repo == "" {
				t.Errorf("info = %+v", info)
			}
		})
	}
}

func TestNoArgsWithoutTerminalPrintsUsage(t *testing.T) {
	_, _ = tuiSandbox(t)
	stdoutIsTTY = func() bool { return false }
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) {
		t.Error("home screen must not run without a terminal")
		return "quit", nil
	}
	if code, out, _ := run2(); code != 0 || !strings.Contains(out, "Usage: athome") {
		t.Errorf("exit %d out %q", code, out)
	}
}

func TestHomeNotSetUpAndNoRepo(t *testing.T) {
	_, _ = tuiSandbox(t)
	t.Chdir(t.TempDir())
	var info tui.HomeInfo
	runHome = func(_ io.Reader, _ io.Writer, i tui.HomeInfo) (string, error) { info = i; return "quit", nil }
	run2()
	if info.SetUp || info.Repo != "" {
		t.Errorf("info = %+v, want not set up and empty repo", info)
	}
}

func TestHomeUpdateRunsAthomeUpdate(t *testing.T) {
	home, shown := tuiSandbox(t)
	repo, err := findRepoRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	f := runtest.NewFake()
	newRunner = func() run.Runner { return f }
	runHome = func(io.Reader, io.Writer, tui.HomeInfo) (string, error) { return "update", nil }
	code, _, stderr := run2()
	if len(*shown) > 0 {
		t.Errorf("update must not open the init form or progress: %v", *shown)
	}
	if code != 1 || !strings.Contains(stderr, "athome update: no release tag (vX.Y.Z) found in the repo") {
		t.Errorf("exit %d stderr %q, want the no-release-tag error of the update path", code, stderr)
	}
	if !hasCall(f, "git", "-C", repo, "fetch", "--quiet", "--tags", "--force", "origin") {
		t.Errorf("update did not fetch tags: %v", f.Calls())
	}
}
