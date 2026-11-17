package steps_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/steps"
)

func TestPromptRestore(t *testing.T) {
	setup := func(t *testing.T, answers string) (*fixture, *bytes.Buffer, string) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["useBitwarden"] = "true"
		fx.Fake.Have("rbw")
		seedVault(fx)
		return fx, pr5Ask(fx, answers), ""
	}
	t.Run("n: not run, no marker, a rerun asks again", func(t *testing.T) {
		fx, out, _ := setup(t, "n\n")
		fx.MustExec(steps.Finish())
		if restoreRan(fx) {
			t.Errorf("restore ran:\n%s", fx.dump())
		}
		if !strings.Contains(out.String(), "restore your encrypted secrets now (athome secrets restore)?") {
			t.Errorf("no prompt: %q", out)
		}
		if exists(fx.Path(".local", "state", "athome", "secrets-restore-attempted")) {
			t.Errorf("a decline must not be remembered")
		}
		if got, _ := os.ReadFile(steps.FollowupLogPath()); !strings.Contains(string(got), "Secrets: run `athome secrets restore`") {
			t.Errorf("a decline must leave the follow-up line, log %q", got)
		}
		out2 := pr5Ask(fx, "n\n")
		fx.MustExec(steps.Finish())
		if !strings.Contains(out2.String(), "restore your encrypted secrets now (athome secrets restore)?") {
			t.Errorf("not asked again: %q", out2)
		}
	})
	t.Run("marker: interactive rerun asks, non-interactive is done", func(t *testing.T) {
		fx, out, _ := setup(t, "n\n")
		writeFile(t, fx.Path(".local", "state", "athome", "secrets-restore-attempted"), "", 0o644)
		fx.MustExec(steps.Finish())
		if !strings.Contains(out.String(), "restore your encrypted secrets now (athome secrets restore)?") {
			t.Errorf("marker must not silence an interactive rerun: %q", out)
		}
		fx2, _, _ := setup(t, "")
		fx2.Env.Ask = nil
		writeFile(t, fx2.Path(".local", "state", "athome", "secrets-restore-attempted"), "", 0o644)
		fx2.MustExec(steps.Finish())
		if restoreRan(fx2) {
			t.Errorf("marker must keep a non-interactive rerun a no-op:\n%s", fx2.dump())
		}
	})
	t.Run("y: runs", func(t *testing.T) {
		fx, _, _ := setup(t, "y\n")
		fx.MustExec(steps.Finish())
		if !restoreRan(fx) {
			t.Errorf("restore did not run:\n%s", fx.dump())
		}
		fx2, _, _ := setup(t, "")
		fx2.Env.Ask = nil // no terminal: the restore is the default
		fx2.MustExec(steps.Finish())
		pr5SameRun(t, fx, fx2)
	})
	t.Run("secrets present: no prompt", func(t *testing.T) {
		fx, out, _ := setup(t, "n\n")
		writeFile(t, fx.Path(".secrets", "proj", ".env"), "A=1\n", 0o600)
		fx.MustExec(steps.Finish())
		if strings.Contains(out.String(), "restore your") {
			t.Errorf("prompted: %q", out)
		}
	})
}
