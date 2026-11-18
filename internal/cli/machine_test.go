package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/keys"
	"github.com/morrieinmaas/athome/internal/machines"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

const inventory = "[machines.acme-dev]\nscope = \"work/acme\"\ntarget = \"dev.acme.example\"\nuser = \"dev\"\nathome = \"v0.5.2\"\n"

// machineEnv is a temp HOME with an ssh config that includes athome/config, a
// fake runner with rbw unlocked, and version 0.5.2.
func machineEnv(t *testing.T) (home string, fake *runtest.Fake) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("USER", "laptopuser")
	t.Setenv("GROFE", "")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Include athome/config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake = runtest.NewFake()
	fake.Have("rbw")
	fake.On("gh", "api", "user", "--jq", ".login").Return(run.Result{Stdout: "octo\n"})
	t.Cleanup(cli.SetRunner(fake))
	old := cli.Version
	cli.Version = "0.5.2"
	t.Cleanup(func() { cli.Version = old })
	t.Cleanup(cli.SetStdinTTY(false))
	return home, fake
}

func writeInventory(t *testing.T, home, body string) {
	t.Helper()
	p := machines.Path(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func machineMain(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := cli.Main(args, &out, &errw)
	return code, out.String(), errw.String()
}

func TestMachineUsage(t *testing.T) {
	machineEnv(t)
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"machine"}, 2, "Usage: athome machine"},
		{[]string{"machine", "bogus"}, 2, "unknown command"},
		{[]string{"machine", "--help"}, 0, "Usage: athome machine"},
		{[]string{"machine", "add", "--help"}, 0, "--forward-agent"},
		{[]string{"machine", "add"}, 2, "expected 1 argument"},
		{[]string{"machine", "add", "a", "b"}, 2, "expected 1 argument"},
		{[]string{"machine", "add", "a", "--bogus"}, 2, "bogus"},
		{[]string{"machine", "add", "a", "--port", "x"}, 2, "port"},
		{[]string{"machine", "sync"}, 2, "usage: athome machine sync"},
		{[]string{"machine", "sync", "a", "--all"}, 2, "usage: athome machine sync"},
		{[]string{"machine", "sync", "a", "b"}, 2, "at most one"},
		{[]string{"machine", "rm"}, 2, "expected 1 argument"},
		{[]string{"machine", "ls", "x"}, 2, "expected 0 argument"},
		{[]string{"project"}, 2, "Usage: athome project"},
		{[]string{"project", "bogus"}, 2, "unknown command"},
		{[]string{"project", "--help"}, 0, "Usage: athome project"},
		{[]string{"project", "clone"}, 2, "expected one repo"},
		{[]string{"project", "clone", "acme/x", "--bogus"}, 2, "bogus"},
		{[]string{"project", "clone", "not a repo"}, 2, "not a repo"},
	} {
		code, out, errw := machineMain(tc.args...)
		if code != tc.code || !strings.Contains(out+errw, tc.want) {
			t.Errorf("%v = %d, %q %q; want %d with %q", tc.args, code, out, errw, tc.code, tc.want)
		}
	}
}

func TestUsageListsMachineAndProject(t *testing.T) {
	var out bytes.Buffer
	cli.Main([]string{"--help"}, &out, &out)
	for _, want := range []string{
		"  machine   manage remote dev machines (add|sync|rm|ls)\n",
		"  project   clone a project onto a remote machine (clone)\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("usage lacks %q:\n%s", want, out.String())
		}
	}
}

func TestMachineLs(t *testing.T) {
	home, _ := machineEnv(t)
	if code, out, _ := machineMain("machine", "ls"); code != 0 || !strings.Contains(out, "no machines") {
		t.Errorf("empty: %d %q", code, out)
	}
	writeInventory(t, home, inventory)
	code, out, _ := machineMain("machine", "ls")
	if code != 0 || !strings.Contains(out, "acme-dev") || !strings.Contains(out, "dev@dev.acme.example") {
		t.Errorf("ls: %d %q", code, out)
	}
}

