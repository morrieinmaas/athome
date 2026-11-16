package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
)

// Main(init ...) executes real steps once it gets past argument and config
// handling. These tests never let it get that far:
//   - the usage-error tests run inside a valid-looking repo, but every case
//     they use must end in exit 2 before any step runs;
//   - the "accepted flag" tests run in a directory with no .chezmoiroot above
//     it, so a flag that parses ends in exit 1 (no repo), never in a step.
//
// HOME is a temp dir throughout.

func sandbox(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, k := range []string{"GITHUB_TOKEN", "ATHOME_CI", "ATHOME_GITHUB_HANDLE"} {
		t.Setenv(k, "")
	}
	return home
}

// inNoRepo runs from a directory that has no .chezmoiroot at or above it.
func inNoRepo(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

// inRepo runs from a directory that looks like the athome checkout.
func inRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "home", ".chezmoiscripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".chezmoiroot"), []byte("home\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	return repo
}

func runMain(args ...string) (code int, stdout, stderr string) {
	var o, e bytes.Buffer
	code = cli.Main(args, &o, &e)
	return code, o.String(), e.String()
}

func tmpFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVersion(t *testing.T) {
	sandbox(t)
	code, out, _ := runMain("version")
	if code != 0 || out != "athome dev (none)\n" {
		t.Errorf("Main(version) = %d, %q; want 0, %q", code, out, "athome dev (none)\n")
	}
}

func TestVersionSetAtBuildTime(t *testing.T) {
	sandbox(t)
	oldV, oldC := cli.Version, cli.Commit
	t.Cleanup(func() { cli.Version, cli.Commit = oldV, oldC })
	cli.Version, cli.Commit = "1.2.3", "abc1234"
	code, out, _ := runMain("version")
	if code != 0 || out != "athome 1.2.3 (abc1234)\n" {
		t.Errorf("Main(version) = %d, %q; want 0, %q", code, out, "athome 1.2.3 (abc1234)\n")
	}
}

func TestHelp(t *testing.T) {
	for _, args := range [][]string{{"init", "-h"}, {"init", "--help"}, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			sandbox(t)
			inNoRepo(t)
			code, out, errOut := runMain(args...)
			if code != 0 {
				t.Errorf("exit = %d, want 0", code)
			}
			if !strings.Contains(out+errOut, "init") {
				t.Errorf("help must mention init, got stdout %q stderr %q", out, errOut)
			}
		})
	}
}

func TestInitHelpListsEveryFlag(t *testing.T) {
	sandbox(t)
	inNoRepo(t)
	_, out, errOut := runMain("init", "--help")
	text := out + errOut
	for _, f := range []string{"--ref", "--machine", "--config", "--no-agents", "--import-from", "--import-ssh", "--import-ssh-bw", "--non-interactive"} {
		if !strings.Contains(text, f) {
			t.Errorf("init --help does not list %s:\n%s", f, text)
		}
	}
}

func TestUnknownFlag(t *testing.T) {
	for _, args := range [][]string{
		{"init", "--bogus"},
		{"init", "-x"},
		{"init", "--machine", "work", "--bogus"},
		{"init", "--bogus", "--machine", "work"},
		{"init", "--no-such-flag=1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			sandbox(t)
			inRepo(t)
			code, _, errOut := runMain(args...)
			if code != 2 {
				t.Errorf("exit = %d, want 2", code)
			}
			if errOut == "" {
				t.Errorf("an unknown flag needs a message on stderr")
			}
		})
	}
}

