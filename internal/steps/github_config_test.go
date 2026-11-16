package steps_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/morrieinmaas/athome/internal/run"
	"github.com/morrieinmaas/athome/internal/steps"
)

func sshConfigSeed(t *testing.T, fx *fixture) string {
	t.Helper()
	return readFile(t, fx.Path(".ssh", "config"))
}

func TestSeed(t *testing.T) {
	t.Run("creates the github.com block", func(t *testing.T) {
		fx := ghFixture(t)
		fx.MustExec(steps.GitHub())
		got := sshConfigSeed(t, fx)
		for _, want := range []string{"Host github.com", "HostName github.com", "User git", "IdentityFile ~/.ssh/id_ed25519", "IdentityFile ~/.ssh/" + testHost + "_ed25519"} {
			if !strings.Contains(got, want) {
				t.Errorf("~/.ssh/config lacks %q:\n%s", want, got)
			}
		}
		if fi, _ := os.Stat(fx.Path(".ssh", "config")); fi.Mode().Perm() != 0o600 {
			t.Errorf("config mode = %v, want 0600", fi.Mode().Perm())
		}
	})
	t.Run("appends after existing content", func(t *testing.T) {
		fx := ghFixture(t)
		writeFile(t, fx.Path(".ssh", "config"), "Host other\n    User me\n", 0o600)
		fx.MustExec(steps.GitHub())
		got := sshConfigSeed(t, fx)
		if !strings.HasPrefix(got, "Host other\n    User me\n") || !strings.Contains(got, "Host github.com") {
			t.Errorf("existing config must be kept and the block appended:\n%s", got)
		}
	})
	t.Run("unchanged when the key basename is already present", func(t *testing.T) {
		fx := ghFixture(t)
		existing := "Host github.com\n    IdentityFile ~/.ssh/" + testHost + "_ed25519\n"
		writeFile(t, fx.Path(".ssh", "config"), existing, 0o600)
		fx.MustExec(steps.GitHub())
		if got := sshConfigSeed(t, fx); got != existing {
			t.Errorf("config must not change:\n%s", got)
		}
	})
	t.Run("an existing config is never rewritten", func(t *testing.T) {
		fx := ghFixture(t)
		existing := "Host github.com\n    IdentityFile ~/.ssh/" + testHost + "_ed25519\n"
		path := fx.Path(".ssh", "config")
		writeFile(t, path, existing, 0o600)
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		fx.MustExec(steps.GitHub())
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(old) {
			t.Errorf("config was rewritten: mtime %v, want %v", fi.ModTime(), old)
		}
	})
	t.Run("never seeded when gh is missing", func(t *testing.T) {
		fx := ghFixture(t)
		fx.Fake.On("gh", "auth", "status").Return(run.Result{Code: 1})
		fx.MustExec(steps.GitHub())
		if exists(fx.Path(".ssh", "config")) {
			t.Errorf("no upload, no seed")
		}
	})
}

func TestRemoteToSSH(t *testing.T) {
	cases := []struct{ name, origin, want string }{
		{"https without .git", "https://github.com/morrieinmaas/athome", "git@github.com:morrieinmaas/athome.git"},
		{"https with .git", "https://github.com/morrieinmaas/athome.git", "git@github.com:morrieinmaas/athome.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fx := ghFixture(t)
			fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: c.origin + "\n"})
			fx.MustExec(steps.GitHub())
			calls := fx.callsTo("git", "remote", "set-url", "origin")
			if len(calls) != 1 || calls[0].Args[len(calls[0].Args)-1] != c.want {
				t.Errorf("want `git remote set-url origin %s`, recorded:\n%s", c.want, fx.dump())
			}
		})
	}
	for _, origin := range []string{"git@github.com:morrieinmaas/athome.git", "https://example.org/x/y", ""} {
		t.Run("untouched "+origin, func(t *testing.T) {
			fx := ghFixture(t)
			fx.Fake.On("git", "-C", fx.Repo, "remote", "get-url", "origin").Return(run.Result{Stdout: origin + "\n"})
			fx.MustExec(steps.GitHub())
			if fx.called("git", "remote", "set-url") {
				t.Errorf("origin %q must be left alone:\n%s", origin, fx.dump())
			}
		})
	}
}

func TestRemoteSetURLFailureIsNotReported(t *testing.T) {
	fx := ghFixture(t)
	fx.Fake.On("git", "-C", fx.Repo, "remote", "set-url", "origin", "git@github.com:morrieinmaas/athome.git").Return(run.Result{Code: 1})
	fx.MustExec(steps.GitHub())
	if strings.Contains(fx.Log.String(), "HTTPS -> SSH") {
		t.Errorf("a failed set-url must not be logged as success:\n%s", fx.Log)
	}
}

// Z5: the seeded block is the one scripts/lib/ssh.sh writes, marker line included.
func TestSeedMatchesBashHelper(t *testing.T) {
	b, err := os.ReadFile("../../scripts/lib/ssh.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	start := strings.Index(src, "cat >> \"$HOME/.ssh/config\" <<SSHSEED\n")
	end := strings.Index(src, "\nSSHSEED\n")
	if start < 0 || end < start {
		t.Fatal("seed heredoc not found in scripts/lib/ssh.sh")
	}
	want := src[start+len("cat >> \"$HOME/.ssh/config\" <<SSHSEED\n") : end+1]
	want = strings.ReplaceAll(want, "$hostkey", testHost+"_ed25519")

	fx := ghFixture(t)
	fx.MustExec(steps.GitHub())
	if got := sshConfigSeed(t, fx); got != want {
		t.Errorf("Go seed differs from ssh.sh:\n--- go\n%q\n--- bash\n%q", got, want)
	}
}
