package secrets_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
	"github.com/morrieinmaas/athome/internal/secrets"
)

// rekeyRig runs git for real and fakes git-crypt, gh and rbw. A fake
// `git-crypt init` installs a clean filter that adds the git-crypt header, so
// the plaintext gate sees ciphertext unless the rig is told otherwise.
type rekeyRig struct {
	t          *testing.T
	home       string
	vault      string
	fake       *runtest.Fake
	out, errb  bytes.Buffer
	calls      []string
	stored     map[string]string // rbw item -> content written through EDITOR
	noEncrypt  bool
	cleanerSh  string
	exportPath string
}

const rekeyDate = "20261117"

func newRekeyRig(t *testing.T) *rekeyRig {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	root := t.TempDir()
	g := &rekeyRig{t: t, fake: runtest.NewFake(), stored: map[string]string{}}
	g.home = filepath.Join(root, "home dir")
	g.vault = filepath.Join(g.home, ".local", "share", "attic")
	g.fake.Have("git-crypt", "rbw", "gh")
	g.fake.On("gh", "repo", "view", "alice/attic-old-"+rekeyDate).Return(run.Result{Code: 1})
	g.fake.On("rbw", "get", "git-crypt/attic-key").Return(run.Result{Stdout: base64.StdEncoding.EncodeToString([]byte("OLDKEY")) + "\n"})
	g.fake.On("rbw", "list").Return(run.Result{Stdout: "git-crypt/attic-key\n"}) // a real vault has its key item
	g.cleanerSh = filepath.Join(root, "clean.sh")
	if err := os.WriteFile(g.cleanerSh, []byte("#!/bin/sh\nprintf '\\000GITCRYPT\\000'\ncat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(g.vault, 0o755); err != nil {
		t.Fatal(err)
	}
	g.git(g.vault, "init", "-q", "-b", "main")
	g.write(".gitattributes", "* filter=git-crypt\n.gitattributes !filter\n")
	g.write(".secrets/proj/.env", "TOKEN=1\n")
	g.write(".aws/credentials", "[default]\n")
	g.git(g.vault, "add", "-A")
	g.git(g.vault, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "init")
	return g
}

func (g *rekeyRig) write(rel, content string) {
	g.t.Helper()
	p := filepath.Join(g.vault, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		g.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rekeyRig) git(dir string, args ...string) {
	g.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if b, err := c.CombinedOutput(); err != nil {
		g.t.Fatalf("git %v: %v\n%s", args, err, b)
	}
}

func (g *rekeyRig) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	g.calls = append(g.calls, c.Name+" "+strings.Join(c.Args, " "))
	switch {
	case c.Name == "git" && c.Args[0] != "pull":
		return run.Exec().Run(ctx, c)
	case c.Name == "git-crypt" && c.Args[0] == "init":
		if !g.noEncrypt {
			g.git(c.Dir, "config", "filter.git-crypt.clean", g.cleanerSh)
		}
	case c.Name == "git-crypt" && c.Args[0] == "export-key":
		g.exportPath = c.Args[1]
		if err := os.WriteFile(c.Args[1], []byte("NEWKEY"), 0o644); err != nil {
			g.t.Fatal(err)
		}
	case c.Name == "rbw" && (c.Args[0] == "add" || c.Args[0] == "edit"):
		g.captureStore(ctx, c)
	}
	return g.fake.Run(ctx, c)
}

func (g *rekeyRig) captureStore(ctx context.Context, c run.Cmd) {
	g.t.Helper()
	var editor string
	for _, e := range c.Env {
		if v, ok := strings.CutPrefix(e, "EDITOR="); ok {
			editor = v
		}
	}
	f := filepath.Join(g.t.TempDir(), "edit")
	if r, err := run.Exec().Run(ctx, run.Cmd{Name: editor, Args: []string{f}}); err != nil || r.Code != 0 {
		g.t.Fatalf("editor failed: %v %v", err, r)
	}
	b, _ := os.ReadFile(f)
	g.stored[c.Args[1]] = string(b)
}

func (g *rekeyRig) LookPath(name string) (string, error) { return g.fake.LookPath(name) }

// ordered returns the recorded calls that match prefixes, in call order.
func (g *rekeyRig) ordered(prefixes ...string) []string {
	var out []string
	for _, c := range g.calls {
		for _, p := range prefixes {
			if strings.HasPrefix(c, p) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (g *rekeyRig) opts(yes, tty bool, in string) secrets.RekeyOpts {
	return secrets.RekeyOpts{
		Opts: secrets.Opts{Home: g.home, Vault: g.vault, Name: "attic", Repo: "alice/attic", Run: g, Out: &g.out, Err: &g.errb},
		Yes:  yes, TTY: tty, In: strings.NewReader(in),
		Now: func() time.Time { return time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC) },
	}
}

func (g *rekeyRig) mutating() []string {
	return g.ordered("gh repo create", "gh repo rename", "gh repo delete", "rbw add", "rbw edit")
}

func TestRekeyHappyPath(t *testing.T) {
	g := newRekeyRig(t)
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 0 {
		t.Fatalf("code %d, stderr %q", code, g.errb.String())
	}
	rk := g.vault + ".rekey"
	want := []string{
		"git init -q -b main",
		"git-crypt init",
		"git-crypt export-key " + filepath.Join(g.home, ".local", "state", "athome", "rekey-"+rekeyDate+".key"),
		"rbw add git-crypt/attic-old-" + rekeyDate + "-key",
		"gh repo rename attic-old-" + rekeyDate + " --repo alice/attic --yes",
		"gh repo create alice/attic --private --source " + rk + " --remote origin --push",
		"rbw edit git-crypt/attic-key",
	}
	got := g.ordered("git init", "git-crypt", "rbw add", "rbw edit", "gh repo create", "gh repo rename")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got, want := g.stored["git-crypt/attic-old-"+rekeyDate+"-key"], base64.StdEncoding.EncodeToString([]byte("OLDKEY"))+"\n\n"; got != want {
		t.Errorf("old key stored %q, want %q", got, want)
	}
	if got, want := g.stored["git-crypt/attic-key"], base64.StdEncoding.EncodeToString([]byte("NEWKEY"))+"\n\n"; got != want {
		t.Errorf("new key stored %q, want %q", got, want)
	}
	if _, err := os.Stat(g.exportPath); err == nil {
		t.Errorf("exported key %s not removed", g.exportPath)
	}
	if _, err := os.Stat(rk); err == nil {
		t.Errorf("%s left behind", rk)
	}
	for _, f := range []string{".gitattributes", ".secrets/proj/.env", ".aws/credentials"} {
		if _, err := os.Stat(filepath.Join(g.vault, f)); err != nil {
			t.Errorf("new vault lacks %s", f)
		}
	}
	b, err := exec.Command("git", "-C", g.vault, "log", "-1", "--format=%an <%ae> %s").Output()
	if err != nil || strings.TrimSpace(string(b)) != "secrets-backup <secrets@local> rekey: new git-crypt key" {
		t.Errorf("commit %q %v", b, err)
	}
	if n, _ := exec.Command("git", "-C", g.vault, "rev-list", "--count", "HEAD").Output(); strings.TrimSpace(string(n)) != "1" {
		t.Errorf("new vault history %q, want one commit", n)
	}
	out := g.out.String()
	for _, s := range []string{"✓ vault re-keyed", "gh repo delete alice/attic-old-" + rekeyDate, "rbw remove git-crypt/attic-old-" + rekeyDate + "-key"} {
		if !strings.Contains(out, s) {
			t.Errorf("output lacks %q: %q", s, out)
		}
	}
	if len(g.ordered("gh repo delete", "rbw remove")) != 0 {
		t.Errorf("rekey ran a deletion")
	}
}

func TestRekeyKeyFileIs0600(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("gh", "repo", "rename", "attic-old-"+rekeyDate, "--repo", "alice/attic", "--yes").Return(run.Result{Code: 1})
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d", code)
	}
	fi, err := os.Stat(g.exportPath)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("exported key: %v %v", fi, err)
	}
	if !strings.Contains(g.errb.String(), g.exportPath) {
		t.Errorf("stderr must name the exported key: %q", g.errb.String())
	}
	if g.stored["git-crypt/attic-key"] != "" {
		t.Errorf("new key stored although the rename failed")
	}
}

func TestRekeyPreconditions(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(g *rekeyRig)
		want  string
	}{
		{"no vault", func(g *rekeyRig) { os.RemoveAll(filepath.Join(g.vault, ".git")) }, "vault not found"},
		{"no git-crypt", func(g *rekeyRig) { g.fake = lacking(g.fake, "git-crypt") }, "git-crypt not found"},
		{"no rbw", func(g *rekeyRig) { g.fake = lacking(g.fake, "rbw") }, "rbw not found"},
		{"rbw locked", func(g *rekeyRig) { g.fake.On("rbw", "unlocked").Return(run.Result{Code: 1}) }, "rbw is locked"},
		{"vault locked", func(g *rekeyRig) { g.write(".aws/credentials", "\x00GITCRYPT\x00x") }, "locked"},
		{"dirty", func(g *rekeyRig) { g.write("new.txt", "x") }, "uncommitted"},
		{"pull fails", func(g *rekeyRig) { g.fake.On("git", "pull", "-q", "--ff-only").Return(run.Result{Code: 1}) }, "pull"},
		{"no owner", func(g *rekeyRig) { g.fake.On("gh", "api", "user", "-q", ".login").Return(run.Result{Code: 1}) }, "owner"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newRekeyRig(t)
			c.setup(g)
			o := g.opts(true, false, "")
			if c.name == "no owner" {
				o.Repo = ""
			}
			if c.name == "vault locked" {
				g.git(g.vault, "add", "-A")
				g.git(g.vault, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "-qm", "locked")
			}
			if code := secrets.Rekey(context.Background(), o); code != 1 {
				t.Fatalf("code %d, stderr %q", code, g.errb.String())
			}
			if !strings.Contains(g.errb.String(), c.want) {
				t.Errorf("stderr %q lacks %q", g.errb.String(), c.want)
			}
			if m := g.mutating(); len(m) != 0 {
				t.Errorf("changed things: %v", m)
			}
			if _, err := os.Stat(g.vault + ".rekey"); err == nil {
				t.Errorf(".rekey was created")
			}
		})
	}
}

