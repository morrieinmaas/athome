package steps_test

import (
	"bytes"
	"os"
	"reflect"
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

// pr5SameRun fails unless both fixtures recorded the same commands, with the
// per-fixture home and repo paths masked. An interactive answer must do exactly
// what the equivalent preset value does without a terminal. The read-only
// hostname and rbw state probes are dropped: asking makes extra ones.
func pr5SameRun(t *testing.T, ask, preset *fixture) {
	t.Helper()
	norm := func(fx *fixture) string {
		var keep []string
		for _, l := range strings.Split(fx.dump(), "\n") {
			if !strings.HasPrefix(l, "chezmoi execute-template") && l != "rbw unlocked" {
				keep = append(keep, l)
			}
		}
		return strings.NewReplacer(fx.Home, "$HOME", fx.Repo, "$REPO").Replace(strings.Join(keep, "\n"))
	}
	if a, b := norm(ask), norm(preset); a != b {
		t.Errorf("interactive answer and preset recorded different commands\nasked:\n%s\npreset:\n%s", a, b)
	}
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
	t.Run("an empty handle is asked again on the next interactive run", func(t *testing.T) {
		fx := newFixture(t)
		pr5Ask(fx, "")
		fx.MustExec(steps.Data())
		out := pr5Ask(fx, "alice\n")
		fx.MustExec(steps.Data())
		if got := pr5Data(t, fx)["githubHandle"]; got != "alice" {
			t.Errorf("githubHandle = %v, want alice", got)
		}
		if !strings.Contains(out.String(), "GitHub username: ") {
			t.Errorf("not asked again: %q", out)
		}
		for _, s := range steps.Data() { // a check never prompts
			out := pr5Ask(fx, "never\n")
			if done, err := s.Check(t.Context(), fx.Env); err != nil || !done || out.Len() != 0 {
				t.Errorf("check done=%v err=%v prompted %q", done, err, out)
			}
		}
	})
	t.Run("answer equals the preset handle", func(t *testing.T) {
		fx := newFixture(t)
		pr5Ask(fx, "alice\n")
		fx.MustExec(steps.Data())
		fx2 := newFixture(t)
		fx2.Env.Cfg.Data["githubHandle"] = "alice"
		fx2.MustExec(steps.Data())
		pr5SameRun(t, fx, fx2)
		if !reflect.DeepEqual(pr5Data(t, fx), pr5Data(t, fx2)) {
			t.Errorf("data differs: %v vs %v", pr5Data(t, fx), pr5Data(t, fx2))
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
	t.Run("answer equals the preset URL", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["githubHandle"] = "octo"
		pr5Ask(fx, "https://vault.bitwarden.eu\n")
		fx.MustExec(steps.Data())
		fx2 := newFixture(t)
		fx2.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "bitwardenUrl": "https://vault.bitwarden.eu"}
		fx2.MustExec(steps.Data())
		pr5SameRun(t, fx, fx2)
		if !reflect.DeepEqual(pr5Data(t, fx), pr5Data(t, fx2)) {
			t.Errorf("data differs: %v vs %v", pr5Data(t, fx), pr5Data(t, fx2))
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
	t.Run("n: local-only, no setup, no marker, asked again", func(t *testing.T) {
		fx, out, bw := pr5BW(t, "n\n")
		fx.MustExec(steps.Finish())
		if fx.mentions(bw) || !strings.Contains(fx.Log.String(), "local-only secrets, Bitwarden skipped") {
			t.Errorf("want local-only, no bw-setup:\n%s\n%s", fx.Log, fx.dump())
		}
		if !strings.Contains(out.String(), "Use Bitwarden (rbw) for secrets sync?") {
			t.Errorf("no prompt: %q", out)
		}
		fx2, _, _ := pr5BW(t, "")
		fx2.Env.Ask = nil
		fx2.Env.Cfg.Data["useBitwarden"] = "false"
		fx2.MustExec(steps.Finish())
		pr5SameRun(t, fx, fx2)
		if exists(fx.Path(".local", "state", "athome", "bitwarden-declined")) {
			t.Errorf("a decline must not be remembered")
		}
		if done, _ := steps.Finish()[3].Check(t.Context(), fx.Env); done {
			t.Errorf("bitwarden-setup done after n, the next run must ask again")
		}
		out2 := pr5Ask(fx, "n\n")
		fx.MustExec(steps.Finish())
		if !strings.Contains(out2.String(), "Use Bitwarden (rbw) for secrets sync?") {
			t.Errorf("not asked again: %q", out2)
		}
	})
	t.Run("asked even without bw-setup", func(t *testing.T) {
		fx, out, bw := pr5BW(t, "y\n")
		if err := os.Remove(bw); err != nil {
			t.Fatal(err)
		}
		fx.MustExec(steps.Finish())
		if !strings.Contains(out.String(), "Use Bitwarden (rbw) for secrets sync?") {
			t.Errorf("no prompt: %q", out)
		}
		if !strings.Contains(fx.Log.String(), "run `bw-setup` in a terminal to sync Bitwarden later") {
			t.Errorf("no local-only line:\n%s", fx.Log)
		}
	})
	t.Run("y: runs bw-setup, like a preset true on a terminal", func(t *testing.T) {
		fx, _, bw := pr5BW(t, "y\n")
		fx.MustExec(steps.Finish())
		if !fx.mentions(bw) {
			t.Errorf("bw-setup did not run:\n%s", fx.dump())
		}
		// Without a terminal bw-setup never runs, so the equivalent preset
		// keeps a terminal and only skips the question.
		fx2, out2, _ := pr5BW(t, "never\n")
		fx2.Env.Cfg.Data["useBitwarden"] = "true"
		fx2.MustExec(steps.Finish())
		if out2.Len() != 0 {
			t.Errorf("preset prompted: %q", out2)
		}
		pr5SameRun(t, fx, fx2)
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
		if !strings.Contains(fx.Log.String(), "Bitwarden already unlocked") {
			t.Errorf("no unlocked line:\n%s", fx.Log)
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
		if !strings.Contains(fx.Log.String(), "device registered") {
			t.Errorf("no registered line:\n%s", fx.Log)
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
	t.Run("n: not run, no marker, a rerun asks again", func(t *testing.T) {
		fx, out, p := setup(t, "n\n")
		fx.MustExec(steps.Finish())
		if fx.mentions(p) {
			t.Errorf("restore ran:\n%s", fx.dump())
		}
		if !strings.Contains(out.String(), "restore your encrypted secrets now (secrets-restore)?") {
			t.Errorf("no prompt: %q", out)
		}
		if exists(fx.Path(".local", "state", "athome", "secrets-restore-attempted")) {
			t.Errorf("a decline must not be remembered")
		}
		out2 := pr5Ask(fx, "n\n")
		fx.MustExec(steps.Finish())
		if !strings.Contains(out2.String(), "restore your encrypted secrets now (secrets-restore)?") {
			t.Errorf("not asked again: %q", out2)
		}
	})
	t.Run("y: runs", func(t *testing.T) {
		fx, _, p := setup(t, "y\n")
		fx.MustExec(steps.Finish())
		if !fx.mentions(p) {
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
