package steps_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/steps"
)

const (
	localBlob = "AAAAC3NzaC1lZDI1NTE5AAAAIlocalkeyblob"
	localPub  = "ssh-ed25519 " + localBlob + " athome@testhost\n"
	allScopes = "github.com\n  - Token scopes: 'admin:public_key', 'admin:ssh_signing_key', 'repo'\n"
	noScopes  = "github.com\n  - Token scopes: 'repo'\n"
)

var (
	sshProbe       = []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=accept-new", "-T", "git@github.com"}
	apiUserKeys    = []string{"api", "user/keys", "--jq", ".[].key"}
	apiSigningKeys = []string{"api", "user/ssh_signing_keys", "--jq", ".[].key"}
)

// ghFixture: gh installed and authenticated with every scope, a local key with
// a .pub, nothing registered on GitHub yet, and an https origin.
func ghFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("GITHUB_TOKEN", "")
	fx := newFixture(t)
	fx.Fake.Have("gh")
	fx.PutKey("PRIV", localPub)
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: allScopes, Stderr: allScopes})
	fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: "https://github.com/morrieinmaas/athome\n"})
	return fx
}

func (fx *fixture) addCalls() []run.Cmd { return fx.callsTo("gh", "ssh-key", "add") }

func (fx *fixture) signingAdds() []run.Cmd {
	var out []run.Cmd
	for _, c := range fx.addCalls() {
		if containsSeq(c.Args, "--type", "signing") {
			out = append(out, c)
		}
	}
	return out
}

func (fx *fixture) authAdds() []run.Cmd {
	var out []run.Cmd
	for _, c := range fx.addCalls() {
		if !containsSeq(c.Args, "--type", "signing") {
			out = append(out, c)
		}
	}
	return out
}

func TestCISkipsUpload(t *testing.T) {
	fx := ghFixture(t)
	fx.Env.Opts.CI = true
	fx.MustExec(steps.GitHub())
	if len(fx.addCalls()) != 0 {
		t.Errorf("ATHOME_CI must never upload a key:\n%s", fx.dump())
	}
	if fx.called("gh", "config", "set") || fx.called("git", "remote", "set-url") {
		t.Errorf("ATHOME_CI skips the whole block:\n%s", fx.dump())
	}
	if exists(fx.Path(".ssh", "config")) {
		t.Errorf("ATHOME_CI must not seed ~/.ssh/config")
	}
}

func TestNoGh(t *testing.T) {
	t.Run("gh missing", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake = runtest.NewFake()
		fx.Env.Run = fx.Fake
		fx.Fake.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Stdout: testHost + "\n"})
		if _, err := fx.Exec(steps.GitHub()); err != nil {
			t.Fatalf("a missing gh must not fail the run: %v", err)
		}
		if fx.called("gh") || len(fx.addCalls()) != 0 {
			t.Errorf("gh is missing, must not be called:\n%s", fx.dump())
		}
		if fx.Log.Len() == 0 {
			t.Errorf("a skip must leave a log line")
		}
	})
	t.Run("gh not authenticated", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Code: 1})
		if _, err := fx.Exec(steps.GitHub()); err != nil {
			t.Fatalf("an unauthenticated gh must not fail the run: %v", err)
		}
		if len(fx.addCalls()) != 0 {
			t.Errorf("not authenticated, must not upload:\n%s", fx.dump())
		}
		if !strings.Contains(strings.ToLower(fx.Log.String()), "skip") {
			t.Errorf("want a log line saying the upload is skipped, got %q", fx.Log.String())
		}
	})
	t.Run("no public key", func(t *testing.T) {
		fx := ghFixture(t)
		if err := os.Remove(keyPath(fx) + ".pub"); err != nil {
			t.Fatal(err)
		}
		if _, err := fx.Exec(steps.GitHub()); err != nil {
			t.Fatal(err)
		}
		if len(fx.addCalls()) != 0 {
			t.Errorf("no .pub, nothing to upload:\n%s", fx.dump())
		}
	})
}

