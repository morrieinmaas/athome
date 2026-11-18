package machines

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/morrieinmaas/athome/internal/run"
)

// hookRun runs before on every command, then hands it to the world. It lets a
// test change the inventory while a command is in the middle of its work.
type hookRun struct {
	*world
	before func(c run.Cmd)
}

func (h hookRun) Run(ctx context.Context, c run.Cmd) (run.Result, error) {
	h.before(c)
	return h.world.Run(ctx, c)
}

func TestValidateRootNamesJumpAndDots(t *testing.T) {
	root := acme()
	root.User = "root"
	if err := root.Validate(); err == nil || !strings.Contains(err.Error(), "user root: log in as a sudo user; the bootstrap turns off root login") {
		t.Errorf("root accepted or wrong message: %v", err)
	}
	for _, name := range []string{"github.com", "a.b", "acme.dev", "-x", "A", ""} {
		m := acme()
		m.Name = name
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "name") {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, name := range []string{"acme-dev", "gpu_1", "0box"} {
		m := acme()
		m.Name = name
		if err := m.Validate(); err != nil {
			t.Errorf("name %q: %v", name, err)
		}
	}
	for _, jump := range []string{"-oProxyCommand=x", "-J", "a@-b", "ops@-oFoo:22", "a b", ".x", "a@"} {
		m := acme()
		m.Jump = jump
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "jump") {
			t.Errorf("jump %q: %v", jump, err)
		}
	}
	for _, jump := range []string{"bastion", "ops@bastion", "ops@bastion.example:2222", "10.0.0.1"} {
		m := acme()
		m.Jump = jump
		if err := m.Validate(); err != nil {
			t.Errorf("jump %q: %v", jump, err)
		}
	}
}

func TestDeleteByTitleKeepsOtherMachinesKeys(t *testing.T) {
	w, d, _ := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{
		{"1", "ssh-ed25519 AAAAA", "athome-foo"},
		{"2", "ssh-ed25519 AAAAB", "athome-foo-sign"}, // machine foo-sign's authentication key
	}
	w.ghKeys["user/ssh_signing_keys"] = []fakeGHKey{
		{"3", "ssh-ed25519 AAAAA", "athome-foo-sign"},
		{"4", "ssh-ed25519 AAAAB", "athome-foo-sign-sign"},
		{"5", "ssh-ed25519 AAAAC", "athome-foo"}, // not a title rm foo owns on this endpoint
	}
	n, err := d.gh(Machine{}).deleteByTitle(t.Context(), "foo")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d: %v", n, err)
	}
	if got := w.ghKeys["user/keys"]; len(got) != 1 || got[0].id != "2" {
		t.Errorf("user/keys = %+v", got)
	}
	if got := w.ghKeys["user/ssh_signing_keys"]; len(got) != 2 || got[0].id != "4" || got[1].id != "5" {
		t.Errorf("user/ssh_signing_keys = %+v", got)
	}
}

func TestRemoveOrderGitHubThenBoxThenLaptop(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	addAcme(t, w, d)
	mark := len(w.logCopy())
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gh-delete user/keys athome-acme-dev",
		"gh-delete user/ssh_signing_keys athome-acme-dev-sign",
		"box-gh-logout acme-dev",
		"box-drop-key acme-dev",
		"grofe machine rm acme-dev",
		"keygen-R acme-dev",
		"backup",
	}
	if missing, ok := inOrder(w.logCopy()[mark:], want); !ok {
		t.Fatalf("stuck at %q:\n%s", missing, strings.Join(w.logCopy()[mark:], "\n"))
	}
}

func TestRemoveByNameOnlyReachesNoBox(t *testing.T) {
	w, d, out := newWorld(t)
	w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-ghost"}}
	// the box would answer
	if err := Remove(t.Context(), d, "ghost", true); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := w.sshArgs(); len(got) != 0 {
		t.Errorf("ssh calls for a name that is neither in the inventory nor pending: %v", got)
	}
	if len(w.ops("box-")) != 0 {
		t.Errorf("box ops: %v", w.ops("box-"))
	}
	if len(w.ops("gh-delete")) != 1 {
		t.Errorf("GitHub keys not revoked: %v", w.logCopy())
	}
}

