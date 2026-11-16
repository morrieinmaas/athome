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
// fails instead of running.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "athome-empty-path")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cli tests:", err)
		os.Exit(1)
	}
	os.Setenv("PATH", dir)
	cli.SetRunner(run.NewFake())
	code := m.Run()
	if cli.RealRunnerInstalled() {
		fmt.Fprintln(os.Stderr, "cli tests: a test left the real runner installed; use cli.SetRunner and restore it")
		code = 1
	}
	os.RemoveAll(dir)
	os.Exit(code)
}
