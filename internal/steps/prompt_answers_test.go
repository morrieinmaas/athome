package steps_test

import (
	"os"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/steps"
)

// noRead fails the test if the step reads an answer.
type noRead struct{ t *testing.T }

func (n noRead) Read([]byte) (int, error) {
	n.t.Helper()
	n.t.Errorf("asked a question although the answer was preset")
	return 0, nil
}

func presetAsk(t *testing.T, fx *fixture) {
	var out strings.Builder
	fx.Env.Ask = prompt.New(noRead{t}, &out)
}

func TestPresetUploadSshKeys(t *testing.T) {
	t.Run("false: skips like no, without asking", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Cfg.Data["uploadSshKeys"] = "false"
		presetAsk(t, fx)
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 0 {
			t.Errorf("ssh-key add ran:\n%s", fx.dump())
		}
		if !strings.Contains(fx.Log.String(), "skipping ssh-key upload, run manually later:") {
			t.Errorf("no skip text:\n%s", fx.Log)
		}
	})
	t.Run("true: uploads without asking", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Cfg.Data["uploadSshKeys"] = "true"
		presetAsk(t, fx)
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 2 {
			t.Errorf("want 2 adds:\n%s", fx.dump())
		}
	})
}

func TestPresetRestoreSecrets(t *testing.T) {
	setup := func(t *testing.T, v string) *fixture {
		fx := newFixture(t)
		fx.Env.Cfg.Data["useBitwarden"] = "true"
		fx.Env.Cfg.Data["restoreSecrets"] = v
		fx.Fake.Have("rbw")
		seedVault(fx)
		presetAsk(t, fx)
		return fx
	}
	t.Run("false: follow-up, no run, no ask", func(t *testing.T) {
		fx := setup(t, "false")
		fx.MustExec(steps.Finish())
		if restoreRan(fx) {
			t.Errorf("restore ran:\n%s", fx.dump())
		}
		if exists(fx.Path(".local", "state", "athome", "secrets-restore-attempted")) {
			t.Errorf("a decline must not be remembered")
		}
		got, _ := os.ReadFile(steps.FollowupLogPath())
		if !strings.Contains(string(got), "Secrets: run `athome secrets restore` to recover") {
			t.Errorf("no secrets follow-up line, log: %q", got)
		}
	})
	t.Run("true: runs without asking", func(t *testing.T) {
		fx := setup(t, "true")
		fx.MustExec(steps.Finish())
		if !restoreRan(fx) {
			t.Errorf("restore did not run:\n%s", fx.dump())
		}
	})
}
