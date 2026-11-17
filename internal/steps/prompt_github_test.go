package steps_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func pr5GHUpload(t *testing.T, answers string) (*fixture, *bytes.Buffer) {
	fx := ghFixture(t)
	out := pr5Ask(fx, answers)
	return fx, out
}

func TestPromptUpload(t *testing.T) {
	t.Run("n: no ssh-key add, no marker, a rerun asks again", func(t *testing.T) {
		fx, out := pr5GHUpload(t, "n\n")
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 0 {
			t.Errorf("ssh-key add ran:\n%s", fx.dump())
		}
		if !strings.Contains(out.String(), "Upload SSH public keys to GitHub now (auth + signing)?") {
			t.Errorf("no prompt: %q", out)
		}
		if !strings.Contains(fx.Log.String(), "skipping ssh-key upload, run manually later:") {
			t.Errorf("no skip text:\n%s", fx.Log)
		}
		if exists(fx.Path(".local", "state", "athome", "github-upload-declined")) {
			t.Errorf("a decline must not be remembered")
		}
		out2 := pr5Ask(fx, "y\n")
		fx.MustExec(steps.GitHub())
		if !strings.Contains(out2.String(), "Upload SSH public keys to GitHub now (auth + signing)?") {
			t.Errorf("not asked again: %q", out2)
		}
		if len(fx.addCalls()) != 2 {
			t.Errorf("want 2 adds after the second answer:\n%s", fx.dump())
		}
	})
	t.Run("y uploads", func(t *testing.T) {
		fx, _ := pr5GHUpload(t, "y\n")
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 2 {
			t.Errorf("want 2 adds:\n%s", fx.dump())
		}
		fx2 := ghFixture(t) // no terminal: the upload is the default
		fx2.MustExec(steps.GitHub())
		pr5SameRun(t, fx, fx2)
	})
	t.Run("refresh n: manual marker, no gh auth refresh", func(t *testing.T) {
		fx, out := pr5GHUpload(t, "y\nn\n")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.MustExec(steps.GitHub())
		if fx.called("gh", "auth", "refresh") {
			t.Errorf("refresh ran:\n%s", fx.dump())
		}
		if !exists(fx.Path(".local", "state", "athome", "github-manual-upload")) {
			t.Errorf("manual marker missing")
		}
		if !strings.Contains(fx.Log.String(), "github.com/settings/ssh/new") {
			t.Errorf("no manual instructions:\n%s", fx.Log)
		}
		if !strings.Contains(out.String(), "refresh via gh?") {
			t.Errorf("no refresh prompt: %q", out)
		}
		// an env token cannot be refreshed, which is the same fallback without a terminal
		fx2 := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "tok")
		fx2.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx2.MustExec(steps.GitHub())
		pr5SameRun(t, fx, fx2)
	})
	t.Run("refresh n: explains the two ways forward first", func(t *testing.T) {
		fx, out := pr5GHUpload(t, "y\nn\n")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.MustExec(steps.GitHub())
		o := out.String()
		i, j := strings.Index(o, "Two ways forward:"), strings.Index(o, "refresh via gh?")
		if i < 0 || j < i || !strings.Contains(o, "[n] Skip. Bootstrap will print pub keys") {
			t.Errorf("explanation missing or after the prompt: %q", o)
		}
		if strings.Contains(o, "\u2014") {
			t.Errorf("em dash in the explanation: %q", o)
		}
	})
	t.Run("refresh n: a second interactive run asks the upload question again", func(t *testing.T) {
		fx, _ := pr5GHUpload(t, "y\nn\n")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.MustExec(steps.GitHub())
		if !exists(fx.Path(".local", "state", "athome", "github-manual-upload")) {
			t.Fatalf("manual marker missing")
		}
		out2 := pr5Ask(fx, "n\n")
		rep2 := fx.MustExec(steps.GitHub())
		if !strings.Contains(out2.String(), "Upload SSH public keys to GitHub now (auth + signing)?") {
			t.Errorf("not asked again: %q %v\n%s", out2, rep2, fx.Log)
		}
		fx.Env.Ask = nil // the marker still quiets a non-interactive run
		if rep := fx.MustRerun(steps.GitHub()); len(rep.Applied) != 0 {
			t.Errorf("non-interactive rerun applied %v", rep.Applied)
		}
	})
	t.Run("force upload and scopes present: no prompt", func(t *testing.T) {
		fx, out := pr5GHUpload(t, "n\n")
		fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
		fx.MustExec(steps.GitHub())
		if out.Len() != 0 || len(fx.addCalls()) == 0 {
			t.Errorf("prompted %q or no upload:\n%s", out, fx.dump())
		}
	})
}