func TestUploadFresh(t *testing.T) {
	fx := ghFixture(t)
	fx.MustExec(steps.GitHub())

	auth, sign := fx.authAdds(), fx.signingAdds()
	if len(auth) != 1 || len(sign) != 1 {
		t.Fatalf("want one auth and one signing upload, got %d and %d:\n%s", len(auth), len(sign), fx.dump())
	}
	if !containsSeq(auth[0].Args, keyPath(fx)+".pub") || !containsSeq(sign[0].Args, keyPath(fx)+".pub") {
		t.Errorf("both uploads must use %s.pub: %q / %q", keyPath(fx), auth[0].Args, sign[0].Args)
	}
	if !containsSeq(auth[0].Args, "--title") || !containsSeq(sign[0].Args, "--title") {
		t.Errorf("uploads need a --title")
	}
	var signTitle string
	for i, a := range sign[0].Args {
		if a == "--title" && i+1 < len(sign[0].Args) {
			signTitle = sign[0].Args[i+1]
		}
	}
	if !strings.HasSuffix(signTitle, "-sign") {
		t.Errorf("signing title = %q, want <host>-sign", signTitle)
	}
	if !fx.called("gh", "config", "set", "git_protocol", "ssh", "--host", "github.com") {
		t.Errorf("gh default protocol must flip to ssh:\n%s", fx.dump())
	}
}

func TestAlreadyRegistered(t *testing.T) {
	t.Run("auth and signing key both on GitHub", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
		fx.Fake.On("gh", apiSigningKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 0 {
			t.Errorf("a registered key must not be uploaded again:\n%s", fx.dump())
		}
		if !fx.called("gh", apiSigningKeys...) {
			t.Errorf("the signing-key check must still run:\n%s", fx.dump())
		}
	})
	t.Run("auth registered, signing missing uploads only the signing key", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
		fx.MustExec(steps.GitHub())
		if len(fx.authAdds()) != 0 {
			t.Errorf("auth key is registered, no auth upload:\n%s", fx.dump())
		}
		if len(fx.signingAdds()) != 1 {
			t.Errorf("want one signing upload:\n%s", fx.dump())
		}
	})
	t.Run("ssh already authenticates", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
		fx.Fake.On("gh", apiSigningKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
		fx.MustExec(steps.GitHub())
		if len(fx.addCalls()) != 0 {
			t.Errorf("ssh works, no upload:\n%s", fx.dump())
		}
		if !fx.called("gh", apiSigningKeys...) {
			t.Errorf("the signing-key check must still run:\n%s", fx.dump())
		}
	})
}

// git signs with ~/.ssh/id_ed25519.pub when present, so that is the key whose
// registration must be checked and uploaded (B:559-566).
func TestSigningKeyPrefersDefaultKey(t *testing.T) {
	fx := ghFixture(t)
	defBlob := "AAAAC3NzaC1lZDI1NTE5AAAAIdefaultkeyblob"
	writeFile(t, fx.Path(".ssh", "id_ed25519.pub"), "ssh-ed25519 "+defBlob+" me\n", 0o644)
	fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
	fx.Fake.On("gh", apiSigningKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + defBlob + "\n"})
	fx.MustExec(steps.GitHub())
	if len(fx.signingAdds()) != 0 {
		t.Errorf("the default key is the signing key and is registered:\n%s", fx.dump())
	}
}

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

func sshConfigSeed(t *testing.T, fx *fixture) string {
	t.Helper()
	return readFile(t, fx.Path(".ssh", "config"))
}

