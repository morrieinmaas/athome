package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

const pubLine = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIderived athome@box\n"

// backup builds an --import-from dir holding ssh/<name>_ed25519 (+ .pub).
func backup(t *testing.T, name, priv, pub string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ssh", name+"_ed25519"), priv, 0o600)
	if pub != "" {
		writeFile(t, filepath.Join(dir, "ssh", name+"_ed25519.pub"), pub, 0o644)
	}
	return dir
}

func keyPath(fx *fixture) string { return fx.Path(".ssh", testHost+"_ed25519") }

func stubDerive(fx *fixture) {
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Stdout: pubLine})
}

func generateCalls(fx *fixture) []run.Cmd { return fx.callsTo("ssh-keygen", "-t", "ed25519") }

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

func TestKeyNoPubDeriveFailureWarns(t *testing.T) {
	fx := newFixture(t)
	fx.PutKey("PRIV", "")
	fx.Fake.On("ssh-keygen", "-y", "-f", keyPath(fx)).Return(run.Result{Code: 1, Stderr: "incorrect passphrase"})
	if _, err := fx.Exec(steps.SSHKey()); err != nil {
		t.Fatalf("a passphrase key whose .pub cannot be derived must not fail the run: %v", err)
	}
	if fx.Env.Warnings != 1 {
		t.Errorf("Warnings = %d, want 1", fx.Env.Warnings)
	}
}

func TestImportBW(t *testing.T) {
	const item = "ssh/otherbox_ed25519"
	t.Run("restores the key from the notes field", func(t *testing.T) {
		fx := newFixture(t)
		fx.Fake.Have("rbw")
		stubDerive(fx)
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
		if !fx.called("ssh-keygen", "-y", "-f", keyPath(fx)) {
			t.Errorf("pub must be derived:\n%s", fx.dump())
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

// Only sshKeyAction=reupload forces the GitHub upload (B:436-438); a generated
// key is merely marked as generated.
func TestKeyFlags(t *testing.T) {
	t.Run("generate does not force the upload", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.SSHKey())
		if fx.Env.FreshKey || !fx.Env.KeyGenerated {
			t.Errorf("FreshKey=%v KeyGenerated=%v, want false and true", fx.Env.FreshKey, fx.Env.KeyGenerated)
		}
	})
	t.Run("reupload forces the upload", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", "ssh-ed25519 AAAA x\n")
		fx.Env.Cfg.Data["sshKeyAction"] = "reupload"
		fx.MustExec(steps.SSHKey())
		if !fx.Env.FreshKey || fx.Env.KeyGenerated {
			t.Errorf("FreshKey=%v KeyGenerated=%v, want true and false", fx.Env.FreshKey, fx.Env.KeyGenerated)
		}
	})
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
