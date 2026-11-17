package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestIsInteractive(t *testing.T) {
	for _, c := range []struct{ nonInter, tty, want bool }{
		{false, false, false}, {false, true, true}, {true, true, false}, {true, false, false},
	} {
		if got := isInteractive(c.nonInter, c.tty); got != c.want {
			t.Errorf("isInteractive(%v, %v) = %v, want %v", c.nonInter, c.tty, got, c.want)
		}
	}
}

func promptSandbox(t *testing.T, tty bool) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("ATHOME_CI", "")
	t.Setenv("GITHUB_TOKEN", "")
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	old := stdinIsTTY
	stdinIsTTY = func() bool { return tty }
	t.Cleanup(func() { stdinIsTTY = old })
}

func askSet(t *testing.T, o initOpts, tty bool) bool {
	t.Helper()
	promptSandbox(t, tty)
	o.Machine, o.MachineSet = "personal", true
	env, code := newInitEnv(o, io.Discard, io.Discard)
	if env == nil {
		t.Fatalf("newInitEnv exit %d", code)
	}
	return env.Ask != nil
}

func TestNewInitEnvAsk(t *testing.T) {
	if askSet(t, initOpts{}, false) {
		t.Error("cli no tty sets nil Ask")
	}
	if !askSet(t, initOpts{}, true) {
		t.Error("tty should set Ask")
	}
	if askSet(t, initOpts{Yes: true}, true) {
		t.Error("cli -y sets nil Ask even with a TTY")
	}
	if askSet(t, initOpts{NonInter: true}, true) {
		t.Error("--non-interactive sets nil Ask")
	}
}
