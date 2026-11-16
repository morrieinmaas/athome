package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/morrieinmaas/athome/internal/steps"
)

func TestPrintSummary(t *testing.T) {
	var b bytes.Buffer
	printSummary(&b, steps.Report{Applied: []string{"a", "b"}, Skipped: []string{"c", "d", "e"}})
	if got, want := b.String(), "athome: applied 2, skipped 3\n"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestPrintFinishKeepsSummaryLast(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "followup.log")
	if err := os.WriteFile(logPath, []byte("run chsh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	printFinish(&b, steps.Report{Applied: []string{"a"}, Skipped: []string{"b"}}, 2, logPath)
	out := b.String()
	for _, want := range []string{"2 warning(s)", "run chsh"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.HasSuffix(out, "athome: applied 1, skipped 1\n") {
		t.Errorf("the summary must be the last line:\n%s", out)
	}
}

func TestPrintFinishQuietWhenClean(t *testing.T) {
	var b bytes.Buffer
	printFinish(&b, steps.Report{}, 0, filepath.Join(t.TempDir(), "missing.log"))
	if got, want := b.String(), "athome: applied 0, skipped 0\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestStopSignalsIncludeSIGTERM(t *testing.T) {
	for _, s := range stopSignals {
		if s == syscall.SIGTERM {
			return
		}
	}
	t.Errorf("SIGTERM must cancel the run so the --ref worktree is cleaned up")
}