func TestRemoveWithoutIncludeReachesNoBox(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	addAcme(t, w, d)
	if err := os.Remove(filepath.Join(d.Home, ".ssh", "config")); err != nil {
		t.Fatal(err)
	}
	before := len(w.sshArgs())

	err := Remove(t.Context(), d, "acme-dev", false)
	if err == nil || !strings.Contains(err.Error(), "does not include athome/config") {
		t.Fatalf("err = %v", err)
	}
	if err := Remove(t.Context(), d, "acme-dev", true); err != nil {
		t.Fatal(err)
	}
	if got := w.sshArgs(); len(got) != before {
		t.Errorf("ssh calls without the include: %v", got[before:])
	}
	if !strings.Contains(out.String(), "does not include athome/config") {
		t.Errorf("no note:\n%s", out)
	}
}

func TestRemoveNotesTheAccountWhenNoKeyMatched(t *testing.T) {
	t.Run("recorded account", func(t *testing.T) {
		w, d, out := newWorld(t)
		m := acme()
		m.GitHubAccount = "acme-bot"
		if err := Save(d.Home, []Machine{m}); err != nil {
			t.Fatal(err)
		}
		if err := Remove(t.Context(), d, "acme-dev", true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "note: no GitHub key titled athome-acme-dev or athome-acme-dev-sign in account acme-bot") {
			t.Errorf("no note:\n%s", out)
		}
		_ = w
	})
	t.Run("by name only resolves the active account", func(t *testing.T) {
		_, d, out := newWorld(t)
		if err := Remove(t.Context(), d, "ghost", true); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "in account octo") {
			t.Errorf("no note:\n%s", out)
		}
	})
	t.Run("a match is no note", func(t *testing.T) {
		w, d, out := newWorld(t)
		w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-ghost"}}
		if err := Remove(t.Context(), d, "ghost", true); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "no GitHub key") {
			t.Errorf("note although a key matched:\n%s", out)
		}
	})
}

func TestAddStoresTheResolvedAccountAndRmUsesIt(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	w.setFail("probe", true)
	if err := Add(t.Context(), d, newAdd()); err == nil {
		t.Fatal("expected the probe failure")
	}
	if p, _ := LoadPending(d.Home, "acme-dev"); p == nil || p.GitHubAccount != "octo" {
		t.Fatalf("pending entry lacks the account: %+v", p)
	}
	w.setFail("probe", false)
	if err := Add(t.Context(), d, newAdd()); err != nil { // same flags: still the same entry
		t.Fatal(err)
	}
	ms, _ := Load(d.Home)
	if len(ms) != 1 || ms[0].GitHubAccount != "octo" {
		t.Fatalf("inventory: %+v", ms)
	}
	for _, v := range w.ghEnv {
		if v != "tok-octo" {
			t.Errorf("GH_TOKEN = %q", v)
		}
	}
	if len(w.ops("gh-user")) != 1 {
		t.Errorf("account resolved %d times", len(w.ops("gh-user")))
	}

	w.ghLogin = "switched" // gh auth switch
	mark := len(w.ghEnv)
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	if len(w.ghEnv) == mark {
		t.Fatal("rm made no gh call with a token")
	}
	for _, v := range w.ghEnv {
		if v != "tok-octo" {
			t.Errorf("GH_TOKEN = %q after the switch", v)
		}
	}
	if len(w.ops("gh-user")) != 1 {
		t.Errorf("account resolved again by rm: %v", w.ops("gh-user"))
	}
}

func TestAddFailsWhenTheAccountCannotBeResolved(t *testing.T) {
	w, d, _ := newWorld(t)
	w.ghLogin = ""
	err := Add(t.Context(), d, newAdd())
	if err == nil || !strings.Contains(err.Error(), "machine-validate") || !strings.Contains(err.Error(), "GitHub account") {
		t.Fatalf("err = %v", err)
	}
	if len(w.sshArgs()) != 0 {
		t.Error("a box was reached")
	}
}

