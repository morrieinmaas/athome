package secrets_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/secrets"
)

const header = "\x00GITCRYPT\x00rest"

type fx struct {
	t           *testing.T
	home, vault string
	fake        *runtest.Fake
	out, errb   bytes.Buffer
	opts        secrets.Opts
}

func newFx(t *testing.T) *fx {
	t.Helper()
	root := t.TempDir()
	f := &fx{t: t, home: filepath.Join(root, "home"), fake: runtest.NewFake()}
	f.vault = filepath.Join(f.home, ".local", "share", "attic")
	f.fake.Have("rbw", "git-crypt", "gh")
	f.opts = secrets.Opts{Home: f.home, Vault: f.vault, Name: "attic", Repo: "alice/attic", Run: f.fake, Out: &f.out, Err: &f.errb}
	return f
}

func (f *fx) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fx) home_(parts ...string) string {
	return filepath.Join(append([]string{f.home}, parts...)...)
}

func (f *fx) withVault() { f.write(filepath.Join(f.vault, ".git", "HEAD"), "ref") }

func (f *fx) calls(name string, args ...string) []run.Cmd {
	var out []run.Cmd
	for _, c := range f.fake.Calls() {
		if c.Name == name && slices.Equal(c.Args[:min(len(args), len(c.Args))], args) {
			out = append(out, c)
		}
	}
	return out
}

