package machines

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/run"
)

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
