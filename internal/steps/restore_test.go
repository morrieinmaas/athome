package steps_test

import (
	"io"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func stepNames(list []steps.Step) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func indexOf(list []string, name string) int {
	for i, n := range list {
		if n == name {
			return i
		}
	}
	return -1
}

// restoreRun is the four restore steps followed by the ssh key steps: what
// RestoreAll runs after the baseline, without the machine-wide steps.
func restoreRun() []steps.Step {
	var list []steps.Step
	for _, s := range steps.RestoreAll() {
		if strings.HasPrefix(s.Name, "restore-") {
			list = append(list, s)
		}
	}
	return append(list, steps.SSHKey()...)
}

func keygenCalls(fx *fixture) []run.Cmd { return fx.callsTo("ssh-keygen", "-t") }

func TestRestoreAllOrder(t *testing.T) {
	got := stepNames(steps.RestoreAll())
	want := []string{"baseline-mise", "restore-bitwarden", "restore-gh", "restore-ssh-key", "restore-secrets", "ssh-import-dir", "ssh-keygen", "github-keys"}
	last := -1
	for _, n := range want {
		i := indexOf(got, n)
		if i < 0 || i <= last {
			t.Fatalf("step %s out of order in %v", n, got)
		}
		last = i
	}
	if indexOf(got, "repo-check") != 0 || indexOf(got, "secrets-restore") < indexOf(got, "github-keys") {
		t.Errorf("Prepare must come first and Finish last: %v", got)
	}
}

func TestRestoreLockedNoTerminal(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("rbw")
	fx.Fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
	_, err := fx.Exec(restoreRun())
	if err == nil || !strings.Contains(err.Error(), "rbw is not unlocked: run bw-setup, then re-run athome init --restore") {
		t.Fatalf("err = %v", err)
	}
	if len(keygenCalls(fx)) != 0 {
		t.Errorf("no key may be generated:\n%s", fx.dump())
	}
	if fx.called("bw-setup") {
		t.Errorf("bw-setup must not run without a terminal")
	}
}

func TestRestoreGhNotLoggedInNoTerminal(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("rbw", "gh")
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Code: 1})
	_, err := fx.Exec(restoreRun())
	if err == nil || !strings.Contains(err.Error(), "gh is not logged in: run gh auth login, then re-run athome init --restore") {
		t.Fatalf("err = %v", err)
	}
	if len(keygenCalls(fx)) != 0 {
		t.Errorf("no key may be generated")
	}
}

const twoItems = "ssh/a_ed25519\nssh/b_ed25519\nssh/a_ed25519.pub\nother\n"

func TestRestoreTwoItemsNoTerminal(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("rbw", "gh")
	fx.Fake.On("rbw", "list").Return(run.Result{Stdout: twoItems})
	_, err := fx.Exec(restoreRun())
	if err == nil {
		t.Fatalf("want an error")
	}
	for _, w := range []string{"ssh/a_ed25519", "ssh/b_ed25519", "--import-ssh-bw"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q lacks %q", err, w)
		}
	}
	if len(keygenCalls(fx)) != 0 || fx.called("rbw", "get") {
		t.Errorf("nothing may be restored or generated:\n%s", fx.dump())
	}
}

func TestRestoreNoItem(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("rbw", "gh")
	fx.Fake.On("rbw", "list").Return(run.Result{Stdout: "other\n"})
	_, err := fx.Exec(restoreRun())
	if err == nil || !strings.Contains(err.Error(), "ssh/<old-host>_ed25519") {
		t.Fatalf("err = %v", err)
	}
}

const restoreItem = "ssh/old_ed25519"

func restoreHappy(t *testing.T) *fixture {
	fx := restoreBare(t)
	writeFile(t, fx.Path(".secrets", "repo", ".env"), "A=1\n", 0o600)
	return fx
}

// restoreBare is restoreHappy without a restored vault.
func restoreBare(t *testing.T) *fixture {
	fx := newFixture(t)
	fx.Fake.Have("rbw", "gh")
	fx.Fake.On("rbw", "list").Return(run.Result{Stdout: restoreItem + "\n"})
	fx.Fake.On("rbw", "get", "--field", "notes", restoreItem).Return(run.Result{Stdout: "BW-PRIVATE\n"})
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)+".import-tmp").Return(run.Result{Stdout: pubLine})
	return fx
}

