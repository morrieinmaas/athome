package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/steps"
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

func TestIsTerminalFalseForDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("/dev/null must not count as a terminal")
	}
}

type exited int

// Ctrl-C at an open prompt cleans up and exits 130; it does not take the default.
func TestCancelAtPromptExits130(t *testing.T) {
	var code int
	old := exit
	exit = func(c int) { code = c; panic(exited(c)) }
	t.Cleanup(func() { exit = old })

	pr, pw := io.Pipe()
	defer pw.Close()
	var errOut bytes.Buffer
	cleaned := false
	env := &steps.Env{Ask: prompt.New(pr, &errOut), Cleanup: []func(){func() { cleaned = true }}}
	ctx, cancel := context.WithCancel(context.Background())
	bindCancel(ctx, env, &errOut)

	got := make(chan any, 1)
	go func() {
		defer func() { got <- recover() }()
		env.Ask.Confirm("go?", true)
	}()
	cancel()
	if r := <-got; r != exited(exitInterrupted) {
		t.Fatalf("Confirm ended with %v, want exit(130)", r)
	}
	if code != 130 || !cleaned || !strings.Contains(errOut.String(), "athome: interrupted") {
		t.Errorf("code=%d cleaned=%v stderr=%q", code, cleaned, errOut.String())
	}
}

// A relative ATHOME_REPO is made absolute.
func TestFindRepoRootAbsolutizesAthomeRepo(t *testing.T) {
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "repo", ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(parent)
	t.Setenv("ATHOME_REPO", "repo")
	got, err := findRepoRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || filepath.Base(got) != "repo" {
		t.Errorf("findRepoRoot = %q, want an absolute path ending in repo", got)
	}
}