func TestSeed(t *testing.T) {
	t.Run("creates the github.com block", func(t *testing.T) {
		fx := ghFixture(t)
		fx.MustExec(steps.GitHub())
		got := sshConfigSeed(t, fx)
		for _, want := range []string{"Host github.com", "HostName github.com", "User git", "IdentityFile ~/.ssh/id_ed25519", "IdentityFile ~/.ssh/" + testHost + "_ed25519"} {
			if !strings.Contains(got, want) {
				t.Errorf("~/.ssh/config lacks %q:\n%s", want, got)
			}
		}
		if fi, _ := os.Stat(fx.Path(".ssh", "config")); fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %v, want 0600", fi.Mode().Perm())
		}
	})
	t.Run("appends after existing content", func(t *testing.T) {
		fx := ghFixture(t)
		writeFile(t, fx.Path(".ssh", "config"), "Host other\n    User me\n", 0o600)
		fx.MustExec(steps.GitHub())
		got := sshConfigSeed(t, fx)
		if !strings.HasPrefix(got, "Host other\n    User me\n") || !strings.Contains(got, "Host github.com") {
			t.Errorf("existing config must be kept and the block appended:\n%s", got)
		}
	})
	t.Run("unchanged when the key basename is already present", func(t *testing.T) {
		fx := ghFixture(t)
		existing := "Host github.com\n    IdentityFile ~/.ssh/" + testHost + "_ed25519\n"
		writeFile(t, fx.Path(".ssh", "config"), existing, 0o600)
		fx.MustExec(steps.GitHub())
		if got := sshConfigSeed(t, fx); got != existing {
			t.Errorf("config must not change:\n%s", got)
		}
	})
	t.Run("an existing config is never rewritten", func(t *testing.T) {
		fx := ghFixture(t)
		existing := "Host github.com\n    IdentityFile ~/.ssh/" + testHost + "_ed25519\n"
		path := fx.Path(".ssh", "config")
		writeFile(t, path, existing, 0o600)
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		fx.MustExec(steps.GitHub())
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(old) {
			t.Errorf("config was rewritten: mtime %v, want %v", fi.ModTime(), old)
		}
	})
	t.Run("never seeded when gh is missing", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Code: 1})
		fx.MustExec(steps.GitHub())
		if exists(fx.Path(".ssh", "config")) {
			t.Errorf("no upload, no seed")
		}
	})
}

func TestRemoteToSSH(t *testing.T) {
	cases := []struct{ name, origin, want string }{
		{"https without .git", "https://github.com/morrieinmaas/athome", "git@github.com:morrieinmaas/athome.git"},
		{"https with .git", "https://github.com/morrieinmaas/athome.git", "git@github.com:morrieinmaas/athome.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := ghFixture(t)
			fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: c.origin + "\n"})
			fx.MustExec(steps.GitHub())
			calls := fx.callsTo("git", "remote", "set-url", "origin")
			if len(calls) != 1 || calls[0].Args[len(calls[0].Args)-1] != c.want {
				t.Errorf("want `git remote set-url origin %s`, recorded:\n%s", c.want, fx.dump())
			}
		})
	}
	for _, origin := range []string{"git@github.com:morrieinmaas/athome.git", "https://example.org/x/y", ""} {
		t.Run("untouched "+origin, func(t *testing.T) {
			fx := ghFixture(t)
			fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: origin + "\n"})
			fx.MustExec(steps.GitHub())
			if fx.called("git", "remote", "set-url") {
				t.Errorf("origin %q must be left alone:\n%s", origin, fx.dump())
			}
		})
	}
}