// Every flag bash accepts parses; with no repo above the cwd the run then ends
// in exit 1 before any step.
func TestFlags(t *testing.T) {
	cases := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{"--ref", func(*testing.T) []string { return []string{"--ref", "v1.2.3"} }},
		{"--machine work", func(*testing.T) []string { return []string{"--machine", "work"} }},
		{"--machine personal", func(*testing.T) []string { return []string{"--machine", "personal"} }},
		{"--config", func(t *testing.T) []string { return []string{"--config", tmpFile(t, "a.toml", "")} }},
		{"--no-agents", func(*testing.T) []string { return []string{"--no-agents"} }},
		{"--import-from", func(t *testing.T) []string { return []string{"--import-from", t.TempDir()} }},
		{"--import-from --import-ssh", func(t *testing.T) []string { return []string{"--import-from", t.TempDir(), "--import-ssh"} }},
		{"--import-ssh-bw", func(*testing.T) []string { return []string{"--import-ssh-bw", "ssh/box_ed25519"} }},
		{"--non-interactive", func(*testing.T) []string { return []string{"--non-interactive"} }},
		{"-y", func(*testing.T) []string { return []string{"-y"} }},
		{"--yes", func(*testing.T) []string { return []string{"--yes"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sandbox(t)
			args := append([]string{"init"}, c.args(t)...)
			inNoRepo(t)
			code, _, errOut := runMain(args...)
			if code != 1 {
				t.Errorf("Main(%q) = %d, want 1 (flag accepted, then no repo found); stderr %q", args, code, errOut)
			}
			if errOut == "" {
				t.Errorf("exit 1 needs a message on stderr")
			}
		})
	}
}

func TestAllFlagsTogether(t *testing.T) {
	sandbox(t)
	cfg := tmpFile(t, "a.toml", "")
	inNoRepo(t)
	code, _, _ := runMain("init", "--ref", "main", "--machine", "work", "--config", cfg, "--no-agents",
		"--import-from", t.TempDir(), "--import-ssh", "--import-ssh-bw", "ssh/x", "--non-interactive", "-y", "--yes")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestUsageError(t *testing.T) {
	t.Run("bad machine", func(t *testing.T) {
		sandbox(t)
		inRepo(t)
		code, _, errOut := runMain("init", "--machine", "laptop")
		if code != 2 || !strings.Contains(errOut, "laptop") {
			t.Errorf("exit %d stderr %q; want 2 naming laptop", code, errOut)
		}
	})
	t.Run("unreadable config", func(t *testing.T) {
		sandbox(t)
		inRepo(t)
		missing := filepath.Join(t.TempDir(), "missing.toml")
		code, _, errOut := runMain("init", "--config", missing)
		if code != 2 || !strings.Contains(errOut, "missing.toml") {
			t.Errorf("exit %d stderr %q; want 2 naming the file", code, errOut)
		}
	})
	t.Run("bad machine in the file", func(t *testing.T) {
		sandbox(t)
		inRepo(t)
		cfg := tmpFile(t, "a.toml", "machine = \"laptop\"\n")
		code, _, errOut := runMain("init", "--config", cfg)
		if code != 2 || !strings.Contains(errOut, "laptop") {
			t.Errorf("exit %d stderr %q; want 2 naming laptop", code, errOut)
		}
	})
	t.Run("bad includeAgents in the file", func(t *testing.T) {
		sandbox(t)
		inRepo(t)
		cfg := tmpFile(t, "a.toml", "includeAgents = \"maybe\"\n")
		code, _, errOut := runMain("init", "--config", cfg)
		if code != 2 || !strings.Contains(errOut, "maybe") {
			t.Errorf("exit %d stderr %q; want 2 naming maybe", code, errOut)
		}
	})
}

func TestNotARepoIsExit1(t *testing.T) {
	sandbox(t)
	inNoRepo(t)
	code, _, errOut := runMain("init", "--non-interactive")
	if code != 1 {
		t.Errorf("exit = %d, want 1 (an ordinary error, not a usage error)", code)
	}
	if !strings.Contains(errOut, "chezmoiroot") && !strings.Contains(strings.ToLower(errOut), "repo") {
		t.Errorf("stderr %q should say the athome repo was not found", errOut)
	}
}

func TestImportSSHAlone(t *testing.T) {
	sandbox(t)
	inRepo(t)
	code, _, errOut := runMain("init", "--import-ssh")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "--import-from") {
		t.Errorf("message %q must point at --import-from", errOut)
	}
}

func TestRepoRootFromSubdirectory(t *testing.T) {
	sandbox(t)
	repo := inRepo(t)
	sub := filepath.Join(repo, "scripts", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	// The repo is found from a subdirectory, so config validation (exit 2) is
	// reached rather than the no-repo error (exit 1).
	code, _, _ := runMain("init", "--machine", "laptop")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}