func TestCloneAppliesOnlyItsOwnChange(t *testing.T) {
	w, d := cloneBox(t)
	added := false
	h := hookRun{world: w, before: func(c run.Cmd) {
		if added || !strings.Contains(strings.Join(c.Args, " "), "git clone") {
			return
		}
		added = true
		ms, err := Load(d.Home)
		if err != nil {
			t.Error(err)
		}
		if err := Save(d.Home, append(ms, gpu())); err != nil {
			t.Error(err)
		}
	}}
	d.Run = h
	if err := Clone(t.Context(), d, "acme/api", "acme-dev"); err != nil {
		t.Fatal(err)
	}
	ms, _ := Load(d.Home)
	if len(ms) != 2 || ms[0].Name != "acme-dev" || ms[1].Name != "acme-gpu" {
		t.Fatalf("a machine added meanwhile was lost: %+v", ms)
	}
	if !slices.Equal(ms[0].Repos, []string{"api"}) {
		t.Errorf("repos = %v", ms[0].Repos)
	}
}

func TestRemoveAppliesOnlyItsOwnChange(t *testing.T) {
	w, d, _ := newWorld(t)
	if err := Save(d.Home, []Machine{acme()}); err != nil {
		t.Fatal(err)
	}
	w.ghKeys["user/keys"] = []fakeGHKey{{"7", "ssh-ed25519 AAAAX", "athome-acme-dev"}}
	added := false
	d.Run = hookRun{world: w, before: func(c run.Cmd) {
		if added || !slices.Contains(c.Args, "DELETE") {
			return
		}
		added = true
		ms, _ := Load(d.Home)
		if err := Save(d.Home, append(ms, gpu())); err != nil {
			t.Error(err)
		}
	}}
	if err := Remove(t.Context(), d, "acme-dev", false); err != nil {
		t.Fatal(err)
	}
	ms, _ := Load(d.Home)
	if len(ms) != 1 || ms[0].Name != "acme-gpu" {
		t.Fatalf("a machine added meanwhile was lost: %+v", ms)
	}
	cfg, _ := os.ReadFile(filepath.Join(SSHDir(d.Home), "config"))
	if !strings.Contains(string(cfg), "Host acme-gpu") || strings.Contains(string(cfg), "Host acme-dev") {
		t.Errorf("config not refreshed:\n%s", cfg)
	}
}

func TestSaveWritesOnlyTheInventory(t *testing.T) {
	home := t.TempDir()
	if err := Save(home, []Machine{acme()}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(SSHDir(home), "config"), projectsPath(home)} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("Save wrote %s", p)
		}
	}
}

func TestBoxCheckGitHubPinsGitHubHostKeys(t *testing.T) {
	f := newRunner(t)
	f.reply = run.Result{Code: 1, Stderr: "Hi x! You've successfully authenticated, but GitHub does not provide shell access."}
	if err := newBox(t, f).CheckGitHub(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	c := f.last(t)
	args := strings.Join(c.Args, " ")
	for _, want := range []string{"UserKnownHostsFile=", "athome_github_known_hosts", "StrictHostKeyChecking=yes"} {
		if !strings.Contains(args, want) {
			t.Errorf("script lacks %s:\n%s", want, args)
		}
	}
	if strings.Contains(args, "accept-new") {
		t.Errorf("trust on first use for github.com:\n%s", args)
	}
	if c.Stdin != githubKnownHosts {
		t.Errorf("stdin = %q", c.Stdin)
	}
	for _, line := range strings.Split(strings.TrimSpace(githubKnownHosts), "\n") {
		if f := strings.Fields(line); len(f) != 3 || f[0] != "github.com" {
			t.Errorf("bad known_hosts line %q", line)
		}
	}
	for _, typ := range []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl", "ecdsa-sha2-nistp256 ", "ssh-rsa "} {
		if !strings.Contains(githubKnownHosts, "github.com "+typ) {
			t.Errorf("no %s", typ)
		}
	}
}

