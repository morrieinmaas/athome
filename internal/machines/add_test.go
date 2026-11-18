package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newAdd() Machine {
	return Machine{Name: "acme-dev", Scope: "work/acme", Target: "dev.acme.example", User: "dev", Port: 22, Athome: "v0.4.0"}
}

// inOrder reports whether want appears in log as a subsequence, matching by prefix.
func inOrder(log, want []string) (string, bool) {
	i := 0
	for _, l := range log {
		if i < len(want) && strings.HasPrefix(l, want[i]) {
			i++
		}
	}
	if i < len(want) {
		return want[i], false
	}
	return "", true
}

func TestAddRunsEightStepsInOrder(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	want := []string{
		"ssh-probe -o StrictHostKeyChecking=accept-new",
		"box-keygen acme-dev",
		"gh-add-auth athome-acme-dev",
		"gh-add-sign athome-acme-dev-sign",
		"box-github-check acme-dev",
		"rbw-add athome/work/acme/break-glass",
		"box-authorize acme-dev",
		"box-upload-bootstrap acme-dev",
		"box-checkout acme-dev v0.4.0",
		"box-bootstrap acme-dev v0.4.0(interactive)",
		"box-gh-login acme-dev(interactive)",
		"grofe machine add acme-dev acme-dev",
		"backup",
	}
	if missing, ok := inOrder(w.logCopy(), want); !ok {
		t.Fatalf("calls out of order, stuck at %q:\n%s", missing, strings.Join(w.logCopy(), "\n"))
	}
	for _, step := range []string{"machine-validate", "machine-host-key", "machine-box-key", "machine-break-glass", "machine-bootstrap", "machine-gh-login", "machine-grofe", "machine-inventory"} {
		if !strings.Contains(out.String(), "apply "+step) {
			t.Errorf("step %s did not run:\n%s", step, out)
		}
	}
	ms, err := Load(d.Home)
	if err != nil || len(ms) != 1 {
		t.Fatalf("inventory %v %v", ms, err)
	}
	if ms[0].Home != "/home/dev" || ms[0].Hostname != "dev" {
		t.Errorf("home/hostname not recorded (uname -n had dots): %+v", ms[0])
	}
	if p, _ := LoadPending(d.Home, "acme-dev"); p != nil {
		t.Error("pending file left")
	}
	cfg, _ := os.ReadFile(filepath.Join(d.Home, ".ssh", "athome", "config"))
	if !strings.Contains(string(cfg), "Host acme-dev") {
		t.Errorf("ssh config lacks the block:\n%s", cfg)
	}
	if got := w.boxes["acme-dev"].bootstrap; !strings.Contains(got, `machine = "remote"`) || !strings.Contains(got, `scope = "work/acme"`) {
		t.Errorf("bootstrap.toml sent: %q", got)
	}
}

