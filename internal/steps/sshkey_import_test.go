package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func TestImportDir(t *testing.T) {
	fx := newFixture(t)
	stubDerive(fx)
	fx.Env.Opts.ImportFrom = backup(t, "oldbox", "PRIVATE-KEY", "")
	fx.Env.Opts.ImportSSH = true

	fx.MustExec(steps.SSHKey())

	if got := readFile(t, keyPath(fx)); got != "PRIVATE-KEY" {
		t.Errorf("imported key = %q, want PRIVATE-KEY", got)
	}
	if fi, _ := os.Stat(keyPath(fx)); fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode = %v, want 0600", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(fx.Path(".ssh")); fi.Mode().Perm() != 0o700 {
		t.Errorf("~/.ssh mode = %v, want 0700", fi.Mode().Perm())
	}
	if !fx.called("ssh-keygen", "-y", "-f", keyPath(fx)) {
		t.Errorf("no `ssh-keygen -y -f <key>` to derive the .pub:\n%s", fx.dump())
	}
	if got := readFile(t, keyPath(fx)+".pub"); !strings.Contains(got, "AAAAC3NzaC1lZDI1NTE5AAAAIderived") {
		t.Errorf(".pub = %q, want the derived key", got)
	}
	if len(generateCalls(fx)) != 0 {
		t.Errorf("a restored key must be reused, not regenerated:\n%s", fx.dump())
	}
}

func TestImportDirWithBracket(t *testing.T) {
	fx := newFixture(t)
	stubDerive(fx)
	odd := filepath.Join(t.TempDir(), "[a] backup")
	writeFile(t, filepath.Join(odd, "ssh", "oldbox_ed25519"), "PRIVATE-KEY", 0o600)
	fx.Env.Opts.ImportFrom = odd
	fx.Env.Opts.ImportSSH = true
	fx.MustExec(steps.SSHKey())
	if got := readFile(t, keyPath(fx)); got != "PRIVATE-KEY" {
		t.Errorf("imported key = %q, want PRIVATE-KEY from a directory holding `[`", got)
	}
}

func TestImportDirKeepsBackupPub(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.ImportFrom = backup(t, "oldbox", "PRIVATE-KEY", "ssh-ed25519 FROMBACKUP x\n")
	fx.Env.Opts.ImportSSH = true
	fx.MustExec(steps.SSHKey())
	if got := readFile(t, keyPath(fx)+".pub"); got != "ssh-ed25519 FROMBACKUP x\n" {
		t.Errorf(".pub = %q, want the backup's pub copied", got)
	}
	if fx.called("ssh-keygen", "-y") {
		t.Errorf("pub exists in the backup, no derive needed:\n%s", fx.dump())
	}
	if fi, _ := os.Stat(keyPath(fx) + ".pub"); fi.Mode().Perm() != 0o644 {
		t.Errorf(".pub mode = %v, want 0644", fi.Mode().Perm())
	}
}

func TestImportTakesAKeyWhateverTheSourceHost(t *testing.T) {
	fx := newFixture(t)
	stubDerive(fx)
	dir := backup(t, "zeta", "ZETA", "")
	writeFile(t, filepath.Join(dir, "ssh", "zeta_ed25519.pub"), "ssh-ed25519 PUB z\n", 0o644)
	fx.Env.Opts.ImportFrom = dir
	fx.Env.Opts.ImportSSH = true
	fx.MustExec(steps.SSHKey())
	if got := readFile(t, keyPath(fx)); got != "ZETA" {
		t.Errorf("key = %q, want ZETA installed under this host's name", got)
	}
}

func TestImportSkipsExisting(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("EXISTING", pubLine)
	fx.Env.Opts.ImportFrom = backup(t, "oldbox", "FROM-BACKUP", "")
	fx.Env.Opts.ImportSSH = true
	fx.MustExec(steps.SSHKey())
	if got := readFile(t, keyPath(fx)); got != "EXISTING" {
		t.Errorf("existing key overwritten with %q", got)
	}
}

func TestImportWithoutImportSSHDoesNothing(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.ImportFrom = backup(t, "oldbox", "FROM-BACKUP", "")
	fx.Env.Opts.ImportSSH = false
	fx.MustExec(steps.SSHKey())
	if exists(keyPath(fx)) {
		t.Errorf("--import-from without --import-ssh must not install a key")
	}
	if len(generateCalls(fx)) == 0 {
		t.Errorf("a fresh key is generated instead:\n%s", fx.dump())
	}
}

func TestImportFromMissingDir(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.ImportFrom = filepath.Join(t.TempDir(), "nope")
	if _, err := fx.Exec(steps.SSHKey()); err == nil {
		t.Fatal("--import-from naming a missing directory must fail")
	}
}

// V5: --import-ssh without --import-from prints a note and the run continues
// with the normal keygen flow (bash skips the import block silently).
func TestImportSSHWithoutImportFromIsANote(t *testing.T) {
	fx := newFixture(t)
	fx.Env.Opts.ImportSSH = true
	fx.MustExec(steps.SSHKey())
	if !strings.Contains(fx.Log.String(), "--import-ssh given without --import-from") {
		t.Errorf("want a note about --import-ssh without --import-from, log:\n%s", fx.Log)
	}
	if len(generateCalls(fx)) == 0 {
		t.Errorf("a fresh key is generated instead:\n%s", fx.dump())
	}
}

// C1: an import without a .pub derives it interactively (a passphrase may be
// asked on the terminal) and, when that fails, behaves like ssh-pub: it logs,
// leaves the underivable marker and does not fail, so run 1 and run 2 agree.
func TestImportDirDeriveIsInteractiveAndFailureOnlyLogs(t *testing.T) {
	fx := newFixture(t)
	stubDerive(fx)
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
	fx.Env.Opts.ImportFrom = backup(t, "oldbox", "PRIVATE-KEY", "")
	fx.Env.Opts.ImportSSH = true

	if _, err := fx.Exec(steps.SSHKey()); err != nil {
		t.Fatalf("an underivable .pub must not fail the import: %v\n%s", err, fx.Log)
	}
	calls := fx.callsTo("ssh-keygen", "-y", "-f", keyPath(fx))
	if len(calls) == 0 || !calls[0].Interactive {
		t.Errorf("the import's ssh-keygen -y must be interactive: %+v", calls)
	}
	if got := readFile(t, keyPath(fx)); got != "PRIVATE-KEY" {
		t.Errorf("key = %q, want the imported key kept", got)
	}
	if !strings.Contains(fx.Log.String(), "could not derive the public key") {
		t.Errorf("the failure must be logged:\n%s", fx.Log)
	}
	if !exists(pubMarker(fx)) {
		t.Errorf("the underivable marker must be written")
	}
	rep, err := fx.Rerun(steps.SSHKey())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(rep.Applied) != 0 {
		t.Errorf("second run applied %v, want nothing", rep.Applied)
	}
}