func TestCheckGitHubScriptOnDisk(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	stub := "#!/bin/sh\necho \"$@\" > \"$HOME/ssh.args\"\ncat \"$HOME/.ssh/athome_github_known_hosts\" > \"$HOME/ssh.kh\"\n" +
		"echo 'Hi x! You have successfully authenticated' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := shRun(t, home, checkGitHubScript, githubKnownHosts, "dev", ".rotate")
	if err == nil || !strings.Contains(out, "successfully authenticated") {
		t.Fatalf("%q %v", out, err)
	}
	kh := filepath.Join(home, ".ssh", "athome_github_known_hosts")
	if b, _ := os.ReadFile(kh); string(b) != githubKnownHosts {
		t.Errorf("known_hosts on the box: %q", b)
	}
	if fi, err := os.Stat(kh); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("known_hosts mode: %v %v", fi, err)
	}
	args, _ := os.ReadFile(filepath.Join(home, "ssh.args"))
	for _, want := range []string{"-o UserKnownHostsFile=" + kh, "-o StrictHostKeyChecking=yes", "-i " + filepath.Join(home, ".ssh", "dev_ed25519.rotate"), "-T git@github.com"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("ssh args lack %q: %s", want, args)
		}
	}
}

func TestLoginBlockSplitsTheJump(t *testing.T) {
	for jump, want := range map[string]string{
		"bastion":                  "ProxyCommand ssh -W [%h]:%p bastion\n",
		"ops@bastion":              "ProxyCommand ssh -W [%h]:%p -l ops bastion\n",
		"bastion:2222":             "ProxyCommand ssh -W [%h]:%p -p 2222 bastion\n",
		"ops@bastion.example:2222": "ProxyCommand ssh -W [%h]:%p -p 2222 -l ops bastion.example\n",
		"ops@10.0.0.1":             "ProxyCommand ssh -W [%h]:%p -l ops 10.0.0.1\n",
	} {
		m := gpu()
		m.Jump = jump
		if got := block(m, "", true); !strings.Contains(got, "    "+want) {
			t.Errorf("jump %q:\n%s", jump, got)
		}
		if got := block(m, "k", false); !strings.Contains(got, "    ProxyJump "+jump+"\n") {
			t.Errorf("jump %q, shared block:\n%s", jump, got)
		}
	}
}

func TestBootstrapTOMLEscapesAndFiltersValues(t *testing.T) {
	data := laptopData()
	data["personalName"] = "A \"B\" \\ C\nD\te\x01"
	data["githubHandle"] = `x" = "y`
	m := Machine{Name: "acme-dev", Scope: "work/acme"}
	got := BootstrapTOML(data, m, "Europe/Amsterdam", "en_US.UTF-8")
	var back map[string]string
	if _, err := toml.Decode(got, &back); err != nil {
		t.Fatalf("not valid TOML: %v\n%s", err, got)
	}
	if back["personalName"] != data["personalName"] || back["githubHandle"] != data["githubHandle"] {
		t.Errorf("round trip: %q %q", back["personalName"], back["githubHandle"])
	}
	if back["githubId"] != "42" || back["timezone"] != "Europe/Amsterdam" || back["locale"] != "en_US.UTF-8" {
		t.Errorf("other values changed: %v", back)
	}

	for _, tz := range []string{`Europe/"x`, "a b", "x\ny", "a\\b"} {
		if s := BootstrapTOML(data, m, tz, ""); strings.Contains(s, "timezone") {
			t.Errorf("timezone %q written:\n%s", tz, s)
		}
	}
	for _, loc := range []string{`en"x`, "a b", "x\ny", "a/b"} {
		if s := BootstrapTOML(data, m, "", loc); strings.Contains(s, "locale") {
			t.Errorf("locale %q written:\n%s", loc, s)
		}
	}
	if s := BootstrapTOML(data, m, "Etc/GMT+5", "de_DE@euro"); !strings.Contains(s, `timezone = "Etc/GMT+5"`) || !strings.Contains(s, `locale = "de_DE@euro"`) {
		t.Errorf("valid values dropped:\n%s", s)
	}
}