// A re-upload key (sshKeyAction=reupload) is uploaded even when SSH already
// authenticates or another local key is registered (B:424-438, ssh_force_upload).
func TestFreshKeyForcesUpload(t *testing.T) {
	t.Run("ssh already authenticates", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
		fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
		fx.MustExec(steps.GitHub())
		if len(fx.authAdds()) != 1 || len(fx.signingAdds()) != 1 {
			t.Errorf("a fresh key must be uploaded:\n%s", fx.dump())
		}
	})
	t.Run("another local key is registered", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
		other := "AAAAC3NzaC1lZDI1NTE5AAAAIotherkeyblob"
		fx.PutKey("PRIV", localPub)
		writeFile(t, fx.Path(".ssh", "other_ed25519.pub"), "ssh-ed25519 "+other+" x@y\n", 0o644)
		fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + other + "\n"})
		fx.MustExec(steps.GitHub())
		if len(fx.authAdds()) != 1 {
			t.Errorf("a fresh key must be uploaded:\n%s", fx.dump())
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

// Check and Apply share one decision: one ssh probe, one log line.
func TestDecisionComputedOnce(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
	fx.MustExec(steps.GitHub()) // signing key missing, so Apply runs after Check
	if n := len(fx.callsTo("ssh", sshProbe...)); n != 1 {
		t.Errorf("ssh -T ran %d times, want 1:\n%s", n, fx.dump())
	}
	if n := strings.Count(fx.Log.String(), "SSH already authenticates"); n != 1 {
		t.Errorf("decision logged %d times, want 1:\n%s", n, fx.Log)
	}
}

func TestRemoteSetURLFailureIsNotReported(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("git", "-C", fx.Repo, "remote", "set-url", "origin", "git@github.com:morrieinmaas/athome.git").Return(run.Result{Code: 1})
	fx.MustExec(steps.GitHub())
	if strings.Contains(fx.Log.String(), "HTTPS -> SSH") {
		t.Errorf("a failed set-url must not be logged as success:\n%s", fx.Log)
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
	rep := fx.MustExec(steps.GitHub())
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing:\n%s", rep.Applied, fx.Log)
	}
	if n := strings.Count(fx.Log.String(), "gh auth refresh -h github.com -s admin:ssh_signing_key"); n != 2 {
		t.Errorf("the manual signing-key hint printed %d times, want once per run:\n%s", n, fx.Log)
	}
}

// A generated key is not force-uploaded (that is only sshKeyAction=reupload): when
// SSH already authenticates it is skipped with the "upload it later" note (B:615-621).
func TestGeneratedKeyIsNotForceUploaded(t *testing.T) {
	fx := ghFixture(t)
	fx.Env.KeyGenerated = true
	fx.Fake.On("ssh", sshProbe...).Return(run.Result{Stderr: "Hi athome! You've successfully authenticated, but GitHub does not provide shell access.\n", Code: 1})
	fx.Fake.On("gh", apiSigningKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
	fx.MustExec(steps.GitHub())
	if len(fx.addCalls()) != 0 {
		t.Errorf("a generated key must go through the already-authenticates skip:\n%s", fx.dump())
	}
	if want := "gh ssh-key add " + keyPath(fx) + ".pub"; !strings.Contains(fx.Log.String(), want) {
		t.Errorf("want the note %q, got:\n%s", want, fx.Log)
	}
}

// S11: B:629-632, the IdentitiesOnly note when SSH does not use the registered key.
func TestLocalKeyOnGitHubNote(t *testing.T) {
	fx := ghFixture(t)
	other := "AAAAC3NzaC1lZDI1NTE5AAAAIotherkeyblob"
	writeFile(t, fx.Path(".ssh", "elsewhere_ed25519.pub"), "ssh-ed25519 "+other+" me\n", 0o644)
	fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + other + "\n"})
	fx.MustExec(steps.GitHub())
	log := fx.Log.String()
	for _, want := range []string{
		"note: SSH to github.com uses " + testHost + "_ed25519 per ~/.ssh/config; if `git push` over",
		"SSH fails, point github.com at elsewhere_ed25519 or upload " + testHost + "_ed25519.",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

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
func TestManualFallbackNeedsMarkerOrToken(t *testing.T) {
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
	t.Run("GITHUB_TOKEN means done", func(t *testing.T) {
		fx := ghFixture(t)
		t.Setenv("GITHUB_TOKEN", "ghp_dummy")
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: noScopes, Stderr: noScopes})
		rep := fx.MustExec(steps.GitHub())
		if len(rep.Applied) != 0 {
			t.Errorf("env token, want nothing applied, applied=%v", rep.Applied)
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
