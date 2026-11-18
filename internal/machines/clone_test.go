package machines

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func cloneBox(t *testing.T) (*world, Deps) {
	t.Helper()
	w, d, _ := newWorld(t)
	m := acme()
	m.Repos = nil
	if err := Save(d.Home, []Machine{m}); err != nil {
		t.Fatal(err)
	}
	return w, d
}

func TestCloneRecordsEverything(t *testing.T) {
	w, d := cloneBox(t)
	w.box("acme-dev").mise = true
	writeSecret(t, d.Home, "api", ".env", "A=1\n")
	writeSecret(t, d.Home, "api", ".env.production", "P=1\n")
	writeSecret(t, d.Home, "api", ".env.example", "X=\n")

	if err := Clone(t.Context(), d, "acme/api", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"box-mkdir /home/dev/work/acme/worktrees",
		"box-clone git@github.com:acme/api.git /home/dev/work/acme/api",
		"box-mise-install /home/dev/work/acme/api",
		"box-env acme-dev api/.env",
		"box-env acme-dev api/.env.production",
		"backup",
	}
	if missing, ok := inOrder(w.logCopy(), want); !ok {
		t.Fatalf("stuck at %q:\n%s", missing, strings.Join(w.logCopy(), "\n"))
	}
	if n := len(w.ops("box-env")); n != 2 {
		t.Errorf("env uploads: %v", w.ops("box-env"))
	}
	ms, _ := Load(d.Home)
	if !slices.Equal(ms[0].Repos, []string{"api"}) {
		t.Errorf("repos = %v", ms[0].Repos)
	}
	proj, _ := os.ReadFile(filepath.Join(d.Home, ".local", "state", "athome", "remote-projects"))
	if string(proj) != "api\tacme-dev\twork/acme\t/home/dev/work/acme/api\n" {
		t.Errorf("remote-projects: %q", proj)
	}
	cmd := ProjectCommand(ms[0], "api")
	if strings.Join(cmd, " ") != "grofe -r acme-dev new-session -A -s api -c /home/dev/work/acme/api" {
		t.Errorf("command: %v", cmd)
	}
	for _, p := range cmd {
		if strings.HasPrefix(p, d.Home) {
			t.Errorf("laptop path %q in the command", p)
		}
	}
}

func TestCloneWithoutMiseConfigSkipsInstall(t *testing.T) {
	w, d := cloneBox(t)
	if err := Clone(t.Context(), d, "acme/api", "acme-dev"); err != nil {
		t.Fatal(err)
	}
	if len(w.ops("box-mise-install")) != 0 {
		t.Error("mise install without a config file")
	}
}

