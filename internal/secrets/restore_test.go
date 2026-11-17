package secrets_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/secrets"
)

func (f *fx) vaultWith(files ...string) {
	f.withVault()
	f.fake.On("git", "ls-files", "-z").Return(run.Result{Stdout: strings.Join(files, "\x00") + "\x00"})
}

func TestRestore(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("KEYBYTES"))
	t.Run("clones when the vault is missing", func(t *testing.T) {
		f := newFx(t)
		f.fake.On("git", "ls-files", "-z").Return(run.Result{})
		secrets.Restore(context.Background(), f.opts)
		c := f.calls("gh", "repo", "clone", "alice/attic", f.vault, "--", "-q")
		if len(c) != 1 {
			t.Errorf("want a clone, calls: %v", f.fake.Calls())
		}
		if !strings.Contains(f.out.String(), "cloning alice/attic") {
			t.Errorf("out %q", f.out.String())
		}
	})
	t.Run("pulls an existing vault and ignores a failed pull", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith()
		f.fake.On("git", "pull", "-q", "--ff-only").Return(run.Result{Code: 1})
		if code := secrets.Restore(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d", code)
		}
		if len(f.calls("git", "pull", "-q", "--ff-only")) != 1 || len(f.calls("gh")) != 0 {
			t.Errorf("calls %v", f.fake.Calls())
		}
	})
	t.Run("an unlocked vault copies files at 0600 and hardens ~/.secrets", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith(".gitattributes", ".secrets/proj/.env", ".aws/credentials")
		f.write(filepath.Join(f.vault, ".secrets/proj/.env"), "A=1")
		f.write(filepath.Join(f.vault, ".aws/credentials"), "k")
		f.write(filepath.Join(f.vault, ".gitattributes"), "x")
		if code := secrets.Restore(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d, out %q", code, f.out.String()+f.errb.String())
		}
		for _, p := range []string{".secrets/proj/.env", ".aws/credentials"} {
			fi, err := os.Stat(f.home_(p))
			if err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s: %v %v", p, fi, err)
			}
			if !strings.Contains(f.out.String(), "  restored  "+f.home_(p)) {
				t.Errorf("out %q", f.out.String())
			}
		}
		if _, err := os.Stat(f.home_(".gitattributes")); err == nil {
			t.Errorf(".gitattributes must not be copied")
		}
		for _, d := range []string{".secrets", ".secrets/proj"} {
			if fi, _ := os.Stat(f.home_(d)); fi.Mode().Perm() != 0o700 {
				t.Errorf("%s mode %v", d, fi.Mode().Perm())
			}
		}
		if len(f.calls("rbw")) != 0 {
			t.Errorf("an unlocked vault needs no rbw")
		}
	})
	t.Run("encrypted vault unlocks with the key from rbw and removes the key file", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith(".gitattributes", "a")
		f.write(filepath.Join(f.vault, "a"), header)
		f.fake.On("rbw", "get", "git-crypt/attic-key").Return(run.Result{Stdout: key + "\n"})
		secrets.Restore(context.Background(), f.opts)
		u := f.calls("git-crypt", "unlock")
		if len(u) != 1 || u[0].Dir != f.vault {
			t.Fatalf("unlock calls %v", u)
		}
		if _, err := os.Stat(u[0].Args[1]); err == nil {
			t.Errorf("key file %s left behind", u[0].Args[1])
		}
		if !strings.Contains(f.out.String(), "unlocked vault with key from Bitwarden") {
			t.Errorf("out %q", f.out.String())
		}
	})
	t.Run("key bytes reach git-crypt decoded", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith("a")
		f.write(filepath.Join(f.vault, "a"), header)
		f.fake.On("rbw", "get", "git-crypt/attic-key").Return(run.Result{Stdout: key})
		var got []byte
		f.opts.Run = readKey{f.fake, &got}
		secrets.Restore(context.Background(), f.opts)
		if string(got) != "KEYBYTES" {
			t.Errorf("key file = %q", got)
		}
	})
	t.Run("locked rbw on an encrypted vault exits 1", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith("a")
		f.write(filepath.Join(f.vault, "a"), header)
		f.fake.On("rbw", "unlocked").Return(run.Result{Code: 1})
		if code := secrets.Restore(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
		if !strings.Contains(f.out.String()+f.errb.String(), "rbw is locked; run: rbw unlock, then re-run athome secrets restore") {
			t.Errorf("out %q", f.out.String()+f.errb.String())
		}
		if len(f.calls("git-crypt")) != 0 {
			t.Errorf("must not unlock")
		}
	})
	t.Run("missing rbw reads as locked", func(t *testing.T) {
		f := newFx(t)
		f.fake = runtest.NewFake()
		f.opts.Run = f.fake
		f.vaultWith("a")
		f.write(filepath.Join(f.vault, "a"), header)
		if code := secrets.Restore(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
	})
	t.Run("a failed unlock exits 1, removes the key file and copies nothing", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith("a")
		f.write(filepath.Join(f.vault, "a"), header)
		f.fake.On("rbw", "get", "git-crypt/attic-key").Return(run.Result{Stdout: key})
		f.opts.Run = failUnlock{f.fake}
		if code := secrets.Restore(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
		u := f.calls("git-crypt", "unlock")
		if len(u) != 1 {
			t.Fatalf("calls %v", f.fake.Calls())
		}
		if _, err := os.Stat(u[0].Args[1]); err == nil {
			t.Errorf("key file left behind")
		}
		if _, err := os.Stat(f.home_("a")); err == nil {
			t.Errorf("ciphertext copied")
		}
	})
	t.Run("a copy failure exits 1 and the rest still copy", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith("missing", "b")
		f.write(filepath.Join(f.vault, "b"), "x")
		if code := secrets.Restore(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
		if !strings.Contains(f.out.String(), "  FAIL  missing") || !strings.Contains(f.out.String(), "  restored  "+f.home_("b")) {
			t.Errorf("out %q", f.out.String())
		}
	})
	t.Run("dry-run still pulls and unlocks but writes nothing under HOME", func(t *testing.T) {
		f := newFx(t)
		f.vaultWith(".gitattributes", "a")
		f.write(filepath.Join(f.vault, "a"), header)
		f.fake.On("rbw", "get", "git-crypt/attic-key").Return(run.Result{Stdout: key})
		f.opts.DryRun = true
		if code := secrets.Restore(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d", code)
		}
		if len(f.calls("git", "pull")) != 1 || len(f.calls("git-crypt", "unlock")) != 1 {
			t.Errorf("calls %v", f.fake.Calls())
		}
		if !strings.Contains(f.out.String(), "  would restore  "+f.home_("a")) {
			t.Errorf("out %q", f.out.String())
		}
		if _, err := os.Stat(f.home_("a")); err == nil {
			t.Errorf("dry run wrote a file")
		}
		if _, err := os.Stat(f.home_(".secrets")); err == nil {
			t.Errorf("dry run touched ~/.secrets")
		}
	})
	t.Run("an underivable owner exits 1 without cloning", func(t *testing.T) {
		f := newFx(t)
		f.opts.Repo = ""
		f.opts.Owner = ""
		if code := secrets.Restore(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
		if len(f.calls("gh", "repo")) != 0 {
			t.Errorf("must not clone")
		}
	})
	t.Run("the owner comes from gh when not given", func(t *testing.T) {
		f := newFx(t)
		f.opts.Repo = ""
		f.fake.On("gh", "api", "user", "-q", ".login").Return(run.Result{Stdout: "bob\n"})
		secrets.Restore(context.Background(), f.opts)
		if len(f.calls("gh", "repo", "clone", "bob/attic")) != 1 {
			t.Errorf("calls %v", f.fake.Calls())
		}
	})
}

// readKey records the contents of the key file handed to git-crypt unlock.
type readKey struct {
	run.Runner
	got *[]byte
}

func (r readKey) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	if c.Name == "git-crypt" && len(c.Args) == 2 {
		*r.got, _ = os.ReadFile(c.Args[1])
	}
	return r.Runner.Run(ctx, c)
}

