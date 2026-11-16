package steps_test

import (
	"context"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// V4: bash counts only chezmoi init, baseline deps and bw-setup as warnings
// (B:916,949,1001); a failed upload, the manual-upload fallback and a failed
// ssh config seed only log.
func TestGitHubFailuresOnlyLog(t *testing.T) {
	t.Run("failed upload", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Run = failAdds{fx.Env.Run}
		fx.MustExec(steps.GitHub())
		if !strings.Contains(fx.Log.String(), "upload failed") {
			t.Fatalf("the failed upload must be logged:\n%s", fx.Log)
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, a failed upload only logs", fx.Env.Warnings)
		}
	})
	t.Run("manual upload fallback", func(t *testing.T) {
		fx := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "ghp_dummy")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.MustExec(steps.GitHub())
		if !strings.Contains(fx.Log.String(), "github.com/settings/ssh/new") {
			t.Fatalf("want manual-upload instructions:\n%s", fx.Log)
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, the manual fallback only logs", fx.Env.Warnings)
		}
	})
	t.Run("failed ssh config seed", func(t *testing.T) {
		fx := ghFixture(t)
		mustMkdir(t, fx.Path(".ssh", "config")) // a directory where the file should be
		fx.MustExec(steps.GitHub())
		if !strings.Contains(fx.Log.String(), "could not seed ~/.ssh/config") {
			t.Fatalf("the failed seed must be logged:\n%s", fx.Log)
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, a failed seed only logs", fx.Env.Warnings)
		}
	})
}

// failAdds makes every `gh ssh-key add` exit 1.
type failAdds struct{ run.Runner }

func (f failAdds) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "gh" && containsSeq(c.Args, "ssh-key", "add") {
		f.Runner.Run(ctx, c)
		return run.Result{Code: 1, Stderr: "nope"}, nil
	}
	return f.Runner.Run(ctx, c)
}

// AC7: the manual-upload fallbacks (an env token without key scopes, a failed
// gh auth refresh) are states the user resolves by hand, so a second run applies
// nothing and prints the instructions again; a failed `gh ssh-key add` is not
// one of them and is retried.
func TestManualUploadFallbackConverges(t *testing.T) {
	t.Run("GITHUB_TOKEN without key scopes", func(t *testing.T) {
		fx := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "ghp_dummy")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.MustExec(steps.GitHub())
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 0 {
			t.Errorf("second run applied %v, want nothing:\n%s", rep.Applied, fx.Log)
		}
		if n := strings.Count(fx.Log.String(), "github.com/settings/ssh/new"); n != 2 {
			t.Errorf("manual instructions printed %d times, want once per run:\n%s", n, fx.Log)
		}
	})
	t.Run("failed gh auth refresh", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.Fake.On("gh", "auth", "refresh", "-h", "github.com", "-s", "admin:public_key,admin:ssh_signing_key").Return(run.Result{Code: 1})
		fx.MustExec(steps.GitHub())
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 0 {
			t.Errorf("second run applied %v, want nothing:\n%s", rep.Applied, fx.Log)
		}
		if n := len(fx.callsTo("gh", "auth", "refresh")); n != 1 {
			t.Errorf("gh auth refresh ran %d times, want 1 (interactive, never from a check)", n)
		}
	})
	t.Run("failed gh ssh-key add is retried", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Run = failAdds{fx.Env.Run}
		fx.MustExec(steps.GitHub())
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 1 {
			t.Errorf("second run applied %v, want the upload retried:\n%s", rep.Applied, fx.Log)
		}
		if n := len(fx.authAdds()); n != 2 {
			t.Errorf("auth key add ran %d times, want 2", n)
		}
	})
}

// Y1: chezmoi's private_config.tmpl writes `<host>_ed25519` into ~/.ssh/config
// on every machine, so that line proves nothing about an earlier manual
// fallback. Only GITHUB_TOKEN or the marker file does.
func TestManualFallbackNeedsMarker(t *testing.T) {
	marker := func(fx *fixture) string {
		return fx.Path(".local", "state", "athome", "github-manual-upload")
	}
	seedConfig := func(t *testing.T, fx *fixture) {
		writeFile(t, fx.Path(".ssh", "config"), "Host github.com\n    IdentityFile ~/.ssh/"+testHost+"_ed25519\n", 0o600)
	}
	t.Run("seeded ssh config alone still tries the refresh", func(t *testing.T) {
		fx := ghFixture(t)
		seedConfig(t, fx)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.Fake.On("gh", "auth", "refresh", "-h", "github.com", "-s", "admin:public_key,admin:ssh_signing_key").Return(run.Result{Code: 1})
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 1 || !fx.called("gh", "auth", "refresh") {
			t.Errorf("want the refresh attempted, applied=%v:\n%s", rep.Applied, fx.dump())
		}
		if !exists(marker(fx)) {
			t.Errorf("a refused refresh must write the marker")
		}
	})
	t.Run("marker means done", func(t *testing.T) {
		fx := ghFixture(t)
		writeFile(t, marker(fx), "", 0o644)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 0 || fx.called("gh", "auth", "refresh") {
			t.Errorf("marker present, want nothing applied, applied=%v:\n%s", rep.Applied, fx.dump())
		}
	})
	t.Run("GITHUB_TOKEN without the marker still runs the first time", func(t *testing.T) {
		fx := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "ghp_dummy")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 1 {
			t.Fatalf("env token, no marker: want the step applied once, applied=%v", rep.Applied)
		}
		if !strings.Contains(fx.Log.String(), "manual SSH key upload") {
			t.Errorf("want the manual upload instructions:\n%s", fx.Log)
		}
		if !fx.called("gh", "config", "set", "git_protocol", "ssh") {
			t.Errorf("want the post-upload git_protocol flip:\n%s", fx.dump())
		}
		if !strings.Contains(readFile(t, fx.Path(".ssh", "config")), testHost+"_ed25519") {
			t.Errorf("want the ssh config seeded")
		}
		if !exists(marker(fx)) {
			t.Errorf("the first run must write the marker")
		}
		rep = fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 0 {
			t.Errorf("second run with the marker: want nothing applied, applied=%v", rep.Applied)
		}
	})
	t.Run("successful upload removes the marker", func(t *testing.T) {
		fx := ghFixture(t)
		writeFile(t, marker(fx), "", 0o644)
		fx.MustExec(steps.GitHub()) // all scopes: the marker is stale, the upload runs
		if len(fx.addCalls()) != 2 {
			t.Fatalf("want the upload to run:\n%s", fx.dump())
		}
		if exists(marker(fx)) {
			t.Errorf("marker must be removed after a successful upload")
		}
	})
}
