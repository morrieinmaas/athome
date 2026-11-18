package steps_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/steps"
)

func remoteFixture(t *testing.T) *fixture {
	fx := newFixture(t)
	fx.Env.Cfg.Machine = "remote"
	fx.Env.Cfg.Data = map[string]string{
		"scope": "work/acme", "machineName": "acme-dev", "timezone": "Europe/Amsterdam", "locale": "en_US.UTF-8",
		"githubHandle": "octo", "githubId": "1",
	}
	return fx
}

func TestRemoteDataEntries(t *testing.T) {
	fx := remoteFixture(t)
	fx.MustExec(steps.Data())
	d := steps.StoredData(fx.Home)
	for k, want := range map[string]string{"machineType": "remote", "scope": "work/acme", "machineName": "acme-dev", "timezone": "Europe/Amsterdam", "locale": "en_US.UTF-8"} {
		if d[k] != want {
			t.Errorf("data %s = %v, want %s", k, d[k], want)
		}
	}
	p := newFixture(t)
	p.Env.Cfg.Data = map[string]string{"githubHandle": "octo", "githubId": "1"}
	p.MustExec(steps.Data())
	if pd := steps.StoredData(p.Home); pd["scope"] != nil || pd["machineName"] != nil {
		t.Errorf("a personal machine must not get remote keys: %v", pd)
	}
}

func TestRemoteProjectDirCheck(t *testing.T) {
	fx := remoteFixture(t)
	if done, _ := steps.Baseline()[projectDirsIndex(t)].Check(t.Context(), fx.Env); done {
		t.Fatal("done before ~/work/acme/.envrc exists")
	}
	writeFile(t, filepath.Join(fx.Home, "work", "acme", ".envrc"), "", 0o644)
	if done, _ := steps.Baseline()[projectDirsIndex(t)].Check(t.Context(), fx.Env); !done {
		t.Fatal("not done with ~/work/acme/.envrc present")
	}
}

func projectDirsIndex(t *testing.T) int {
	t.Helper()
	for i, s := range steps.Baseline() {
		if strings.Contains(s.Name, "project-dir") {
			return i
		}
	}
	t.Fatal("no project dirs step")
	return -1
}

// Criterion 7: a remote init never starts Bitwarden, never generates a key and asks nothing.
func TestRemoteInitRunsNoBitwardenOrKeygen(t *testing.T) {
	fx := remoteFixture(t)
	out := &strings.Builder{}
	fx.Env.Ask = prompt.New(strings.NewReader(""), out)
	fx.Env.Cfg.Data["bitwardenUrl"] = ""
	list := append(append(steps.Data(), steps.SSHKey()...), steps.Finish()...)
	if _, err := fx.Exec(list); err != nil {
		t.Fatalf("%v\n%s", err, fx.Log)
	}
	for _, c := range fx.Fake.Calls() {
		line := c.Name + " " + strings.Join(c.Args, " ")
		for _, bad := range []string{"bw-setup", "rbw register", "rbw login", "ssh-keygen -t"} {
			if strings.Contains(line, bad) {
				t.Errorf("remote init ran %q", line)
			}
		}
	}
	if out.Len() != 0 {
		t.Errorf("remote init prompted:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(fx.Home, ".ssh", "testhost_ed25519")); err == nil {
		t.Error("a key was generated")
	}
}

// An existing key on a remote box is used as is, whatever sshKeyAction says.
func TestRemoteExistingKeyIsUsedAsIs(t *testing.T) {
	for _, action := range []string{"", "generate", "reupload"} {
		t.Run("action="+action, func(t *testing.T) {
			fx := remoteFixture(t)
			out := &strings.Builder{}
			fx.Env.Ask = prompt.New(strings.NewReader(""), out)
			if action != "" {
				fx.Env.Cfg.Data["sshKeyAction"] = action
			}
			key := filepath.Join(fx.Home, ".ssh", "testhost_ed25519")
			writeFile(t, key, "PRIVATE", 0o600)
			writeFile(t, key+".pub", "ssh-ed25519 AAAA", 0o644)
			list := append(append(steps.Data(), steps.SSHKey()...), steps.Finish()...)
			if _, err := fx.Exec(list); err != nil {
				t.Fatalf("%v\n%s", err, fx.Log)
			}
			for _, c := range fx.Fake.Calls() {
				line := c.Name + " " + strings.Join(c.Args, " ")
				if strings.Contains(line, "ssh-keygen -t") || strings.Contains(line, "ssh-key add") {
					t.Errorf("remote init ran %q", line)
				}
			}
			if out.Len() != 0 {
				t.Errorf("remote init prompted:\n%s", out)
			}
			if b, _ := os.ReadFile(key); string(b) != "PRIVATE" {
				t.Errorf("key changed: %q", b)
			}
		})
	}
}