// lacking wraps a fake so LookPath fails for one tool.
func lacking(f *runtest.Fake, tool string) *runtest.Fake {
	n := runtest.NewFake()
	for _, t := range []string{"git-crypt", "rbw", "gh"} {
		if t != tool {
			n.Have(t)
		}
	}
	return n
}

func TestRekeyPlaintextGate(t *testing.T) {
	g := newRekeyRig(t)
	g.noEncrypt = true
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 2 {
		t.Fatalf("code %d, stderr %q", code, g.errb.String())
	}
	if !strings.Contains(g.errb.String(), "plaintext detected") {
		t.Errorf("stderr %q", g.errb.String())
	}
	if m := g.mutating(); len(m) != 0 {
		t.Errorf("changed things: %v", m)
	}
}

func TestRekeyCreateFailureNamesUndo(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("gh", "repo", "create", "alice/attic", "--private", "--source", g.vault+".rekey", "--remote", "origin", "--push").Return(run.Result{Code: 1, Stderr: "boom"})
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d", code)
	}
	undo := "gh repo rename attic --repo alice/attic-old-" + rekeyDate + " --yes"
	if !strings.Contains(g.errb.String(), undo) {
		t.Errorf("stderr %q lacks %q", g.errb.String(), undo)
	}
	if del := "gh repo delete alice/attic"; strings.Index(g.errb.String(), del) < 0 || strings.Index(g.errb.String(), del) > strings.Index(g.errb.String(), undo) {
		t.Errorf("stderr %q must name %q before the rename undo", g.errb.String(), del)
	}
	if !strings.Contains(g.errb.String(), g.exportPath) {
		t.Errorf("stderr %q lacks the exported key path", g.errb.String())
	}
	if _, err := os.Stat(g.vault + "/.git"); err != nil {
		t.Errorf("old vault was replaced")
	}
}

