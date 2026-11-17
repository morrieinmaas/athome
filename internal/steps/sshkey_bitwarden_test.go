package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestImportBW(t *testing.T) {
	const item = "ssh/otherbox_ed25519"
	t.Run("restores the key from the notes field", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp").Return(run.Result{Stdout: pubLine})
		fx.Fake.On("rbw", "get", "--field", "notes", item).Return(run.Result{Stdout: "BW-PRIVATE\n"})
		fx.Env.Opts.ImportSSHBW = item
		fx.MustExec(steps.SSHKey())
		if !fx.called("rbw", "get", "--field", "notes", item) {
			t.Fatalf("no `rbw get --field notes %s`:\n%s", item, fx.dump())
		}
		if got := readFile(t, keyPath(fx)); !strings.HasPrefix(got, "BW-PRIVATE") {
			t.Errorf("key = %q, want the Bitwarden notes", got)
		}
		if fi, _ := os.Stat(keyPath(fx)); fi.Mode().Perm() != 0o600 {
			t.Errorf("key mode = %v, want 0600", fi.Mode().Perm())
		}
		if !fx.called("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp") {
			t.Errorf("pub must be derived from the not yet installed key:\n%s", fx.dump())
		}
		if got := readFile(t, keyPath(fx)+".pub"); got != pubLine {
			t.Errorf(".pub = %q, want the derived key", got)
		}
		if len(generateCalls(fx)) != 0 {
			t.Errorf("no keygen after a successful restore:\n%s", fx.dump())
		}
	})
	t.Run("rbw not installed: no call, no error", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Opts.ImportSSHBW = item
		if _, err := fx.Exec(steps.SSHKey()); err != nil {
			t.Fatalf("a missing rbw is a message, not an error: %v", err)
		}
		if fx.called("rbw", "get") {
			t.Errorf("rbw is not installed, must not be called:\n%s", fx.dump())
		}
	})
	t.Run("an existing key is kept and the log says how to restore", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.PutKey("OLD", localPub)
		fx.Env.Opts.ImportSSHBW = item
		fx.MustExec(steps.SSHKey())
		if fx.called("rbw", "get") || readFile(t, keyPath(fx)) != "OLD" {
			t.Errorf("an existing key must never be overwritten:\n%s", fx.dump())
		}
		want := "existing key kept at " + keyPath(fx) + "; move it away and rerun to restore from Bitwarden"
		if !strings.Contains(fx.Log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, fx.Log)
		}
	})
	t.Run("a note that is no key leaves nothing behind and a rerun retries", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "get", "--field", "notes", item).Return(run.Result{Stdout: "NOT A KEY\n"})
		fail := true
		fx.Env.Run = flakyDerive{fx.Env.Run, &fail}
		fx.Env.Opts.ImportSSHBW = item
		if _, err := fx.Exec(steps.SSHKey()); err == nil {
			t.Fatalf("an unreadable key must fail the import:\n%s", fx.Log)
		}
		if left, _ := filepath.Glob(fx.Path(".ssh", "*")); len(left) != 0 {
			t.Errorf("a failed import left %v in ~/.ssh", left)
		}
		fail = false
		fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp").Return(run.Result{Stdout: pubLine})
		fx.MustExec(steps.SSHKey())
		if got := readFile(t, keyPath(fx)); !strings.HasPrefix(got, "NOT A KEY") {
			t.Errorf("rerun did not retry the import, key = %q", got)
		}
		if exists(keyPath(fx) + ".import-tmp") {
			t.Errorf("temp file left behind")
		}
	})
	t.Run("empty note leaves no key behind", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "get", "--field", "notes", item).Return(run.Result{Code: 1})
		fx.Env.Opts.ImportSSHBW = item
		fx.Exec(steps.SSHKey())
		if exists(keyPath(fx)) {
			if b, _ := os.ReadFile(keyPath(fx)); len(b) == 0 {
				t.Errorf("an empty key file was left behind")
			}
		}
		if fx.Env.Warnings != 0 {
			t.Errorf("Warnings = %d, a failed --import-ssh-bw read only logs (B:409)", fx.Env.Warnings)
		}
	})
}

// noTTYDerive fails a non-interactive `ssh-keygen -y` as a passphrase key does
// without a terminal; the interactive derive goes through to the inner runner.
type noTTYDerive struct{ run.Runner }

func (n noTTYDerive) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "ssh-keygen" && containsSeq(c.Args, "-y") && !c.Interactive {
		return run.Result{Code: 1, Stderr: "incorrect passphrase supplied to decrypt private key"}, nil
	}
	return n.Runner.Run(ctx, c)
}

