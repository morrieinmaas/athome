package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestValidatePortAndJumpPortRanges(t *testing.T) {
	for _, port := range []int{0, -1, 65536, 70000} {
		m := acme()
		m.Port = port
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "port") {
			t.Errorf("port %d: %v", port, err)
		}
	}
	for _, port := range []int{1, 22, 65535} {
		m := acme()
		m.Port = port
		if err := m.Validate(); err != nil {
			t.Errorf("port %d: %v", port, err)
		}
	}
	for _, jump := range []string{"bastion:0", "bastion:00", "ops@bastion:65536", "bastion:99999"} {
		m := acme()
		m.Jump = jump
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "jump") {
			t.Errorf("jump %q: %v", jump, err)
		}
	}
	for _, jump := range []string{"bastion:1", "bastion:65535", "ops@bastion:22"} {
		m := acme()
		m.Jump = jump
		if err := m.Validate(); err != nil {
			t.Errorf("jump %q: %v", jump, err)
		}
	}
}

// A machine built from flags without --port gets port 22 before validation, and
// an inventory entry without a port reads as 22.
func TestPortDefaultsToTwentyTwoBeforeValidation(t *testing.T) {
	_, d, _ := newWorld(t)
	m := newAdd()
	m.Port = 0
	if err := Add(context.Background(), d, m); err != nil {
		t.Fatal(err)
	}
	ms, err := Load(d.Home)
	if err != nil || len(ms) != 1 || ms[0].Port != 22 {
		t.Fatalf("%v %v", ms, err)
	}
	if err := os.WriteFile(Path(d.Home), []byte("[machines.old]\nscope = \"work/acme\"\ntarget = \"h\"\nuser = \"dev\"\nathome = \"v0.4.0\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ms, err := Load(d.Home); err != nil || len(ms) != 1 || ms[0].Port != 22 {
		t.Fatalf("%v %v", ms, err)
	}
}
