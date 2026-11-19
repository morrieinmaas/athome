package machines

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

func acme() Machine {
	return Machine{Name: "acme-dev", Scope: "work/acme", Target: "dev.acme.example", User: "dev", Port: 22, Athome: "v0.4.0", Repos: []string{"api"}, Home: "/home/dev", Hostname: "dev"}
}

func gpu() Machine {
	return Machine{Name: "acme-gpu", Scope: "work/acme", Target: "10.0.0.7", User: "ops", Port: 2222, Jump: "bastion", ForwardAgent: true, Athome: "v0.4.0"}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		field string
		edit  func(*Machine)
	}{
		{"scope", func(m *Machine) { m.Scope = "home/acme" }},
		{"scope", func(m *Machine) { m.Scope = "work" }},
		{"scope", func(m *Machine) { m.Scope = "work/../x" }},
		{"target", func(m *Machine) { m.Target = "dev host" }},
		{"user", func(m *Machine) { m.User = "-oProxyCommand=x" }},
		{"port", func(m *Machine) { m.Port = 70000 }},
		{"repos", func(m *Machine) { m.Repos = []string{"../x"} }},
		{"repos", func(m *Machine) { m.Repos = []string{"a", "a"} }},
		{"name", func(m *Machine) { m.Name = "local" }},
		{"jump", func(m *Machine) { m.Jump = "a b" }},
		{"athome", func(m *Machine) { m.Athome = "0.4" }},
		{"home", func(m *Machine) { m.Home = "relative" }},
		{"hostname", func(m *Machine) { m.Hostname = "a.b" }},
		{"github_account", func(m *Machine) { m.GitHubAccount = "a b" }},
	}
	for _, c := range cases {
		m := acme()
		c.edit(&m)
		err := m.Validate()
		if err == nil || !strings.Contains(err.Error(), c.field) || !strings.Contains(err.Error(), m.Name) {
			t.Errorf("%s: err = %v", c.field, err)
		}
	}
	if err := acme().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryRoundTrip(t *testing.T) {
	t.Setenv("HOSTNAME", "lap")
	home := t.TempDir()
	if ms, err := Load(home); err != nil || len(ms) != 0 {
		t.Fatalf("missing file: %v %v", ms, err)
	}
	b, a := gpu(), acme() // two machines sharing a scope
	if err := Save(home, []Machine{b, a}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "acme-dev" || got[1].Name != "acme-gpu" {
		t.Fatalf("order: %+v", got)
	}
	if got[0].Home != "/home/dev" || got[0].Hostname != "dev" || got[0].Repos[0] != "api" || !got[1].ForwardAgent {
		t.Fatalf("fields lost: %+v", got)
	}
	fi, _ := os.Stat(Path(home))
	di, _ := os.Stat(filepath.Dir(Path(home)))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("modes %v %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	if err := Refresh(home, "laptop_ed25519"); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(SSHDir(home), "config"))
	if err != nil || !strings.Contains(string(cfg), "Host acme-gpu") {
		t.Fatalf("config: %v %s", err, cfg)
	}
	proj, _ := os.ReadFile(projectsPath(home))
	if string(proj) != "api\tacme-dev\twork/acme\t/home/dev/work/acme/api\n" {
		t.Fatalf("projects: %q", proj)
	}
}

func TestLoadRejectsAndNamesFile(t *testing.T) {
	home := t.TempDir()
	write := func(s string) {
		os.MkdirAll(filepath.Dir(Path(home)), 0o700)
		os.WriteFile(Path(home), []byte(s), 0o600)
	}
	write("[machines.acme-dev]\nscope = \"work\"\ntarget = \"t\"\nuser = \"dev\"\nathome = \"v1.0.0\"\n")
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "acme-dev") || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("err = %v", err)
	}
	write("[machines.x\nbroken")
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "machines.toml") {
		t.Fatalf("err = %v", err)
	}
	write("[machines.acme-dev]\nscope = \"work/acme\"\nscope = \"work/b\"\n")
	if _, err := Load(home); err == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestCheckLocalScope(t *testing.T) {
	home := t.TempDir()
	mk := func(p string) { os.MkdirAll(filepath.Join(home, p), 0o755) }
	if err := CheckLocalScope(home, "work/acme"); err != nil {
		t.Fatal(err)
	}
	mk("work/acme/r")
	if err := CheckLocalScope(home, "work/acme"); err != nil {
		t.Fatal(err)
	}
	mk("work/acme/a/b/c/d/.git") // .git at depth 5
	if err := CheckLocalScope(home, "work/acme"); err != nil {
		t.Fatalf("depth 5: %v", err)
	}
	mk("work/acme/r/.git")
	err := CheckLocalScope(home, "work/acme")
	want := "~/work/acme holds git repositories (~/work/acme/r/.git); a scope is local or remote, never both"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if w := ScopeWarnings(home, []Machine{acme(), gpu()}); len(w) != 1 || w[0] != want {
		t.Fatalf("warnings %v", w)
	}
}

func TestSSHConfigGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/ssh-config.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := SSHConfig([]Machine{acme(), gpu()}, "laptop_ed25519"); got != string(want) {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRefreshIncludesPendingAndNameOrder(t *testing.T) {
	home := t.TempDir()
	if err := SavePending(home, gpu()); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPending(home, "acme-gpu")
	if err != nil || p == nil || p.Jump != "bastion" || p.Name != "acme-gpu" {
		t.Fatalf("pending %+v %v", p, err)
	}
	fi, _ := os.Stat(filepath.Join(pendingDir(home), "acme-gpu.toml"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if err := Save(home, []Machine{acme()}); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(home, "laptop_ed25519"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(SSHDir(home), "config"))
	if strings.Index(string(cfg), "Host acme-dev") > strings.Index(string(cfg), "Host acme-gpu") || !strings.Contains(string(cfg), "Host acme-gpu") {
		t.Fatalf("config:\n%s", cfg)
	}
	di, _ := os.Stat(SSHDir(home))
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", di.Mode().Perm())
	}
	if err := RemovePending(home, "acme-gpu"); err != nil {
		t.Fatal(err)
	}
	if p, err := LoadPending(home, "acme-gpu"); p != nil || err != nil {
		t.Fatalf("after remove: %v %v", p, err)
	}
	if err := RemovePending(home, "acme-gpu"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPending(home, "../x"); err == nil {
		t.Fatal("path traversal name accepted")
	}
}

func TestCheckInclude(t *testing.T) {
	home := t.TempDir()
	want := "~/.ssh/config does not include athome/config; run chezmoi apply ~/.ssh/config"
	if err := CheckInclude(home); err == nil || err.Error() != want {
		t.Fatalf("missing file: %v", err)
	}
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	cfg := filepath.Join(home, ".ssh", "config")
	os.WriteFile(cfg, []byte("Host x\n# Include athome/config\n"), 0o600)
	if err := CheckInclude(home); err == nil {
		t.Fatal("comment counted")
	}
	os.WriteFile(cfg, []byte("Include config.local\nInclude athome/config\nHost *\n"), 0o600)
	if err := CheckInclude(home); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(cfg, []byte("Include athome/*.conf\nHost *\n    Include config.d/*\n"), 0o600)
	if err := CheckInclude(home); err != nil {
		t.Fatalf("the chezmoi hook line: %v", err)
	}
}

func TestProjects(t *testing.T) {
	m := acme()
	got := strings.Join(ProjectCommand(m, "api"), " ")
	if got != "grofe -r acme-dev new-session -A -s api -c /home/dev/work/acme/api" {
		t.Fatal(got)
	}
}

// sshFake records ssh calls and answers every one with reply. The seam check
// (SUPERLITE_REAL=1) has nothing real to swap in: these tests assert the exact
// argv and stdin, and a real ssh would leave the machine.
type sshFake struct {
	mu    sync.Mutex
	calls []run.Cmd
	reply run.Result
}

func newRunner(t *testing.T) *sshFake { t.Helper(); return &sshFake{} }

func (f *sshFake) Run(_ context.Context, c run.Cmd) (run.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	return f.reply, nil
}

func (f *sshFake) LookPath(string) (string, error) { return "", exec.ErrNotFound }

func (f *sshFake) Calls() []run.Cmd { return f.calls }

func (f *sshFake) last(t *testing.T) run.Cmd {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("no calls")
	}
	return f.calls[len(f.calls)-1]
}

func newRemote(t *testing.T, f run.Runner) Remote {
	return Remote{Run: f, Home: t.TempDir(), Host: "lap"}
}

func TestExecAndInteractiveArgs(t *testing.T) {
	f := newRunner(t)
	r := newRemote(t, f)
	if _, err := r.Exec(context.Background(), "acme-dev", "echo $1", "data", "it's"); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	want := `-o BatchMode=yes acme-dev sh -c 'echo $1' athome 'it'\''s'`
	if c.Name != "ssh" || strings.Join(c.Args, " ") != want || c.Stdin != "data" || c.Interactive {
		t.Fatalf("%+v", c)
	}
	if _, err := r.Interactive(context.Background(), "acme-dev", "true"); err != nil {
		t.Fatal(err)
	}
	c = f.last(t)
	if strings.Join(c.Args, " ") != `-t acme-dev sh -c 'true' athome` || !c.Interactive {
		t.Fatalf("%+v", c)
	}
	// the .old key is used while it exists
	os.MkdirAll(filepath.Join(r.Home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(r.Home, ".ssh", "lap_ed25519.old"), []byte("k"), 0o600)
	r.Exec(context.Background(), "acme-dev", "true", "")
	c = f.last(t)
	if !strings.HasPrefix(strings.Join(c.Args, " "), "-o BatchMode=yes -i "+filepath.Join(r.Home, ".ssh", "lap_ed25519.old")+" acme-dev") {
		t.Fatalf("%v", c.Args)
	}
}

func TestLoginCheck(t *testing.T) {
	var cfg string
	f := &configSpy{Fake: runtest.NewFake()}
	r := newRemote(t, f)
	m := gpu()
	if err := r.LoginCheck(context.Background(), m, "/k/key"); err != nil {
		t.Fatal(err)
	}
	cfg = f.cfg
	c := f.Calls()[0]
	if len(c.Args) != 10 || c.Args[0] != "-F" || c.Args[2] != "-i" || c.Args[3] != "/k/key" || c.Args[8] != "acme-gpu" || c.Args[9] != "true" {
		t.Fatalf("%v", c.Args)
	}
	for _, bad := range []string{"IdentityFile", "ControlMaster", "ControlPersist", "ProxyJump", "SendEnv"} {
		if strings.Contains(cfg, bad) {
			t.Errorf("config has %s:\n%s", bad, cfg)
		}
	}
	for _, want := range []string{"ControlPath none", "ProxyCommand ssh -W [%h]:%p bastion", "HostKeyAlias acme-gpu", "UserKnownHostsFile ~/.ssh/athome/known_hosts", "StrictHostKeyChecking yes"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %s:\n%s", want, cfg)
		}
	}
	if f.mode != 0o600 {
		t.Errorf("tmp mode %v", f.mode)
	}
	if _, err := os.Stat(c.Args[1]); err == nil {
		t.Error("temp config left behind")
	}
	// failure names the machine and still removes the temp file
	f2 := &configSpy{Fake: runtest.NewFake()}
	f2.On("ssh").Return(run.Result{Code: 255})
	f2.fail = true
	err := newRemote(t, f2).LoginCheck(context.Background(), m, "/k/key")
	if err == nil || !strings.Contains(err.Error(), "acme-gpu") {
		t.Fatalf("err = %v", err)
	}
	if _, e := os.Stat(f2.Calls()[0].Args[1]); e == nil {
		t.Error("temp config left behind on failure")
	}
}

// configSpy reads the -F config while the command "runs".
type configSpy struct {
	*runtest.Fake
	cfg  string
	mode os.FileMode
	fail bool
}

func (s *configSpy) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	for i, a := range c.Args {
		if a == "-F" {
			b, _ := os.ReadFile(c.Args[i+1])
			s.cfg = string(b)
			if fi, err := os.Stat(c.Args[i+1]); err == nil {
				s.mode = fi.Mode().Perm()
			}
		}
	}
	res, err := s.Fake.Run(ctx, c)
	if s.fail {
		res.Code = 255
	}
	return res, err
}

func newBox(t *testing.T, f run.Runner) *Box {
	return NewBox(newRemote(t, f), acme())
}

const pub1 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAA one"

func TestBoxAuthorizeAndResetUseStdinOnly(t *testing.T) {
	f := newRunner(t)
	b := newBox(t, f)
	ctx := context.Background()
	if err := b.Authorize(ctx, pub1); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	if c.Stdin != pub1+"\n" || strings.Contains(strings.Join(c.Args, " "), "AAAAC3") {
		t.Fatalf("%+v", c)
	}
	if err := b.Reset(ctx, []string{pub1, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBB two"}); err != nil {
		t.Fatal(err)
	}
	c = f.last(t)
	if !strings.Contains(c.Stdin, pub1+"\n") || strings.Count(c.Stdin, "\n") != 2 || strings.Contains(strings.Join(c.Args, " "), "AAAAC3") {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(strings.Join(c.Args, " "), "authorized_keys.athome") || !strings.Contains(strings.Join(c.Args, " "), "mv ") {
		t.Fatalf("%v", c.Args)
	}
}

func TestBoxRefusesBadKeysBeforeSSH(t *testing.T) {
	f := newRunner(t)
	b := newBox(t, f)
	ctx := context.Background()
	for _, k := range []string{
		pub1 + "\nssh-ed25519 AAAA evil",
		pub1 + "\n",
		`command="x" ` + pub1,
		"ssh-rsa AAAAB3 x",
		"",
	} {
		if err := b.Authorize(ctx, k); err == nil {
			t.Errorf("Authorize accepted %q", k)
		}
		if err := b.Reset(ctx, []string{pub1, k}); err == nil {
			t.Errorf("Reset accepted %q", k)
		}
	}
	if err := b.Reset(ctx, nil); err == nil {
		t.Error("empty reset accepted")
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("%d ssh calls", n)
	}
}

func TestBoxLoginUsesTempConfig(t *testing.T) {
	f := &configSpy{Fake: runtest.NewFake()}
	if err := newBox(t, f).Login(context.Background(), "/k/new"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.cfg, "ControlPath none") || strings.Contains(f.cfg, "IdentityFile") {
		t.Fatalf("%s", f.cfg)
	}
}

func TestBoxKeyMethods(t *testing.T) {
	f := newRunner(t)
	b := newBox(t, f)
	ctx := context.Background()
	f.reply = run.Result{Stdout: pub1 + "\n"}
	if got, err := b.CurrentPub(ctx); err != nil || got != pub1 {
		t.Fatalf("%q %v", got, err)
	}
	if args := strings.Join(f.last(t).Args, " "); !strings.HasSuffix(args, "athome 'dev'") {
		t.Fatalf("%s", args)
	}
	if got, err := b.NewKey(ctx); err != nil || got != pub1 {
		t.Fatalf("%q %v", got, err)
	}
	if args := strings.Join(f.last(t).Args, " "); !strings.Contains(args, "ssh-keygen -q -t ed25519 -N") || !strings.HasSuffix(args, "athome 'dev' 'acme-dev'") {
		t.Fatalf("%s", args)
	}
	if err := b.PromoteKey(ctx, pub1); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(f.last(t).Args, " "); !strings.HasSuffix(args, "athome 'dev' 'AAAAC3NzaC1lZDI1NTE5AAAAIAAA'") {
		t.Fatalf("%s", args)
	}
	if err := b.PromoteKey(ctx, "garbage"); err == nil {
		t.Fatal("bad pub accepted")
	}
}

func TestBoxCheckGitHub(t *testing.T) {
	f := newRunner(t)
	b := newBox(t, f)
	ctx := context.Background()
	if err := b.CheckGitHub(ctx, true); err == nil {
		t.Fatal("empty output passed")
	}
	if args := strings.Join(f.last(t).Args, " "); !strings.HasSuffix(args, "athome 'dev' '.rotate'") || !strings.Contains(args, "-T git@github.com") {
		t.Fatalf("%s", args)
	}
	f.reply = run.Result{Code: 1, Stderr: "Hi x! You've successfully authenticated, but GitHub does not provide shell access."}
	if err := b.CheckGitHub(ctx, false); err != nil {
		t.Fatal(err)
	}
	if args := strings.Join(f.last(t).Args, " "); !strings.HasSuffix(args, "athome 'dev' ''") {
		t.Fatalf("%s", args)
	}
}

func TestKeysInventory(t *testing.T) {
	t.Setenv("HOSTNAME", "lap")
	home := t.TempDir()
	if err := Save(home, []Machine{gpu(), acme()}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Include athome/config\n"), 0o600)
	bs, err := KeysInventory(home, runtest.NewFake(), "lap")
	if err != nil || len(bs) != 2 || bs[0].Name() != "acme-dev" || bs[1].Scope() != "work/acme" {
		t.Fatalf("%v %v", bs, err)
	}
}
