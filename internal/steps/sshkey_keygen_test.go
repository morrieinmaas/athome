package steps_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

// V7: a passphrase key whose .pub cannot be derived logs once, is no warning,
// and the step counts as done, so a second run applies nothing (B parity).
func TestKeyNoPubDeriveFailureOnlyLogs(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", "")
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
	if _, err := fx.Exec(steps.SSHKey()); err != nil {
		t.Fatalf("a passphrase key whose .pub cannot be derived must not fail the run: %v", err)
	}
	if fx.Env.Warnings != 0 {
		t.Errorf("Warnings = %d, an underivable .pub only logs", fx.Env.Warnings)
	}
	if n := strings.Count(fx.Log.String(), "could not derive the public key"); n != 1 {
		t.Errorf("the failure is logged %d times, want once:\n%s", n, fx.Log)
	}
	rep, err := fx.Rerun(steps.SSHKey())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing", rep.Applied)
	}
}

func pubMarker(fx *fixture) string {
	return fx.Path(".local", "state", "athome", "ssh-pub-underivable")
}

// The underivable verdict outlives the process, via a marker naming the key.
func TestUnderivablePubMarker(t *testing.T) {
	t.Run("written with the key path, kept by a rerun", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", "")
		fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
		fx.MustExec(steps.SSHKey())
		if got := strings.TrimSpace(readFile(t, pubMarker(fx))); got != keyPath(fx) {
			t.Errorf("marker = %q, want the key path %q", got, keyPath(fx))
		}
		fx.MustRerun(steps.SSHKey())
		if !exists(pubMarker(fx)) {
			t.Errorf("the marker must survive a rerun while the .pub stays underivable")
		}
	})
	t.Run("removed once a .pub exists", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", "")
		writeFile(t, pubMarker(fx), keyPath(fx)+"\n", 0o644)
		fx.MustExec(steps.SSHKey())
		writeFile(t, keyPath(fx)+".pub", pubLine, 0o644)
		fx.MustRerun(steps.SSHKey())
		if exists(pubMarker(fx)) {
			t.Errorf("a .pub exists, the marker must go")
		}
	})
	t.Run("a marker naming another key does not count", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", "")
		stubDerive(fx)
		writeFile(t, pubMarker(fx), fx.Path(".ssh", "other_ed25519")+"\n", 0o644)
		rep := fx.MustExec(steps.SSHKey())
		if !slices.Contains(rep.Applied, "ssh-pub") {
			t.Errorf("a marker for another key must not skip ssh-pub, applied=%v", rep.Applied)
		}
		if exists(pubMarker(fx)) {
			t.Errorf("a derived .pub must remove the marker")
		}
	})
}

func TestGenerate(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data["githubHandle"] = "athome-ci"
	fx.MustExec(steps.SSHKey())

	calls := generateCalls(fx)
	if len(calls) != 1 {
		t.Fatalf("want exactly one ssh-keygen -t ed25519, got %d:\n%s", len(calls), fx.dump())
	}
	a := calls[0].Args
	if !containsSeq(a, "-t", "ed25519") || !containsSeq(a, "-N", "") || !containsSeq(a, "-f", keyPath(fx)) {
		t.Errorf("args = %q, want -t ed25519 -N \"\" -f %s", a, keyPath(fx))
	}
	if !containsSeq(a, "-C", "athome-ci@"+testHost) {
		t.Errorf("args = %q, want comment athome-ci@%s", a, testHost)
	}
	if fi, err := os.Stat(fx.Path(".ssh")); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("~/.ssh must exist with mode 0700: %v", err)
	}
}

func TestGenerateDisabled(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Cfg.Data["generateSshKey"] = "false"
	fx.MustExec(steps.SSHKey())
	if len(generateCalls(fx)) != 0 {
		t.Errorf("generateSshKey = false must not generate:\n%s", fx.dump())
	}
}

func TestKeyHostFromChezmoi(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Stdout: "other-host\n"})
	fx.MustExec(steps.SSHKey())
	calls := generateCalls(fx)
	if len(calls) != 1 || !containsSeq(calls[0].Args, "-f", fx.Path(".ssh", "other-host_ed25519")) {
		t.Errorf("key must be named after chezmoi's hostname, calls:\n%s", fx.dump())
	}
}

func TestKeyExistingIsKept(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", pubLine)
	fx.MustExec(steps.SSHKey())
	if len(generateCalls(fx)) != 0 || fx.called("ssh-keygen", "-y") {
		t.Errorf("an existing key with a .pub needs no ssh-keygen:\n%s", fx.dump())
	}
	if got := readFile(t, keyPath(fx)); got != "PRIV" {
		t.Errorf("key changed to %q", got)
	}
}

