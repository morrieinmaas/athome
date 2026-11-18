package machines

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shRun executes a box script locally against a temp HOME, as the box would.
func shRun(t *testing.T, home, script, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{"-c", script, "athome"}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestAuthorizedKeysScripts(t *testing.T) {
	home := t.TempDir()
	ak := filepath.Join(home, ".ssh", "authorized_keys")
	const k2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBBB two"
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o755)
	os.WriteFile(ak, []byte(`restrict `+pub1+"\nssh-rsa AAAAB3 hand"), 0o644) // no trailing newline

	if out, err := shRun(t, home, authorizeScript, pub1+"\n"); err != nil {
		t.Fatal(out, err)
	}
	if got, _ := os.ReadFile(ak); strings.Count(string(got), "AAAAC3NzaC1lZDI1NTE5AAAAIAAA") != 1 {
		t.Fatalf("blob present behind an option prefix was appended again:\n%s", got)
	}
	if out, err := shRun(t, home, authorizeScript, k2+"\n"); err != nil {
		t.Fatal(out, err)
	}
	got, _ := os.ReadFile(ak)
	if !strings.Contains(string(got), "hand\n"+k2+"\n") {
		t.Fatalf("not appended on its own line:\n%s", got)
	}
	for p, want := range map[string]os.FileMode{ak: 0o600, filepath.Dir(ak): 0o700} {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != want {
			t.Errorf("%s mode %v", p, fi.Mode().Perm())
		}
	}

	if out, err := shRun(t, home, resetScript, pub1+"\n"+k2+"\n"); err != nil {
		t.Fatal(out, err)
	}
	if got, _ := os.ReadFile(ak); string(got) != pub1+"\n"+k2+"\n" {
		t.Fatalf("reset left:\n%s", got)
	}
	if _, err := os.Stat(ak + ".athome"); err == nil {
		t.Error("temp authorized_keys.athome left")
	}
	if fi, _ := os.Stat(ak); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}

func TestRotateScripts(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	home := t.TempDir()
	out, err := shRun(t, home, newKeyScript, "", "dev", "acme-dev")
	if err != nil {
		t.Fatal(out, err)
	}
	pub := strings.TrimSpace(out)
	if checkPub(pub) != nil || !strings.HasSuffix(pub, "athome@acme-dev") {
		t.Fatalf("pub %q", pub)
	}
	if again, _ := shRun(t, home, newKeyScript, "", "dev", "acme-dev"); strings.TrimSpace(again) != pub {
		t.Fatal("rotate key regenerated")
	}
	if out, err := shRun(t, home, promoteScript, "", "dev", blobOf(pub)); err != nil {
		t.Fatal(out, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "dev_ed25519.rotate")); err == nil {
		t.Error(".rotate left")
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".ssh", "dev_ed25519.pub")); strings.TrimSpace(string(got)) != pub {
		t.Fatalf("pub not promoted: %s", got)
	}
	if out, err := shRun(t, home, promoteScript, "", "dev", blobOf(pub)); err != nil { // already promoted
		t.Fatal(out, err)
	}
	if _, err := shRun(t, home, promoteScript, "", "dev", "OTHER"); err == nil {
		t.Fatal("promote of an unrelated key passed")
	}
}

// A box whose athome checkout sits on a newer commit or tag than the laptop
// pinned goes back to exactly the pinned tag, detached, never to a branch tip.
func TestCheckoutScriptKeepsBoxOnPinnedTag(t *testing.T) {
	home := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin")
	git := func(dir string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
		c.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(origin, "init", "-q", "-b", "main")
	git(origin, "commit", "-q", "--allow-empty", "-m", "one")
	git(origin, "tag", "v1.4.0")
	pinned := git(origin, "rev-parse", "HEAD")
	git(origin, "commit", "-q", "--allow-empty", "-m", "two")
	git(origin, "tag", "v1.5.0")

	d := filepath.Join(home, ".local", "share", "athome")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	git(home, "clone", "-q", origin, d) // on main, at the newest commit
	if out, err := shRun(t, home, checkoutScript, "", "v1.4.0"); err != nil {
		t.Fatal(out, err)
	}
	if got := git(d, "rev-parse", "HEAD"); got != pinned {
		t.Errorf("HEAD %s, want the pinned tag %s", got, pinned)
	}
	if b := git(d, "branch", "--show-current"); b != "" {
		t.Errorf("box is on branch %q, want detached", b)
	}
}