func TestBackup(t *testing.T) {
	staged := func(f *fx, names string, magic map[string]string) {
		f.fake.On("git", "diff", "--cached", "--quiet").Return(run.Result{Code: 1})
		f.fake.On("git", "diff", "--cached", "--name-only", "-z").Return(run.Result{Stdout: nul(names)})
		for n, m := range magic {
			f.fake.On("git", "cat-file", "-p", ":"+n).Return(run.Result{Stdout: m})
		}
	}
	t.Run("no vault exits 1 with the hint", func(t *testing.T) {
		f := newFx(t)
		if code := secrets.Backup(context.Background(), f.opts); code != 1 {
			t.Errorf("code = %d", code)
		}
		want := "vault not found at " + f.vault + "; run 'athome secrets restore' to clone it first"
		if !strings.Contains(f.errb.String()+f.out.String(), want) {
			t.Errorf("output %q", f.errb.String()+f.out.String())
		}
	})
	t.Run("mirrors the listed files and skips templates", func(t *testing.T) {
		f := newFx(t)
		f.withVault()
		f.write(f.home_(".secrets", "proj", ".env"), "A=1")
		f.write(f.home_(".secrets", "proj", ".env.local"), "B=2")
		f.write(f.home_(".secrets", "proj", ".env.example"), "C=3")
		f.write(f.home_(".secrets", "proj", ".env.sample"), "C=3")
		f.write(f.home_(".secrets", "proj", ".env.template"), "C=3")
		f.write(f.home_(".secrets", "proj", ".env.dist"), "C=3")
		f.write(f.home_(".aws", "credentials"), "k")
		f.write(f.home_(".aws", "other"), "no")
		f.write(f.home_(".config", "scw", "config.yaml"), "s")
		f.write(f.home_(".config", "stripe", "config.toml"), "t")
		f.write(f.home_(".codeium", "windsurf", "mcp_config.json"), "{}")
		secrets.Backup(context.Background(), f.opts)
		for _, p := range []string{".secrets/proj/.env", ".secrets/proj/.env.local", ".aws/credentials", ".config/scw/config.yaml", ".config/stripe/config.toml", ".codeium/windsurf/mcp_config.json"} {
			if _, err := os.Stat(filepath.Join(f.vault, p)); err != nil {
				t.Errorf("not mirrored: %s", p)
			}
		}
		for _, p := range []string{".env.example", ".env.sample", ".env.template", ".env.dist"} {
			if _, err := os.Stat(filepath.Join(f.vault, ".secrets", "proj", p)); err == nil {
				t.Errorf("template mirrored: %s", p)
			}
		}
		if _, err := os.Stat(filepath.Join(f.vault, ".aws", "other")); err == nil {
			t.Errorf(".aws/other must not be mirrored")
		}
		if len(f.calls("git", "add", "-A")) != 1 {
			t.Errorf("want git add -A")
		}
	})
	t.Run("nothing staged is in sync", func(t *testing.T) {
		f := newFx(t)
		f.withVault()
		if code := secrets.Backup(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d", code)
		}
		if !strings.Contains(f.out.String(), "✓ vault already in sync (no changes)") {
			t.Errorf("out %q", f.out.String())
		}
		if len(f.calls("git", "commit")) != 0 && len(f.calls("git", "-c")) != 0 {
			t.Errorf("must not commit")
		}
	})
	t.Run("plaintext aborts with 2 and no commit", func(t *testing.T) {
		f := newFx(t)
		f.withVault()
		staged(f, ".gitattributes\na/b\nc\n", map[string]string{"a/b": header, "c": "PLAINTEXT=1"})
		if code := secrets.Backup(context.Background(), f.opts); code != 2 {
			t.Errorf("code = %d", code)
		}
		all := f.out.String() + f.errb.String()
		if !strings.Contains(all, "!! NOT ENCRYPTED in index: c") || strings.Contains(all, "NOT ENCRYPTED in index: a/b") ||
			!strings.Contains(all, "ABORT: plaintext detected in staged files") {
			t.Errorf("output %q", all)
		}
		for _, c := range f.fake.Calls() {
			if slices.Contains(c.Args, "commit") || slices.Contains(c.Args, "push") {
				t.Errorf("must not commit or push: %v", c.Args)
			}
		}
	})
	t.Run("encrypted commits with the fixed identity and pushes", func(t *testing.T) {
		f := newFx(t)
		f.withVault()
		staged(f, ".gitattributes\na\n", map[string]string{"a": header})
		f.fake.On("git", "rev-parse", "--short", "HEAD").Return(run.Result{Stdout: "abc1234\n"})
		f.fake.On("git", "diff", "--name-only", "HEAD~1").Return(run.Result{Stdout: "a\n.gitattributes\n"})
		if code := secrets.Backup(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d", code)
		}
		var commit run.Cmd
		for _, c := range f.fake.Calls() {
			if slices.Contains(c.Args, "commit") {
				commit = c
			}
		}
		j := strings.Join(commit.Args, " ")
		if commit.Name != "git" || !strings.Contains(j, "-c user.email=secrets@local -c user.name=secrets-backup commit -qm secrets snapshot ") || commit.Dir != f.vault {
			t.Errorf("commit = %+v", commit)
		}
		if len(f.calls("git", "push", "-q", "origin", "HEAD")) != 1 {
			t.Errorf("want a push")
		}
		if !strings.Contains(f.out.String(), "✓ backed up + pushed (abc1234), 2 file(s) changed") {
			t.Errorf("out %q", f.out.String())
		}
	})
	t.Run("failed push warns and still exits 0", func(t *testing.T) {
		f := newFx(t)
		f.withVault()
		staged(f, "a\n", map[string]string{"a": header})
		f.fake.On("git", "push", "-q", "origin", "HEAD").Return(run.Result{Code: 1})
		f.fake.On("git", "rev-parse", "--short", "HEAD").Return(run.Result{Stdout: "abc1234\n"})
		if code := secrets.Backup(context.Background(), f.opts); code != 0 {
			t.Errorf("code = %d", code)
		}
		if !strings.Contains(f.out.String(), "committed locally (abc1234) but push failed") {
			t.Errorf("out %q", f.out.String())
		}
	})
}

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