func TestRekeyConfirmation(t *testing.T) {
	t.Run("no terminal and no --yes exits 2", func(t *testing.T) {
		g := newRekeyRig(t)
		if code := secrets.Rekey(context.Background(), g.opts(false, false, "y\n")); code != 2 {
			t.Fatalf("code %d", code)
		}
		if !strings.Contains(g.errb.String(), "--yes") || len(g.mutating()) != 0 {
			t.Errorf("stderr %q mutating %v", g.errb.String(), g.mutating())
		}
		if p := g.ordered("git pull"); len(p) != 0 {
			t.Errorf("pulled before the usage check: %v", p)
		}
	})
	t.Run("n cancels", func(t *testing.T) {
		g := newRekeyRig(t)
		if code := secrets.Rekey(context.Background(), g.opts(false, true, "n\n")); code != 0 {
			t.Fatalf("code %d", code)
		}
		if !strings.Contains(g.out.String(), "athome secrets rekey: cancelled, nothing changed") {
			t.Errorf("out %q", g.out.String())
		}
		if len(g.mutating()) != 0 || len(g.ordered("git-crypt")) != 0 {
			t.Errorf("changed things: %v", g.calls)
		}
		if _, err := os.Stat(g.vault + ".rekey"); err == nil {
			t.Errorf(".rekey was created")
		}
		if p := g.ordered("git pull"); len(p) != 0 {
			t.Errorf("pulled although the answer was no: %v", p)
		}
	})
	t.Run("default is no", func(t *testing.T) {
		g := newRekeyRig(t)
		if code := secrets.Rekey(context.Background(), g.opts(false, true, "\n")); code != 0 || len(g.mutating()) != 0 {
			t.Errorf("code %d mutating %v", code, g.mutating())
		}
	})
	t.Run("y proceeds and names the old repo", func(t *testing.T) {
		g := newRekeyRig(t)
		if code := secrets.Rekey(context.Background(), g.opts(false, true, "y\n")); code != 0 {
			t.Fatalf("code %d stderr %q", code, g.errb.String())
		}
		if !strings.Contains(g.out.String(), "re-key alice/attic? The old repo becomes alice/attic-old-"+rekeyDate) {
			t.Errorf("out %q", g.out.String())
		}
	})
}