type failUnlock struct{ run.Runner }

func (r failUnlock) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	res, err := r.Runner.Run(ctx, c)
	if c.Name == "git-crypt" {
		res.Code = 1
	}
	return res, err
}

func TestRestoreLsFilesFailureExits1(t *testing.T) {
	f := newFx(t)
	f.withVault()
	f.fake.On("git", "ls-files", "-z").Return(run.Result{Code: 128, Stderr: "fatal: not a git repository"})
	if code := secrets.Restore(context.Background(), f.opts); code != 1 {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(f.errb.String(), "git ls-files failed") {
		t.Errorf("stderr %q", f.errb.String())
	}
}

func TestRestoreVaultWithNoTrackedFiles(t *testing.T) {
	f := newFx(t)
	f.withVault()
	f.fake.On("git", "ls-files", "-z").Return(run.Result{})
	if code := secrets.Restore(context.Background(), f.opts); code != 0 {
		t.Errorf("code = %d", code)
	}
	if _, err := os.Stat(f.home_(".secrets")); err == nil {
		t.Error("restore created ~/.secrets for an empty vault")
	}
	if strings.Contains(f.out.String(), "restored") {
		t.Errorf("out %q", f.out.String())
	}
}

func TestRestoreRepoEnvWinsOverOwnerInClone(t *testing.T) {
	t.Setenv("SECRETS_VAULT", "")
	t.Setenv("SECRETS_VAULT_NAME", "")
	t.Setenv("SECRETS_VAULT_REPO", "org/custom")
	t.Setenv("ATHOME_GITHUB_HANDLE", "carol")
	f := newFx(t)
	o := secrets.FromEnv(f.home, f.fake, &f.out, &f.errb)
	f.fake.On("git", "ls-files", "-z").Return(run.Result{})
	secrets.Restore(context.Background(), o)
	if len(f.calls("gh", "repo", "clone", "org/custom", o.Vault)) != 1 || len(f.calls("gh", "repo", "clone", "carol/attic")) != 0 {
		t.Errorf("calls %v", f.fake.Calls())
	}
}

func TestRestoreCopiesNonASCIINames(t *testing.T) {
	f := newFx(t)
	name := ".secrets/prøj/日本.env"
	f.vaultWith(".gitattributes", name)
	f.write(filepath.Join(f.vault, name), "A=1")
	if code := secrets.Restore(context.Background(), f.opts); code != 0 {
		t.Errorf("code = %d, out %q", code, f.out.String()+f.errb.String())
	}
	if len(f.calls("git", "ls-files", "-z")) != 1 {
		t.Errorf("want git ls-files -z: %v", f.fake.Calls())
	}
	if fi, err := os.Stat(f.home_(name)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("%s: %v %v", name, fi, err)
	}
}
