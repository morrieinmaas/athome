package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// A home whose name holds glob metacharacters is a literal path: the local key
// is still found among the .pub files.
func TestLocalKeyFoundInHomeWithBracket(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	fx := newFixture(t)
	fx.Home = filepath.Join(filepath.Dir(fx.Home), "[x] home")
	fx.Env.Home = fx.Home
	fx.Fake.Have("gh")
	fx.PutKey("PRIV", localPub)
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Stdout: allScopes, Stderr: allScopes})
	fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: "https://github.com/morrieinmaas/athome\n"})
	fx.Fake.On("gh", apiUserKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
	fx.Fake.On("gh", apiSigningKeys...).Return(run.Result{Stdout: "ssh-ed25519 " + localBlob + "\n"})
	fx.MustExec(steps.GitHub())
	if !strings.Contains(fx.Log.String(), "A local SSH key is already on GitHub") {
		t.Errorf("the local .pub must be found in a home holding `[`:\n%s", fx.Log)
	}
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
