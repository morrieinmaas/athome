package steps_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// pr5Ask attaches a scripted Asker and returns its prompt output.
func pr5Ask(fx *fixture, answers string) *bytes.Buffer {
	out := &bytes.Buffer{}
	fx.Env.Ask = prompt.New(strings.NewReader(answers), out)
	return out
}

func pr5Data(t *testing.T, fx *fixture) map[string]any {
	t.Helper()
	return readChezmoiData(t, chezmoiTomlPath(fx))
}

func TestPromptHandle(t *testing.T) {
	t.Run("underivable handle asked", func(t *testing.T) {
		fx := newFixture(t)
		out := pr5Ask(fx, "\nalice\n")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["githubHandle"]; got != "alice" {
			t.Errorf("githubHandle = %v, want alice", got)
		}
		if !strings.Contains(out.String(), "GitHub username: ") {
			t.Errorf("no username prompt: %q", out)
		}
	})
	t.Run("derived by gh: no prompt", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("gh")
		fx.Fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Stdout: "bob\n"})
		out := pr5Ask(fx, "alice\n")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["githubHandle"]; got != "bob" {
			t.Errorf("githubHandle = %v, want bob", got)
		}
		if strings.Contains(out.String(), "GitHub username") {
			t.Errorf("must not ask: %q", out)
		}
	})
	t.Run("EOF leaves it empty", func(t *testing.T) {
		fx := newFixture(t)
		pr5Ask(fx, "")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["githubHandle"]; got != "" {
			t.Errorf("githubHandle = %v, want empty", got)
		}
	})
}

func TestPromptBitwardenURL(t *testing.T) {
	t.Run("empty is US cloud", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["githubHandle"] = "octo"
		out := pr5Ask(fx, "\n")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["bitwardenUrl"]; got != "" {
			t.Errorf("bitwardenUrl = %v, want empty", got)
		}
		if !strings.Contains(out.String(), "Bitwarden server URL [empty = US cloud]: ") {
			t.Errorf("no URL prompt: %q", out)
		}
	})
	t.Run("answer lands in data and is not asked again", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["githubHandle"] = "octo"
		pr5Ask(fx, "https://vault.bitwarden.eu\n")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["bitwardenUrl"]; got != "https://vault.bitwarden.eu" {
			t.Errorf("bitwardenUrl = %v", got)
		}
	})
	t.Run("not asked when preset, or bitwarden is off", func(t *testing.T) {
		for name, data := range map[string]map[string]string{
			"preset": {"githubHandle": "octo", "bitwardenUrl": "https://x"},
			"off":    {"githubHandle": "octo", "useBitwarden": "false"},
		} {
			fx := newFixture(t)
			fx.Env.Cfg.Data = data
			out := pr5Ask(fx, "never\n")
			fx.MustExec(steps.Data())
			if out.Len() != 0 {
				t.Errorf("%s: prompted: %q", name, out)
			}
		}
	})
	t.Run("a check never prompts", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["githubHandle"] = "octo"
		pr5Ask(fx, "https://vault.bitwarden.eu\n")
		fx.MustExec(steps.Data())
		out := pr5Ask(fx, "never\n")
		ctx := t.Context()
		for _, s := range steps.Data() {
			if done, err := s.Check(ctx, fx.Env); err != nil || !done {
				t.Fatalf("second check done=%v err=%v", done, err)
			}
		}
		if out.Len() != 0 {
			t.Errorf("check prompted: %q", out)
		}
	})
}

func pr5BW(t *testing.T, answers string) (*fixture, *bytes.Buffer, string) {
	fx := newFixture(t)
	bw := fx.Path(".local", "bin", "bw-setup")
	writeFile(t, bw, "#!/bin/sh\nexit 0\n", 0o755)
	fx.Fake.Have("rbw")
	fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
	return fx, pr5Ask(fx, answers), bw
}