func TestKeyNoPub(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", "")
	stubDerive(fx)
	fx.MustExec(steps.SSHKey())
	if got := readFile(t, keyPath(fx)+".pub"); !strings.Contains(got, "AAAAC3NzaC1lZDI1NTE5AAAAIderived") {
		t.Errorf(".pub = %q, want it regenerated from the private key", got)
	}
	if len(generateCalls(fx)) != 0 {
		t.Errorf("the key itself must be kept, not regenerated:\n%s", fx.dump())
	}
	if got := readFile(t, keyPath(fx)); got != "PRIV" {
		t.Errorf("private key changed to %q", got)
	}
}

func TestKeyActionGenerateBacksUp(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("OLD-PRIV", pubLine)
	fx.Env.Cfg.Data["sshKeyAction"] = "generate"
	fx.MustExec(steps.SSHKey())
	if len(generateCalls(fx)) != 1 {
		t.Fatalf("sshKeyAction = generate must generate:\n%s", fx.dump())
	}
	baks, _ := filepath.Glob(keyPath(fx) + ".[0-9]*.bak")
	if len(baks) != 1 || readFile(t, baks[0]) != "OLD-PRIV" {
		t.Errorf("the old key must be kept as %s.<ts>.bak, found %v", keyPath(fx), baks)
	}
	pubBaks, _ := filepath.Glob(keyPath(fx) + ".pub.*.bak")
	if len(pubBaks) != 1 || readFile(t, pubBaks[0]) != pubLine {
		t.Errorf("the old .pub must be kept as %s.pub.<ts>.bak, found %v", keyPath(fx), pubBaks)
	}
	if !strings.Contains(fx.Log.String(), "sshKeyAction=generate rotates this host's key on every run") {
		t.Errorf("the rotation must be announced:\n%s", fx.Log)
	}
}

// The key host falls back to the same short hostname the GitHub step uses
// (B:93-101), so both steps agree on the key name.
func TestKeyHostFallbackMatchesShortHostname(t *testing.T) {
	t.Setenv("HOSTNAME", "fallbackbox.example.org")
	fx := newFixture(t)
	fx.Fake.On("chezmoi", "execute-template", "{{ .chezmoi.hostname }}").Return(run.Result{Code: 1})
	fx.MustExec(steps.SSHKey())
	want := fx.Path(".ssh", "fallbackbox_ed25519")
	if calls := fx.callsTo("ssh-keygen", "-t", "ed25519", "-f", want); len(calls) != 1 {
		t.Errorf("want ssh-keygen -f %s:\n%s", want, fx.dump())
	}
}

// A generated key is marked as generated; an existing one is not (the GitHub
// step forces the upload for sshKeyAction=reupload, see TestFreshKeyForcesUpload).
func TestKeyFlags(t *testing.T) {
	t.Run("generate does not force the upload", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.SSHKey())
		if !fx.Env.KeyGenerated {
			t.Errorf("KeyGenerated=false, want true")
		}
	})
	t.Run("reupload forces the upload", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", "ssh-ed25519 AAAA x\n")
		fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
		fx.MustExec(steps.SSHKey())
		if fx.Env.KeyGenerated {
			t.Errorf("KeyGenerated=true, want false")
		}
	})
}

// Z3: the check decides from cheap state; the derive runs once, in Apply.
func TestKeyNoPubDerivesOnce(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", "")
	stubDerive(fx)
	fx.MustExec(steps.SSHKey())
	if n := len(fx.callsTo("ssh-keygen", "-y")); n != 1 {
		t.Errorf("ssh-keygen -y ran %d times, want once (in Apply):\n%s", n, fx.dump())
	}
	if got := readFile(t, keyPath(fx)+".pub"); !strings.Contains(got, "AAAAC3NzaC1lZDI1NTE5AAAAIderived") {
		t.Errorf(".pub = %q, want the derived key", got)
	}
}

// C4: a marker that cannot be written is logged, never fatal.
func TestPubMarkerWriteFailureIsLogged(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", "")
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
	writeFile(t, fx.Path(".local"), "in the way", 0o644) // MkdirAll(.local/state/...) fails
	if _, err := fx.Exec(steps.SSHKey()); err != nil {
		t.Fatalf("a marker write failure must not fail the run: %v", err)
	}
	if !strings.Contains(fx.Log.String(), "could not record") {
		t.Errorf("want the marker write failure logged:\n%s", fx.Log)
	}
}