func TestRekeyLeftoverRekeyDirIsRebuilt(t *testing.T) {
	g := newRekeyRig(t)
	stale := filepath.Join(g.vault+".rekey", "stale.txt")
	os.MkdirAll(filepath.Dir(stale), 0o755)
	os.WriteFile(stale, []byte("x"), 0o600)
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 0 {
		t.Fatalf("code %d stderr %q", code, g.errb.String())
	}
	if _, err := os.Stat(filepath.Join(g.vault, "stale.txt")); err == nil {
		t.Errorf("stale file survived into the new vault")
	}
}

func TestRekeyStoreFailureWritesNothing(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("rbw", "list").Return(run.Result{Code: 1, Stderr: "nope"})
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d", code)
	}
	if len(g.stored) != 0 || len(g.mutating()) != 0 {
		t.Errorf("stored %v calls %v", g.stored, g.mutating())
	}
}

func TestRekeySameDayClashRefuses(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(g *rekeyRig)
		want  string
	}{
		{"bitwarden item", func(g *rekeyRig) {
			g.fake.On("rbw", "list").Return(run.Result{Stdout: "other\ngit-crypt/attic-old-" + rekeyDate + "-key\n"})
		}, "Bitwarden already has git-crypt/attic-old-" + rekeyDate + "-key"},
		{"github repo", func(g *rekeyRig) {
			g.fake.On("gh", "repo", "view", "alice/attic-old-"+rekeyDate).Return(run.Result{Code: 0})
		}, "alice/attic-old-" + rekeyDate + " already exists"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newRekeyRig(t)
			c.setup(g)
			if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
				t.Fatalf("code %d, stderr %q", code, g.errb.String())
			}
			if !strings.Contains(g.errb.String(), c.want) || !strings.Contains(g.errb.String(), "retry tomorrow") {
				t.Errorf("stderr %q lacks %q", g.errb.String(), c.want)
			}
			if m := g.mutating(); len(m) != 0 || len(g.stored) != 0 {
				t.Errorf("changed things: %v %v", m, g.stored)
			}
			if len(g.ordered("git-crypt", "git pull")) != 0 {
				t.Errorf("went on past the clash: %v", g.calls)
			}
		})
	}
}

func TestRekeyNewKeyStoreFailureNamesRetry(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("rbw", "list").Return(run.Result{Stdout: "git-crypt/attic-key\n"})
	g.fake.On("rbw", "edit", "git-crypt/attic-key").Return(run.Result{Code: 1, Stderr: "boom"})
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d", code)
	}
	e := g.errb.String()
	for _, s := range []string{"rbw edit git-crypt/attic-key", g.exportPath, "git-crypt/attic-old-" + rekeyDate + "-key", "alice/attic is live"} {
		if !strings.Contains(e, s) {
			t.Errorf("stderr %q lacks %q", e, s)
		}
	}
	if strings.Contains(e, "gh repo rename") {
		t.Errorf("stderr %q offers a rename undo although the new repo is live", e)
	}
	if strings.Contains(e, base64.StdEncoding.EncodeToString([]byte("NEWKEY"))) {
		t.Errorf("stderr prints the key")
	}
	if _, err := os.Stat(g.exportPath); err != nil {
		t.Errorf("exported key was removed: %v", err)
	}
}

