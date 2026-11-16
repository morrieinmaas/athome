package steps_test

import (
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestMissingScopes(t *testing.T) {
	t.Run("GITHUB_TOKEN set: no refresh, manual instructions, no error", func(t *testing.T) {
		fx := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "ghp_dummy")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		if _, err := fx.Exec(steps.GitHub()); err != nil {
			t.Fatalf("must never fail: %v", err)
		}
		if fx.called("gh", "auth", "refresh") {
			t.Errorf("scopes cannot be refreshed on an env token:\n%s", fx.dump())
		}
		if len(fx.addCalls()) != 0 {
			t.Errorf("manual path must not upload:\n%s", fx.dump())
		}
		if !strings.Contains(fx.Log.String(), "github.com/settings/ssh/new") {
			t.Errorf("want manual-upload instructions in the log, got %q", fx.Log.String())
		}
	})
	t.Run("failed refresh falls back to manual, no error", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		fx.Fake.On("gh", "auth", "refresh", "-h", "github.com", "-s", "admin:public_key,admin:ssh_signing_key").Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.GitHub()); err != nil {
			t.Fatalf("a failed refresh must not fail the run: %v", err)
		}
		if !fx.called("gh", "auth", "refresh") {
			t.Errorf("non-interactive default is to try the refresh:\n%s", fx.dump())
		}
		if len(fx.addCalls()) != 0 {
			t.Errorf("after a failed refresh nothing is uploaded:\n%s", fx.dump())
		}
		if !strings.Contains(fx.Log.String(), "github.com/settings/ssh/new") {
			t.Errorf("want manual-upload instructions in the log, got %q", fx.Log.String())
		}
	})
}

// "SSH already works" with no signing scope must still tell the user how to
// upload the signing key (B:575-578).
func TestRegisteredWithoutSigningScopePrintsInstructions(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
	fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
	fx.MustExec(steps.GitHub())
	if !strings.Contains(fx.Log.String(), "gh auth refresh -h github.com -s admin:ssh_signing_key") {
		t.Errorf("want the manual signing-key instructions in the log, got %q", fx.Log.String())
	}
	if len(fx.addCalls()) != 0 {
		t.Errorf("no scope, nothing to upload:\n%s", fx.dump())
	}
}

// The device code from `gh auth refresh` must reach the terminal.
func TestAuthRefreshIsInteractive(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
	fx.MustExec(steps.GitHub())
	calls := fx.callsTo("gh", "auth", "refresh")
	if len(calls) != 1 || !calls[0].Interactive {
		t.Errorf("want one interactive gh auth refresh, got %+v", calls)
	}
}

// AC7: "SSH works, no signing scope" is a state the user resolves by hand, so a
// second run applies nothing for GitHub; the hint still shows on each run.
func TestNoSigningScopeConvergesAfterOneRun(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
	fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
	fx.MustExec(steps.GitHub())
	rep := fx.MustRerun(steps.GitHub())
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing:\n%s", rep.Applied, fx.Log)
	}
	if n := strings.Count(fx.Log.String(), "gh auth refresh -h github.com -s admin:ssh_signing_key"); n != 2 {
		t.Errorf("the manual signing-key hint printed %d times, want once per run:\n%s", n, fx.Log)
	}
}