func TestAddResumesAfterEachFailure(t *testing.T) {
	needKeygen(t)
	cases := []struct{ fail, step string }{
		{"rbw-locked", "machine-validate"},
		{"probe", "machine-host-key"},
		{"keygen", "machine-box-key"},
		{"gh-add-auth", "machine-box-key"},
		{"gh-add-sign", "machine-box-key"},
		{"github-check", "machine-box-key"},
		{"authorize", "machine-break-glass"},
		{"upload", "machine-bootstrap"},
		{"bootstrap", "machine-bootstrap"},
		{"gh-login", "machine-gh-login"},
		{"grofe-add", "machine-grofe"},
		{"inventory", "machine-inventory"},
	}
	for _, c := range cases {
		t.Run(c.fail, func(t *testing.T) {
			w, d, out := newWorld(t)
			blocked := filepath.Join(d.Home, ".config")
			switch c.fail {
			case "rbw-locked":
				w.rbwLocked = true
			case "inventory":
				if os.Getuid() == 0 {
					t.Skip("root ignores directory modes")
				}
				os.Mkdir(blocked, 0o500)
				t.Cleanup(func() { os.Chmod(blocked, 0o700) })
			default:
				w.setFail(c.fail, true)
			}
			err := Add(context.Background(), d, newAdd())
			if err == nil || !strings.HasPrefix(err.Error(), "athome machine add: "+c.step+": ") {
				t.Fatalf("err = %v, want it to name %s", err, c.step)
			}
			if c.step != "machine-validate" && (!strings.Contains(err.Error(), "left behind: ") || !strings.Contains(err.Error(), "rerun the same command to resume, or remove it with athome machine rm acme-dev --force")) {
				t.Errorf("no left-behind list: %v", err)
			}
			if c.step == "machine-break-glass" && !strings.Contains(err.Error(), "left behind: pinned host key, GitHub keys athome-acme-dev[-sign]\n") {
				t.Errorf("left behind: %v", err)
			}
			if ms, _ := Load(d.Home); len(ms) != 0 {
				t.Fatalf("inventory entry written despite the failure: %v", ms)
			}
			// fix the cause and rerun the same command
			w.rbwLocked = false
			w.setFail(c.fail, false)
			os.Chmod(blocked, 0o700)
			out.Reset()
			if err := Add(context.Background(), d, newAdd()); err != nil {
				t.Fatalf("resume: %v", err)
			}
			b := w.boxes["acme-dev"]
			if b.keygens != 1 {
				t.Errorf("ssh-keygen ran %d times", b.keygens)
			}
			for prefix, n := range map[string]int{"gh-add-auth": 1, "gh-add-sign": 1, "rbw-add": 1, "box-authorize": 1, "grofe machine add": 1, "backup": 1} {
				if got := len(w.ops(prefix)); got != n {
					t.Errorf("%s ran %d times, want %d:\n%s", prefix, got, n, strings.Join(w.logCopy(), "\n"))
				}
			}
			ms, err := Load(d.Home)
			if err != nil || len(ms) != 1 || ms[0].Home != "/home/dev" || ms[0].Hostname != "dev" {
				t.Fatalf("final inventory %+v %v", ms, err)
			}
			if p, _ := LoadPending(d.Home, "acme-dev"); p != nil {
				t.Error("pending left")
			}
			if c.step != "machine-validate" && c.step != "machine-host-key" && !strings.Contains(out.String(), "skip  machine-host-key") {
				t.Errorf("host key step was not skipped on resume:\n%s", out)
			}
		})
	}
}