func TestRekeySwapKeepsOldCheckoutUntilReplaced(t *testing.T) {
	t.Run("old dir name taken gets a suffix", func(t *testing.T) {
		g := newRekeyRig(t)
		taken := g.vault + ".old-" + rekeyDate
		if err := os.MkdirAll(taken, 0o755); err != nil {
			t.Fatal(err)
		}
		if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 0 {
			t.Fatalf("code %d stderr %q", code, g.errb.String())
		}
		if _, err := os.Stat(taken); err != nil {
			t.Errorf("a pre-existing %s was removed", taken)
		}
		if _, err := os.Stat(g.vault + ".old-" + rekeyDate + ".1"); err == nil {
			t.Errorf("suffixed old checkout left behind")
		}
	})
	t.Run("a failed swap reports both checkouts and no GitHub undo", func(t *testing.T) {
		g := newRekeyRig(t)
		o := g.opts(true, false, "")
		o.Run = &vanishRekey{rekeyRig: g}
		if code := secrets.Rekey(context.Background(), o); code != 1 {
			t.Fatalf("code %d", code)
		}
		e := g.errb.String()
		if strings.Contains(e, "gh repo rename") || strings.Contains(e, "gh repo delete") {
			t.Errorf("stderr %q names a GitHub undo", e)
		}
		if !strings.Contains(e, g.vault+".old-"+rekeyDate) || !strings.Contains(e, g.vault+".rekey") {
			t.Errorf("stderr %q does not say where each checkout is", e)
		}
		if _, err := os.Stat(filepath.Join(g.vault+".old-"+rekeyDate, ".git")); err != nil {
			t.Errorf("old checkout was destroyed: %v", err)
		}
	})
}

// vanishRekey removes the .rekey checkout once the new key is stored, so the
// final rename fails after the old vault has been moved aside.
type vanishRekey struct{ *rekeyRig }

func (v *vanishRekey) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	r, err := v.rekeyRig.Run(ctx, c)
	if c.Name == "rbw" && (c.Args[0] == "add" || c.Args[0] == "edit") && c.Args[1] == "git-crypt/attic-key" {
		os.RemoveAll(v.vault + ".rekey")
	}
	return r, err
}

func TestRekeyRefusesVaultRepoThatIsNotOwnerSlashName(t *testing.T) {
	for _, c := range []struct{ name, repo, want string }{
		{"repo name differs from the vault name", "alice/vault", "SECRETS_VAULT_REPO names the repo vault but SECRETS_VAULT_NAME is attic"},
		{"https url", "https://github.com/alice/attic", "must be <owner>/<repo>"},
		{"ssh url", "git@github.com:alice/attic", "must be <owner>/<repo>"},
		{"extra slash", "github.com/alice/attic", "must be <owner>/<repo>"},
		{"empty owner", "/attic", "must be <owner>/<repo>"},
		{"empty repo", "alice/", "must be <owner>/<repo>"},
		{"no slash", "attic", "must be <owner>/<repo>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := newRekeyRig(t)
			o := g.opts(true, false, "")
			o.Repo = c.repo
			if code := secrets.Rekey(context.Background(), o); code != 1 {
				t.Fatalf("code %d, stderr %q", code, g.errb.String())
			}
			if !strings.Contains(g.errb.String(), c.want) {
				t.Errorf("stderr %q lacks %q", g.errb.String(), c.want)
			}
			if m := g.mutating(); len(m) != 0 || len(g.ordered("git-crypt", "git pull", "gh repo")) != 0 {
				t.Errorf("went on: %v", g.calls)
			}
			if _, err := os.Stat(g.vault + ".rekey"); err == nil {
				t.Errorf(".rekey was created")
			}
		})
	}
}

func TestRekeyUsesTheConfiguredOwnerAndRepo(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("gh", "repo", "view", "bob/attic-old-"+rekeyDate).Return(run.Result{Code: 1})
	o := g.opts(true, false, "")
	o.Repo = "bob/attic"
	if code := secrets.Rekey(context.Background(), o); code != 0 {
		t.Fatalf("code %d, stderr %q", code, g.errb.String())
	}
	if got := g.ordered("gh repo rename"); len(got) != 1 || got[0] != "gh repo rename attic-old-"+rekeyDate+" --repo bob/attic --yes" {
		t.Errorf("rename calls %v", got)
	}
	if !strings.Contains(g.out.String(), "gh repo delete bob/attic-old-"+rekeyDate) {
		t.Errorf("out %q", g.out.String())
	}
}

func TestRekeyRenameFailureNamesTheStep3Undo(t *testing.T) {
	g := newRekeyRig(t)
	g.fake.On("gh", "repo", "rename", "attic-old-"+rekeyDate, "--repo", "alice/attic", "--yes").Return(run.Result{Code: 1, Stderr: "boom"})
	if code := secrets.Rekey(context.Background(), g.opts(true, false, "")); code != 1 {
		t.Fatalf("code %d", code)
	}
	if undo := "rbw remove git-crypt/attic-old-" + rekeyDate + "-key"; !strings.Contains(g.errb.String(), undo) {
		t.Errorf("stderr %q lacks %q", g.errb.String(), undo)
	}
}