func TestFromEnv(t *testing.T) {
	t.Setenv("SECRETS_VAULT", "")
	t.Setenv("SECRETS_VAULT_NAME", "")
	t.Setenv("SECRETS_VAULT_REPO", "")
	t.Setenv("ATHOME_GITHUB_HANDLE", "carol")
	o := secrets.FromEnv("/h", runtest.NewFake(), nil, nil)
	if o.Vault != "/h/.local/share/attic" || o.Name != "attic" || o.Owner != "carol" || o.Repo != "" {
		t.Errorf("defaults %+v", o)
	}
	t.Setenv("SECRETS_VAULT", "/v")
	t.Setenv("SECRETS_VAULT_NAME", "vlt")
	t.Setenv("SECRETS_VAULT_REPO", "x/y")
	o = secrets.FromEnv("/h", runtest.NewFake(), nil, nil)
	if o.Vault != "/v" || o.Name != "vlt" || o.Repo != "x/y" {
		t.Errorf("overrides %+v", o)
	}
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

func TestBackupSkipsDotDirsUnderSecrets(t *testing.T) {
	f := newFx(t)
	f.withVault()
	f.write(f.home_(".secrets", ".hidden", ".env"), "H=1")
	f.write(f.home_(".secrets", "proj", ".env"), "A=1")
	secrets.Backup(context.Background(), f.opts)
	if _, err := os.Stat(filepath.Join(f.vault, ".secrets", ".hidden", ".env")); err == nil {
		t.Error("a dot directory under ~/.secrets was mirrored")
	}
	if _, err := os.Stat(filepath.Join(f.vault, ".secrets", "proj", ".env")); err != nil {
		t.Error("the normal directory was not mirrored")
	}
}

func TestBackupVaultDefaultIgnoresVaultName(t *testing.T) {
	t.Setenv("SECRETS_VAULT", "")
	t.Setenv("SECRETS_VAULT_NAME", "vlt")
	b := secrets.BackupFromEnv("/h", runtest.NewFake(), nil, nil)
	if b.Vault != "/h/.local/share/attic" {
		t.Errorf("backup vault = %q", b.Vault)
	}
	if r := secrets.FromEnv("/h", runtest.NewFake(), nil, nil); r.Vault != "/h/.local/share/vlt" {
		t.Errorf("restore vault = %q", r.Vault)
	}
	t.Setenv("SECRETS_VAULT", "/v")
	if b := secrets.BackupFromEnv("/h", runtest.NewFake(), nil, nil); b.Vault != "/v" {
		t.Errorf("SECRETS_VAULT ignored: %q", b.Vault)
	}
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

// nul turns a newline-separated name list into git's -z output.
func nul(names string) string {
	return strings.ReplaceAll(names, "\n", "\x00")
}

func TestBackupGatesNonASCIINamesUnquoted(t *testing.T) {
	f := newFx(t)
	f.withVault()
	name := ".secrets/prøj/.env"
	f.fake.On("git", "diff", "--cached", "--quiet").Return(run.Result{Code: 1})
	f.fake.On("git", "diff", "--cached", "--name-only", "-z").Return(run.Result{Stdout: ".gitattributes\x00" + name + "\x00"})
	f.fake.On("git", "cat-file", "-p", ":"+name).Return(run.Result{Stdout: "PLAIN=1"})
	if code := secrets.Backup(context.Background(), f.opts); code != 2 {
		t.Errorf("code = %d", code)
	}
	if !strings.Contains(f.out.String(), "!! NOT ENCRYPTED in index: "+name) {
		t.Errorf("out %q", f.out.String())
	}
	if len(f.calls("git", "cat-file", "-p", ":"+name)) != 1 {
		t.Errorf("the file was not checked under its real name: %v", f.fake.Calls())
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

func TestBackupFailedCommitExits1(t *testing.T) {
	f := newFx(t)
	f.withVault()
	f.fake.On("git", "diff", "--cached", "--quiet").Return(run.Result{Code: 1})
	f.fake.On("git", "diff", "--cached", "--name-only", "-z").Return(run.Result{Stdout: "a\x00"})
	f.fake.On("git", "cat-file", "-p", ":a").Return(run.Result{Stdout: header})
	f.opts.Run = failCommit{f.fake}
	if code := secrets.Backup(context.Background(), f.opts); code != 1 {
		t.Errorf("code = %d", code)
	}
	if len(f.calls("git", "push")) != 0 {
		t.Errorf("must not push after a failed commit")
	}
	if !strings.Contains(f.errb.String(), "commit failed") {
		t.Errorf("stderr %q", f.errb.String())
	}
}

type failCommit struct{ run.Runner }

func (r failCommit) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	res, err := r.Runner.Run(ctx, c)
	if slices.Contains(c.Args, "commit") {
		res.Code, res.Stderr = 1, "nothing to commit"
	}
	return res, err
}