func TestAddRefusesPendingWithOtherValues(t *testing.T) {
	w, d, _ := newWorld(t)
	w.setFail("probe", true)
	if err := Add(context.Background(), d, newAdd()); err == nil {
		t.Fatal("expected the probe failure")
	}
	other := newAdd()
	other.Target = "elsewhere.example"
	err := Add(context.Background(), d, other)
	if err == nil || !strings.Contains(err.Error(), "machine add acme-dev is pending with other values; finish it with the same flags or run athome machine rm acme-dev --force") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddValidateRefusals(t *testing.T) {
	t.Run("git repositories in the scope run no command", func(t *testing.T) {
		w, d, _ := newWorld(t)
		os.MkdirAll(filepath.Join(d.Home, "work", "acme", "r", ".git"), 0o755)
		err := Add(context.Background(), d, newAdd())
		if err == nil || !strings.Contains(err.Error(), "holds git repositories") {
			t.Fatalf("err = %v", err)
		}
		if len(w.calls) != 0 {
			t.Fatalf("commands ran: %v", w.calls)
		}
	})
	t.Run("ssh config include missing", func(t *testing.T) {
		_, d, _ := newWorld(t)
		os.Remove(filepath.Join(d.Home, ".ssh", "config"))
		err := Add(context.Background(), d, newAdd())
		if err == nil || !strings.Contains(err.Error(), "does not include athome/config; run chezmoi apply ~/.ssh/config") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("rbw locked", func(t *testing.T) {
		w, d, _ := newWorld(t)
		w.rbwLocked = true
		err := Add(context.Background(), d, newAdd())
		if err == nil || !strings.Contains(err.Error(), "rbw is locked") {
			t.Fatalf("err = %v", err)
		}
		if len(w.sshArgs()) != 0 {
			t.Error("a box was reached while rbw was locked")
		}
	})
	t.Run("name exists", func(t *testing.T) {
		_, d, _ := newWorld(t)
		if err := Save(d.Home, []Machine{acme()}); err != nil {
			t.Fatal(err)
		}
		err := Add(context.Background(), d, newAdd())
		if err == nil || err.Error() != "machine acme-dev already exists" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad field names machine and field", func(t *testing.T) {
		_, d, _ := newWorld(t)
		m := newAdd()
		m.Target = "dev host"
		if err := Add(context.Background(), d, m); err == nil || !strings.Contains(err.Error(), "acme-dev") || !strings.Contains(err.Error(), "target") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAddForwardAgentWarning(t *testing.T) {
	needKeygen(t)
	_, d, out := newWorld(t)
	m := newAdd()
	m.ForwardAgent = true
	if err := Add(context.Background(), d, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "warning: root on acme-dev can use your laptop key while you are attached") {
		t.Errorf("no warning:\n%s", out)
	}
}

func TestAddGitHubAccountTokenStaysInEnv(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	m := newAdd()
	m.GitHubAccount = "acme-bot"
	if err := Add(context.Background(), d, m); err != nil {
		t.Fatal(err)
	}
	if len(w.ghEnv) < 4 {
		t.Fatalf("gh calls carrying GH_TOKEN: %d", len(w.ghEnv))
	}
	for _, v := range w.ghEnv {
		if v != "tok-acme-bot" {
			t.Errorf("GH_TOKEN = %q", v)
		}
	}
	for _, c := range w.calls {
		for _, a := range c.Args {
			if strings.Contains(a, "tok-acme-bot") {
				t.Errorf("token in args of %s %v", c.Name, c.Args)
			}
		}
	}
	if strings.Contains(out.String(), "tok-acme-bot") {
		t.Error("token in output")
	}
	if len(w.ops("gh-token acme-bot")) != 1 {
		t.Errorf("token fetched %d times", len(w.ops("gh-token")))
	}
}

func TestAddGrofeAbsentPasses(t *testing.T) {
	needKeygen(t)
	w, d, out := newWorld(t)
	w.grofeOK = false
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "grofe not installed: run grofe machine add acme-dev acme-dev once it is") {
		t.Errorf("no note:\n%s", out)
	}
	if len(w.ops("grofe")) != 0 {
		t.Error("grofe ran")
	}
	if ms, _ := Load(d.Home); len(ms) != 1 {
		t.Error("entry not written")
	}
}

func TestGrofeFallbackPath(t *testing.T) {
	w, d, _ := newWorld(t)
	w.grofeOK = false
	if _, ok := d.grofe(); ok {
		t.Fatal("grofe found from nowhere")
	}
	p := filepath.Join(d.Home, ".local", "lib", "grofe", "grofe")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755)
	if got, ok := d.grofe(); !ok || got != p {
		t.Fatalf("grofe = %q %v", got, ok)
	}
}

func TestAddReusesExistingBoxKeyAndGrofeEntry(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	b := w.box("acme-dev")
	b.pub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEXISTING athome@acme-dev"
	w.grofe["acme-dev"] = "acme-dev"
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatal(err)
	}
	if b.keygens != 0 {
		t.Error("existing key regenerated")
	}
	if len(w.ops("grofe machine add")) != 0 {
		t.Error("grofe machine add ran although grofe lists the name")
	}
	if len(w.ops("gh-add-auth")) != 1 || len(w.ops("gh-add-sign")) != 1 {
		t.Errorf("gh adds: %v", w.logCopy())
	}
}

func TestAddScopeHintOnGhFailure(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	w.setFail("gh-add-auth", true)
	err := Add(context.Background(), d, newAdd())
	if err == nil || !strings.Contains(err.Error(), "gh auth refresh -h github.com -s admin:public_key,admin:ssh_signing_key") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddWithoutBreakGlassItemCreatesOne(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatal(err)
	}
	if len(w.rbwItems["athome/work/acme/break-glass"]) != 1 {
		t.Errorf("items: %v", w.rbwItems)
	}
	// a second machine in the scope reuses the item
	m := newAdd()
	m.Name = "acme-gpu"
	m.Target = "10.0.0.7"
	if err := Add(context.Background(), d, m); err != nil {
		t.Fatal(err)
	}
	if len(w.ops("rbw-add")) != 1 {
		t.Errorf("break-glass created again: %v", w.ops("rbw-"))
	}
	if n := strings.Count(w.boxes["acme-gpu"].authkeys, "ssh-ed25519"); n != 1 {
		t.Errorf("authorized_keys on the second box: %q", w.boxes["acme-gpu"].authkeys)
	}
}

func TestSameEntryIgnoresFilledFields(t *testing.T) {
	a, b := newAdd(), newAdd()
	b.Home, b.Hostname, b.Repos, b.Port = "/home/dev", "dev", []string{"x"}, 22
	if !sameEntry(a, b) {
		t.Fatal("home, hostname, repos and the default port are not flags")
	}
	b.Jump = "j"
	if sameEntry(a, b) {
		t.Fatal("jump differs")
	}
}

// GitHub answers "already" when the key is on a different account, so add
// must not take it for success.
func TestAddFailsWhenTheBoxKeyIsOnAnotherAccount(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	w.ghForeign = map[string]bool{"AAAAC3NzaC1lZDI1NTE5AAAABOXKEY8": true}
	err := Add(context.Background(), d, newAdd())
	if err == nil || !strings.HasPrefix(err.Error(), "athome machine add: machine-box-key: ") || !strings.Contains(err.Error(), "not on GitHub account octo") {
		t.Fatalf("err = %v", err)
	}
	if ms, _ := Load(d.Home); len(ms) != 0 {
		t.Errorf("inventory written: %v", ms)
	}
}

func TestBoxCheckGitHubNeedsTheMachinesAccount(t *testing.T) {
	w, d, _ := newWorld(t)
	b := w.box("acme-dev")
	b.pub = "ssh-ed25519 AAAAKEY athome@acme-dev"
	w.ghKeys["user/keys"] = []fakeGHKey{{"1", "ssh-ed25519 AAAAKEY", "athome-acme-dev"}}
	w.ghOwner["AAAAKEY"] = "someone-else"
	m := newAdd()
	m.Hostname, m.GitHubAccount = "dev", "acme-bot"
	if err := NewBox(d.remote(), m).CheckGitHub(context.Background(), false); err == nil || !strings.Contains(err.Error(), "another account than acme-bot") {
		t.Fatalf("err = %v", err)
	}
	w.ghOwner["AAAAKEY"] = "Acme-Bot"
	if err := NewBox(d.remote(), m).CheckGitHub(context.Background(), false); err != nil {
		t.Fatalf("the account's own login failed: %v", err)
	}
}

func TestAddRerunChecksTheBoxGitHubLoginAgain(t *testing.T) {
	needKeygen(t)
	w, d, _ := newWorld(t)
	w.setFail("authorize", true)
	if err := Add(context.Background(), d, newAdd()); err == nil {
		t.Fatal("want the authorize failure")
	}
	w.setFail("authorize", false)
	if err := Add(context.Background(), d, newAdd()); err != nil {
		t.Fatal(err)
	}
	if n := len(w.ops("box-github-check")); n != 2 {
		t.Errorf("box github check ran %d times, want 2 (the first pass, then the rerun's check):\n%s", n, strings.Join(w.logCopy(), "\n"))
	}
}

func TestAddLeftBehindOmitsGrofeWhenNotInstalled(t *testing.T) {
	needKeygen(t)
	if os.Getuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	w, d, _ := newWorld(t)
	w.grofeOK = false
	blocked := filepath.Join(d.Home, ".config")
	os.Mkdir(blocked, 0o500)
	t.Cleanup(func() { os.Chmod(blocked, 0o700) })
	err := Add(context.Background(), d, newAdd())
	if err == nil || !strings.Contains(err.Error(), "machine-inventory") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "grofe machine") {
		t.Errorf("grofe machine listed as left behind though grofe is not installed: %v", err)
	}
	if !strings.Contains(err.Error(), "gh login on the box") {
		t.Errorf("left-behind list lost the other steps: %v", err)
	}
}

// A login banner before the probe's output must not be taken for the home.
func TestParseHostProbeIgnoresBanners(t *testing.T) {
	out := "Welcome to Ubuntu\r\nlast login: today\n" + hostProbeMark + "\n/home/dev\ndev.example.org\n"
	home, host, ok := parseHostProbe(out)
	if !ok || home != "/home/dev" || host != "dev.example.org" {
		t.Errorf("got %q %q %v", home, host, ok)
	}
	for _, bad := range []string{"", "/home/dev\ndev\n", hostProbeMark + "\n/home/dev\n"} {
		if _, _, ok := parseHostProbe(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}
