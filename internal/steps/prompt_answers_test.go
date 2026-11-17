package steps_test

import (
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
	setup := func(t *testing.T, v string) (*fixture, string) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["useBitwarden"] = "true"
		fx.Env.Cfg.Data["restoreSecrets"] = v
		fx.Fake.Have("rbw")
		p := fx.Path(".local", "bin", "secrets-restore")
		writeFile(t, p, "#!/bin/sh\nexit 0\n", 0o755)
		presetAsk(t, fx)
		return fx, p
	}
	t.Run("false: follow-up, no run, no ask", func(t *testing.T) {
		fx, p := setup(t, "false")
		fx.MustExec(steps.Finish())
		if fx.mentions(p) {
			t.Errorf("restore ran:\n%s", fx.dump())
		}
		if exists(fx.Path(".local", "state", "athome", "secrets-restore-attempted")) {
			t.Errorf("a decline must not be remembered")
		}
	})
	t.Run("true: runs without asking", func(t *testing.T) {
		fx, p := setup(t, "true")
		fx.MustExec(steps.Finish())
		if !fx.mentions(p) {
			t.Errorf("restore did not run:\n%s", fx.dump())
		}
	})
}
