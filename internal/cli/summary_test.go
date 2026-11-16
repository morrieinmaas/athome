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

// S10: B:1050-1105, the finish footer comes before the summary line.
func TestPrintFinishFooter(t *testing.T) {
	var b bytes.Buffer
	printFinish(&b, steps.Report{}, 0, filepath.Join(t.TempDir(), "missing.log"))
	out := b.String()
	for _, want := range []string{
		"baseline ready",
		"mise run apply",
		"Day-to-day (mise is the front door",
		"Shell aliases (anywhere):",
		"Next manual steps:",
		"bw-setup",
		"sudo netbird up",
		"After `mise run apply` (it installs zsh",
		"exec zsh",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("footer lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\\$") || strings.Contains(out, "\\`") {
		t.Errorf("heredoc escapes leaked into the footer:\n%s", out)
	}
	if !strings.HasSuffix(out, "athome: applied 0, skipped 0\n") {
		t.Errorf("the summary must be the last line:\n%s", out)
	}
	if strings.Index(out, "exec zsh") > strings.Index(out, "athome: applied") {
		t.Errorf("the footer must come before the summary:\n%s", out)
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