func TestBootstrapBoxNotesWhenTimezoneOrLocaleIsLeftOut(t *testing.T) {
	w, d, out := newWorld(t)
	link := filepath.Join(t.TempDir(), "localtime")
	os.Symlink(`/usr/share/zoneinfo/Bad"Zone`, link)
	old := localtimePath
	localtimePath = link
	t.Cleanup(func() { localtimePath = old })
	t.Setenv("LANG", "en US")
	if err := bootstrapBox(t.Context(), d, acme(), true, d.Out); err != nil {
		t.Fatal(err)
	}
	got := w.boxes["acme-dev"].bootstrap
	if strings.Contains(got, "timezone") || strings.Contains(got, "locale") {
		t.Errorf("bad values sent:\n%s", got)
	}
	for _, want := range []string{"note: timezone", "note: locale"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("no %q:\n%s", want, out)
		}
	}
}

func TestAuthorizeScriptIgnoresCommentLines(t *testing.T) {
	home := t.TempDir()
	ak := filepath.Join(home, ".ssh", "authorized_keys")
	os.MkdirAll(filepath.Dir(ak), 0o700)
	os.WriteFile(ak, []byte("# "+pub1+"\n   #"+pub1+"\n"), 0o600)
	if out, err := shRun(t, home, authorizeScript, pub1+"\n"); err != nil {
		t.Fatal(out, err)
	}
	b, _ := os.ReadFile(ak)
	if !strings.Contains(string(b), "\n"+pub1+"\n") {
		t.Fatalf("the key behind a comment was not authorized:\n%s", b)
	}
	if out, err := shRun(t, home, authorizeScript, pub1+"\n"); err != nil { // now present for real
		t.Fatal(out, err)
	}
	if b2, _ := os.ReadFile(ak); string(b2) != string(b) {
		t.Errorf("appended twice:\n%s", b2)
	}
}

func TestAuthorizedHasSkipsComments(t *testing.T) {
	blob := blobOf(pub1)
	for in, want := range map[string]bool{
		"# " + pub1 + "\n":                      false,
		"   #" + pub1 + "\n":                    false,
		pub1 + "\n":                             true,
		"# note\nrestrict " + pub1 + "\n":       true,
		"ssh-ed25519 AAAAOTHER x\n":             false,
		"# " + pub1 + "\nssh-ed25519 OTHER x\n": false,
	} {
		if got := authorizedHas(in, blob); got != want {
			t.Errorf("%q: got %v", in, got)
		}
	}
}

func TestBreakGlassCheckIgnoresCommentedKey(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	addAcme(t, w, d)
	b := w.boxes["acme-dev"]
	var bg string
	for _, l := range strings.Split(b.authkeys, "\n") {
		if strings.HasPrefix(l, "ssh-ed25519") {
			bg = l
		}
	}
	if bg == "" {
		t.Fatalf("no break-glass key on the box: %q", b.authkeys)
	}
	b.authkeys = "# " + bg + "\n"
	steps := AddSteps(d, &Machine{Name: "acme-dev", Scope: "work/acme", Target: "t", User: "dev", Athome: "v0.4.0", Home: "/home/dev", Hostname: "dev"})
	for _, s := range steps {
		if s.Name != "machine-break-glass" {
			continue
		}
		ok, err := s.Check(t.Context(), nil)
		if err != nil || ok {
			t.Fatalf("a commented key counts as authorized: %v %v", ok, err)
		}
		return
	}
	t.Fatal("no break-glass step")
}

