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

func TestTeardownKeyPathUsesTheShortHostname(t *testing.T) {
	home := sandbox(t)
	t.Setenv("HOSTNAME", "box.example.org")
	defer cli.SetRunner(runtest.NewFake())()
	code, out, _ := runMain("teardown", "--all")
	want := "would: rm -f " + home + "/.ssh/box_ed25519 " + home + "/.ssh/box_ed25519.pub"
	if code != 0 || !strings.Contains(out, want) {
		t.Errorf("exit %d, want %q in:\n%s", code, want, out)
	}
}
