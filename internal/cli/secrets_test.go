package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/cli"
	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/run/runtest"
)

func secretsEnv(t *testing.T) (home string, fake *runtest.Fake) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"SECRETS_VAULT", "SECRETS_VAULT_NAME", "SECRETS_VAULT_REPO", "ATHOME_GITHUB_HANDLE"} {
		t.Setenv(k, "")
	}
	fake = runtest.NewFake()
	fake.Have("gh")
	t.Cleanup(cli.SetRunner(fake))
	return home, fake
}

func TestSecretsUsage(t *testing.T) {
	secretsEnv(t)
	for _, args := range [][]string{
		{"secrets"}, {"secrets", "rekey"}, {"secrets", "backup", "x"}, {"secrets", "restore", "--wet"}, {"secrets", "restore", "--dry-run", "x"},
	} {
		code, _, errOut := runMain(args...)
		if code != 2 || !strings.Contains(errOut, "backup|restore") {
			t.Errorf("%v: code %d, stderr %q; want 2 naming backup|restore", args, code, errOut)
		}
	}
	if code, out, _ := runMain("secrets", "--help"); code != 0 || !strings.Contains(out, "backup|restore") {
		t.Errorf("--help: code %d out %q", code, out)
	}
	if _, out, _ := runMain("--help"); !strings.Contains(out, "secrets") {
		t.Errorf("main usage must list secrets: %q", out)
	}
}

func TestSecretsBackupNoVault(t *testing.T) {
	home, _ := secretsEnv(t)
	code, _, errOut := runMain("secrets", "backup")
	want := "vault not found at " + filepath.Join(home, ".local", "share", "attic")
	if code != 1 || !strings.Contains(errOut, want) {
		t.Errorf("code %d stderr %q", code, errOut)
	}
}

func TestSecretsRestoreDryRun(t *testing.T) {
	home, fake := secretsEnv(t)
	vault := filepath.Join(home, ".local", "share", "attic")
	os.MkdirAll(filepath.Join(vault, ".git"), 0o755)
	os.WriteFile(filepath.Join(vault, "a"), []byte("x"), 0o600)
	fake.On("git", "ls-files").Return(run.Result{Stdout: "a\n"})
	code, out, _ := runMain("secrets", "restore", "--dry-run")
	if code != 0 || !strings.Contains(out, "  would restore  "+filepath.Join(home, "a")) {
		t.Errorf("code %d out %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, "a")); err == nil {
		t.Errorf("dry run wrote")
	}
}