func TestKeysInventoryNeedsTheInclude(t *testing.T) {
	t.Setenv("HOSTNAME", "lap")
	home := t.TempDir()
	if bs, err := KeysInventory(home, run.Exec(), "lap"); err != nil || len(bs) != 0 {
		t.Fatalf("empty inventory: %v %v", bs, err)
	}
	if err := Save(home, []Machine{acme()}); err != nil {
		t.Fatal(err)
	}
	if _, err := KeysInventory(home, run.Exec(), "lap"); err == nil || !strings.Contains(err.Error(), "does not include athome/config") {
		t.Fatalf("err = %v", err)
	}
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Include athome/config\n"), 0o600)
	if bs, err := KeysInventory(home, run.Exec(), "lap"); err != nil || len(bs) != 1 {
		t.Fatalf("%v %v", bs, err)
	}
}

func TestAddLeftBehindNamesGitHubKeysWhenBoxKeyFailed(t *testing.T) {
	needKeygen(t)
	for _, fail := range []string{"gh-add-sign", "github-check"} {
		t.Run(fail, func(t *testing.T) {
			w, d, _ := newWorld(t)
			w.setFail(fail, true)
			err := Add(t.Context(), d, newAdd())
			if err == nil || !strings.HasPrefix(err.Error(), "athome machine add: machine-box-key: ") {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), "left behind: pinned host key, GitHub keys athome-acme-dev[-sign]\n") {
				t.Errorf("left behind: %v", err)
			}
		})
	}
}

func TestPromoteScriptResumesAfterAnInterruption(t *testing.T) {
	needKeygen(t)
	setup := func(t *testing.T) (home, pub string) {
		home = t.TempDir()
		out, err := shRun(t, home, newKeyScript, "", "dev", "acme-dev")
		if err != nil {
			t.Fatal(out, err)
		}
		return home, strings.TrimSpace(out)
	}
	assertPromoted := func(t *testing.T, home, pub string) {
		t.Helper()
		ssh := filepath.Join(home, ".ssh")
		for _, gone := range []string{"dev_ed25519.rotate", "dev_ed25519.rotate.pub"} {
			if _, err := os.Stat(filepath.Join(ssh, gone)); err == nil {
				t.Errorf("%s left", gone)
			}
		}
		if b, _ := os.ReadFile(filepath.Join(ssh, "dev_ed25519.pub")); strings.TrimSpace(string(b)) != pub {
			t.Errorf("pub = %s", b)
		}
		o, err := exec.Command("ssh-keygen", "-y", "-f", filepath.Join(ssh, "dev_ed25519")).Output()
		out := string(o)
		if err != nil || !strings.HasPrefix(pub, strings.TrimSpace(out)) {
			t.Errorf("private key is not the new one: %q %v", out, err)
		}
	}

	t.Run("the pub moved first, the private key still waits", func(t *testing.T) {
		home, pub := setup(t)
		ssh := filepath.Join(home, ".ssh")
		if err := os.Rename(filepath.Join(ssh, "dev_ed25519.rotate.pub"), filepath.Join(ssh, "dev_ed25519.pub")); err != nil {
			t.Fatal(err)
		}
		if out, err := shRun(t, home, promoteScript, "", "dev", blobOf(pub)); err != nil {
			t.Fatal(out, err)
		}
		assertPromoted(t, home, pub)
	})
	t.Run("a .rotate.pub is left behind", func(t *testing.T) {
		home, pub := setup(t)
		ssh := filepath.Join(home, ".ssh")
		if err := os.Rename(filepath.Join(ssh, "dev_ed25519.rotate"), filepath.Join(ssh, "dev_ed25519")); err != nil {
			t.Fatal(err)
		}
		if out, err := shRun(t, home, promoteScript, "", "dev", blobOf(pub)); err != nil {
			t.Fatal(out, err)
		}
		assertPromoted(t, home, pub)
	})
	t.Run("the order is pub first", func(t *testing.T) {
		pubMove := strings.Index(promoteScript, `"$k.rotate.pub" "$k.pub"`)
		keyMove := strings.Index(promoteScript, `"$k.rotate" "$k"`)
		if pubMove < 0 || keyMove < 0 || pubMove > keyMove {
			t.Errorf("the .rotate.pub move must come before the .rotate move:\n%s", promoteScript)
		}
	})
}