// A passphrase key in the note is derived with the terminal attached, as for --import-from.
func TestImportBWPassphraseKey(t *testing.T) {
	const item = "ssh/otherbox_ed25519"
	setup := func(t *testing.T) *fixture {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "get", "--field", "notes", item).Return(run.Result{Stdout: "BW-PRIVATE\n"})
		fx.Env.Run = noTTYDerive{deriveSim{fx.Env.Run, fx.Fake}}
		fx.Env.Opts.ImportSSHBW = item
		return fx
	}
	t.Run("the interactive derive installs key and pub", func(t *testing.T) {
		fx := setup(t)
		fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp").Return(run.Result{Stdout: pubLine})
		fx.MustExec(steps.SSHKey())
		if got := readFile(t, keyPath(fx)); !strings.HasPrefix(got, "BW-PRIVATE") {
			t.Errorf("key = %q, want the Bitwarden notes", got)
		}
		if got := readFile(t, keyPath(fx)+".pub"); got != pubLine {
			t.Errorf(".pub = %q, want the derived key", got)
		}
		if left, _ := filepath.Glob(fx.Path(".ssh", "*tmp*")); len(left) != 0 {
			t.Errorf("temp files left behind: %v", left)
		}
	})
	t.Run("a wrong passphrase keeps the key and writes the marker", func(t *testing.T) {
		fx := setup(t)
		fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp").Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
		if _, err := fx.Exec(steps.SSHKey()); err != nil {
			t.Fatalf("an underivable .pub must not fail the import: %v\n%s", err, fx.Log)
		}
		if got := readFile(t, keyPath(fx)); !strings.HasPrefix(got, "BW-PRIVATE") {
			t.Errorf("the key must be kept, got %q", got)
		}
		if got := readFile(t, fx.Path(".local", "state", "athome", "ssh-pub-underivable")); strings.TrimSpace(got) != keyPath(fx) {
			t.Errorf("marker = %q, want the key path", got)
		}
		if left, _ := filepath.Glob(fx.Path(".ssh", "*tmp*")); len(left) != 0 {
			t.Errorf("temp files left behind: %v", left)
		}
	})
}

func TestBWCopyOnlyFresh(t *testing.T) {
	item := "ssh/" + testHost + "_ed25519"

	t.Run("after a fresh generate with rbw unlocked", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.MustExec(steps.SSHKey())
		adds := fx.callsTo("rbw", "add", item)
		if len(adds) != 1 {
			t.Fatalf("want one `rbw add %s`:\n%s", item, fx.dump())
		}
		hasEditor := false
		for _, e := range adds[0].Env {
			if strings.HasPrefix(e, "EDITOR=") {
				hasEditor = true
			}
		}
		if !hasEditor {
			t.Errorf("rbw add needs an EDITOR in its env, got %v", adds[0].Env)
		}
	})
	t.Run("not when the key already existed", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.PutKey("PRIV", pubLine)
		fx.MustExec(steps.SSHKey())
		if fx.called("rbw", "add") {
			t.Errorf("an existing key must not be copied to Bitwarden:\n%s", fx.dump())
		}
	})
	t.Run("not when rbw is locked", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
		fx.MustExec(steps.SSHKey())
		if fx.called("rbw", "add") {
			t.Errorf("a locked rbw must not be asked to add:\n%s", fx.dump())
		}
	})
	t.Run("not when rbw is missing, and no error", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.SSHKey())
		if fx.called("rbw") {
			t.Errorf("rbw is missing, must not be called:\n%s", fx.dump())
		}
	})
}

// C2: `rbw unlock` may ask for the master password on the terminal.
func TestImportBWUnlockIsInteractive(t *testing.T) {
	const item = "ssh/otherbox_ed25519"
	fx := newFixture(t)
	fx.Fake.Have("rbw")
	stubDeriveTmp := keyPath(fx) + ".import-tmp"
	fx.Fake.On("ssh-keygen", "-y", "-f", stubDeriveTmp).Return(run.Result{Stdout: pubLine})
	fx.Fake.On("rbw", "get", "--field", "notes", item).Return(run.Result{Stdout: "BW-PRIVATE\n"})
	fx.Env.Opts.ImportSSHBW = item
	fx.MustExec(steps.SSHKey())
	calls := fx.callsTo("rbw", "unlock")
	if len(calls) != 1 || !calls[0].Interactive {
		t.Errorf("rbw unlock must run interactive: %+v", calls)
	}
}