func TestPromptUseBitwarden(t *testing.T) {
	t.Run("n: local-only, marker, no setup, rerun done", func(t *testing.T) {
		fx, out, bw := pr5BW(t, "n\n")
		fx.MustExec(steps.Finish())
		if fx.mentions(bw) || !strings.Contains(fx.Log.String(), "local-only secrets, Bitwarden skipped") {
			t.Errorf("want local-only, no bw-setup:\n%s\n%s", fx.Log, fx.dump())
		}
		if !strings.Contains(out.String(), "Use Bitwarden (rbw) for secrets sync?") {
			t.Errorf("no prompt: %q", out)
		}
		done, _ := steps.Finish()[3].Check(t.Context(), fx.Env)
		if !done {
			t.Errorf("bitwarden-setup not done after n")
		}
	})
	t.Run("y: runs bw-setup", func(t *testing.T) {
		fx, _, bw := pr5BW(t, "y\n")
		fx.MustExec(steps.Finish())
		if !fx.mentions(bw) {
			t.Errorf("bw-setup did not run:\n%s", fx.dump())
		}
	})
	t.Run("failed bw-setup warns and leaves a follow-up", func(t *testing.T) {
		fx, _, bw := pr5BW(t, "y\n")
		fx.Fake.On(bw).Return(run.Result{Code: 1})
		fx.MustExec(steps.Finish())
		if fx.Env.Warnings != 1 || !strings.Contains(fx.Log.String(), "Bitwarden login didn't complete") {
			t.Errorf("warnings=%d:\n%s", fx.Env.Warnings, fx.Log)
		}
	})
	t.Run("unlocked: no setup", func(t *testing.T) {
		fx, _, bw := pr5BW(t, "y\n")
		fx.Fake.On("rbw", "unlocked").Return(run.Result{})
		fx.MustExec(steps.Finish())
		if fx.mentions(bw) {
			t.Errorf("bw-setup ran while unlocked")
		}
	})
	t.Run("device registered: no setup", func(t *testing.T) {
		fx, _, bw := pr5BW(t, "y\n")
		writeFile(t, fx.Path(".local", "share", "rbw", "device_id"), "x", 0o600)
		t.Setenv("XDG_DATA_HOME", fx.Path(".local", "share"))
		fx.MustExec(steps.Finish())
		if fx.mentions(bw) {
			t.Errorf("bw-setup ran with a registered device:\n%s", fx.Log)
		}
	})
	t.Run("preset or no Ask: no prompt, no setup when off", func(t *testing.T) {
		fx, out, bw := pr5BW(t, "n\n")
		fx.Env.Cfg.Data["useBitwarden"] = "false"
		fx.MustExec(steps.Finish())
		if out.Len() != 0 || fx.mentions(bw) {
			t.Errorf("prompted %q or ran bw-setup", out)
		}
		fx2, _, bw2 := pr5BW(t, "")
		fx2.Env.Ask = nil
		fx2.MustExec(steps.Finish())
		if fx2.mentions(bw2) {
			t.Errorf("bw-setup ran without a terminal")
		}
	})
}

func pr5GHUpload(t *testing.T, answers string) (*fixture, *bytes.Buffer) {
	fx := ghFixture(t)
	out := pr5Ask(fx, answers)
	return fx, out
}

func TestPromptUpload(t *testing.T) {
	t.Run("n: no ssh-key add, and a rerun applies nothing", func(t *testing.T) {
		fx, out := pr5GHUpload(t, "n\n")
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 0 {
			t.Errorf("ssh-key add ran:\n%s", fx.dump())
		}
		if !strings.Contains(out.String(), "Upload SSH public keys to GitHub now (auth + signing)?") {
			t.Errorf("no prompt: %q", out)
		}
		if rep := fx.MustRerun(steps.GitHub()); len(rep.Applied) != 0 {
			t.Errorf("rerun applied %v", rep.Applied)
		}
	})
	t.Run("y uploads", func(t *testing.T) {
		fx, _ := pr5GHUpload(t, "y\n")
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 2 {
			t.Errorf("want 2 adds:\n%s", fx.dump())
		}
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

func TestPromptRestore(t *testing.T) {
	setup := func(t *testing.T, answers string) (*fixture, *bytes.Buffer, string) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["useBitwarden"] = "true"
		fx.Fake.Have("rbw")
		p := fx.Path(".local", "bin", "secrets-restore")
		writeFile(t, p, "#!/bin/sh\nexit 0\n", 0o755)
		return fx, pr5Ask(fx, answers), p
	}
	t.Run("n: not run, rerun applies nothing new", func(t *testing.T) {
		fx, out, p := setup(t, "n\n")
		fx.MustExec(steps.Finish())
		if fx.mentions(p) {
			t.Errorf("restore ran:\n%s", fx.dump())
		}
		if !strings.Contains(out.String(), "restore your encrypted secrets now (secrets-restore)?") {
			t.Errorf("no prompt: %q", out)
		}
		rep := fx.MustRerun(steps.Finish())
		for _, a := range rep.Applied {
			if a == "secrets-restore" {
				t.Errorf("secrets-restore applied again")
			}
		}
	})
	t.Run("y: runs", func(t *testing.T) {
		fx, _, p := setup(t, "y\n")
		fx.MustExec(steps.Finish())
		if !fx.mentions(p) {
			t.Errorf("restore did not run:\n%s", fx.dump())
		}
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
