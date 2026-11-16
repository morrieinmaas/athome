package cli

import (
	"bytes"
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