func TestCloneAgainKeepsOneCloneAndOneRepo(t *testing.T) {
	w, d := cloneBox(t)
	writeSecret(t, d.Home, "api", ".env", "A=1\n")
	for i := 0; i < 2; i++ {
		if err := Clone(t.Context(), d, "acme/api", ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.ops("box-clone")) != 1 {
		t.Errorf("clones: %v", w.ops("box-clone"))
	}
	if len(w.ops("box-env")) != 2 {
		t.Errorf("the .env copies must still happen: %v", w.ops("box-env"))
	}
	if ms, _ := Load(d.Home); !slices.Equal(ms[0].Repos, []string{"api"}) {
		t.Errorf("repos = %v", ms[0].Repos)
	}
	if len(w.ops("backup")) != 1 {
		t.Errorf("backups: %v", w.ops("backup"))
	}
}

func TestParseRepoForms(t *testing.T) {
	for in, want := range map[string]string{
		"acme/api":                        "api",
		"git@github.com:acme/api.git":     "api",
		"git@github.com:acme/api":         "api",
		"https://github.com/acme/api.git": "api",
		"https://github.com/acme/api":     "api",
		"acme/dotted.name.git":            "dotted.name",
	} {
		url, name, err := parseRepo(in)
		if err != nil || name != want || !strings.HasPrefix(url, "git@github.com:acme/") || !strings.HasSuffix(url, ".git") {
			t.Errorf("%s -> %q %q %v", in, url, name, err)
		}
	}
	for _, in := range []string{"", "api", "a/b/c", "https://gitlab.com/a/b", "http://github.com/a/b", "../x", "a/..", "-oProxy/x y", "git@github.com:a/b;rm", "ssh://git@github.com/a/b"} {
		if _, _, err := parseRepo(in); err == nil || !errors.As(err, new(UsageError)) {
			t.Errorf("%q accepted or not a usage error: %v", in, err)
		}
	}
}

func TestCloneMachineSelection(t *testing.T) {
	w, d, _ := newWorld(t)
	a, g := acme(), gpu()
	a.Repos, g.Home, g.Hostname = nil, "/home/ops", "ops"
	Save(d.Home, []Machine{a, g})
	err := Clone(t.Context(), d, "acme/api", "")
	var u UsageError
	if !errors.As(err, &u) || !strings.Contains(err.Error(), "acme-dev, acme-gpu") {
		t.Fatalf("err = %v", err)
	}
	if len(w.sshArgs()) != 0 {
		t.Error("a box was reached")
	}
	if err := Clone(t.Context(), d, "acme/api", "acme-gpu"); err != nil {
		t.Fatal(err)
	}
	if got := w.ops("box-clone"); len(got) != 1 || !strings.HasSuffix(got[0], "/home/ops/work/acme/api") {
		t.Errorf("clone: %v", got)
	}
	if err := Clone(t.Context(), d, "bad", ""); !errors.As(err, &u) {
		t.Errorf("bad form: %v", err)
	}
	if err := Clone(t.Context(), d, "acme/api", "nope"); err == nil || errors.As(err, &u) {
		t.Errorf("unknown machine: %v", err)
	}
}

func TestCloneNoMachines(t *testing.T) {
	_, d, _ := newWorld(t)
	if err := Clone(t.Context(), d, "acme/api", ""); !errors.As(err, new(UsageError)) {
		t.Fatalf("err = %v", err)
	}
}

// The box scripts run for real against a temp HOME.
func TestBoxScriptsOnDisk(t *testing.T) {
	home := t.TempDir()
	if out, err := shRun(t, home, uploadScript, "A = \"1\"\n"); err != nil {
		t.Fatal(out, err)
	}
	p := filepath.Join(home, ".config", "athome", "bootstrap.toml")
	if b, _ := os.ReadFile(p); string(b) != "A = \"1\"\n" {
		t.Errorf("bootstrap.toml: %q", b)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if out, err := shRun(t, home, envCopyScript, "K=v\n", "api", ".env.local"); err != nil {
		t.Fatal(out, err)
	}
	e := filepath.Join(home, ".secrets", "api", ".env.local")
	if fi, err := os.Stat(e); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("env file: %v %v", fi, err)
	}

	ak := filepath.Join(home, ".ssh", "authorized_keys")
	os.MkdirAll(filepath.Dir(ak), 0o700)
	os.WriteFile(ak, []byte("ssh-ed25519 AAAAKEEP laptop\nrestrict ssh-ed25519 AAAADROP bg\nssh-ed25519 AAAAOTHER x\n"), 0o600)
	if out, err := shRun(t, home, dropKeyScript, "", "AAAADROP"); err != nil {
		t.Fatal(out, err)
	}
	if b, _ := os.ReadFile(ak); string(b) != "ssh-ed25519 AAAAKEEP laptop\nssh-ed25519 AAAAOTHER x\n" {
		t.Errorf("authorized_keys: %q", b)
	}
	if fi, _ := os.Stat(ak); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	os.Remove(ak)
	if out, err := shRun(t, home, dropKeyScript, "", "AAAADROP"); err != nil {
		t.Fatalf("missing file must pass: %s %v", out, err)
	}

	proj := filepath.Join(home, "work", "acme", "api")
	os.MkdirAll(proj, 0o755)
	if out, _ := shRun(t, home, miseDetectScript, "", filepath.Dir(proj), "api"); strings.TrimSpace(out) != "" {
		t.Errorf("detected mise: %q", out)
	}
	os.WriteFile(filepath.Join(proj, ".tool-versions"), nil, 0o644)
	if out, _ := shRun(t, home, miseDetectScript, "", filepath.Dir(proj), "api"); strings.TrimSpace(out) != "yes" {
		t.Errorf("not detected: %q", out)
	}
}

func TestCloneAndKeyScriptsOnDisk(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	needKeygen(t)
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	// Neither the user's git config nor a signing setup may reach the fixture.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, c := range [][]string{{"init", "-q", src}, {"-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
	scope := filepath.Join(home, "work", "acme")
	for i := 0; i < 2; i++ { // the second run leaves the clone alone
		if out, err := shRun(t, home, cloneScript, githubKnownHosts, scope, "api", src, "dev"); err != nil {
			t.Fatal(out, err)
		}
	}
	if _, err := os.Stat(filepath.Join(scope, "api", ".git")); err != nil {
		t.Error("not cloned")
	}

	out, err := shRun(t, home, boxKeyScript, "", "dev", "acme-dev")
	if err != nil {
		t.Fatal(out, err)
	}
	pub := strings.TrimSpace(out)
	if checkPub(pub) != nil || !strings.HasSuffix(pub, "athome@acme-dev") {
		t.Fatalf("pub %q", pub)
	}
	if again, _ := shRun(t, home, boxKeyScript, "", "dev", "acme-dev"); strings.TrimSpace(again) != pub {
		t.Error("existing key regenerated")
	}
	if fi, _ := os.Stat(filepath.Join(home, ".ssh", "dev_ed25519")); fi.Mode().Perm() != 0o600 {
		t.Errorf("key mode %v", fi.Mode().Perm())
	}
}

// scope/worktrees is the worktree layout, so no repo may take that name.
func TestCloneRefusesTheWorktreesName(t *testing.T) {
	for _, in := range []string{"acme/worktrees", "git@github.com:acme/worktrees.git", "https://github.com/acme/worktrees"} {
		if _, _, err := parseRepo(in); err == nil || !errors.As(err, new(UsageError)) {
			t.Errorf("%q accepted or not a usage error: %v", in, err)
		}
	}
}

// A box whose ~/.ssh/config chezmoi has not rendered yet must still not trust
// GitHub on first use: the clone pins the host keys first and tells git's ssh
// to read only them.
func TestCloneScriptPinsGitHubHostKeys(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	stub := "#!/bin/sh\necho \"$GIT_SSH_COMMAND\" > \"$HOME/git.ssh\"\ncat \"$HOME/.ssh/athome_github_known_hosts\" > \"$HOME/git.kh\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	scope := filepath.Join(home, "work", "acme")
	if out, err := shRun(t, home, cloneScript, githubKnownHosts, scope, "api", "git@github.com:acme/api.git", "dev"); err != nil {
		t.Fatal(out, err)
	}
	kh := filepath.Join(home, ".ssh", "athome_github_known_hosts")
	if b, _ := os.ReadFile(filepath.Join(home, "git.kh")); string(b) != githubKnownHosts {
		t.Errorf("known_hosts when git ran: %q", b)
	}
	if fi, err := os.Stat(kh); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("known_hosts mode: %v %v", fi, err)
	}
	// git hands GIT_SSH_COMMAND to a shell, so $HOME in it expands there.
	b, _ := os.ReadFile(filepath.Join(home, "git.ssh"))
	cmd := exec.Command("sh", "-c", `eval "set -- $(cat "$HOME/git.ssh")"; printf '%s\n' "$@"`)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%q: %v", b, err)
	}
	// Only the box's own key is offered: no id_ed25519 fallback.
	key := filepath.Join(home, ".ssh", "dev_ed25519")
	for _, want := range []string{"UserKnownHostsFile=" + kh, "StrictHostKeyChecking=yes", "IdentitiesOnly=yes", "-i\n" + key + "\n"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("GIT_SSH_COMMAND lacks %q: %s", want, b)
		}
	}
}
