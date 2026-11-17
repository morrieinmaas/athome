package steps_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/morrieinmaas/athome/internal/prompt"
	"github.com/morrieinmaas/athome/internal/steps"
)

func sk2Ask(fx *fixture, answers string) *bytes.Buffer {
	out := &bytes.Buffer{}
	fx.Env.Ask = prompt.New(strings.NewReader(answers), out)
	return out
}

func sk2Keygens(fx *fixture) int {
	return len(fx.callsTo("ssh-keygen", "-t", "ed25519", "-f", keyPath(fx)))
}

func TestPromptKeyAbsent(t *testing.T) {
	t.Run("yes generates like the default", func(t *testing.T) {
		fx := newFixture(t)
		out := sk2Ask(fx, "y\n")
		fx.MustExec(steps.SSHKey())
		if sk2Keygens(fx) != 1 || !fx.Env.KeyGenerated {
			t.Errorf("want one keygen and KeyGenerated, got %d / %v", sk2Keygens(fx), fx.Env.KeyGenerated)
		}
		if !strings.Contains(out.String(), "create one?") {
			t.Errorf("no prompt shown: %q", out)
		}
		fx2 := newFixture(t) // no terminal: generating is the default
		fx2.MustExec(steps.SSHKey())
		pr5SameRun(t, fx, fx2)
	})
	t.Run("no equals generateSshKey=false", func(t *testing.T) {
		fx := newFixture(t)
		sk2Ask(fx, "n\n")
		fx.MustExec(steps.SSHKey())
		if sk2Keygens(fx) != 0 || fx.Env.KeyGenerated {
			t.Errorf("n must not generate (keygens %d, generated %v)", sk2Keygens(fx), fx.Env.KeyGenerated)
		}
		if fx.Env.Cfg.Data["generateSshKey"] != "false" {
			t.Errorf("answer not recorded: %v", fx.Env.Cfg.Data)
		}
		fx2 := newFixture(t)
		fx2.Env.Cfg.Data["generateSshKey"] = "false"
		fx2.MustExec(steps.SSHKey())
		pr5SameRun(t, fx, fx2)
	})
	t.Run("preset prints nothing", func(t *testing.T) {
		fx := newFixture(t)
		fx.Env.Cfg.Data["generateSshKey"] = "false"
		out := sk2Ask(fx, "y\n")
		fx.MustExec(steps.SSHKey())
		if out.Len() != 0 || sk2Keygens(fx) != 0 {
			t.Errorf("preset false must not ask or generate: %q", out)
		}
	})
	t.Run("nil Ask generates", func(t *testing.T) {
		fx := newFixture(t)
		fx.MustExec(steps.SSHKey())
		if sk2Keygens(fx) != 1 {
			t.Errorf("keygens = %d, want 1", sk2Keygens(fx))
		}
	})
}

func TestPromptKeyExisting(t *testing.T) {
	cases := map[string]struct {
		answer   string
		keygens  int
		recorded string
	}{
		"use":      {"u\n", 0, "use"},
		"default":  {"\n", 0, "use"},
		"generate": {"generate\n", 1, "generate"},
		"reupload": {"reupload\n", 0, "reupload"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.PutKey("PRIV", pubLine)
			out := sk2Ask(fx, c.answer)
			fx.MustExec(steps.SSHKey())
			if sk2Keygens(fx) != c.keygens {
				t.Errorf("keygens = %d, want %d", sk2Keygens(fx), c.keygens)
			}
			if fx.Env.Cfg.Data["sshKeyAction"] != c.recorded {
				t.Errorf("sshKeyAction = %q, want %q", fx.Env.Cfg.Data["sshKeyAction"], c.recorded)
			}
			if !strings.Contains(out.String(), "found") {
				t.Errorf("no prompt shown: %q", out)
			}
		})
	}
	t.Run("preset is taken silently", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", pubLine)
		fx.Env.Cfg.Data["sshKeyAction"] = "generate"
		out := sk2Ask(fx, "use\n")
		fx.MustExec(steps.SSHKey())
		if out.Len() != 0 || sk2Keygens(fx) != 1 {
			t.Errorf("preset generate: out %q, keygens %d", out, sk2Keygens(fx))
		}
	})
	t.Run("nil Ask keeps the key", func(t *testing.T) {
		fx := newFixture(t)
		fx.PutKey("PRIV", pubLine)
		rep := fx.MustExec(steps.SSHKey())
		if sk2Keygens(fx) != 0 || len(rep.Applied) != 0 {
			t.Errorf("keygens %d applied %v", sk2Keygens(fx), rep.Applied)
		}
	})
}

func TestPromptSymlink(t *testing.T) {
	t.Run("yes links", func(t *testing.T) {
		fx := newFixture(t)
		out := sk2Ask(fx, "y\n")
		fx.MustExec(steps.Prepare())
		if got, _ := os.Readlink(chezmoiLink(fx)); got != fx.Repo {
			t.Errorf("link = %q, want %q", got, fx.Repo)
		}
		fx2 := newFixture(t) // no terminal: linking is the default
		fx2.MustExec(steps.Prepare())
		pr5SameRun(t, fx, fx2)
		if !strings.Contains(out.String(), "symlink it?") || !strings.Contains(out.String(), fx.Repo) {
			t.Errorf("prompt = %q", out)
		}
	})
	t.Run("no continues without the link", func(t *testing.T) {
		fx := newFixture(t)
		sk2Ask(fx, "n\n")
		fx.MustExec(steps.Prepare())
		if exists(chezmoiLink(fx)) {
			t.Errorf("n must not create the link")
		}
		if !strings.Contains(fx.Log.String(), "continuing with source="+fx.Repo) {
			t.Errorf("log: %s", fx.Log)
		}
	})
	t.Run("existing link does not ask", func(t *testing.T) {
		fx := newFixture(t)
		mustMkdir(t, chezmoiLink(fx)[:len(chezmoiLink(fx))-len("/chezmoi")])
		if err := os.Symlink(fx.Repo, chezmoiLink(fx)); err != nil {
			t.Fatal(err)
		}
		out := sk2Ask(fx, "n\n")
		fx.MustExec(steps.Prepare())
		if out.Len() != 0 {
			t.Errorf("asked about an existing link: %q", out)
		}
	})
}
