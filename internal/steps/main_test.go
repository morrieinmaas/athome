package steps_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points PATH at an empty directory, so a step that escapes the Fake
// runner fails instead of running a real tool, and TMPDIR at a fresh one, so
// the follow-up log never lands in the real $TMPDIR. After the run it checks
// that no test leaked a changed PATH or TMPDIR.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "athome-empty-path")
	if err != nil {
		fmt.Fprintln(os.Stderr, "steps tests:", err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "athome-tmpdir")
	if err != nil {
		fmt.Fprintln(os.Stderr, "steps tests:", err)
		os.Exit(1)
	}
	os.Setenv("PATH", dir)
	os.Setenv("TMPDIR", tmp)
	code := m.Run()
	if got := os.Getenv("PATH"); got != dir {
		fmt.Fprintf(os.Stderr, "steps tests: a test leaked PATH=%q; restore it with t.Setenv\n", got)
		code = 1
	}
	if got := os.Getenv("TMPDIR"); got != tmp {
		fmt.Fprintf(os.Stderr, "steps tests: a test leaked TMPDIR=%q\n", got)
		code = 1
	}
	os.RemoveAll(dir)
	os.RemoveAll(tmp)
	os.Exit(code)
}