func TestMachineAddFlagsAndDefaults(t *testing.T) {
	// the host-key step reads nothing from the fake ssh, so add stops there
	// with the pending entry saved: it shows what the flags and defaults made.
	for name, args := range map[string][]string{
		"flags after the name":  {"machine", "add", "box", "--scope", "work/acme", "--target", "dev.example", "--jump", "j", "--port", "2222", "--github-account", "acct", "--forward-agent"},
		"flags before the name": {"machine", "add", "--scope", "work/acme", "--target", "dev.example", "--jump", "j", "--port", "2222", "--github-account", "acct", "--forward-agent", "box"},
	} {
		t.Run(name, func(t *testing.T) {
			home, _ := machineEnv(t)
			code, _, errw := machineMain(args...)
			if code != 1 || !strings.Contains(errw, "machine-host-key") {
				t.Fatalf("exit %d, stderr %q; want 1 at machine-host-key", code, errw)
			}
			p, err := machines.LoadPending(home, "box")
			if err != nil || p == nil {
				t.Fatalf("pending %v, %v", p, err)
			}
			want := machines.Machine{Name: "box", Scope: "work/acme", Target: "dev.example", User: "laptopuser", Jump: "j", Port: 2222,
				Athome: "v0.5.2", ForwardAgent: true, GitHubAccount: "acct"}
			if p.Name != want.Name || p.Scope != want.Scope || p.Target != want.Target || p.User != want.User || p.Jump != want.Jump ||
				p.Port != want.Port || p.Athome != want.Athome || !p.ForwardAgent || p.GitHubAccount != want.GitHubAccount {
				t.Errorf("pending = %+v, want %+v", *p, want)
			}
		})
	}
}

func TestMachineAddExplicitUserAndAthome(t *testing.T) {
	home, _ := machineEnv(t)
	cli.Version = "dev"
	code, _, errw := machineMain("machine", "add", "box", "--scope", "work/acme", "--target", "t", "--user", "me", "--athome", "v1.2.3")
	if code != 1 || !strings.Contains(errw, "machine-host-key") {
		t.Fatalf("exit %d, stderr %q", code, errw)
	}
	p, _ := machines.LoadPending(home, "box")
	if p == nil || p.User != "me" || p.Athome != "v1.2.3" {
		t.Errorf("pending = %+v", p)
	}
}

func TestMachineAddNeedsFlagsWithoutTerminal(t *testing.T) {
	_, fake := machineEnv(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"machine", "add", "box", "--target", "t"}, "--scope"},
		{[]string{"machine", "add", "box", "--scope", "work/acme"}, "--target"},
	} {
		code, _, errw := machineMain(tc.args...)
		if code != 2 || !strings.Contains(errw, tc.want) {
			t.Errorf("%v = %d %q; want 2 naming %s", tc.args, code, errw, tc.want)
		}
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d commands ran", n)
	}
}

func TestMachineAddDevVersionNeedsAthomeFlag(t *testing.T) {
	_, fake := machineEnv(t)
	cli.Version = "dev"
	code, _, errw := machineMain("machine", "add", "box", "--scope", "work/acme", "--target", "t")
	if code != 2 || !strings.Contains(errw, "--athome") {
		t.Errorf("exit %d, stderr %q; want 2 asking for --athome", code, errw)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d commands ran", n)
	}
}

func TestMachineAddAsksOnATerminal(t *testing.T) {
	home, _ := machineEnv(t)
	t.Cleanup(cli.SetStdinTTY(true))
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("work/acme\ndev.example\n")
	w.Close()
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
	code, out, errw := machineMain("machine", "add", "box")
	if code != 1 || !strings.Contains(errw, "machine-host-key") || !strings.Contains(out, "scope") {
		t.Fatalf("exit %d, stdout %q stderr %q", code, out, errw)
	}
	p, _ := machines.LoadPending(home, "box")
	if p == nil || p.Scope != "work/acme" || p.Target != "dev.example" {
		t.Errorf("pending = %+v", p)
	}
}

