package cli_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run"
)

// TestMain makes it impossible for a test in this package to run a real tool:
// Main gets a Fake runner, and PATH is an empty directory, so an escaped call
// fails instead of running. TMPDIR is a fresh directory, so nothing reaches the
// real follow-up log. After the run it checks that no test leaked either.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "athome-empty-path")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli tests:", err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "athome-tmpdir")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli tests:", err)
		os.Exit(1)
	}
	os.Setenv("PATH", dir)
	os.Setenv("TMPDIR", tmp)
	cli.SetRunner(run.NewFake())
	code := m.Run()
	if cli.RealRunnerInstalled() {
		fmt.Fprintln(os.Stderr, "cli tests: a test left the real runner installed; use cli.SetRunner and restore it")
		code = 1
	}
	if got := os.Getenv("PATH"); got != dir {
		fmt.Fprintf(os.Stderr, "cli tests: a test leaked PATH=%q; restore it with t.Setenv\n", got)
		code = 1
	}
	if got := os.Getenv("TMPDIR"); got != tmp {
		fmt.Fprintf(os.Stderr, "cli tests: a test leaked TMPDIR=%q\n", got)
		code = 1
	}
	os.RemoveAll(dir)
	os.RemoveAll(tmp)
	os.Exit(code)
}
