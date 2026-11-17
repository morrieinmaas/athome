package cli_test

import (
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

func TestTeardownIsWired(t *testing.T) {
	home := sandbox(t)
	f := runtest.NewFake()
	defer cli.SetRunner(f)()
	if code, _, errOut := runMain("teardown"); code != 2 || !strings.Contains(errOut, "pick a tier") {
		t.Errorf("no tier: exit %d, stderr %q", code, errOut)
	}
	code, out, _ := runMain("teardown", "--state")
	if code != 0 || !strings.Contains(out, "would: rm -rf "+home+"/.cache/chezmoi") {
		t.Errorf("--state: exit %d, stdout %q", code, out)
	}
	if _, out, _ := runMain("--help"); !strings.Contains(out, "teardown") {
		t.Error("usage lacks teardown")
	}
}
