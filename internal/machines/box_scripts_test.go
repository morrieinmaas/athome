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
	if err := os.WriteFile(filepath.Join(origin, "f"), []byte("pinned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(origin, "add", "f")
	git(origin, "commit", "-q", "-m", "one")
	git(origin, "tag", "v1.4.0")
	pinned := git(origin, "rev-parse", "HEAD")
	git(origin, "commit", "-q", "--allow-empty", "-m", "two")
	git(origin, "tag", "v1.5.0")

	d := filepath.Join(home, ".local", "share", "athome")
	if err := os.MkdirAll(filepath.Dir(d), 0o755); err != nil {
		t.Fatal(err)
	}
	git(home, "clone", "-q", origin, d) // on main, at the newest commit
	// The box's origin points elsewhere and its tree has local edits: the script
	// puts origin back to the canonical URL (here redirected to the fixture) and discards the edits.
	git(d, "remote", "set-url", "origin", "https://example.invalid/fork/athome")
	if err := os.WriteFile(filepath.Join(d, "f"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[url \""+origin+"\"]\n\tinsteadOf = https://github.com/morrieinmaas/athome\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := shRun(t, home, checkoutScript, "", "v1.4.0"); err != nil {
		t.Fatal(out, err)
	}
	if got := git(d, "rev-parse", "HEAD"); got != pinned {
		t.Errorf("HEAD %s, want the pinned tag %s", got, pinned)
	}
	if b := git(d, "branch", "--show-current"); b != "" {
		t.Errorf("box is on branch %q, want detached", b)
	}
	if got, _ := os.ReadFile(filepath.Join(d, "f")); string(got) != "pinned\n" {
		t.Errorf("local edit survived the checkout: %q", got)
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

// The box's own ~/.ssh/config must not widen the check: -F /dev/null drops it,
// so only the one -i key is offered.
func TestCheckGitHubScriptOffersOnlyTheGivenKey(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	stub := "#!/bin/sh\necho \"$@\" > \"$HOME/ssh.args\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	shRun(t, home, checkGitHubScript, githubKnownHosts, "dev", "")
	b, _ := os.ReadFile(filepath.Join(home, "ssh.args"))
	args := string(b)
	for _, want := range []string{"-F /dev/null", "-o IdentitiesOnly=yes", "-o BatchMode=yes", "-o StrictHostKeyChecking=yes", "-o UserKnownHostsFile="} {
		if !strings.Contains(args, want) {
			t.Errorf("ssh args lack %q: %s", want, args)
		}
	}
	if n := strings.Count(args, "-i "); n != 1 {
		t.Errorf("%d identity files offered: %s", n, args)
	}
}
