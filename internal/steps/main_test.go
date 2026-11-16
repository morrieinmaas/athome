package steps_test

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points PATH at an empty directory, so a step that escapes the Fake
// runner fails instead of running a real tool.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "athome-empty-path")
	if err != nil {
		fmt.Fprintln(os.Stderr, "steps tests:", err)
		os.Exit(1)
	}
	os.Setenv("PATH", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