func TestRestoreSingleItem(t *testing.T) {
	fx := restoreHappy(t)
	fx.MustExec(restoreRun())
	if got := readFile(t, keyPath(fx)); !strings.HasPrefix(got, "BW-PRIVATE") {
		t.Errorf("key = %q", got)
	}
	if len(keygenCalls(fx)) != 0 {
		t.Errorf("a restored key must skip ssh-keygen:\n%s", fx.dump())
	}
	if fx.Env.Cfg.Data["sshKeyAction"] != "use" {
		t.Errorf("sshKeyAction = %q, want use", fx.Env.Cfg.Data["sshKeyAction"])
	}
	if !fx.called("chezmoi", "apply") {
		t.Errorf("chezmoi apply must follow the restore:\n%s", fx.dump())
	}
	if fx.called("ssh-keygen", "-t") || fx.called("rbw", "add") {
		t.Errorf("unexpected key generation or backup:\n%s", fx.dump())
	}
}

func TestRestoreChezmoiApplyFailureIsANote(t *testing.T) {
	fx := restoreHappy(t)
	fx.Fake.On("chezmoi", "apply").Return(run.Result{Code: 1})
	fx.MustExec(restoreRun())
}

func TestRestoreImportFlagSkipsList(t *testing.T) {
	fx := restoreHappy(t)
	fx.Env.Opts.ImportSSHBW = restoreItem
	fx.MustExec(restoreRun())
	if fx.called("rbw", "list") {
		t.Errorf("--import-ssh-bw must skip `rbw list`:\n%s", fx.dump())
	}
	if !strings.HasPrefix(readFile(t, keyPath(fx)), "BW-PRIVATE") {
		t.Errorf("the named item was not restored")
	}
}

func TestRestoreKeyPresentIsKept(t *testing.T) {
	fx := restoreHappy(t)
	fx.PutKey("OLD", pubLine)
	rep := fx.MustExec(restoreRun())
	if indexOf(rep.Skipped, "restore-ssh-key") < 0 {
		t.Errorf("restore-ssh-key must be skipped: %+v", rep)
	}
	if fx.called("rbw", "get") || readFile(t, keyPath(fx)) != "OLD" {
		t.Errorf("an existing key is kept:\n%s", fx.dump())
	}
}

func TestRestoreIdKeyCountsAsPresent(t *testing.T) {
	fx := restoreHappy(t)
	writeFile(t, fx.Path(".ssh", "id_ed25519"), "ID", 0o600)
	rep := fx.MustExec(restoreRun())
	if indexOf(rep.Skipped, "restore-ssh-key") < 0 {
		t.Errorf("restore-ssh-key must be skipped: %+v", rep)
	}
}

func TestRestoreNoKeyFileFails(t *testing.T) {
	fx := restoreHappy(t)
	fx.Fake.On("rbw", "get", "--field", "notes", restoreItem).Return(run.Result{Code: 1})
	_, err := fx.Exec(restoreRun())
	if err == nil || len(keygenCalls(fx)) != 0 {
		t.Fatalf("err = %v, keygen calls = %d", err, len(keygenCalls(fx)))
	}
}

func TestRestoreSecrets(t *testing.T) {
	t.Run("done when an env file exists", func(t *testing.T) {
		fx := restoreHappy(t)
		rep := fx.MustExec(restoreRun())
		if indexOf(rep.Skipped, "restore-secrets") < 0 {
			t.Errorf("restore-secrets must be skipped: %+v", rep)
		}
	})
	t.Run("a failing restore fails the run", func(t *testing.T) {
		fx := restoreBare(t) // gh knows no owner, so the vault clone fails
		_, err := fx.Exec(restoreRun())
		if err == nil || !strings.Contains(err.Error(), "restore-secrets") {
			t.Fatalf("err = %v", err)
		}
		if len(keygenCalls(fx)) != 0 {
			t.Errorf("no key may be generated")
		}
	})
}

func TestBWBackupEditsExistingItem(t *testing.T) {
	item := "ssh/" + testHost + "_ed25519"
	fx := newFixture(t)
	fx.Env.Run = keygenWrites{fx.Env.Run}
	fx.Fake.Have("rbw")
	fx.Fake.On("rbw", "list").Return(run.Result{Stdout: item + "\n"})
	fx.MustExec(steps.SSHKey())
	if !fx.called("rbw", "edit", item) || fx.called("rbw", "add") {
		t.Errorf("want rbw edit, not add:\n%s", fx.dump())
	}
}

// gh's login must not offer to generate an ssh key: that would become the
// laptop key before the old one is restored.
func TestRestoreGhLoginMakesNoKey(t *testing.T) {
	fx := newFixture(t)
	fx.Fake.Have("rbw", "gh")
	fx.Fake.On("gh", "auth", "status").Return(run.Result{Code: 1})
	fx.Env.Ask = prompt.New(strings.NewReader(""), io.Discard)
	fx.Exec(restoreRun())
	logins := fx.callsTo("gh", "auth", "login")
	if len(logins) != 1 || strings.Join(logins[0].Args, " ") != "auth login --git-protocol https --skip-ssh-key" {
		t.Errorf("gh auth login calls = %+v", logins)
	}
}