func TestMachineAddRefusesLocalScope(t *testing.T) {
	home, fake := machineEnv(t)
	if err := os.MkdirAll(filepath.Join(home, "work", "acme", "r", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, errw := machineMain("machine", "add", "box", "--scope", "work/acme", "--target", "t")
	if code != 1 || !strings.Contains(errw, "holds git repositories") {
		t.Errorf("exit %d, stderr %q", code, errw)
	}
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d commands ran: %v", n, fake.Calls())
	}
}

func TestMachineAddExistingNameExits1(t *testing.T) {
	home, _ := machineEnv(t)
	writeInventory(t, home, inventory)
	code, _, errw := machineMain("machine", "add", "acme-dev", "--scope", "work/acme", "--target", "t")
	if code != 1 || !strings.Contains(errw, "machine acme-dev already exists") {
		t.Errorf("exit %d, stderr %q", code, errw)
	}
}

func TestMachineSyncAndRmUnknown(t *testing.T) {
	machineEnv(t)
	for _, args := range [][]string{{"machine", "sync", "ghost"}, {"machine", "rm", "ghost"}} {
		code, _, errw := machineMain(args...)
		if code != 1 || !strings.Contains(errw, "ghost") || !strings.HasPrefix(errw, "athome machine") {
			t.Errorf("%v = %d %q", args, code, errw)
		}
	}
}

func TestProjectCloneNeedsMachine(t *testing.T) {
	machineEnv(t)
	code, _, errw := machineMain("project", "clone", "acme/widget")
	if code != 2 {
		t.Errorf("exit %d, stderr %q; want 2 with no machines", code, errw)
	}
}

func TestKeysInventoryIsWired(t *testing.T) {
	home, _ := machineEnv(t)
	writeInventory(t, home, inventory)
	old := keys.Inventory
	t.Cleanup(func() { keys.Inventory = old })
	keys.Inventory = func(string) ([]keys.Box, error) { return nil, nil }
	if code, _, _ := machineMain("keys", "--help"); code != 0 {
		t.Fatal("keys --help failed")
	}
	boxes, err := keys.Inventory(home)
	if err != nil || len(boxes) != 1 || boxes[0].Name() != "acme-dev" {
		t.Errorf("keys.Inventory = %v, %v; want the one machine", boxes, err)
	}
}

func TestStatusMachines(t *testing.T) {
	env := newStatusEnv(t)
	writeInventory(t, env.home, inventory)
	if err := os.MkdirAll(filepath.Join(env.home, "work", "acme", "r", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runStatus("--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var rep struct {
		Current  bool `json:"current"`
		Machines struct {
			Count    int      `json:"count"`
			Warnings []string `json:"warnings"`
		} `json:"machines"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if !rep.Current || rep.Machines.Count != 1 || len(rep.Machines.Warnings) != 1 || !strings.Contains(rep.Machines.Warnings[0], "work/acme") {
		t.Errorf("report = %+v", rep)
	}
	code, text, _ := runStatus()
	if code != 0 || !strings.Contains(text, "machines: ~/work/acme holds git repositories") {
		t.Errorf("text exit %d: %s", code, text)
	}
}

func TestStatusBrokenInventoryIsAWarning(t *testing.T) {
	env := newStatusEnv(t)
	writeInventory(t, env.home, strings.Replace(inventory, "work/acme", "home/acme", 1))
	code, out, _ := runStatus("--json")
	if code != 0 || !strings.Contains(out, `"machines":{"count":0,"warnings":["`) || !strings.Contains(out, "acme-dev") {
		t.Errorf("exit %d: %s", code, out)
	}
}
